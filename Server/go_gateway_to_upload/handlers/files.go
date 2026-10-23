package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iamcredentials/v1"
	"google.golang.org/api/iterator"

	"ricovr/gateway/middleware"
	"ricovr/gateway/models"
)

// macNormalRe matches any non-empty alphanumeric device ID (upper-case after normalisation).
var macNormalRe = regexp.MustCompile(`^[0-9A-Z]+$`)

// sessionIDRe matches the gateway's own session_id format:
// YYYYMMDD_HHMMSS_microseconds  (e.g. "20240101_120000_000001").
var sessionIDRe = regexp.MustCompile(`^\d{8}_\d{6}_\d{6}$`)

// normaliseMACForPath uppercases and validates an alphanumeric device ID for use
// in GCS paths. Returns an error if the input contains non-alphanumeric characters.
func normaliseMACForPath(raw string) (string, error) {
	clean := strings.ToUpper(strings.TrimSpace(raw))
	if !macNormalRe.MatchString(clean) {
		return "", fmt.Errorf("not a valid device ID: %q", raw)
	}
	return clean, nil
}

// signBytesFunc returns a SignBytes-compatible function backed by the IAM Credentials API.
func signBytesFunc(iamSvc *iamcredentials.Service, saEmail string) func([]byte) ([]byte, error) {
	return func(payload []byte) ([]byte, error) {
		resource := fmt.Sprintf("projects/-/serviceAccounts/%s", saEmail)
		req := &iamcredentials.SignBlobRequest{
			Payload: base64.StdEncoding.EncodeToString(payload),
		}
		resp, err := iamSvc.Projects.ServiceAccounts.SignBlob(resource, req).Do()
		if err != nil {
			return nil, err
		}
		return base64.StdEncoding.DecodeString(resp.SignedBlob)
	}
}

// checkOrgAccess verifies the caller may access the given mac_id.
// Writes 403/404 and returns false on failure; returns the DeviceEntry on success.
// Root org users bypass the org check and can access all devices.
//
// ACL PAUSED: org check is skipped; unknown devices get a synthetic entry so all
// authenticated callers can reach any mac_id. Re-enable enforcement later.
func checkOrgAccess(c *gin.Context, store *models.ACLStore, macID string) (models.DeviceEntry, bool) {
	_ = c.MustGet(middleware.ContextKeyUser).(models.User) // keep type assertion intact

	device, ok := store.GetDevice(macID)
	if !ok {
		// ACL PAUSED: return synthetic entry instead of 404
		device = models.DeviceEntry{MacID: macID, OrganizationID: ""}
	}

	// ACL PAUSED: if user.OrganizationID != "root" && user.OrganizationID != device.OrganizationID { 403 }

	return device, true
}

// ListFiles handles GET /api/files?mac_id=X
// Returns deduplicated base filenames for the device, considering only:
//   - legacy files:  {mac_id}/{file}
//   - files in sessions that have a _COMMIT.json marker: {mac_id}/{session_id}/{file}
//
// When a base filename appears in multiple committed sessions, the latest session_id
// (lexicographic max, safe because format is 20060102_150405_microseconds) wins.
// The session_id is an internal storage detail and is never exposed to the client.
func ListFiles(store *models.ACLStore, gcsClient *storage.Client, bucket string) gin.HandlerFunc {
	return func(c *gin.Context) {
		macID, err := normaliseMACForPath(c.Query("mac_id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mac_id is missing or not a valid MAC address"})
			return
		}

		if _, ok := checkOrgAccess(c, store, macID); !ok {
			return
		}

		prefix := macID + "/"
		it := gcsClient.Bucket(bucket).Objects(context.Background(), &storage.Query{Prefix: prefix})

		committedSessions := map[string]bool{}
		fileLatestSession := map[string]string{} // base_filename → latest session_id seen
		var legacyFiles []string

		for {
			attrs, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Printf("ERROR ListFiles GCS bucket=%s mac_id=%s: %v", bucket, macID, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "storage error"})
				return
			}
			rel := strings.TrimPrefix(attrs.Name, prefix)
			parts := strings.SplitN(rel, "/", 2)

			if len(parts) == 1 {
				// legacy flat file: {mac_id}/{file}
				if parts[0] != "_COMMIT.json" {
					legacyFiles = append(legacyFiles, parts[0])
				}
			} else {
				// session file: {mac_id}/{session_id}/{file}
				sessionID, base := parts[0], parts[1]
				if base == "_COMMIT.json" {
					committedSessions[sessionID] = true
				} else if existing, ok := fileLatestSession[base]; !ok || sessionID > existing {
					fileLatestSession[base] = sessionID
				}
			}
		}

		// Collect unique base names: legacy files first, then files from their latest
		// committed session. Legacy files take precedence if a name collides.
		seen := map[string]bool{}
		var files []string
		for _, f := range legacyFiles {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
		for base, latestSID := range fileLatestSession {
			if committedSessions[latestSID] && !seen[base] {
				seen[base] = true
				files = append(files, base)
			}
		}

		if files == nil {
			files = []string{}
		}
		c.JSON(http.StatusOK, files)
	}
}

// resolveObjectName returns the full GCS object path for a bare filename under macID.
// It tries the legacy flat path {mac_id}/{filename} first, then searches committed
// sessions ({mac_id}/{session_id}/{filename}) and returns the one from the latest
// session_id (lexicographic max — safe because format is 20060102_150405_microseconds).
// The session_id is never exposed to the caller.
func resolveObjectName(ctx context.Context, gcsClient *storage.Client, bucket, macID, filename string) (string, error) {
	// 1. Try legacy path: {mac_id}/{filename}
	legacyName := macID + "/" + filename
	if _, err := gcsClient.Bucket(bucket).Object(legacyName).Attrs(ctx); err == nil {
		return legacyName, nil
	}

	// 2. Scan all blobs under {mac_id}/ to find committed sessions that contain filename.
	prefix := macID + "/"
	it := gcsClient.Bucket(bucket).Objects(ctx, &storage.Query{Prefix: prefix})

	committedSessions := map[string]bool{}
	candidates := map[string]string{} // session_id → full object name

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return "", fmt.Errorf("GCS list failed: %v", err)
		}
		rel := strings.TrimPrefix(attrs.Name, prefix)
		parts := strings.SplitN(rel, "/", 2)
		if len(parts) != 2 {
			continue
		}
		sessionID, base := parts[0], parts[1]
		if base == "_COMMIT.json" {
			committedSessions[sessionID] = true
		} else if base == filename {
			candidates[sessionID] = attrs.Name
		}
	}

	bestSession, bestObj := "", ""
	for sessionID, objName := range candidates {
		if committedSessions[sessionID] && sessionID > bestSession {
			bestSession = sessionID
			bestObj = objName
		}
	}
	if bestSession != "" {
		return bestObj, nil
	}

	return "", fmt.Errorf("file %q not found in any committed session under %q", filename, macID)
}

// SignedReadURL handles GET /api/signed-url/read?mac_id=X&filename=Y
// filename is a bare base name (e.g. "logfile.hex") — the session_id is resolved
// server-side. Validates auth/ACL then returns a short-lived GCS signed GET URL.
// The caller fetches the file directly from GCS without routing bytes through the gateway.
func SignedReadURL(env *Env) gin.HandlerFunc {
	return func(c *gin.Context) {
		macID, err := normaliseMACForPath(c.Query("mac_id"))
		filename := c.Query("filename")
		if err != nil || filename == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mac_id and filename query params required"})
			return
		}

		if _, ok := checkOrgAccess(c, env.ACLStore, macID); !ok {
			return
		}

		objectName, err := resolveObjectName(c.Request.Context(), env.GCSClient, env.GCSBucket, macID, filename)
		if err != nil {
			log.Printf("ERROR SignedReadURL resolve mac_id=%s filename=%s: %v", macID, filename, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}

		url, err := env.GCSClient.Bucket(env.GCSBucket).SignedURL(objectName, &storage.SignedURLOptions{
			GoogleAccessID: env.ServiceAccountEmail,
			SignBytes:      signBytesFunc(env.IAMService, env.ServiceAccountEmail),
			Method:         "GET",
			Expires:        time.Now().Add(15 * time.Minute),
		})
		if err != nil {
			log.Printf("ERROR SignedReadURL sign mac_id=%s object=%s: %v", macID, objectName, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate download URL"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"url": url})
	}
}

// SignedUploadURL handles POST /api/signed-url/upload
// Body: {"mac_id": "X", "files": ["f1.hex", "f2.hex"]}
// Generates a new gateway-owned session_id, signs a PUT URL for each filename,
// and returns both so the client can upload directly to GCS then commit in one
// subsequent call — no separate session/start round-trip needed.
// Response: {"session_id": "S", "urls": {"f1.hex": "...", "f2.hex": "..."}}
func SignedUploadURL(env *Env) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			MacID string   `json:"mac_id" binding:"required"`
			Files []string `json:"files"  binding:"required"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "body must be {mac_id, files[]}"})
			return
		}
		if len(body.Files) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "files array must not be empty"})
			return
		}

		macID, err := normaliseMACForPath(body.MacID)
		if err != nil {
			log.Printf("[DEBUG] SignedUploadURL: invalid mac_id=%q err=%v", body.MacID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "mac_id must be a non-empty alphanumeric string"})
			return
		}
		log.Printf("[DEBUG] SignedUploadURL: mac_id=%q files=%v", macID, body.Files)

		if _, ok := checkOrgAccess(c, env.ACLStore, macID); !ok {
			return
		}

		now := time.Now().UTC()
		sessionID := fmt.Sprintf("%s_%06d", now.Format("20060102_150405"), now.Nanosecond()/1000)

		signBytes := signBytesFunc(env.IAMService, env.ServiceAccountEmail)

		// Headers the client must include verbatim in each PUT request.
		signedHeaders := []string{"X-Goog-Content-Length-Range: 0,52428800"}
		requiredHeaders := make(map[string]string, len(signedHeaders))
		for _, h := range signedHeaders {
			if parts := strings.SplitN(h, ": ", 2); len(parts) == 2 {
				requiredHeaders[parts[0]] = parts[1]
			}
		}

		urls := make(map[string]string, len(body.Files))
		for _, filename := range body.Files {
			objectName := macID + "/" + sessionID + "/" + filename
			u, err := env.GCSClient.Bucket(env.GCSBucket).SignedURL(objectName, &storage.SignedURLOptions{
				GoogleAccessID: env.ServiceAccountEmail,
				SignBytes:      signBytes,
				Method:         "PUT",
				ContentType:    "application/octet-stream",
				Expires:        time.Now().Add(15 * time.Minute),
				Headers:        signedHeaders,
			})
			if err != nil {
				log.Printf("ERROR SignedUploadURL sign mac_id=%s filename=%s: %v", macID, filename, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate upload URL"})
				return
			}
			urls[filename] = u
		}

		c.JSON(http.StatusOK, gin.H{"session_id": sessionID, "urls": urls, "required_headers": requiredHeaders})
	}
}

// CommitSession handles POST /api/commit?mac_id=X&session_id=Y
// Body: JSON array of uploaded filenames, e.g. ["logfile.hex", "sensor.hex"].
// Gateway writes {mac_id}/{session_id}/_COMMIT.json to GCS, taking ownership of
// the commit marker away from the client.
func CommitSession(store *models.ACLStore, gcsClient *storage.Client, bucket string) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawMacID := c.Query("mac_id")
		sessionID := c.Query("session_id")
		log.Printf("[DEBUG] CommitSession: raw_mac_id=%q session_id=%q", rawMacID, sessionID)

		macID, err := normaliseMACForPath(rawMacID)
		if err != nil || sessionID == "" {
			log.Printf("[DEBUG] CommitSession: bad params mac_id_err=%v session_id=%q", err, sessionID)
			c.JSON(http.StatusBadRequest, gin.H{"error": "mac_id and session_id required"})
			return
		}
		if !sessionIDRe.MatchString(sessionID) {
			log.Printf("[DEBUG] CommitSession: invalid session_id format %q", sessionID)
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id format invalid"})
			return
		}

		if _, ok := checkOrgAccess(c, store, macID); !ok {
			return
		}

		var files []string
		if err := c.ShouldBindJSON(&files); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "body must be a JSON array of filenames"})
			return
		}

		commit := map[string]any{
			"session_id": sessionID,
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
			"files":      files,
		}
		data, _ := json.Marshal(commit)

		objectName := macID + "/" + sessionID + "/_COMMIT.json"
		wc := gcsClient.Bucket(bucket).Object(objectName).NewWriter(context.Background())
		wc.ContentType = "application/json"
		if _, err := wc.Write(data); err != nil {
			_ = wc.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "GCS write failed"})
			return
		}
		if err := wc.Close(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "GCS finalize failed"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"committed": objectName})
	}
}
