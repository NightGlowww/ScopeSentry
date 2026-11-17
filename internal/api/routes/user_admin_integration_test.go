//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 用户管理面（对标 ARL-Next user.py）的端到端验收：真实路由链 + JWT + Mongo。
//
// 覆盖：
//   - 权限：未认证 401、普通 user 角色 403（PermissionMiddleware）、超管全通；
//   - 主链路：创建 → 登录 → 列表 → 改角色/邮箱 → 删除 → 删除后登录失败；
//   - 校验：重复用户名 / 重复邮箱 / 非法角色 / 弱密码 / pageSize>200 / 非法 id 全部 400，
//     合法但不存在的 id 404；
//   - 安全：不能删自己、不能降级或删除最后一个 super_admin、响应里没有密码哈希。

// userAdminDo 发一个 JSON 请求，返回状态码、原始响应体和解包后的 data。
// 原始响应体用来断言「密码哈希绝不出现在响应里」——解包成 map 会丢掉 key 的存在性。
func userAdminDo(t *testing.T, router *gin.Engine, method, path string, body interface{}, token string) (int, string, map[string]interface{}) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body for %s %s: %v", method, path, err)
		}
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(w, req)

	raw := w.Body.String()
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]interface{})
	return w.Code, raw, data
}

// userAdminJWTUserID 从 access_token 里取 userID（seedUserAndLogin 只返回 token）
func userAdminJWTUserID(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token 不是三段式 JWT: %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode jwt payload: %v", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("decode jwt claims: %v", err)
	}
	userID, _ := claims["userID"].(string)
	if userID == "" {
		t.Fatalf("jwt 里没有 userID: %s", payload)
	}
	return userID
}

// userAdminParkSuperAdminsExcept 把除 keep 之外的 super_admin 临时降成 admin。
//
// 「最后一个 super_admin」守卫只有在目标真的是唯一超管时才触发，而 JWT 里的 role 是签发时
// 的快照，所以把当前操作者也降级后，它的 token 仍然能通过 PermissionMiddleware。
// 用例结束（含 t.Fatalf 路径）按 _id 还原，避免污染测试库。
func userAdminParkSuperAdminsExcept(t *testing.T, keepIDs ...primitive.ObjectID) {
	t.Helper()
	users := mongodb.DB.Collection("user")
	filter := bson.M{"role": constants.RoleSuperAdmin}
	if len(keepIDs) > 0 {
		filter["_id"] = bson.M{"$nin": keepIDs}
	}
	cursor, err := users.Find(t.Context(), filter, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		t.Fatalf("find super admins: %v", err)
	}
	var docs []struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := cursor.All(t.Context(), &docs); err != nil {
		t.Fatalf("decode super admins: %v", err)
	}
	ids := make([]primitive.ObjectID, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	if len(ids) == 0 {
		return
	}
	if _, err := users.UpdateMany(t.Context(),
		bson.M{"_id": bson.M{"$in": ids}},
		bson.M{"$set": bson.M{"role": constants.RoleAdmin}},
	); err != nil {
		t.Fatalf("park super admins: %v", err)
	}
	t.Cleanup(func() {
		// t.Context() 在 Cleanup 之前已取消，这里用独立 context
		_, _ = users.UpdateMany(context.Background(),
			bson.M{"_id": bson.M{"$in": ids}},
			bson.M{"$set": bson.M{"role": constants.RoleSuperAdmin}},
		)
	})
}

// userAdminAssertNoSecret 断言响应体里没有密码字段 / 密码哈希
func userAdminAssertNoSecret(t *testing.T, label, raw string, password string) {
	t.Helper()
	if strings.Contains(raw, `"password"`) {
		t.Fatalf("%s 响应里出现了 password 字段: %s", label, raw)
	}
	hash := helper.Sha256Hex(password)
	if strings.Contains(raw, hash) {
		t.Fatalf("%s 响应里出现了密码哈希: %s", label, raw)
	}
}

// TestUserAdminCRUDAndGuards 主链路 + 参数校验 + 权限。
func TestUserAdminCRUDAndGuards(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	prefix := "ua-" + suffix
	users := mongodb.DB.Collection("user")
	t.Cleanup(func() {
		_, _ = users.DeleteMany(context.Background(), bson.M{"username": bson.M{"$regex": "^" + prefix}})
	})

	const password = "userpass123"
	usernameA := prefix + "-a"
	emailA := usernameA + "@example.invalid"

	// --- 未认证 401 -------------------------------------------------------
	status, _, _ := userAdminDo(t, router, http.MethodGet, "/api/user", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("未认证列表 = %d, want 401", status)
	}
	status, _, _ = userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": prefix + "-anon", "password": password, "email": prefix + "-anon@example.invalid", "role": "user",
	}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("未认证创建 = %d, want 401", status)
	}

	// --- 创建 ------------------------------------------------------------
	status, raw, data := userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": usernameA, "password": password, "email": emailA, "role": "user",
	}, token)
	if status != http.StatusOK {
		t.Fatalf("创建用户 = %d: %s", status, raw)
	}
	userAdminAssertNoSecret(t, "创建", raw, password)
	idA, _ := data["id"].(string)
	if idA == "" {
		t.Fatalf("创建响应缺少 id: %s", raw)
	}
	if role, _ := data["role"].(string); role != "user" {
		t.Fatalf("创建后 role = %v, want user", data["role"])
	}
	if state, _ := data["status"].(string); state != "active" {
		t.Fatalf("未传 status 时默认应为 active, got %v", data["status"])
	}

	// 重复用户名 / 重复邮箱 / 非法角色 / 弱密码 -> 400
	status, raw, _ = userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": usernameA, "password": password, "email": prefix + "-dup@example.invalid", "role": "user",
	}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("重复用户名 = %d, want 400: %s", status, raw)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": prefix + "-dup", "password": password, "email": emailA, "role": "user",
	}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("重复邮箱 = %d, want 400: %s", status, raw)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": prefix + "-badrole", "password": password, "email": prefix + "-badrole@example.invalid", "role": "root",
	}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("非法角色 = %d, want 400: %s", status, raw)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": prefix + "-weak", "password": "1234567", "email": prefix + "-weak@example.invalid", "role": "user",
	}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("弱密码 = %d, want 400: %s", status, raw)
	}

	// --- 登录 ------------------------------------------------------------
	tokenA := userAdminLogin(t, router, usernameA, password)
	// 刚创建时 role=user：没有任何 user:* 权限，列表直接 403
	status, raw, _ = userAdminDo(t, router, http.MethodGet, "/api/user", nil, tokenA)
	if status != http.StatusForbidden {
		t.Fatalf("普通用户列表 = %d, want 403: %s", status, raw)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodPost, "/api/user/login", map[string]interface{}{
		"username": usernameA, "password": "wrong-password",
	}, "")
	if status == http.StatusOK {
		t.Fatalf("错误密码居然登录成功: %s", raw)
	}

	// --- 详情 / 非法 id / 不存在 ----------------------------------------
	status, raw, data = userAdminDo(t, router, http.MethodGet, "/api/user/"+idA, nil, token)
	if status != http.StatusOK {
		t.Fatalf("详情 = %d: %s", status, raw)
	}
	userAdminAssertNoSecret(t, "详情", raw, password)
	if got, _ := data["username"].(string); got != usernameA {
		t.Fatalf("详情 username = %v, want %s", data["username"], usernameA)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodGet, "/api/user/not-an-object-id", nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("非法 id = %d, want 400: %s", status, raw)
	}
	status, _, _ = userAdminDo(t, router, http.MethodGet, "/api/user/507f1f77bcf86cd799439011", nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("不存在 id = %d, want 404", status)
	}

	// --- 列表 ------------------------------------------------------------
	status, raw, data = userAdminDo(t, router, http.MethodGet, "/api/user?keyword="+usernameA+"&pageIndex=1&pageSize=20", nil, token)
	if status != http.StatusOK {
		t.Fatalf("列表 = %d: %s", status, raw)
	}
	userAdminAssertNoSecret(t, "列表", raw, password)
	if total, _ := data["total"].(float64); total < 1 {
		t.Fatalf("关键字过滤 total = %v, want >=1 (%s)", data["total"], raw)
	}
	list, _ := data["list"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("关键字过滤命中 %d 条, want 1: %s", len(list), raw)
	}
	row, _ := list[0].(map[string]interface{})
	if got, _ := row["id"].(string); got != idA {
		t.Fatalf("列表首行 id = %v, want %s", row["id"], idA)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodGet, "/api/user?pageSize=201", nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("pageSize=201 = %d, want 400: %s", status, raw)
	}

	// --- 普通用户 403 ----------------------------------------------------
	usernameC := prefix + "-c"
	status, raw, data = userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": usernameC, "password": password, "email": usernameC + "@example.invalid", "role": "user",
	}, token)
	if status != http.StatusOK {
		t.Fatalf("创建普通用户 = %d: %s", status, raw)
	}
	idC, _ := data["id"].(string)
	tokenC := userAdminLogin(t, router, usernameC, password)
	status, raw, _ = userAdminDo(t, router, http.MethodGet, "/api/user", nil, tokenC)
	if status != http.StatusForbidden {
		t.Fatalf("普通用户列表 = %d, want 403: %s", status, raw)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": prefix + "-byc", "password": password, "email": prefix + "-byc@example.invalid", "role": "user",
	}, tokenC)
	if status != http.StatusForbidden {
		t.Fatalf("普通用户创建 = %d, want 403: %s", status, raw)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodDelete, "/api/user/"+idC, nil, tokenC)
	if status != http.StatusForbidden {
		t.Fatalf("普通用户删除 = %d, want 403: %s", status, raw)
	}

	// --- 改角色 / 改邮箱 -------------------------------------------------
	emailA2 := prefix + "-a2@example.invalid"
	status, raw, data = userAdminDo(t, router, http.MethodPut, "/api/user/"+idA, map[string]interface{}{
		"role": "admin", "email": emailA2,
	}, token)
	if status != http.StatusOK {
		t.Fatalf("更新用户 = %d: %s", status, raw)
	}
	userAdminAssertNoSecret(t, "更新", raw, password)
	if role, _ := data["role"].(string); role != "admin" {
		t.Fatalf("改角色后 role = %v, want admin", data["role"])
	}
	if email, _ := data["email"].(string); email != emailA2 {
		t.Fatalf("改邮箱后 email = %v, want %s", data["email"], emailA2)
	}
	// admin 有 user:list / user:read
	tokenA2 := userAdminLogin(t, router, usernameA, password)
	status, raw, _ = userAdminDo(t, router, http.MethodGet, "/api/user?keyword="+usernameA, nil, tokenA2)
	if status != http.StatusOK {
		t.Fatalf("admin 列表 = %d, want 200: %s", status, raw)
	}
	// admin 没有 user:create
	status, raw, _ = userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": prefix + "-bya", "password": password, "email": prefix + "-bya@example.invalid", "role": "user",
	}, tokenA2)
	if status != http.StatusForbidden {
		t.Fatalf("admin 创建 = %d, want 403: %s", status, raw)
	}

	// --- 非法角色 / 弱密码的更新也 400 -----------------------------------
	status, raw, _ = userAdminDo(t, router, http.MethodPut, "/api/user/"+idA, map[string]interface{}{"role": "root"}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("更新为非法角色 = %d, want 400: %s", status, raw)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodPut, "/api/user/"+idA, map[string]interface{}{"password": "1234567"}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("更新为弱密码 = %d, want 400: %s", status, raw)
	}

	// --- 删除 → 登录失败 → 详情 404 --------------------------------------
	status, raw, _ = userAdminDo(t, router, http.MethodDelete, "/api/user/"+idA, nil, token)
	if status != http.StatusOK {
		t.Fatalf("删除 = %d: %s", status, raw)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodPost, "/api/user/login", map[string]interface{}{
		"username": usernameA, "password": password,
	}, "")
	if status == http.StatusOK {
		t.Fatalf("删除后仍能登录: %s", raw)
	}
	status, _, _ = userAdminDo(t, router, http.MethodGet, "/api/user/"+idA, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("删除后详情 = %d, want 404", status)
	}
}

// TestUserAdminSelfAndLastSuperAdmin 自我删除 / 最后一个超管的两道守卫。
func TestUserAdminSelfAndLastSuperAdmin(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	actorID := userAdminJWTUserID(t, token)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	prefix := "ua-" + suffix
	users := mongodb.DB.Collection("user")
	t.Cleanup(func() {
		_, _ = users.DeleteMany(context.Background(), bson.M{"username": bson.M{"$regex": "^" + prefix}})
	})

	const password = "userpass123"

	// 不能删除自己
	status, raw, _ := userAdminDo(t, router, http.MethodDelete, "/api/user/"+actorID, nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("删除自己 = %d, want 400: %s", status, raw)
	}

	// 造一个超管目标，然后把其他超管（含当前操作者）临时降级，让它成为唯一超管。
	usernameB := prefix + "-b"
	status, raw, data := userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": usernameB, "password": password, "email": usernameB + "@example.invalid", "role": "super_admin",
	}, token)
	if status != http.StatusOK {
		t.Fatalf("创建超管 = %d: %s", status, raw)
	}
	idB, _ := data["id"].(string)
	if idB == "" {
		t.Fatalf("创建超管响应缺少 id: %s", raw)
	}
	userAdminParkSuperAdminsExcept(t, mustObjectID(t, idB))

	// 降级唯一超管 -> 400
	status, raw, _ = userAdminDo(t, router, http.MethodPut, "/api/user/"+idB, map[string]interface{}{"role": "admin"}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("降级最后一个超管 = %d, want 400: %s", status, raw)
	}
	// 删除唯一超管 -> 400
	status, raw, _ = userAdminDo(t, router, http.MethodDelete, "/api/user/"+idB, nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("删除最后一个超管 = %d, want 400: %s", status, raw)
	}
	// 停用唯一超管同样会锁死用户管理入口 -> 400
	status, raw, _ = userAdminDo(t, router, http.MethodPut, "/api/user/"+idB, map[string]interface{}{"status": "disabled"}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("停用最后一个超管 = %d, want 400: %s", status, raw)
	}

	// 普通用户的 status 可以自由切换：停用后登录失败，恢复后又能登录
	usernameD := prefix + "-d"
	status, raw, data = userAdminDo(t, router, http.MethodPost, "/api/user", map[string]interface{}{
		"username": usernameD, "password": password, "email": usernameD + "@example.invalid", "role": "user",
	}, token)
	if status != http.StatusOK {
		t.Fatalf("创建普通用户 = %d: %s", status, raw)
	}
	idD, _ := data["id"].(string)
	userAdminLogin(t, router, usernameD, password)
	status, raw, _ = userAdminDo(t, router, http.MethodPut, "/api/user/"+idD, map[string]interface{}{"status": "disabled"}, token)
	if status != http.StatusOK {
		t.Fatalf("停用普通用户 = %d, want 200: %s", status, raw)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodPost, "/api/user/login", map[string]interface{}{
		"username": usernameD, "password": password,
	}, "")
	if status == http.StatusOK {
		t.Fatalf("停用用户仍然登录成功: %s", raw)
	}
	status, raw, _ = userAdminDo(t, router, http.MethodPut, "/api/user/"+idD, map[string]interface{}{"status": "active"}, token)
	if status != http.StatusOK {
		t.Fatalf("恢复普通用户 = %d, want 200: %s", status, raw)
	}
	userAdminLogin(t, router, usernameD, password)
}

// TestUserAdminIndexes 断言 initdb 真把用户表的索引建出来了（不是「测试库没索引」的假阴性）。
func TestUserAdminIndexes(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}
	cursor, err := mongodb.DB.Collection("user").Indexes().List(t.Context())
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	var docs []bson.M
	if err := cursor.All(t.Context(), &docs); err != nil {
		t.Fatalf("decode indexes: %v", err)
	}
	found := make(map[string]bson.M, len(docs))
	for _, doc := range docs {
		if name, ok := doc["name"].(string); ok {
			found[name] = doc
		}
	}
	for _, name := range []string{"user_created_at", "user_role", "user_email"} {
		if _, ok := found[name]; !ok {
			t.Fatalf("缺少索引 %s，现有: %v", name, indexNames(found))
		}
	}
	doc, ok := found["user_username_unique"]
	if !ok {
		t.Fatalf("缺少唯一索引 user_username_unique，现有: %v", indexNames(found))
	}
	if unique, _ := doc["unique"].(bool); !unique {
		t.Fatalf("索引 user_username_unique 不是唯一索引: %v", doc)
	}
}

func indexNames(found map[string]bson.M) []string {
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	return names
}

// userAdminLogin 登录并返回 access_token（失败直接终止用例）
func userAdminLogin(t *testing.T, router *gin.Engine, username, password string) string {
	t.Helper()
	status, raw, data := userAdminDo(t, router, http.MethodPost, "/api/user/login", map[string]interface{}{
		"username": username, "password": password,
	}, "")
	if status != http.StatusOK {
		t.Fatalf("登录 %s = %d: %s", username, status, raw)
	}
	token, _ := data["access_token"].(string)
	if token == "" {
		t.Fatalf("登录响应缺少 access_token: %s", raw)
	}
	return token
}

func mustObjectID(t *testing.T, hex string) primitive.ObjectID {
	t.Helper()
	objectID, err := primitive.ObjectIDFromHex(hex)
	if err != nil {
		t.Fatalf("非法 ObjectID %q: %v", hex, err)
	}
	return objectID
}
