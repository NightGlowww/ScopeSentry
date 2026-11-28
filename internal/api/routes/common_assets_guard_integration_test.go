//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 通用资产操作守卫（/api/assets/common/*）：每个资产页的「删除 / 打标签 / 改状态」都走这一族，
// 而它此前没有任何集成用例（rest 引用索引里整族为空）。这一族最危险的地方是「空参数 / 未知集合 /
// 不存在的 id」必须明确失败 —— 一旦退化成 200 + 什么也没删，或者退化成 500，用户看到的就是
// 「点了没反应」或「服务挂了」，两种都会把排查引向错误方向。
//
// 只用不存在的 id，绝不删真实数据。
func TestCommonAssetOpsGuards(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	const nope = "000000000000000000000000"

	do := func(method, path string, body interface{}, withAuth bool) *httptest.ResponseRecorder {
		t.Helper()
		var payload []byte
		if body != nil {
			var err error
			payload, err = json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if withAuth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, req)
		return w
	}

	cases := []struct {
		name string
		path string
		body interface{}
		auth bool
		want int
	}{
		{"delete 未认证", "/api/assets/common/delete", map[string]interface{}{"index": "asset", "ids": []string{nope}}, false, http.StatusUnauthorized},
		// 现有约定：空 ids 走的是服务层的 ErrNoValidIDs，与「id 不存在」同一个 404 桶（换掉它要动前端语义，先记着）
		{"delete 空 ids", "/api/assets/common/delete", map[string]interface{}{"index": "asset", "ids": []string{}}, true, http.StatusNotFound},
		{"delete 未知集合", "/api/assets/common/delete", map[string]interface{}{"index": "no_such_collection", "ids": []string{nope}}, true, http.StatusNotFound},
		{"delete 不存在的 id", "/api/assets/common/delete", map[string]interface{}{"index": "asset", "ids": []string{nope}}, true, http.StatusNotFound},
		{"add_tag 未认证", "/api/assets/common/add_tag", map[string]interface{}{"tp": "asset", "id": nope, "tag": "x"}, false, http.StatusUnauthorized},
		{"add_tag 缺 tag", "/api/assets/common/add_tag", map[string]interface{}{"tp": "asset", "id": nope}, true, http.StatusBadRequest},
		{"add_tag 未知集合", "/api/assets/common/add_tag", map[string]interface{}{"tp": "no_such_collection", "id": nope, "tag": "x"}, true, http.StatusNotFound},
		{"add_tag 不存在的 id", "/api/assets/common/add_tag", map[string]interface{}{"tp": "asset", "id": nope, "tag": "x"}, true, http.StatusNotFound},
		{"delete_tag 未认证", "/api/assets/common/delete_tag", map[string]interface{}{"tp": "asset", "id": nope, "tag": "x"}, false, http.StatusUnauthorized},
		{"delete_tag 不存在的 id", "/api/assets/common/delete_tag", map[string]interface{}{"tp": "asset", "id": nope, "tag": "x"}, true, http.StatusNotFound},
		{"update_status 未认证", "/api/assets/common/update_status", map[string]interface{}{"tp": "SensitiveResult", "id": nope, "status": 1}, false, http.StatusUnauthorized},
		{"update_status 未知类型", "/api/assets/common/update_status", map[string]interface{}{"tp": "asset", "id": nope, "status": 1}, true, http.StatusNotFound},
		{"update_status 坏 ObjectID", "/api/assets/common/update_status", map[string]interface{}{"tp": "SensitiveResult", "id": "zzz", "status": 1}, true, http.StatusNotFound},
		// 关键一条：id 合法但文档不存在。mongo 驱动返回 ErrNoDocuments，
		// 打标签那条已经映射成 404，改状态这条之前没处理 → 500（本轮修）
		{"update_status 不存在的 id", "/api/assets/common/update_status", map[string]interface{}{"tp": "SensitiveResult", "id": nope, "status": 1}, true, http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(http.MethodPost, tc.path, tc.body, tc.auth)
			if w.Code != tc.want {
				t.Fatalf("%s %s = %d, want %d (%s)", tc.path, tc.name, w.Code, tc.want, w.Body.String())
			}
		})
	}
}
