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

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 扫描策略路由面走真实路由链（SetupRouter + JWT + Mongo）：
//   - 守卫：7 个端点未带 token 一律 401；pageSize>200 一律 400；未知/非法 id 一律 404；
//   - 主链路：create（重名 400）→ detail（usage 说明）→ list（enabled 过滤）→ update（改名/改参数）
//     → apply（对象基线 + 模板 ID 基线）→ test（范围 + 存在性）→ delete（删后 detail 404、名字释放）。
//
// 模板 / 插件引用用临时文档注入（扫描模板走 ScanTemplates 集合，插件走 plugins 集合），
// 用例结束按 _id / hash 清理，不污染真实数据。

func policySeedPlugin(t *testing.T, suffix, tag string) (hash, name string) {
	t.Helper()
	hash = fmt.Sprintf("%032x", time.Now().UnixNano())
	name = "policy-plugin-" + tag + "-" + suffix
	if _, err := mongodb.DB.Collection("plugins").InsertOne(t.Context(), bson.M{
		"module": "DirScan",
		"name":   name,
		"hash":   hash,
		"type":   "built-in",
	}); err != nil {
		t.Fatalf("seed plugin %s: %v", tag, err)
	}
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("plugins").DeleteMany(t.Context(), bson.M{"hash": hash})
	})
	return hash, name
}

func policySeedTemplate(t *testing.T, suffix, subdomainPlugin, dirPlugin string) string {
	t.Helper()
	doc := bson.M{
		"name":          "policy-template-" + suffix,
		"SubdomainScan": []string{subdomainPlugin},
		"DirScan":       []string{dirPlugin},
	}
	res, err := mongodb.DB.Collection("ScanTemplates").InsertOne(t.Context(), doc)
	if err != nil {
		t.Fatalf("seed template: %v", err)
	}
	oid, ok := res.InsertedID.(primitive.ObjectID)
	if !ok {
		t.Fatalf("template inserted id type = %T", res.InsertedID)
	}
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("ScanTemplates").DeleteOne(t.Context(), bson.M{"_id": oid})
	})
	return oid.Hex()
}

// policyAssertIndexes 断言 initdb 里的两条必需索引真的建出来了（name 唯一 / enabled+updatedAt）。
func policyAssertIndexes(t *testing.T) {
	t.Helper()
	cursor, err := mongodb.DB.Collection("scan_policy").Indexes().List(t.Context())
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
	uniq, ok := found["scan_policy_name_unique"]
	if !ok {
		t.Fatalf("缺少 scan_policy_name_unique 索引: %v", found)
	}
	if unique, _ := uniq["unique"].(bool); !unique {
		t.Fatalf("scan_policy_name_unique 不是唯一索引: %v", uniq)
	}
	if _, ok := found["scan_policy_enabled_updated"]; !ok {
		t.Fatalf("缺少 scan_policy_enabled_updated 索引: %v", found)
	}
}

func TestPolicyRoutesRequireAuth(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	router := SetupRouter()
	for _, path := range []string{
		"/api/policy/list",
		"/api/policy/create",
		"/api/policy/update",
		"/api/policy/delete",
		"/api/policy/detail",
		"/api/policy/apply",
		"/api/policy/test",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s 未认证 = %d, want 401 (%s)", path, w.Code, w.Body.String())
		}
	}
}

func TestPolicyRoutesCRUDApplyAndTest(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}
	policyAssertIndexes(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	do := func(method, path string, body interface{}, withAuth bool) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			reader = bytes.NewReader(payload)
		} else {
			reader = bytes.NewReader(nil)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		if withAuth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, req)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		var payload map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode (%s): %v", w.Body.String(), err)
		}
		return payload
	}
	dataOf := func(w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		payload := decode(w)
		data, ok := payload["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("响应缺少 data 对象: %v", payload)
		}
		return data
	}

	hashA, nameA := policySeedPlugin(t, suffix, "a")
	hashB, _ := policySeedPlugin(t, suffix, "b")
	templateID := policySeedTemplate(t, suffix, hashA, hashB)
	unknownTemplate := "0123456789abcdef01234567"

	policyName := "policy-" + suffix
	otherName := "policy-other-" + suffix

	// ---------- 1) pageSize 上限 200 ----------
	if w := do(http.MethodPost, "/api/policy/list", map[string]interface{}{"pageSize": 500}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("list pageSize=500 = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/policy/list", map[string]interface{}{"pageSize": 200}, true); w.Code != http.StatusOK {
		t.Fatalf("list pageSize=200 = %d, want 200 (%s)", w.Code, w.Body.String())
	}

	// ---------- 2) create ----------
	createBody := map[string]interface{}{
		"name":        policyName,
		"description": "集成测试策略",
		"enabled":     true,
		"config": map[string]interface{}{
			"templateId":     templateID,
			"plugins":        []string{hashA},
			"threadNum":      50,
			"rateLimit":      100,
			"timeoutSeconds": 120,
			"dicts":          map[string]interface{}{"dir": "dir-top", "subdomain": "sub-top"},
			"options":        map[string]interface{}{"a": "1"},
		},
	}
	w := do(http.MethodPost, "/api/policy/create", createBody, true)
	if w.Code != http.StatusOK {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	created := dataOf(w)
	policyID, _ := created["id"].(string)
	if policyID == "" {
		t.Fatalf("create 未返回 id: %v", created)
	}
	if created["name"] != policyName {
		t.Fatalf("create name = %v, want %s", created["name"], policyName)
	}
	if enabled, _ := created["enabled"].(bool); !enabled {
		t.Fatalf("create enabled = %v, want true", created["enabled"])
	}
	createdCfg, _ := created["config"].(map[string]interface{})
	if createdCfg["threadNum"].(float64) != 50 || createdCfg["rateLimit"].(float64) != 100 || createdCfg["timeoutSeconds"].(float64) != 120 {
		t.Fatalf("create config 数值不符: %v", createdCfg)
	}

	// 重名 → 400
	if w := do(http.MethodPost, "/api/policy/create", createBody, true); w.Code != http.StatusBadRequest {
		t.Fatalf("重名 create = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	// 参数越界 → 400（复用同一套校验规则）
	badRange := map[string]interface{}{
		"name":   "policy-bad-" + suffix,
		"config": map[string]interface{}{"threadNum": 201},
	}
	if w := do(http.MethodPost, "/api/policy/create", badRange, true); w.Code != http.StatusBadRequest {
		t.Fatalf("越界 create = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	// 未知插件 → 400
	badPlugin := map[string]interface{}{
		"name":   "policy-bad-plugin-" + suffix,
		"config": map[string]interface{}{"plugins": []string{"ghost-" + suffix}},
	}
	if w := do(http.MethodPost, "/api/policy/create", badPlugin, true); w.Code != http.StatusBadRequest {
		t.Fatalf("未知插件 create = %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// ---------- 3) detail ----------
	w = do(http.MethodPost, "/api/policy/detail", map[string]interface{}{"id": policyID}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("detail = %d: %s", w.Code, w.Body.String())
	}
	detail := dataOf(w)
	usage, ok := detail["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("detail 缺少 usage: %v", detail)
	}
	if tracked, _ := usage["tracked"].(bool); tracked {
		t.Fatalf("usage.tracked = true, want false（任务未存 policyId）")
	}
	if note, _ := usage["note"].(string); note == "" {
		t.Fatalf("usage.note 为空: %v", usage)
	}
	detailCfg, _ := detail["config"].(map[string]interface{})
	detailDicts, _ := detailCfg["dicts"].(map[string]interface{})
	if detailDicts["dir"] != "dir-top" || detailDicts["subdomain"] != "sub-top" {
		t.Fatalf("detail dicts = %v", detailDicts)
	}
	if plugins, _ := detailCfg["plugins"].([]interface{}); len(plugins) != 1 || plugins[0] != hashA {
		t.Fatalf("detail plugins = %v, want [%s]", detailCfg["plugins"], hashA)
	}

	// 不存在 / 非法 ID → 404
	if w := do(http.MethodPost, "/api/policy/detail", map[string]interface{}{"id": unknownTemplate}, true); w.Code != http.StatusNotFound {
		t.Fatalf("未知 id detail = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/policy/detail", map[string]interface{}{"id": "not-an-objectid"}, true); w.Code != http.StatusNotFound {
		t.Fatalf("非法 id detail = %d, want 404 (%s)", w.Code, w.Body.String())
	}

	// ---------- 4) list ----------
	w = do(http.MethodPost, "/api/policy/list", map[string]interface{}{"keyword": suffix, "pageSize": 50}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", w.Code, w.Body.String())
	}
	listData := dataOf(w)
	if total, _ := listData["total"].(float64); total < 1 {
		t.Fatalf("list total = %v, want >= 1", listData["total"])
	}
	list, _ := listData["list"].([]interface{})
	foundOurs := false
	for _, item := range list {
		row, _ := item.(map[string]interface{})
		if row["id"] == policyID {
			foundOurs = true
		}
	}
	if !foundOurs {
		t.Fatalf("list 未返回新建策略: %v", listData)
	}

	// ---------- 5) update ----------
	updatedName := "policy-updated-" + suffix
	w = do(http.MethodPost, "/api/policy/update", map[string]interface{}{
		"id":      policyID,
		"name":    updatedName,
		"enabled": false,
		"config": map[string]interface{}{
			"templateId":     templateID,
			"plugins":        []string{hashA},
			"threadNum":      80,
			"rateLimit":      100,
			"timeoutSeconds": 120,
			"dicts":          map[string]interface{}{"dir": "dir-top"},
			"options":        map[string]interface{}{"a": "1"},
		},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", w.Code, w.Body.String())
	}
	updated := dataOf(w)
	if updated["name"] != updatedName {
		t.Fatalf("update name = %v, want %s", updated["name"], updatedName)
	}
	if enabled, _ := updated["enabled"].(bool); enabled {
		t.Fatalf("update enabled = true, want false")
	}
	updatedCfg, _ := updated["config"].(map[string]interface{})
	if updatedCfg["threadNum"].(float64) != 80 {
		t.Fatalf("update threadNum = %v, want 80", updatedCfg["threadNum"])
	}

	// enabled=false 过滤能命中，enabled=true 过滤不命中
	w = do(http.MethodPost, "/api/policy/list", map[string]interface{}{
		"keyword": suffix, "enabled": false, "pageSize": 50,
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("list enabled=false = %d: %s", w.Code, w.Body.String())
	}
	if total, _ := dataOf(w)["total"].(float64); total < 1 {
		t.Fatalf("enabled=false 过滤 total = %v, want >= 1", total)
	}

	// 第二个策略：用于重名改名冲突
	w = do(http.MethodPost, "/api/policy/create", map[string]interface{}{
		"name": otherName, "description": "另一个策略", "enabled": true,
		"config": map[string]interface{}{"threadNum": 10},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("create other = %d: %s", w.Code, w.Body.String())
	}
	otherID := dataOf(w)["id"].(string)
	if w := do(http.MethodPost, "/api/policy/update", map[string]interface{}{
		"id": otherID, "name": updatedName,
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("改名冲突 = %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// ---------- 6) apply：对象基线 ----------
	w = do(http.MethodPost, "/api/policy/apply", map[string]interface{}{
		"policyId": policyID,
		"taskTemplate": map[string]interface{}{
			"templateId":     unknownTemplate,
			"plugins":        []string{hashB},
			"threadNum":      10,
			"rateLimit":      500,
			"timeoutSeconds": 60,
			"dicts":          map[string]interface{}{"dir": "dir-tpl", "brute": "brute-tpl"},
			"options":        map[string]interface{}{"a": "0", "b": "2"},
		},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", w.Code, w.Body.String())
	}
	applyData := dataOf(w)
	if applyData["taskTemplateSource"] != "object" {
		t.Fatalf("taskTemplateSource = %v, want object", applyData["taskTemplateSource"])
	}
	mergedCfg, _ := applyData["config"].(map[string]interface{})
	if mergedCfg["threadNum"].(float64) != 80 {
		t.Fatalf("apply threadNum = %v, want 80（策略覆盖）", mergedCfg["threadNum"])
	}
	if mergedCfg["rateLimit"].(float64) != 100 {
		t.Fatalf("apply rateLimit = %v, want 100（策略覆盖）", mergedCfg["rateLimit"])
	}
	if mergedCfg["timeoutSeconds"].(float64) != 120 {
		t.Fatalf("apply timeoutSeconds = %v, want 120（策略覆盖）", mergedCfg["timeoutSeconds"])
	}
	mergedPlugins, _ := mergedCfg["plugins"].([]interface{})
	if len(mergedPlugins) != 1 || mergedPlugins[0] != hashA {
		t.Fatalf("apply plugins = %v, want [%s]", mergedCfg["plugins"], hashA)
	}
	mergedDicts, _ := mergedCfg["dicts"].(map[string]interface{})
	if mergedDicts["dir"] != "dir-top" {
		t.Fatalf("apply dicts.dir = %v, want dir-top", mergedDicts["dir"])
	}
	if mergedDicts["brute"] != "brute-tpl" {
		t.Fatalf("apply dicts.brute = %v, want brute-tpl（模板默认保留）", mergedDicts["brute"])
	}
	mergedOpts, _ := mergedCfg["options"].(map[string]interface{})
	if mergedOpts["a"] != "1" || mergedOpts["b"] != "2" {
		t.Fatalf("apply options = %v, want {a:1,b:2}", mergedOpts)
	}

	warnings, _ := applyData["warnings"].([]interface{})
	warnCodes := make(map[string]bool, len(warnings))
	for _, item := range warnings {
		row, _ := item.(map[string]interface{})
		if code, ok := row["code"].(string); ok {
			warnCodes[code] = true
		}
	}
	for _, code := range []string{
		"template_override", "plugins_override", "thread_num_override",
		"rate_limit_override", "timeout_override", "dict_override", "option_override",
	} {
		if !warnCodes[code] {
			t.Fatalf("apply 缺 warning %s（实际 %v）", code, warnings)
		}
	}
	appliedFields, _ := applyData["appliedFields"].([]interface{})
	applied := make(map[string]bool, len(appliedFields))
	for _, f := range appliedFields {
		if s, ok := f.(string); ok {
			applied[s] = true
		}
	}
	for _, field := range []string{"threadNum", "plugins", "templateId", "dicts.dir", "options"} {
		if !applied[field] {
			t.Fatalf("apply appliedFields 缺 %s（实际 %v）", field, appliedFields)
		}
	}

	// ---------- 7) apply：模板 ID 基线（只读模板取 templateId + 插件并集） ----------
	w = do(http.MethodPost, "/api/policy/apply", map[string]interface{}{
		"policyId":     otherID,
		"taskTemplate": templateID,
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("apply template = %d: %s", w.Code, w.Body.String())
	}
	tplApply := dataOf(w)
	if tplApply["taskTemplateSource"] != "template" {
		t.Fatalf("taskTemplateSource = %v, want template", tplApply["taskTemplateSource"])
	}
	tplCfg, _ := tplApply["config"].(map[string]interface{})
	if tplCfg["templateId"] != templateID {
		t.Fatalf("apply templateId = %v, want %s", tplCfg["templateId"], templateID)
	}
	tplPlugins, _ := tplCfg["plugins"].([]interface{})
	if len(tplPlugins) != 2 || tplPlugins[0] != hashA || tplPlugins[1] != hashB {
		t.Fatalf("apply 模板基线 plugins = %v, want [%s %s]", tplCfg["plugins"], hashA, hashB)
	}
	if warnings, _ := tplApply["warnings"].([]interface{}); len(warnings) != 0 {
		t.Fatalf("模板基线 + 无冲突策略不应有 warning: %v", warnings)
	}

	// apply 参数错误
	if w := do(http.MethodPost, "/api/policy/apply", map[string]interface{}{
		"policyId": unknownTemplate, "taskTemplate": map[string]interface{}{},
	}, true); w.Code != http.StatusNotFound {
		t.Fatalf("apply 未知策略 = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/policy/apply", map[string]interface{}{
		"taskTemplate": map[string]interface{}{},
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("apply 缺 policyId = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/policy/apply", map[string]interface{}{
		"policyId": otherID, "taskTemplate": 123,
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("apply 非法 taskTemplate = %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// ---------- 8) test ----------
	w = do(http.MethodPost, "/api/policy/test", map[string]interface{}{
		"config": map[string]interface{}{"threadNum": 201, "rateLimit": 1001, "timeoutSeconds": 9},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("test 越界 = %d, want 200（校验端点用 valid=false 表达）: %s", w.Code, w.Body.String())
	}
	testData := dataOf(w)
	if valid, _ := testData["valid"].(bool); valid {
		t.Fatalf("test 越界 valid = true, want false")
	}
	issues, _ := testData["errors"].([]interface{})
	issueFields := make(map[string]bool, len(issues))
	for _, item := range issues {
		row, _ := item.(map[string]interface{})
		if field, ok := row["field"].(string); ok {
			issueFields[field] = true
		}
	}
	for _, field := range []string{"threadNum", "rateLimit", "timeoutSeconds"} {
		if !issueFields[field] {
			t.Fatalf("test 越界缺 field %s（实际 %v）", field, issues)
		}
	}

	// 插件不存在（按 hash/name 都查不到）
	w = do(http.MethodPost, "/api/policy/test", map[string]interface{}{
		"config": map[string]interface{}{"plugins": []string{"ghost-" + suffix}},
	}, true)
	testData = dataOf(w)
	if valid, _ := testData["valid"].(bool); valid {
		t.Fatalf("未知插件 valid = true, want false")
	}
	if !policyHasIssueCode(testData, "plugin_not_found") {
		t.Fatalf("未知插件缺 plugin_not_found: %v", testData["errors"])
	}

	// 模板 ID 形状非法
	w = do(http.MethodPost, "/api/policy/test", map[string]interface{}{
		"config": map[string]interface{}{"templateId": "not-hex"},
	}, true)
	if !policyHasIssueCode(dataOf(w), "template_invalid") {
		t.Fatalf("模板形状非法缺 template_invalid: %s", w.Body.String())
	}
	// 模板不存在（合法 ObjectID 但库中没有）
	w = do(http.MethodPost, "/api/policy/test", map[string]interface{}{
		"config": map[string]interface{}{"templateId": unknownTemplate},
	}, true)
	if !policyHasIssueCode(dataOf(w), "template_not_found") {
		t.Fatalf("未知模板缺 template_not_found: %s", w.Body.String())
	}

	// 合法参数：hash 或 name 引用插件都算命中
	for _, pluginRef := range []string{hashA, nameA} {
		w = do(http.MethodPost, "/api/policy/test", map[string]interface{}{
			"config": map[string]interface{}{
				"templateId":     templateID,
				"plugins":        []string{pluginRef},
				"threadNum":      1,
				"rateLimit":      0,
				"timeoutSeconds": 10,
				"dicts":          map[string]interface{}{"dir": "dir-top"},
				"options":        map[string]interface{}{"k": "v"},
			},
		}, true)
		if w.Code != http.StatusOK {
			t.Fatalf("test 合法 = %d: %s", w.Code, w.Body.String())
		}
		validData := dataOf(w)
		if valid, _ := validData["valid"].(bool); !valid {
			t.Fatalf("合法参数（plugin=%s）valid = false: %v", pluginRef, validData["errors"])
		}
	}

	// ---------- 9) delete ----------
	w = do(http.MethodPost, "/api/policy/delete", map[string]interface{}{"ids": []string{policyID}}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", w.Code, w.Body.String())
	}
	if deleted, _ := dataOf(w)["deleted"].(float64); deleted < 1 {
		t.Fatalf("delete deleted = %v, want >= 1", dataOf(w)["deleted"])
	}
	if w := do(http.MethodPost, "/api/policy/detail", map[string]interface{}{"id": policyID}, true); w.Code != http.StatusNotFound {
		t.Fatalf("删除后 detail = %d, want 404 (%s)", w.Code, w.Body.String())
	}

	// 名字释放后可复用（唯一索引没有残留）
	w = do(http.MethodPost, "/api/policy/create", map[string]interface{}{
		"name": updatedName, "config": map[string]interface{}{"threadNum": 20},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("删除后复用名字 = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("scan_policy").DeleteMany(t.Context(), bson.M{
			"name": bson.M{"$regex": suffix},
		})
	})
}

// policyHasIssueCode test 响应里是否包含指定 issue code
func policyHasIssueCode(data map[string]interface{}, code string) bool {
	issues, _ := data["errors"].([]interface{})
	for _, item := range issues {
		row, _ := item.(map[string]interface{})
		if row["code"] == code {
			return true
		}
	}
	return false
}
