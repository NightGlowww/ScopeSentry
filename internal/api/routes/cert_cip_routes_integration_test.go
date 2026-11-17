//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
)

// 新增证书 / ICP / 总览接口的鉴权与参数校验：走真实路由 + 中间件链，
// 确认 401 与 pageSize>200 的 400 不会被中间件改写成别的状态码。
func TestCertCIPRouteGuards(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	post := func(path string, body map[string]interface{}, withToken bool) *httptest.ResponseRecorder {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if withToken {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, req)
		return w
	}

	cases := []struct {
		name  string
		path  string
		body  map[string]interface{}
		token bool
		want  int
	}{
		{name: "cert list requires auth", path: "/api/assets/cert/list", body: map[string]interface{}{}, want: http.StatusUnauthorized},
		{name: "cert list rejects pageSize 500", path: "/api/assets/cert/list", body: map[string]interface{}{"pageSize": 500}, token: true, want: http.StatusBadRequest},
		{name: "cert list accepts pageSize 200", path: "/api/assets/cert/list", body: map[string]interface{}{"pageSize": 200}, token: true, want: http.StatusOK},
		{name: "cert detail requires auth", path: "/api/assets/cert/detail", body: map[string]interface{}{"id": "fp:x"}, want: http.StatusUnauthorized},
		{name: "cert detail rejects empty id", path: "/api/assets/cert/detail", body: map[string]interface{}{}, token: true, want: http.StatusBadRequest},
		{name: "cert detail missing cert", path: "/api/assets/cert/detail", body: map[string]interface{}{"id": "fp:no-such"}, token: true, want: http.StatusNotFound},
		{name: "cip list requires auth", path: "/api/assets/cip/list", body: map[string]interface{}{}, want: http.StatusUnauthorized},
		{name: "cip list rejects pageSize 500", path: "/api/assets/cip/list", body: map[string]interface{}{"pageSize": 500}, token: true, want: http.StatusBadRequest},
		{name: "cip list accepts pageSize 200", path: "/api/assets/cip/list", body: map[string]interface{}{"pageSize": 200}, token: true, want: http.StatusOK},
		{name: "cip detail requires auth", path: "/api/assets/cip/detail", body: map[string]interface{}{"domain": "x.invalid"}, want: http.StatusUnauthorized},
		{name: "cip detail rejects empty domain", path: "/api/assets/cip/detail", body: map[string]interface{}{}, token: true, want: http.StatusBadRequest},
		{name: "cip detail rejects pageSize 500", path: "/api/assets/cip/detail", body: map[string]interface{}{"domain": "x.invalid", "pageSize": 500}, token: true, want: http.StatusBadRequest},
		{name: "cip detail missing subject", path: "/api/assets/cip/detail", body: map[string]interface{}{"domain": "no-such.invalid"}, token: true, want: http.StatusNotFound},
		{name: "overview count requires auth", path: "/api/assets/asset_overview/count", body: map[string]interface{}{}, want: http.StatusUnauthorized},
		{name: "overview count accepts auth", path: "/api/assets/asset_overview/count", body: map[string]interface{}{}, token: true, want: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := post(tc.path, tc.body, tc.token)
			if w.Code != tc.want {
				t.Fatalf("%s = %d, want %d: %s", tc.path, w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusOK && tc.path == "/api/assets/asset_overview/count" {
				if !bytes.Contains(w.Body.Bytes(), []byte("cert")) {
					t.Fatalf("overview payload missing cert: %s", w.Body.String())
				}
			}
		})
	}
}
