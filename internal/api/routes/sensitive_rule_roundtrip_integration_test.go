//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 敏感信息规则的 add/update 此前没有用例点名。规则存不进去不会报错，
// 只会表现为「扫出来的敏感信息少了」——而这类缺口只有对比历史才看得出来。
func TestSensitiveRuleAddUpdateRoundTrip(t *testing.T) {
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
	find := func(keyword string) map[string]interface{} {
		w := do("/api/sensitive/data", map[string]interface{}{
			"search": keyword, "pageIndex": 1, "pageSize": 20,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("sensitive list = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				List []map[string]interface{} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode sensitive list: %v", err)
		}
		for _, it := range resp.Data.List {
			if it["name"] == keyword {
				return it
			}
		}
		return nil
	}

	name := "it-sensitive-" + t.Name()
	w := do("/api/sensitive/add", map[string]interface{}{
		"name": name, "regular": "it-probe-[0-9]{4}", "color": "red", "state": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("sensitive add = %d: %s", w.Code, w.Body.String())
	}
	created := find(name)
	if created == nil {
		t.Fatalf("新增的敏感规则搜不到（保存没落库）")
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("规则缺少 id: %v", created)
	}
	t.Cleanup(func() { do("/api/sensitive/delete", map[string]interface{}{"ids": []string{id}}) })

	renamed := name + "-v2"
	w = do("/api/sensitive/update", map[string]interface{}{
		"id": id, "name": renamed, "regular": "it-probe-[0-9]{5}", "color": "blue", "state": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("sensitive update = %d: %s", w.Code, w.Body.String())
	}
	if find(name) != nil {
		t.Fatalf("改名后旧名字仍能搜到，update 没落到这条记录")
	}
	updated := find(renamed)
	if updated == nil {
		t.Fatalf("改名后新名字搜不到，update 没生效")
	}
	if got, _ := updated["state"].(bool); got {
		t.Fatalf("state 没更新成 false: %v", updated["state"])
	}
	if got, _ := updated["color"].(string); got != "blue" {
		t.Fatalf("color 没更新: %v", updated["color"])
	}

	w = do("/api/sensitive/delete", map[string]interface{}{"ids": []string{id}})
	if w.Code != http.StatusOK {
		t.Fatalf("sensitive delete = %d: %s", w.Code, w.Body.String())
	}
	if find(renamed) != nil {
		t.Fatalf("删除后规则仍能搜到")
	}
}
