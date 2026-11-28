//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 两条「保存类」写接口此前没有任何测试/契约脚本点名（REST_CLOSURE 的未覆盖写操作）：
// 它们最典型的失败模式就是假成功——返回 200，但下一轮读回来还是旧值。
// 这里做完整往返：读原值 → 写入新值 → 读回断言 → 恢复原值。
func TestDeduplicationConfigPersists(t *testing.T) {
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

	readConfig := func() map[string]interface{} {
		w := do(http.MethodGet, "/api/configuration/deduplication/config", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("get config = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		// 不同版本的响应可能是 {data:{...}} 或直接 {data:{config:{...}}}
		if inner, ok := resp.Data["config"].(map[string]interface{}); ok {
			return inner
		}
		return resp.Data
	}

	original := readConfig()
	t.Cleanup(func() {
		if original != nil {
			_, _ = json.Marshal(original)
			do(http.MethodPost, "/api/configuration/deduplication/save", original)
		}
	})

	updated := map[string]interface{}{
		"asset": true, "subdomain": true, "UrlScan": false,
		"crawler": false, "SensitiveResult": false, "DirScanResult": false,
		"vulnerability": false, "PageMonitoring": false,
		"hour": 7, "flag": false, "runNow": false,
	}
	w := do(http.MethodPost, "/api/configuration/deduplication/save", updated)
	if w.Code != http.StatusOK {
		t.Fatalf("save config = %d: %s", w.Code, w.Body.String())
	}

	got := readConfig()
	if v, ok := got["hour"].(float64); !ok || int(v) != 7 {
		t.Fatalf("hour 没写进去: %v", got["hour"])
	}
	if v, ok := got["UrlScan"].(bool); !ok || v {
		t.Fatalf("UrlScan 应该被保存成 false: %v", got["UrlScan"])
	}
	if v, ok := got["asset"].(bool); !ok || !v {
		t.Fatalf("asset 应该被保存成 true: %v", got["asset"])
	}
}

// 通知配置：新增 → 列表可见 → 删除 → 列表消失。
// 删除走的是批量接口，单条也必须真的删掉（假成功会让「关掉某条通知」变成无声失效）。
func TestNotificationAddAndDeleteRoundTrip(t *testing.T) {
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

	list := func() []map[string]interface{} {
		w := do(http.MethodGet, "/api/configuration/notification/data", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("notification list = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				List []map[string]interface{} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return resp.Data.List
	}

	name := "it-notify-" + t.Name()
	w := do(http.MethodPost, "/api/configuration/notification/add", map[string]interface{}{
		"name": name, "method": "POST", "url": "http://127.0.0.1:9/hook",
		"contentType": "application/json", "data": "{}", "state": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("add notification = %d: %s", w.Code, w.Body.String())
	}

	var id string
	found := false
	for _, it := range list() {
		if it["name"] == name {
			found = true
			id, _ = it["id"].(string)
		}
	}
	if !found {
		t.Fatalf("新增的通知没出现在列表里")
	}
	if id == "" {
		t.Fatalf("通知缺少 id，无法删除")
	}
	t.Cleanup(func() { do(http.MethodPost, "/api/configuration/notification/delete", map[string]interface{}{"ids": []string{id}}) })

	w = do(http.MethodPost, "/api/configuration/notification/delete", map[string]interface{}{"ids": []string{id}})
	if w.Code != http.StatusOK {
		t.Fatalf("delete notification = %d: %s", w.Code, w.Body.String())
	}
	for _, it := range list() {
		if it["name"] == name {
			t.Fatalf("删除后通知仍在列表里: %v", it)
		}
	}
}
