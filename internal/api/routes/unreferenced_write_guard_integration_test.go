//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 未被任何测试/契约脚本点名的写接口（rest_closure.py 的清单）里，挑「传不存在的目标必须失败」的一批。
// 断言只有一条：**不能回 200**。回 400（绑定失败）也算通过，但会记进日志，方便下一步把 body 补齐；
// 真正的靶子是「200 + 什么也没删」这种假成功 —— 上一轮的 update_status 就是这么被揪出来的。
func TestUnreferencedWriteGuards(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	const nope = "000000000000000000000000"

	do := func(path string, body interface{}) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}

	cases := []struct {
		name string
		path string
		body interface{}
	}{
		{"poc/delete 不存在 id", "/api/poc/delete", map[string]interface{}{"ids": []string{nope}}},
		{"node/delete 不存在名字", "/api/node/delete", map[string]interface{}{"names": []string{"no-such-node-r73"}}},
		{"task/template/delete 不存在 id", "/api/task/template/delete", map[string]interface{}{"ids": []string{nope}}},
		{"task/scheduled/delete 不存在 id", "/api/task/scheduled/delete", map[string]interface{}{"ids": []string{nope}}},
		{"apikey/delete 不存在 id", "/api/apikey/delete", map[string]interface{}{"id": nope}},
		{"assets/fileleak/delete 不存在 id", "/api/assets/fileleak/delete", map[string]interface{}{"ids": []string{nope}}},
		{"assets/npoc_service/delete 不存在 id", "/api/assets/npoc_service/delete", map[string]interface{}{"ids": []string{nope}}},
		{"assets/nuclei_result/status 不存在 id", "/api/assets/nuclei_result/status", map[string]interface{}{"id": nope, "status": 1}},
		{"project/delete 不存在 id", "/api/project/delete", map[string]interface{}{"ids": []string{nope}}},
		{"sensitive/delete 不存在 id", "/api/sensitive/delete", map[string]interface{}{"ids": []string{nope}}},
		{"fingerprint/delete 不存在 id", "/api/fingerprint/delete", map[string]interface{}{"ids": []string{nope}}},
		{"project/update 不存在的 id", "/api/project/update", map[string]interface{}{"id": nope, "name": "no-such-project-r74"}},
		{"sensitive/update/state 不存在的 id", "/api/sensitive/update/state", map[string]interface{}{"ids": []string{nope}, "state": false}},
		{"plugin/delete 不存在的插件", "/api/plugin/delete", map[string]interface{}{"data": []map[string]string{{"hash": "no-such-hash-r73", "module": "TargetHandler"}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(tc.path, tc.body)
			// 期望 404（目标不存在）或 400（入参不合法）。500 也要报失败 ——
			// 这条断言就是靠它抓到 /api/task/scheduled/delete 的 nil pointer panic
			// （Gin Recovery 兜住后对外是 500，只看"不是 200"会漏过去）。
			switch w.Code {
			case http.StatusNotFound, http.StatusBadRequest:
			case http.StatusOK:
				t.Fatalf("%s 对不存在的目标回了 200（假成功）: %s", tc.path, w.Body.String())
			case http.StatusInternalServerError:
				t.Fatalf("%s 回 500（服务端崩了）: %s", tc.path, w.Body.String())
			default:
				t.Fatalf("%s 回 %d，期望 400/404: %s", tc.path, w.Code, w.Body.String())
			}
		})
	}
}