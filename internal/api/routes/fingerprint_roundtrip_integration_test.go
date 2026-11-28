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

// 指纹的 add / update 此前没有用例点名。「加进去了但库里是旧内容」这种失败，
// 界面上只会看到指纹列表没变，扫描端也不会按新规则识别。
func TestFingerprintAddUpdateRoundTrip(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	post := func(path string, body interface{}) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}
	name := fmt.Sprintf("it-fp-%d", time.Now().UnixNano())
	content := func(company string) string {
		return fmt.Sprintf("fingerprint:\n  name: \"%s\"\n  category: \"Service\"\n  company: \"%s\"\n  rules:\n    - logic: \"OR\"\n      conditions:\n        - location: \"body\"\n          match_type: \"contains\"\n          pattern: \"%s\"\n", name, company, name)
	}

	var added struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	w := post("/api/fingerprint/add", map[string]interface{}{
		"content": content("IT-Corp"),
		"state":   false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("fingerprint add = %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode add: %v", err)
	}
	id := added.Data.ID
	if id == "" {
		t.Fatalf("add 没返回 id: %s", w.Body.String())
	}
	t.Cleanup(func() { post("/api/fingerprint/delete", map[string]interface{}{"ids": []string{id}}) })

	// 列表里按名字能查到，且 company 是新增时那份
	current := func() map[string]interface{} {
		w := post("/api/fingerprint/data", map[string]interface{}{
			"search": name, "pageIndex": 1, "pageSize": 10,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("fingerprint data = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				List []map[string]interface{} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode data: %v", err)
		}
		if len(resp.Data.List) == 0 {
			return nil
		}
		return resp.Data.List[0]
	}
	row := current()
	if row == nil {
		t.Fatalf("新增的指纹在列表里查不到")
	}
	if row["company"] != "IT-Corp" {
		t.Fatalf("新增后 company = %v，期望 IT-Corp", row["company"])
	}

	// 改 company + 打开 state，再读回来
	w = post("/api/fingerprint/update", map[string]interface{}{
		"id": id, "content": content("IT-Corp-2"), "state": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("fingerprint update = %d: %s", w.Code, w.Body.String())
	}
	row = current()
	if row == nil {
		t.Fatalf("更新后指纹消失了")
	}
	if row["company"] != "IT-Corp-2" {
		t.Fatalf("更新没落库：company = %v", row["company"])
	}
	if row["state"] != true {
		t.Fatalf("更新没落库：state = %v", row["state"])
	}

	// 改不存在的 id 必须是 404，不能回 200 假装成功
	w = post("/api/fingerprint/update", map[string]interface{}{
		"id": "0123456789abcdef01234567", "content": content("ghost"),
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("给不存在的指纹 id 更新 = %d，期望 404: %s", w.Code, w.Body.String())
	}

	w = post("/api/fingerprint/delete", map[string]interface{}{"ids": []string{id}})
	if w.Code != http.StatusOK {
		t.Fatalf("fingerprint delete = %d: %s", w.Code, w.Body.String())
	}
	if current() != nil {
		t.Fatalf("删除后指纹仍在列表里")
	}
}
