//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 备份是灾备路径：它的假成功最贵——今天回 200，真出事时才发现包是空的或根本没有。
// 这里跑完整往返：create → 包里 size>0 且 sha256 非空 → list 能查到 → delete → list 消失。
func TestBackupCreateListDeleteRoundTrip(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	do := func(method, path string, body interface{}) *httptest.ResponseRecorder {
		var payload []byte
		if body != nil {
			payload, _ = json.Marshal(body)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}

	note := "it-backup-" + t.Name()
	w := do(http.MethodPost, "/api/backup/create", map[string]interface{}{"note": note})
	if w.Code != http.StatusOK {
		t.Fatalf("backup create = %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Data.Name == "" {
		t.Fatalf("备份没有返回文件名: %s", w.Body.String())
	}
	if created.Data.Size <= 0 {
		t.Fatalf("备份包是空的（size=%d），灾备时等于没有", created.Data.Size)
	}
	if created.Data.SHA256 == "" {
		t.Fatalf("备份没有校验和，无法验证完整性")
	}
	t.Cleanup(func() { do(http.MethodDelete, "/api/backup/"+created.Data.Name, nil) })

	list := func() []map[string]interface{} {
		lw := do(http.MethodGet, "/api/backup/list", nil)
		if lw.Code != http.StatusOK {
			t.Fatalf("backup list = %d: %s", lw.Code, lw.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(lw.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return resp.Data
	}

	found := false
	for _, it := range list() {
		if it["name"] == created.Data.Name {
			found = true
		}
	}
	if !found {
		t.Fatalf("刚建的备份没出现在列表里: %s", created.Data.Name)
	}

	if dw := do(http.MethodDelete, "/api/backup/"+created.Data.Name, nil); dw.Code != http.StatusOK {
		t.Fatalf("backup delete = %d: %s", dw.Code, dw.Body.String())
	}
	for _, it := range list() {
		if it["name"] == created.Data.Name {
			t.Fatalf("删除后备份仍在列表里: %s", created.Data.Name)
		}
	}
}
