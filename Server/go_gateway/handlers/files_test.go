package handlers

// Tests for resolveObjectName and ListFiles.
//
// The storage.Client talks to GCS over HTTP JSON API. We use STORAGE_EMULATOR_HOST
// + option.WithoutAuthentication() to point it at an in-process httptest.Server
// that implements only the two GCS endpoints these functions call:
//
//   GET /storage/v1/b/{bucket}/o?prefix=…   — object list
//   GET /storage/v1/b/{bucket}/o/{name}     — object attrs (legacy path probe)
//
// No real GCS credentials or network access required.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/option"

)

// ---------------------------------------------------------------------------
// Fake GCS HTTP server
// ---------------------------------------------------------------------------

type fakeObject struct {
	name        string // full GCS object name, e.g. "AABBCC/20260101_090000_000000/file.txt"
	size        int64
	updated     time.Time
	contentType string
	metadata    map[string]string
}

// fakeGCS starts a test HTTP server that answers GCS list and attrs requests.
// It sets STORAGE_EMULATOR_HOST so storage.NewClient routes to it automatically.
func fakeGCS(t *testing.T, bucket string, objects []fakeObject) *storage.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// List: GET /storage/v1/b/{bucket}/o
		if r.Method == http.MethodGet && strings.HasSuffix(path, "/o") {
			prefix := r.URL.Query().Get("prefix")
			type item struct {
				Kind        string            `json:"kind"`
				Name        string            `json:"name"`
				Size        string            `json:"size,omitempty"`
				Updated     string            `json:"updated,omitempty"`
				ContentType string            `json:"contentType,omitempty"`
				Metadata    map[string]string `json:"metadata,omitempty"`
			}
			var items []item
			for _, o := range objects {
				if strings.HasPrefix(o.name, prefix) {
					it := item{Kind: "storage#object", Name: o.name, ContentType: o.contentType, Metadata: o.metadata}
					if o.size != 0 {
						it.Size = fmt.Sprint(o.size)
					}
					if !o.updated.IsZero() {
						it.Updated = o.updated.Format(time.RFC3339Nano)
					}
					items = append(items, it)
				}
			}
			if items == nil {
				items = []item{}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"kind":  "storage#objects",
				"items": items,
			})
			return
		}

		// Attrs: GET /storage/v1/b/{bucket}/o/{encoded-name}
		if r.Method == http.MethodGet && strings.Contains(path, "/o/") {
			parts := strings.SplitN(path, "/o/", 2)
			if len(parts) == 2 {
				objName, _ := url.PathUnescape(parts[1])
				for _, o := range objects {
					if o.name == objName {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]any{
							"kind":    "storage#object",
							"name":    o.name,
							"bucket":  bucket,
							"updated": time.Now().Format(time.RFC3339),
						})
						return
					}
				}
			}
			http.NotFound(w, r)
			return
		}

		http.Error(w, fmt.Sprintf("fakeGCS: unhandled %s %s", r.Method, path), 500)
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
	return client
}

// commit builds a _COMMIT.json fakeObject so a session counts as committed.
func commit(mac, sessionID string) fakeObject {
	return fakeObject{name: mac + "/" + sessionID + "/_COMMIT.json"}
}

// logObj builds a session-scoped log fakeObject.
func logObj(mac, sessionID, filename string) fakeObject {
	return fakeObject{name: mac + "/" + sessionID + "/" + filename}
}

// listFiles calls the ListFiles handler via a fake gin.Context and decodes
// the JSON response into a []string.
func listFiles(t *testing.T, client *storage.Client, mac string) ([]string, int) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	handler := ListFiles(client, testBucket)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/files?mac_id="+mac, nil)

	c, _ := gin.CreateTestContext(w)
	c.Request = r

	handler(c)

	var files []string
	_ = json.NewDecoder(w.Body).Decode(&files)
	return files, w.Code
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	testBucket    = "xaliva_logs"
	testMac       = "AABBCCDDEEFF"
	deviceLogName = "AABBCCDDEEFF_devicelog.txt"
	testLogName   = "AABBCCDDEEFF_001.txt"
	sessOld       = "20260101_090000_000000"
	sessNew       = "20260424_183012_042731"
)

// ---------------------------------------------------------------------------
// resolveObjectName
// ---------------------------------------------------------------------------

func TestResolveObjectName_LatestSessionWins(t *testing.T) {
	// Two committed sessions both containing the device log.
	// resolveObjectName must return the object path from the newer session.
	client := fakeGCS(t, testBucket, []fakeObject{
		logObj(testMac, sessOld, deviceLogName), commit(testMac, sessOld),
		logObj(testMac, sessNew, deviceLogName), commit(testMac, sessNew),
	})

	got, err := resolveObjectName(context.Background(), client, testBucket, testMac, deviceLogName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := testMac + "/" + sessNew + "/" + deviceLogName
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveObjectName_UncommittedNewestSessionIgnored(t *testing.T) {
	// old and mid are committed; newest has no _COMMIT.json.
	// Must resolve to mid, not the uncommitted newest.
	sessMid := "20260301_120000_000000"
	sessUncommitted := "20260501_000000_000000"
	client := fakeGCS(t, testBucket, []fakeObject{
		logObj(testMac, sessOld, deviceLogName), commit(testMac, sessOld),
		logObj(testMac, sessMid, deviceLogName), commit(testMac, sessMid),
		logObj(testMac, sessUncommitted, deviceLogName), // no commit marker
	})

	got, err := resolveObjectName(context.Background(), client, testBucket, testMac, deviceLogName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := testMac + "/" + sessMid + "/" + deviceLogName
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveObjectName_NoCommitReturnsError(t *testing.T) {
	// Session exists but has no _COMMIT.json — must return an error.
	client := fakeGCS(t, testBucket, []fakeObject{
		logObj(testMac, sessNew, deviceLogName),
	})

	_, err := resolveObjectName(context.Background(), client, testBucket, testMac, deviceLogName)
	if err == nil {
		t.Error("expected error for uncommitted session, got nil")
	}
}

func TestResolveObjectName_FileAbsentInCommittedSession(t *testing.T) {
	// Committed session exists but does not contain the requested file.
	client := fakeGCS(t, testBucket, []fakeObject{
		logObj(testMac, sessNew, "other_file.txt"), commit(testMac, sessNew),
	})

	_, err := resolveObjectName(context.Background(), client, testBucket, testMac, deviceLogName)
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestResolveObjectName_LegacyFlatPathTakesPrecedence(t *testing.T) {
	// A legacy (pre-session) object exists alongside a newer session object.
	// resolveObjectName probes the legacy path first and must return it.
	client := fakeGCS(t, testBucket, []fakeObject{
		{name: testMac + "/" + deviceLogName}, // legacy flat path, no session prefix
		logObj(testMac, sessNew, deviceLogName), commit(testMac, sessNew),
	})

	got, err := resolveObjectName(context.Background(), client, testBucket, testMac, deviceLogName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := testMac + "/" + deviceLogName
	if got != want {
		t.Errorf("got %q, want %q — legacy flat path should take precedence", got, want)
	}
}

// ---------------------------------------------------------------------------
// ListFiles
// ---------------------------------------------------------------------------

func TestListFiles_DeviceLogDeduplicatedAcrossSessions(t *testing.T) {
	// Two committed sessions both containing the device log.
	// ListFiles must return exactly one entry for that filename.
	client := fakeGCS(t, testBucket, []fakeObject{
		logObj(testMac, sessOld, deviceLogName), commit(testMac, sessOld),
		logObj(testMac, sessNew, deviceLogName), commit(testMac, sessNew),
	})

	files, code := listFiles(t, client, testMac)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	count := 0
	for _, f := range files {
		if f == deviceLogName {
			count++
		}
	}
	if count != 1 {
		t.Errorf("device log appears %d times, want exactly 1; files=%v", count, files)
	}
}

func TestListFiles_DeviceLogPresentAfterMultipleUploads(t *testing.T) {
	// The device log must still be visible in the listing after being re-uploaded
	// into a second session.
	client := fakeGCS(t, testBucket, []fakeObject{
		logObj(testMac, sessOld, deviceLogName), commit(testMac, sessOld),
		logObj(testMac, sessNew, deviceLogName), commit(testMac, sessNew),
	})

	files, _ := listFiles(t, client, testMac)
	for _, f := range files {
		if f == deviceLogName {
			return
		}
	}
	t.Errorf("device log missing from listing; files=%v", files)
}

func TestListFiles_UncommittedFilesExcluded(t *testing.T) {
	// A session with no _COMMIT.json must not contribute any filenames.
	sessUncommitted := "20260501_000000_000000"
	client := fakeGCS(t, testBucket, []fakeObject{
		logObj(testMac, sessNew, deviceLogName), commit(testMac, sessNew),
		logObj(testMac, sessUncommitted, "sneaky.txt"), // no commit
	})

	files, _ := listFiles(t, client, testMac)
	for _, f := range files {
		if f == "sneaky.txt" {
			t.Errorf("uncommitted file in listing; files=%v", files)
		}
	}
}

func TestListFiles_EmptyBucketReturnsEmptySlice(t *testing.T) {
	client := fakeGCS(t, testBucket, nil)

	files, code := listFiles(t, client, testMac)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(files) != 0 {
		t.Errorf("expected [], got %v", files)
	}
}

func TestListFiles_MultipleFilesInSessionAllListed(t *testing.T) {
	// A session containing both a device log and a test log — both must appear.
	client := fakeGCS(t, testBucket, []fakeObject{
		logObj(testMac, sessNew, deviceLogName),
		logObj(testMac, sessNew, testLogName),
		commit(testMac, sessNew),
	})

	files, _ := listFiles(t, client, testMac)
	want := map[string]bool{deviceLogName: false, testLogName: false}
	for _, f := range files {
		want[f] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("%q missing from listing; files=%v", name, files)
		}
	}
}
