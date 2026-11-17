//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	assetgroup "github.com/Autumn-27/ScopeSentry/internal/services/assets/assetgroup"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ICP 路由面走真实路由链（SetupRouter + JWT + Mongo + 假 ICP 服务）：
//   - 守卫：全部端点未带 token 一律 401；pageSize>200 一律 400；未知 taskId 一律 404；
//   - 主链路：tyc_check → create → restart（轮询）→ statistics → asset → log
//     → export CSV → sync（写入 asset_scope）→ backfill_details → 重跑去重 → delete。
//
// 假 ICP 服务通过系统配置 icp.provider_url 注入，全程不访问外部 API。

// icpFakeDomains 用例专属域名（带唯一后缀）：icp_result 的去重键是全局唯一的
// (domain,company,icpNo)，固定值会让「上一次运行的残留行」把本次 found 抵消成 0。
func icpFakeDomains(suffix string) []string {
	return []string{
		"a-route-" + suffix + ".example.com",
		"b-route-" + suffix + ".example.com",
		"c-route-" + suffix + ".example.com",
	}
}

// icpFakeProvider 假 ICP 服务：/icp/domain 固定返回 3 条，/icp/company 返回 2 条
func icpFakeProvider(t *testing.T, suffix string) *httptest.Server {
	t.Helper()
	domains := icpFakeDomains(suffix)
	company := "测试科技有限公司-" + suffix
	icpNo := "京ICP备" + suffix + "号"
	domainRecords := make([]map[string]string, 0, len(domains))
	for i, domain := range domains {
		domainRecords = append(domainRecords, map[string]string{
			"domain":   domain,
			"company":  company,
			"icpNo":    fmt.Sprintf("%s-%d", icpNo, i+1),
			"siteName": []string{"官网", "商城", "开放平台"}[i],
			"type":     "web",
		})
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/icp/domain":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"records": domainRecords})
		case "/icp/company":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"records": domainRecords[:2]},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// icpSetConfig 写入系统配置并在用例结束后还原（原来不存在就删掉）
func icpSetConfig(t *testing.T, name, value string) {
	t.Helper()
	ctx := context.Background()
	collection := mongodb.DB.Collection("config")

	var prev struct {
		Value interface{} `bson:"value"`
	}
	found := collection.FindOne(ctx, bson.M{"name": name}).Decode(&prev) == nil
	if found {
		t.Cleanup(func() {
			_, _ = collection.UpdateOne(context.Background(), bson.M{"name": name}, bson.M{"$set": bson.M{"value": prev.Value}})
		})
	} else {
		t.Cleanup(func() {
			_, _ = collection.DeleteMany(context.Background(), bson.M{"name": name})
		})
	}

	if _, err := collection.UpdateOne(ctx, bson.M{"name": name}, bson.M{"$set": bson.M{
		"name": name, "value": value, "type": "icp",
	}}, options.Update().SetUpsert(true)); err != nil {
		t.Fatalf("set config %s: %v", name, err)
	}
}

func TestIcpRoutesGuardsAndFlow(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	domains := icpFakeDomains(suffix)
	fakeServer := icpFakeProvider(t, suffix)
	defer fakeServer.Close()
	icpSetConfig(t, "icp.provider_url", fakeServer.URL)
	icpSetConfig(t, "icp.provider", "")

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

	// ---------- 1) 未认证一律 401 ----------
	guards := []struct{ method, path string }{
		{http.MethodPost, "/api/icp/task/list"},
		{http.MethodPost, "/api/icp/task/create"},
		{http.MethodPost, "/api/icp/task/stop"},
		{http.MethodPost, "/api/icp/task/restart"},
		{http.MethodPost, "/api/icp/task/resume"},
		{http.MethodPost, "/api/icp/task/delete"},
		{http.MethodPost, "/api/icp/task/log"},
		{http.MethodPost, "/api/icp/task/statistics"},
		{http.MethodPost, "/api/icp/task/overview_count"},
		{http.MethodPost, "/api/icp/tyc_check"},
		{http.MethodPost, "/api/icp/asset"},
		{http.MethodPost, "/api/icp/sync"},
		{http.MethodPost, "/api/icp/backfill_details"},
		{http.MethodGet, "/api/icp/export?taskId=0123456789abcdef01234567"},
	}
	for _, tc := range guards {
		w := do(tc.method, tc.path, nil, false)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s 未认证 = %d, want 401 (%s)", tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	// ---------- 2) pageSize 上限 200 ----------
	for _, path := range []string{"/api/icp/task/list", "/api/icp/asset"} {
		w := do(http.MethodPost, path, map[string]interface{}{"pageSize": 500}, true)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s pageSize=500 = %d, want 400 (%s)", path, w.Code, w.Body.String())
		}
	}
	if w := do(http.MethodPost, "/api/icp/task/list", map[string]interface{}{"pageSize": 200}, true); w.Code != http.StatusOK {
		t.Fatalf("task/list pageSize=200 = %d, want 200 (%s)", w.Code, w.Body.String())
	}

	// ---------- 3) provider 状态 ----------
	status := dataOf(do(http.MethodPost, "/api/icp/tyc_check", map[string]interface{}{}, true))
	if status["provider"] != "http" {
		t.Fatalf("tyc_check provider = %v, want http (%v)", status["provider"], status)
	}
	if available, _ := status["available"].(bool); !available {
		t.Fatalf("tyc_check available = %v, want true (%v)", status["available"], status)
	}
	if configured, _ := status["configured"].(bool); !configured {
		t.Fatalf("tyc_check configured = %v, want true (%v)", status["configured"], status)
	}

	// ---------- 4) 建任务 ----------
	name := "icp-route-" + suffix
	created := dataOf(do(http.MethodPost, "/api/icp/task/create", map[string]interface{}{
		"name":    name,
		"type":    "domain",
		"queries": []string{"route.example.com"},
	}, true))
	taskID, _ := created["id"].(string)
	if taskID == "" {
		t.Fatalf("create 响应缺少 id: %v", created)
	}
	if created["provider"] != "http" {
		t.Fatalf("create provider = %v, want http", created["provider"])
	}
	t.Cleanup(func() {
		do(http.MethodPost, "/api/icp/task/delete", map[string]interface{}{"ids": []string{taskID}}, true)
	})

	// 参数校验：非法 type / 空 queries / 未知 provider → 400
	if w := do(http.MethodPost, "/api/icp/task/create", map[string]interface{}{
		"type": "ip", "queries": []string{"x"},
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("非法 type = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/icp/task/create", map[string]interface{}{
		"type": "domain",
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("空 queries = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/icp/task/create", map[string]interface{}{
		"type": "domain", "queries": []string{"x"}, "provider": "tyc",
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("未知 provider = %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// ---------- 5) restart + 状态轮询 ----------
	if w := do(http.MethodPost, "/api/icp/task/restart", map[string]interface{}{"taskId": taskID}, true); w.Code != http.StatusOK {
		t.Fatalf("restart = %d: %s", w.Code, w.Body.String())
	}

	waitTask := func(want int) map[string]interface{} {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			data := dataOf(do(http.MethodPost, "/api/icp/task/list", map[string]interface{}{"keyword": name}, true))
			list, _ := data["list"].([]interface{})
			for _, raw := range list {
				item, _ := raw.(map[string]interface{})
				if id, _ := item["id"].(string); id == taskID {
					state, _ := item["state"].(float64)
					if int(state) == want {
						return item
					}
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("任务 %s 在 10s 内没到状态 %d", taskID, want)
		return nil
	}
	doneTask := waitTask(3)
	statsMap, _ := doneTask["stats"].(map[string]interface{})
	for key, want := range map[string]float64{"total": 3, "done": 3, "found": 3, "failed": 0} {
		if got, _ := statsMap[key].(float64); got != want {
			t.Fatalf("stats.%s = %v, want %v (%v)", key, statsMap[key], want, statsMap)
		}
	}
	if lastError, _ := doneTask["lastError"].(string); lastError != "" {
		t.Fatalf("完成后 lastError = %q, want 空", lastError)
	}

	// ---------- 6) statistics ----------
	stats := dataOf(do(http.MethodPost, "/api/icp/task/statistics", map[string]interface{}{"taskId": taskID}, true))
	for key, want := range map[string]float64{"total": 3, "done": 3, "found": 3, "failed": 0, "results": 3} {
		if got, _ := stats[key].(float64); got != want {
			t.Fatalf("statistics.%s = %v, want %v (%v)", key, stats[key], want, stats)
		}
	}
	if state, _ := stats["state"].(float64); int(state) != 3 {
		t.Fatalf("statistics.state = %v, want 3", stats["state"])
	}

	// ---------- 7) asset 分页 / 404 ----------
	assets := dataOf(do(http.MethodPost, "/api/icp/asset", map[string]interface{}{
		"taskId": taskID, "pageIndex": 1, "pageSize": 2,
	}, true))
	if total, _ := assets["total"].(float64); total != 3 {
		t.Fatalf("asset total = %v, want 3 (%v)", assets["total"], assets)
	}
	if list, _ := assets["list"].([]interface{}); len(list) != 2 {
		t.Fatalf("asset 第一页 = %d 条, want 2", len(list))
	}
	unknownID := "0123456789abcdef01234567"
	if w := do(http.MethodPost, "/api/icp/asset", map[string]interface{}{"taskId": unknownID}, true); w.Code != http.StatusNotFound {
		t.Fatalf("未知 taskId asset = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/icp/asset", map[string]interface{}{"taskId": taskID, "pageSize": 500}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("asset pageSize=500 = %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// ---------- 8) log ----------
	logData := dataOf(do(http.MethodPost, "/api/icp/task/log", map[string]interface{}{"taskId": taskID}, true))
	if total, _ := logData["total"].(float64); total < 1 {
		t.Fatalf("log total = %v, want >=1 (%v)", logData["total"], logData)
	}
	if total, _ := logData["total"].(float64); total > 200 {
		t.Fatalf("log total = %v, want <=200", total)
	}
	if lines, _ := logData["lines"].([]interface{}); len(lines) == 0 {
		t.Fatalf("log lines 为空")
	}
	if w := do(http.MethodPost, "/api/icp/task/log", map[string]interface{}{"taskId": unknownID}, true); w.Code != http.StatusNotFound {
		t.Fatalf("未知 taskId log = %d, want 404 (%s)", w.Code, w.Body.String())
	}

	// ---------- 9) overview_count ----------
	overview := dataOf(do(http.MethodPost, "/api/icp/task/overview_count", map[string]interface{}{}, true))
	if done, _ := overview["done"].(float64); done < 1 {
		t.Fatalf("overview.done = %v, want >=1 (%v)", overview["done"], overview)
	}

	// ---------- 10) CSV 导出 ----------
	w := do(http.MethodGet, "/api/icp/export?taskId="+taskID, nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("export Content-Type = %q, want text/csv", ct)
	}
	csvBody := bytes.TrimPrefix(w.Body.Bytes(), []byte{0xEF, 0xBB, 0xBF})
	rows, err := csv.NewReader(bytes.NewReader(csvBody)).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v (%s)", err, w.Body.String())
	}
	if len(rows) != 4 {
		t.Fatalf("CSV 行数 = %d, want 4（表头 + 3 条）: %v", len(rows), rows)
	}
	if rows[0][1] != "domain" || rows[0][3] != "icpNo" {
		t.Fatalf("CSV 表头不符: %v", rows[0])
	}
	if w := do(http.MethodGet, "/api/icp/export?taskId="+unknownID, nil, true); w.Code != http.StatusNotFound {
		t.Fatalf("未知 taskId export = %d, want 404 (%s)", w.Code, w.Body.String())
	}

	// ---------- 11) sync → asset_scope ----------
	syncData := dataOf(do(http.MethodPost, "/api/icp/sync", map[string]interface{}{"taskId": taskID}, true))
	if added, _ := syncData["added"].(float64); added != 3 {
		t.Fatalf("sync added = %v, want 3 (%v)", syncData["added"], syncData)
	}
	scopeID, _ := syncData["scopeId"].(string)
	if scopeID == "" {
		t.Fatalf("sync 响应缺少 scopeId: %v", syncData)
	}
	t.Cleanup(func() {
		if err := assetgroup.NewService().DeleteScopes(context.Background(), []string{scopeID}); err != nil {
			t.Logf("cleanup scope %s: %v", scopeID, err)
		}
	})

	var scope struct {
		Name        string   `bson:"name"`
		DomainArray []string `bson:"domain_array"`
		ScopeArray  []string `bson:"scope_array"`
	}
	oidErr := mongodb.DB.Collection("asset_scope").FindOne(context.Background(), bson.M{"name": name}).Decode(&scope)
	if oidErr != nil {
		t.Fatalf("load synced scope: %v", oidErr)
	}
	if len(scope.DomainArray) != 3 {
		t.Fatalf("domain_array = %v, want 3 条", scope.DomainArray)
	}
	seen := map[string]int{}
	for _, domain := range scope.DomainArray {
		seen[domain]++
	}
	for _, domain := range domains {
		if seen[domain] != 1 {
			t.Fatalf("domain_array 里 %s 出现 %d 次: %v", domain, seen[domain], scope.DomainArray)
		}
	}

	// 幂等：再 sync 一次不新增
	secondSync := dataOf(do(http.MethodPost, "/api/icp/sync", map[string]interface{}{"taskId": taskID}, true))
	if added, _ := secondSync["added"].(float64); added != 0 {
		t.Fatalf("二次 sync added = %v, want 0 (%v)", secondSync["added"], secondSync)
	}

	// ---------- 12) backfill_details ----------
	backfill := dataOf(do(http.MethodPost, "/api/icp/backfill_details", map[string]interface{}{"taskId": taskID}, true))
	if scanned, _ := backfill["scanned"].(float64); scanned < 0 {
		t.Fatalf("backfill scanned = %v, want >=0 (%v)", backfill["scanned"], backfill)
	}
	if w := do(http.MethodPost, "/api/icp/backfill_details", map[string]interface{}{"taskId": unknownID}, true); w.Code != http.StatusNotFound {
		t.Fatalf("未知 taskId backfill = %d, want 404 (%s)", w.Code, w.Body.String())
	}

	// ---------- 13) 重跑去重 ----------
	if w := do(http.MethodPost, "/api/icp/task/restart", map[string]interface{}{"taskId": taskID}, true); w.Code != http.StatusOK {
		t.Fatalf("二次 restart = %d: %s", w.Code, w.Body.String())
	}
	// 等状态回到 3 后看 found=0（restart 会先把 state 置 1，所以要等到 3）
	deadline := time.Now().Add(10 * time.Second)
	dedupOK := false
	for time.Now().Before(deadline) {
		stats = dataOf(do(http.MethodPost, "/api/icp/task/statistics", map[string]interface{}{"taskId": taskID}, true))
		state, _ := stats["state"].(float64)
		if int(state) == 3 {
			found, _ := stats["found"].(float64)
			if found != 0 {
				t.Fatalf("二次运行 found = %v, want 0（去重失效）", found)
			}
			dedupOK = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !dedupOK {
		t.Fatalf("二次运行没有在 10s 内完成")
	}

	// ---------- 14) stop / resume 语义 ----------
	if w := do(http.MethodPost, "/api/icp/task/resume", map[string]interface{}{"taskId": taskID}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("非暂停态 resume = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/icp/task/stop", map[string]interface{}{"ids": []string{taskID}}, true); w.Code != http.StatusOK {
		// 已完成任务不允许被改成暂停：SetState 只改 waiting/running/paused，这里是 no-op（updated=0）
		t.Fatalf("stop 已完成任务 = %d: %s", w.Code, w.Body.String())
	}

	// ---------- 15) delete ----------
	if w := do(http.MethodPost, "/api/icp/task/delete", map[string]interface{}{"ids": []string{taskID}}, true); w.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/icp/asset", map[string]interface{}{"taskId": taskID}, true); w.Code != http.StatusNotFound {
		t.Fatalf("删除后 asset = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	// 删除必须连带清掉结果，否则去重键会在下一次运行里把 found 抵消成 0
	left, err := mongodb.DB.Collection("icp_result").CountDocuments(context.Background(), bson.M{"taskId": taskID})
	if err != nil {
		t.Fatalf("count leftover results: %v", err)
	}
	if left != 0 {
		t.Fatalf("删除任务后仍有 %d 条 icp_result 残留", left)
	}
}
