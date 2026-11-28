//go:build integration

package routes

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 一批「写操作」此前既没测试也没契约脚本点名（scripts/ci/rest_closure.py 的未覆盖清单）。
// 不要求每条都造数据跑全生命周期，最低限度必须守住一条：**畸形入参不能回 200**。
// 假成功的典型形态就是「绑定/删除/改状态」回 200 但库里一条没动，
// 客户端据此以为改好了，后面的聚合视图全是错的（2026-11-28 实测过 asset_scope/delete）。
func TestWriteEndpointsRejectMalformedBody(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	postRaw := func(path, body string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w.Code
	}

	cases := []struct {
		name string
		path string
		body string
	}{
		{"asset_group/edit 缺 id", "/api/assets/asset_group/edit", `{}`},
		{"asset_group/edit id 类型错", "/api/assets/asset_group/edit", `{"id":123}`},
		{"asset_group/reorder 缺 orderIds", "/api/assets/asset_group/reorder", `{}`},
		{"asset_scope/update 缺 id", "/api/assets/asset_scope/update", `{}`},
		{"asset_scope/delete 缺 ids", "/api/assets/asset_scope/delete", `{}`},
		{"asset_scope/bind_enterprise 缺 id", "/api/assets/asset_scope/bind_enterprise", `{}`},
		{"asset_scope/clear_update_error 缺 ids", "/api/assets/asset_scope/clear_update_error", `{}`},
		{"nuclei_result/status/batch 缺 ids", "/api/assets/nuclei_result/status/batch", `{"status":2}`},
		{"notification/delete 缺 ids", "/api/configuration/notification/delete", `{}`},
	}

	for _, tc := range cases {
		code := postRaw(tc.path, tc.body)
		if code == http.StatusOK {
			t.Errorf("%s：畸形入参回了 200（%s %s）", tc.name, tc.path, tc.body)
		}
		if code == http.StatusUnauthorized || code == http.StatusForbidden {
			t.Errorf("%s：被鉴权拦住了，说明用例本身没打到位（%d）", tc.name, code)
		}
	}
}
