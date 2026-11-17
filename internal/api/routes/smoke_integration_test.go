//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

// seedUserAndLogin 建一个临时超管并登录，返回 access_token。
// 用户名按测试名生成，避免并行/重复运行时互相踩。
func seedUserAndLogin(t *testing.T, router *gin.Engine) string {
	t.Helper()
	username := "e2e-smoke-" + strings.ReplaceAll(t.Name(), "/", "-")
	const password = "e2e-smoke-pass"

	users := mongodb.DB.Collection("user")
	_, _ = users.DeleteMany(t.Context(), bson.M{"username": username})
	t.Cleanup(func() { _, _ = users.DeleteMany(t.Context(), bson.M{"username": username}) })
	if _, err := users.InsertOne(t.Context(), bson.M{
		"username": username,
		"password": helper.Sha256Hex(password),
		"email":    "e2e@example.invalid",
		"role":     "super_admin",
		"status":   "active",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", w.Code, w.Body.String())
	}
	var loginResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login: %v (%s)", err, w.Body.String())
	}
	data, _ := loginResp["data"].(map[string]interface{})
	token, _ := data["access_token"].(string)
	if token == "" {
		t.Fatalf("login payload missing access_token: %s", w.Body.String())
	}
	return token
}

// API 冒烟：走真实路由 + 中间件 + Mongo/Redis，验证「登录 -> 带 token 访问受保护接口」
// 这条链路是通的，以及未认证请求确实被拦下。等价于前端 E2E，但不依赖浏览器。
func TestAPISmokeLoginAndProtectedRoute(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	// 1) 健康检查
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("healthz = %d: %s", w.Code, w.Body.String())
	}

	// 2) 未认证访问受保护接口 -> 401
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/assets/statistics", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated statistics = %d, want 401", w.Code)
	}

	// 3) 带 token 访问受保护接口 -> 200
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/assets/statistics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated statistics = %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("assetCount")) {
		t.Fatalf("statistics payload missing assetCount: %s", w.Body.String())
	}
}

// 新增接口的鉴权与参数校验走真实路由再验一遍：
// 这些分支不碰业务数据，但只有在完整路由链上跑过，才能确认 handler 真的挂上了、
// 中间件顺序没把 400/404 变成 500。
func TestAPISmokeNewEndpointGuards(t *testing.T) {
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

	remarkPath := "/api/assets/asset/remark"
	deletePath := "/api/assets/common/delete_by_search"
	validID := "507f1f77bcf86cd799439011"

	cases := []struct {
		name  string
		path  string
		body  map[string]interface{}
		token bool
		want  int
	}{
		{
			name: "remark requires auth",
			path: remarkPath, body: map[string]interface{}{"id": validID, "remark": "x"},
			token: false, want: http.StatusUnauthorized,
		},
		{
			name: "remark rejects bad object id",
			path: remarkPath, body: map[string]interface{}{"id": "not-an-object-id", "remark": "x"},
			token: true, want: http.StatusBadRequest,
		},
		{
			name: "remark rejects too long text",
			path: remarkPath, body: map[string]interface{}{"id": validID, "remark": strings.Repeat("字", 501)},
			token: true, want: http.StatusBadRequest,
		},
		{
			name: "delete by search requires confirmAll when filter is empty",
			path: deletePath, body: map[string]interface{}{"index": "DirScanResult"},
			token: true, want: http.StatusBadRequest,
		},
		{
			name: "delete by search rejects unknown collection",
			path: deletePath, body: map[string]interface{}{"index": "not_a_collection", "confirmAll": true},
			token: true, want: http.StatusNotFound,
		},
		{
			name: "export rejects bad time range",
			path: "/api/export",
			body: map[string]interface{}{
				"index": "asset", "quantity": 1, "type": "search", "filetype": "xlsx",
				"field": []string{"host"}, "startTime": "2026/01/01",
			},
			token: true, want: http.StatusBadRequest,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := post(c.path, c.body, c.token)
			if w.Code != c.want {
				t.Fatalf("%s = %d, want %d: %s", c.path, w.Code, c.want, w.Body.String())
			}
		})
	}

}
