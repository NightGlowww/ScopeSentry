//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 资产组地契（asset_scope）的 update / delete / bind_enterprise / clear_update_error
// 此前都没有测试或契约脚本点名——而这四个动作一旦假成功，用户会以为归属改好了、
// 错误清掉了，实际库里没变，后面所有按归属聚合的视图全是错的。
// 这里走完整生命周期：add → update → 读回 → bind_enterprise → clear_update_error → delete → 确认消失。
func TestAssetScopeLifecyclePersists(t *testing.T) {
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
	find := func(id string) map[string]interface{} {
		w := do("/api/assets/asset_scope/list", map[string]interface{}{
			"pageIndex": 1, "pageSize": 200,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("scope list = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				List []map[string]interface{} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode scope list: %v", err)
		}
		for _, it := range resp.Data.List {
			if it["id"] == id {
				return it
			}
		}
		return nil
	}

	name := "it-scope-" + t.Name()
	w := do("/api/assets/asset_scope/add", map[string]interface{}{
		"name": name,
		"scope": "example.org\n203.0.113.7",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("add scope = %d: %s", w.Code, w.Body.String())
	}
	var addResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &addResp); err != nil || addResp.Data.ID == "" {
		t.Fatalf("add scope 没返回 id: %s", w.Body.String())
	}
	id := addResp.Data.ID
	t.Cleanup(func() { do("/api/assets/asset_scope/delete", map[string]interface{}{"ids": []string{id}}) })

	// 域名/IP 混填必须被解析成 mixed（解析失败会让「按范围」聚合永远为空）
	got := find(id)
	if got == nil {
		t.Fatalf("新增的地契在列表里查不到")
	}
	if got["scopeType"] != "mixed" {
		t.Fatalf("scopeType = %v, want mixed", got["scopeType"])
	}

	renamed := name + "-v2"
	w = do("/api/assets/asset_scope/update", map[string]interface{}{
		"id": id, "name": renamed, "scope": "example.net",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update scope = %d: %s", w.Code, w.Body.String())
	}
	got = find(id)
	if got == nil || got["name"] != renamed {
		t.Fatalf("update 没落库: %v", got)
	}
	if got["scopeType"] != "domain" {
		t.Fatalf("改成纯域名后 scopeType 应为 domain，实际 %v", got["scopeType"])
	}

	w = do("/api/assets/asset_scope/bind_enterprise", map[string]interface{}{
		"id": id, "enterprise": "IT 企业",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("bind enterprise = %d: %s", w.Code, w.Body.String())
	}
	if got = find(id); got == nil || got["enterprise"] != "IT 企业" {
		t.Fatalf("绑定企业没落库: %v", got)
	}

	w = do("/api/assets/asset_scope/clear_update_error", map[string]interface{}{"ids": []string{id}})
	if w.Code != http.StatusOK {
		t.Fatalf("clear update error = %d: %s", w.Code, w.Body.String())
	}

	w = do("/api/assets/asset_scope/delete", map[string]interface{}{"ids": []string{id}})
	if w.Code != http.StatusOK {
		t.Fatalf("delete scope = %d: %s", w.Code, w.Body.String())
	}
	if find(id) != nil {
		t.Fatalf("删除后地契仍在列表里")
	}

	// 再删一次必须报错，不能回 200（历史 bug：不存在的 id 也报成功）
	w = do("/api/assets/asset_scope/delete", map[string]interface{}{"ids": []string{id}})
	if w.Code == http.StatusOK {
		t.Fatalf("删除不存在的地契应当报错，实际 200: %s", w.Body.String())
	}
}
