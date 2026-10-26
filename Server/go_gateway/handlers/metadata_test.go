package handlers

// Tests for ListFileMetadata. Uses the fake GCS server from files_test.go.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/gin-gonic/gin"
)

type metadataRow struct {
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	Updated     time.Time `json:"updated"`
	ContentType string    `json:"content_type"`
	SessionID   *string   `json:"session_id"`
	UploadedBy  *string   `json:"uploaded_by"`
}

// listFileMetadata calls ListFileMetadata and decodes the JSON response.
func listFileMetadata(t *testing.T, client *storage.Client, mac string) ([]metadataRow, int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/files/metadata?mac_id="+mac, nil)

	ListFileMetadata(client, testBucket)(c)

	raw := w.Body.String()
	var rows []metadataRow
	_ = json.Unmarshal(w.Body.Bytes(), &rows)
	return rows, w.Code, raw
}

func TestListFileMetadata_SessionFile_AllFields(t *testing.T) {
	updated := time.Date(2026, 10, 20, 9, 0, 1, 0, time.UTC)
	client := fakeGCS(t, testBucket, []fakeObject{
		{
			name: testMac + "/" + sessNew + "/" + deviceLogName, size: 48213, updated: updated,
			contentType: "application/octet-stream", metadata: map[string]string{"uploaded-by": "uid-abc"},
		},
		commit(testMac, sessNew),
	})

	rows, code, raw := listFileMetadata(t, client, testMac)
	if code != http.StatusOK || len(rows) != 1 {
		t.Fatalf("want 200 with 1 row, got %d: %s", code, raw)
	}
	r := rows[0]
	if r.Name != deviceLogName || r.Size != 48213 || !r.Updated.Equal(updated) || r.ContentType != "application/octet-stream" {
		t.Errorf("row: got %+v", r)
	}
	if r.SessionID == nil || *r.SessionID != sessNew {
		t.Errorf("session_id: got %v, want %q", r.SessionID, sessNew)
	}
	if r.UploadedBy == nil || *r.UploadedBy != "uid-abc" {
		t.Errorf("uploaded_by: got %v, want uid-abc", r.UploadedBy)
	}
}

func TestListFileMetadata_LegacyAndUnknownUploader_AreJSONNull(t *testing.T) {
	client := fakeGCS(t, testBucket, []fakeObject{
		{name: testMac + "/" + deviceLogName, size: 10},
	})

	rows, code, raw := listFileMetadata(t, client, testMac)
	if code != http.StatusOK || len(rows) != 1 {
		t.Fatalf("want 200 with 1 row, got %d: %s", code, raw)
	}
	var generic []map[string]any
	_ = json.Unmarshal([]byte(raw), &generic)
	for _, key := range []string{"session_id", "uploaded_by"} {
		v, present := generic[0][key]
		if !present || v != nil {
			t.Errorf("%s must be present and null, got present=%v value=%v", key, present, v)
		}
	}
}

func TestListFileMetadata_PicksLatestCommittedSession(t *testing.T) {
	client := fakeGCS(t, testBucket, []fakeObject{
		{name: testMac + "/" + sessOld + "/" + deviceLogName, size: 1, metadata: map[string]string{"uploaded-by": "old"}},
		commit(testMac, sessOld),
		{name: testMac + "/" + sessNew + "/" + deviceLogName, size: 2, metadata: map[string]string{"uploaded-by": "new"}},
		commit(testMac, sessNew),
	})

	rows, _, raw := listFileMetadata(t, client, testMac)
	if len(rows) != 1 {
		t.Fatalf("want 1 deduplicated row, got %s", raw)
	}
	r := rows[0]
	if r.Size != 2 || r.UploadedBy == nil || *r.UploadedBy != "new" || r.SessionID == nil || *r.SessionID != sessNew {
		t.Errorf("must describe the latest session's object, got %s", raw)
	}
}

func TestListFileMetadata_SameFilesAsListFiles_SortedByName(t *testing.T) {
	sessUncommitted := "20260501_000000_000000"
	client := fakeGCS(t, testBucket, []fakeObject{
		{name: testMac + "/legacy.txt"},
		logObj(testMac, sessOld, testLogName), commit(testMac, sessOld),
		logObj(testMac, sessNew, deviceLogName), logObj(testMac, sessNew, "b.txt"), commit(testMac, sessNew),
		logObj(testMac, sessUncommitted, "sneaky.txt"),
	})

	names, _ := listFiles(t, client, testMac)
	sort.Strings(names)

	rows, code, raw := listFileMetadata(t, client, testMac)
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, raw)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Name)
	}
	if !reflect.DeepEqual(got, names) {
		t.Errorf("metadata names %v must equal sorted ListFiles names %v", got, names)
	}
}

func TestListFileMetadata_EmptyBucket_ReturnsEmptyArray(t *testing.T) {
	client := fakeGCS(t, testBucket, nil)
	_, code, raw := listFileMetadata(t, client, testMac)
	if code != http.StatusOK || raw != "[]" {
		t.Errorf("want 200 [], got %d %s", code, raw)
	}
}

func TestListFileMetadata_InvalidMAC_Returns400(t *testing.T) {
	client := fakeGCS(t, testBucket, nil)
	_, code, _ := listFileMetadata(t, client, "AA:BB")
	if code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", code)
	}
}
