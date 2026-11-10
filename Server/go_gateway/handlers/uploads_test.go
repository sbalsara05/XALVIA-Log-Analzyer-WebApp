package handlers

// Tests that SignedUploadURL and CommitSession record the uploader's UID.
//
// SignedUploadURL signs through the IAM Credentials API; a fake IAM server
// captures the string-to-sign so the test can prove the uploader header is part
// of the signature. CommitSession writes _COMMIT.json through a fake GCS upload
// endpoint that captures the request body.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iamcredentials/v1"
	"google.golang.org/api/option"

	"ricovr/gateway/middleware"
)

const testUploaderUID = "uploader-uid-123"

// fakeIAM starts a fake IAM Credentials server and returns a service pointed at
// it plus a func that returns every payload it was asked to sign.
func fakeIAM(t *testing.T) (*iamcredentials.Service, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var payloads []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":signBlob") {
			http.Error(w, "fakeIAM: unhandled "+r.URL.Path, http.StatusInternalServerError)
			return
		}
		var req iamcredentials.SignBlobRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		raw, _ := base64.StdEncoding.DecodeString(req.Payload)
		mu.Lock()
		payloads = append(payloads, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"keyId":      "fake-key",
			"signedBlob": base64.StdEncoding.EncodeToString([]byte("fake-signature")),
		})
	}))
	t.Cleanup(srv.Close)

	svc, err := iamcredentials.NewService(context.Background(),
		option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("iamcredentials.NewService: %v", err)
	}
	return svc, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), payloads...)
	}
}

// fakeGCSUpload starts a fake GCS server that accepts object uploads and
// returns a client pointed at it plus a func that returns the captured bodies.
func fakeGCSUpload(t *testing.T) (*storage.Client, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/upload/storage/v1/b/") {
			http.Error(w, "fakeGCSUpload: unhandled "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind": "storage#object", "name": "uploaded", "bucket": testBucket,
		})
	}))

	t.Setenv("STORAGE_EMULATOR_HOST", strings.TrimPrefix(srv.URL, "http://"))
	client, err := storage.NewClient(context.Background(), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("storage.NewClient: %v", err)
	}
	t.Cleanup(func() {
		client.Close()
		srv.Close()
		os.Unsetenv("STORAGE_EMULATOR_HOST")
	})
	return client, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

// runAsUploader runs handler with the uploader UID in context, as
// VerifyFirebaseJWT would set it.
func runAsUploader(handler gin.HandlerFunc, method, target string, body []byte) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.ContextKeyUID, testUploaderUID)
	handler(c)
	return w
}

func TestSignedUploadURL_UploaderHeaderRequiredAndSigned(t *testing.T) {
	iamSvc, signedPayloads := fakeIAM(t)
	gcsClient := fakeGCS(t, testBucket, nil)
	env := &Env{
		GCSClient:           gcsClient,
		GCSBucket:           testBucket,
		ServiceAccountEmail: "gateway@example.iam.gserviceaccount.com",
		IAMService:          iamSvc,
	}

	body, _ := json.Marshal(map[string]any{"mac_id": testMac, "files": []string{"a.hex", "b.hex"}})
	w := runAsUploader(SignedUploadURL(env), http.MethodPost, "/api/signed-url/upload", body)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		URLs            map[string]string `json:"urls"`
		RequiredHeaders map[string]string `json:"required_headers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := resp.RequiredHeaders["X-Goog-Meta-Uploaded-By"]; got != testUploaderUID {
		t.Errorf("required_headers uploader: got %q, want %q; all=%v", got, testUploaderUID, resp.RequiredHeaders)
	}
	if got := resp.RequiredHeaders["X-Goog-Content-Length-Range"]; got != "0,52428800" {
		t.Errorf("size-limit header must be kept, got %q", got)
	}
	if len(resp.URLs) != 2 {
		t.Errorf("want 2 signed URLs, got %v", resp.URLs)
	}

	payloads := signedPayloads()
	if len(payloads) != 2 {
		t.Fatalf("want one signature per file (2), got %d", len(payloads))
	}
	for _, p := range payloads {
		if !strings.Contains(strings.ToLower(p), "x-goog-meta-uploaded-by:"+testUploaderUID) {
			t.Errorf("uploader header missing from signed payload:\n%s", p)
		}
	}
}

func TestCommitSession_RecordsUploader(t *testing.T) {
	gcsClient, uploads := fakeGCSUpload(t)
	session := "20261020_090000_123456"
	body, _ := json.Marshal([]string{"a.hex"})

	w := runAsUploader(CommitSession(gcsClient, testBucket), http.MethodPost,
		"/api/commit?mac_id="+testMac+"&session_id="+session, body)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	bodies := uploads()
	if len(bodies) != 1 {
		t.Fatalf("want 1 GCS write, got %d", len(bodies))
	}
	for _, want := range []string{
		`"uploaded_by":"` + testUploaderUID + `"`,
		`"session_id":"` + session + `"`,
		`"files":["a.hex"]`,
	} {
		if !strings.Contains(bodies[0], want) {
			t.Errorf("_COMMIT.json missing %s; body:\n%s", want, bodies[0])
		}
	}
}
