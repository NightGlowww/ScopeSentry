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

func postJSON(t *testing.T, router http.Handler, token, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	payload, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	return w
}

func decodeData(t *testing.T, w *httptest.ResponseRecorder, out interface{}) {
	t.Helper()
	var resp struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode data: %v (%s)", err, w.Body.String())
	}
	if out == nil {
		return
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		t.Fatalf("decode data body: %v (%s)", err, string(resp.Data))
	}
}

// 端口字典的 delete 以前对任意 id 都回成功。端口字典删不掉的表现是
// 扫描端一直按旧端口表跑，界面上看不出任何异常。
func TestPortDictionaryDeleteRoundTrip(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	name := fmt.Sprintf("it-port-%d", time.Now().UnixNano())
	w := postJSON(t, router, token, "/api/dictionary/port/add", map[string]interface{}{
		"name": name, "value": "65000",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("port add = %d: %s", w.Code, w.Body.String())
	}

	findID := func() string {
		w := postJSON(t, router, token, "/api/dictionary/port/data", map[string]interface{}{
			"pageIndex": 1, "pageSize": 50, "search": name,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("port data = %d: %s", w.Code, w.Body.String())
		}
		var data struct {
			List []map[string]interface{} `json:"list"`
		}
		decodeData(t, w, &data)
		for _, it := range data.List {
			if it["name"] == name {
				id, _ := it["id"].(string)
				return id
			}
		}
		return ""
	}
	id := findID()
	if id == "" {
		t.Fatalf("新增的端口字典在列表里查不到")
	}
	t.Cleanup(func() {
		postJSON(t, router, token, "/api/dictionary/port/delete", map[string]interface{}{"ids": []string{id}})
	})

	w = postJSON(t, router, token, "/api/dictionary/port/delete", map[string]interface{}{"ids": []string{id}})
	if w.Code != http.StatusOK {
		t.Fatalf("port delete = %d: %s", w.Code, w.Body.String())
	}
	var del struct {
		Deleted int `json:"deleted"`
	}
	decodeData(t, w, &del)
	if del.Deleted != 1 {
		t.Fatalf("port delete 报告删除 %d 条，期望 1: %s", del.Deleted, w.Body.String())
	}
	if findID() != "" {
		t.Fatalf("删除后端口字典仍在列表里")
	}

	w = postJSON(t, router, token, "/api/dictionary/port/delete", map[string]interface{}{
		"ids": []string{"0123456789abcdef01234567"},
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("删不存在的端口字典 = %d，期望 404: %s", w.Code, w.Body.String())
	}
}

// MCP Key 的启用/禁用是治理面的核心动作：禁用后这个 key 必须真的调不动工具。
// 以前这里没有用例点名，status 写不进去时界面上的开关会反弹，调用面却照旧放行。
func TestMCPKeyStatusRoundTrip(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	name := fmt.Sprintf("it-mcp-%d", time.Now().UnixNano())
	payload, _ := json.Marshal(map[string]interface{}{
		"name": name, "scope": []string{"asset:read"},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/keys", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("mcp key create = %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	id := created.Data.ID
	if id == "" {
		t.Fatalf("create 没返回 id: %s", w.Body.String())
	}
	t.Cleanup(func() {
		req := httptest.NewRequest(http.MethodDelete, "/api/mcp/keys/"+id, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(httptest.NewRecorder(), req)
	})

	readEnabled := func() bool {
		req := httptest.NewRequest(http.MethodGet, "/api/mcp/keys?pageIndex=1&pageSize=50&keyword="+name, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("mcp key list = %d: %s", rec.Code, rec.Body.String())
		}
		var data struct {
			List []struct {
				ID      string `json:"id"`
				Enabled bool   `json:"enabled"`
			} `json:"list"`
		}
		decodeData(t, rec, &data)
		for _, k := range data.List {
			if k.ID == id {
				return k.Enabled
			}
		}
		t.Fatalf("列表里找不到刚建的 key %s", id)
		return false
	}
	if !readEnabled() {
		t.Fatalf("新建的 key 默认应为启用")
	}

	w = postJSON(t, router, token, "/api/mcp/keys/"+id+"/status", map[string]interface{}{"enabled": false})
	if w.Code != http.StatusOK {
		t.Fatalf("mcp key disable = %d: %s", w.Code, w.Body.String())
	}
	if readEnabled() {
		t.Fatalf("禁用后 key 仍是启用状态")
	}

	w = postJSON(t, router, token, "/api/mcp/keys/"+id+"/status", map[string]interface{}{"enabled": true})
	if w.Code != http.StatusOK {
		t.Fatalf("mcp key enable = %d: %s", w.Code, w.Body.String())
	}
	if !readEnabled() {
		t.Fatalf("重新启用后 key 仍是禁用状态")
	}

	// 缺 enabled 字段要 400，不能默默当成 false 把 key 停掉
	w = postJSON(t, router, token, "/api/mcp/keys/"+id+"/status", map[string]interface{}{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status 缺 enabled = %d，期望 400: %s", w.Code, w.Body.String())
	}
}
