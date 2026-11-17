package backup

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const missingArchive = "backup-19700101-000000.tar.gz"

func init() {
	gin.SetMode(gin.TestMode)
}

func runHandler(handler gin.HandlerFunc, method, target, body string, params gin.Params) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	request := httptest.NewRequest(method, target, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = request
	c.Params = params
	handler(c)
	return recorder
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return payload
}

func restoreBody(t *testing.T, name string, confirm *string) string {
	t.Helper()
	payload := map[string]string{"name": name}
	if confirm != nil {
		payload["confirm"] = *confirm
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal restore body: %v", err)
	}
	return string(data)
}

func stringPtr(v string) *string { return &v }

func TestRestoreRequiresExplicitConfirm(t *testing.T) {
	lower := "restore"
	recorder := runHandler(Restore, http.MethodPost, "/api/backup/restore",
		restoreBody(t, missingArchive, &lower), nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for confirm=restore, got %d: %s", recorder.Code, recorder.Body.String())
	}
	payload := decodeResponse(t, recorder)
	if data, _ := payload["data"].(string); !strings.Contains(data, "RESTORE") {
		t.Fatalf("expected confirm error detail, got %v", payload["data"])
	}

	empty := ""
	for _, body := range []string{
		restoreBody(t, missingArchive, nil),
		restoreBody(t, missingArchive, &empty),
		"{",
	} {
		recorder := runHandler(Restore, http.MethodPost, "/api/backup/restore", body, nil)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for body %q, got %d", body, recorder.Code)
		}
	}
}

func TestRestoreMissingArchiveReturnsNotFound(t *testing.T) {
	confirm := "RESTORE"
	recorder := runHandler(Restore, http.MethodPost, "/api/backup/restore",
		restoreBody(t, missingArchive, &confirm), nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing archive, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestRestoreRejectsTraversalName(t *testing.T) {
	confirm := "RESTORE"
	recorder := runHandler(Restore, http.MethodPost, "/api/backup/restore",
		restoreBody(t, "../../etc/passwd.tar.gz", &confirm), nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal restore name, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDownloadRejectsPathTraversal(t *testing.T) {
	names := []string{
		"../../etc/passwd",
		"../" + missingArchive,
		"/etc/passwd",
		"..\\..\\windows\\system32\\config",
		"nested/" + missingArchive,
	}
	for _, name := range names {
		recorder := runHandler(Download, http.MethodGet, "/api/backup/download/"+name, "", gin.Params{
			{Key: "name", Value: name},
		})
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for download name %q, got %d: %s", name, recorder.Code, recorder.Body.String())
		}
		payload := decodeResponse(t, recorder)
		if data, _ := payload["data"].(string); !strings.Contains(data, "invalid backup name") {
			t.Fatalf("expected invalid-name detail for %q, got %v", name, payload["data"])
		}
	}

	recorder := runHandler(Download, http.MethodGet, "/api/backup/download/"+missingArchive, "", gin.Params{
		{Key: "name", Value: missingArchive},
	})
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing archive, got %d", recorder.Code)
	}
}

func TestDeleteRejectsPathTraversal(t *testing.T) {
	recorder := runHandler(Delete, http.MethodDelete, "/api/backup/../x.tar.gz", "", gin.Params{
		{Key: "name", Value: "../../x.tar.gz"},
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal delete, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestListAndStatusRespond(t *testing.T) {
	statusRecorder := runHandler(Status, http.MethodGet, "/api/backup/status", "", nil)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status endpoint: %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	payload := decodeResponse(t, statusRecorder)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("status data must be an object: %v", payload["data"])
	}
	if running, _ := data["running"].(bool); running {
		t.Fatalf("no backup task should be running: %+v", data)
	}
	if _, ok := data["progress"]; !ok {
		t.Fatalf("status must expose progress: %+v", data)
	}

	listRecorder := runHandler(List, http.MethodGet, "/api/backup/list", "", nil)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list endpoint: %d %s", listRecorder.Code, listRecorder.Body.String())
	}
}
