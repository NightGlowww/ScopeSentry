//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// POC 的 add / update 此前没有任何测试点名，而它是「扫描器实际会加载什么」的唯一入口：
// 保存失败不会报错给用户看，只会表现为「这条 POC 明明加了却扫不出来」。
// 这里走 add → 搜索命中 → update 改名 → 搜索反映 → delete → 消失。
func TestPocAddUpdateRoundTrip(t *testing.T) {
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
		w := do("/api/poc", map[string]interface{}{
			"search": keyword, "pageIndex": 1, "pageSize": 20,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("poc list = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				List []map[string]interface{} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode poc list: %v", err)
		}
		for _, it := range resp.Data.List {
			if it["name"] == keyword {
				return it
			}
		}
		return nil
	}

	findByKeyword := func(keyword string) map[string]interface{} {
		w := do("/api/poc", map[string]interface{}{
			"search": keyword, "pageIndex": 1, "pageSize": 20,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("poc list(%q) = %d: %s", keyword, w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				List []map[string]interface{} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode poc list: %v", err)
		}
		if len(resp.Data.List) == 0 {
			return nil
		}
		return resp.Data.List[0]
	}

	pocID := "it-poc-round86"
	firstName := "IT POC Round86"
	secondName := "IT POC Round86 Renamed"

	w := do("/api/poc/add", map[string]interface{}{
		"content": pocYAML(pocID, firstName),
	})
	if w.Code != http.StatusOK {
		t.Fatalf("poc add = %d: %s", w.Code, w.Body.String())
	}

	created := find(firstName)
	if created == nil {
		t.Fatalf("新增的 POC 搜不到（保存没落库，或模板解析把 name 丢了）")
	}
	// 用户从 nuclei 模板复制过来的是 id，不是展示名：按 id 必须也能搜到
	if byID := findByKeyword(pocID); byID == nil {
		t.Fatalf("按 POC id 搜索搜不到（搜索只匹配 name）")
	}
	// 输入带正则元字符不能炸（未转义的 $regex 会报错或把输入当正则执行）
	if weird := findByKeyword("it-poc-round86("); weird != nil {
		t.Fatalf("含元字符的关键词居然命中了记录: %v", weird["name"])
	}
	id, _ := created["id"].(string)
	if id == "" {
		id = pocID
	}
	t.Cleanup(func() { do("/api/poc/delete", map[string]interface{}{"ids": []string{id}}) })

	// 详情要能按 nuclei 模板 id 取（人从模板/命令行复制的是它，不是库内 _id）。
	// 以前非 ObjectID 直接 500「invalid id format」。
	wDetail := do("/api/poc/detail", map[string]interface{}{"id": pocID})
	if wDetail.Code != http.StatusOK {
		t.Fatalf("按模板 id 取详情 = %d: %s", wDetail.Code, wDetail.Body.String())
	}
	var detail struct {
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(wDetail.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode poc detail: %v", err)
	}
	if detail.Data.Name != firstName {
		t.Fatalf("按模板 id 取到的不是这条 POC: %q", detail.Data.Name)
	}
	// 不存在的标识必须是 404，而不是 500
	if wMissing := do("/api/poc/detail", map[string]interface{}{"id": "no-such-poc-" + t.Name()}); wMissing.Code == http.StatusInternalServerError {
		t.Fatalf("不存在的 POC 详情回了 500: %s", wMissing.Body.String())
	}

	w = do("/api/poc/update", map[string]interface{}{
		"id": id, "content": pocYAML(pocID, secondName),
	})
	if w.Code != http.StatusOK {
		t.Fatalf("poc update = %d: %s", w.Code, w.Body.String())
	}
	if find(firstName) != nil {
		t.Fatalf("改名后旧名字仍能搜到，update 没落到这条记录")
	}
	if find(secondName) == nil {
		t.Fatalf("改名后新名字搜不到，update 没生效")
	}

	w = do("/api/poc/delete", map[string]interface{}{"ids": []string{id}})
	if w.Code != http.StatusOK {
		t.Fatalf("poc delete = %d: %s", w.Code, w.Body.String())
	}
	if find(secondName) != nil {
		t.Fatalf("删除后仍能搜到 POC")
	}
}

func pocYAML(id, name string) string {
	return "id: " + id + "\n" +
		"info:\n" +
		"  name: " + name + "\n" +
		"  author: ci-round86\n" +
		"  severity: info\n" +
		"http:\n" +
		"  - method: GET\n" +
		"    path:\n" +
		"      - \"{{BaseURL}}/\"\n" +
		"    matchers:\n" +
		"      - type: status\n" +
		"        status:\n" +
		"          - 200\n"
}
