//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 页面监控（page monitoring）的 add/delete 此前没有用例点名。
// 失败形态很安静：监控项没建上 → 页面变化永远不会被发现，界面上只是「没有监控项」。
func TestPageMonitAddAndDeleteRoundTrip(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	do := func(path string, body interface{}) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}
	url := fmt.Sprintf("https://it-monit-%d.example.com/", time.Now().UnixNano())
	find := func() map[string]interface{} {
		w := do("/api/task/scheduled/pagemonit/data", map[string]interface{}{
			"pageIndex": 1, "pageSize": 50,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("pagemonit data = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				List []map[string]interface{} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode pagemonit: %v", err)
		}
		for _, it := range resp.Data.List {
			if it["url"] == url {
				return it
			}
		}
		return nil
	}

	w := do("/api/task/scheduled/pagemonit/add", map[string]interface{}{"url": url})
	if w.Code != http.StatusOK {
		t.Fatalf("pagemonit add = %d: %s", w.Code, w.Body.String())
	}
	created := find()
	if created == nil {
		t.Fatalf("新增的监控项在列表里查不到（页面变化永远不会被发现）")
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("监控项缺少 id: %v", created)
	}
	t.Cleanup(func() { do("/api/task/scheduled/pagemonit/delete", map[string]interface{}{"ids": []string{id}}) })

	w = do("/api/task/scheduled/pagemonit/delete", map[string]interface{}{"ids": []string{id}})
	if w.Code != http.StatusOK {
		t.Fatalf("pagemonit delete = %d: %s", w.Code, w.Body.String())
	}
	if find() != nil {
		t.Fatalf("删除后监控项仍在列表里")
	}
}
