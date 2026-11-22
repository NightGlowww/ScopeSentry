//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"go.mongodb.org/mongo-driver/bson"
)

// 仪表盘 / 控制台的 HTTP 层：鉴权、400 边界、响应结构、聚合结果。
// 走真实路由 + 中间件 + Mongo/Redis，等价于前端把 Dashboard 页打开一次。
func TestDashboardAndConsoleRoutes(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-dash-routes-" + suffix

	// 日期用与 dashboard 相同的口径（配置时区）算，避免跨零点跑测试时日期漂
	now := helper.GetNowTime()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	stamp := func(day time.Time, hour, minute int) string {
		return day.Format("2006-01-02") + fmt.Sprintf(" %02d:%02d:00", hour, minute)
	}

	assetColl := mongodb.DB.Collection("asset")
	vulnColl := mongodb.DB.Collection("vulnerability")
	taskColl := mongodb.DB.Collection("task")
	subColl := mongodb.DB.Collection("subdomain")
	urlColl := mongodb.DB.Collection("UrlScan")
	sensColl := mongodb.DB.Collection("SensitiveResult")
	t.Cleanup(func() {
		filter := bson.M{"project": project}
		_, _ = assetColl.DeleteMany(context.Background(), filter)
		_, _ = vulnColl.DeleteMany(context.Background(), filter)
		_, _ = taskColl.DeleteMany(context.Background(), filter)
		_, _ = subColl.DeleteMany(context.Background(), filter)
		_, _ = urlColl.DeleteMany(context.Background(), filter)
		_, _ = sensColl.DeleteMany(context.Background(), filter)
	})

	host := func(tag string) string { return fmt.Sprintf("dashr-%s-%s.example.com", suffix, tag) }
	if _, err := assetColl.InsertMany(context.Background(), []interface{}{
		bson.M{"host": host("a1"), "port": "443", "project": project, "time": stamp(today, 9, 0)},
		bson.M{"host": host("a2"), "port": "8443", "project": project, "time": stamp(yesterday, 9, 0)},
	}); err != nil {
		t.Fatalf("seed assets: %v", err)
	}
	if _, err := vulnColl.InsertOne(context.Background(), bson.M{
		"project": project, "time": stamp(today, 10, 0), "vulnname": "xss",
		"level": "high", "hash": project + "-v1", "status": 1,
	}); err != nil {
		t.Fatalf("seed vulnerability: %v", err)
	}
	if _, err := taskColl.InsertMany(context.Background(), []interface{}{
		bson.M{"name": "dashr-task-1-" + suffix, "project": []string{project}, "status": 3, "endTime": stamp(today, 8, 0)},
		bson.M{"name": "dashr-task-2-" + suffix, "project": []string{project}, "status": 1, "endTime": ""},
	}); err != nil {
		t.Fatalf("seed tasks: %v", err)
	}
	if _, err := subColl.InsertOne(context.Background(), bson.M{"host": "sub-" + suffix + ".example.com", "project": project, "time": stamp(today, 8, 30)}); err != nil {
		t.Fatalf("seed subdomain: %v", err)
	}
	if _, err := urlColl.InsertOne(context.Background(), bson.M{"url": "https://dashr-" + suffix + ".example.com/a", "project": project, "time": stamp(today, 8, 40)}); err != nil {
		t.Fatalf("seed url: %v", err)
	}
	if _, err := sensColl.InsertOne(context.Background(), bson.M{
		"project": project, "time": stamp(today, 8, 50), "sid": "apikey", "md5": project + "-m1",
	}); err != nil {
		t.Fatalf("seed sensitive: %v", err)
	}

	post := func(path string, body interface{}, withToken bool) *httptest.ResponseRecorder {
		t.Helper()
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
	postNoBody := func(path string, withToken bool) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if withToken {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, req)
		return w
	}
	get := func(path string, withToken bool) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if withToken {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, req)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		var payload map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %q: %v (%s)", w.Body.String(), err, w.Body.String())
		}
		return payload
	}
	dataOf := func(w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		data, _ := decode(w)["data"].(map[string]interface{})
		if data == nil {
			t.Fatalf("response missing data: %s", w.Body.String())
		}
		return data
	}
	numOf := func(data map[string]interface{}, key string) int64 {
		t.Helper()
		raw, ok := data[key].(float64)
		if !ok {
			t.Fatalf("data.%s is not a number: %v", key, data[key])
		}
		return int64(raw)
	}

	// 1) 未认证：6 个端点全部 401（WithAuth 真的挂上了）
	for _, tc := range []struct {
		path string
		body map[string]interface{}
	}{
		{"/api/dashboard/stats", map[string]interface{}{}},
		{"/api/dashboard/trend", map[string]interface{}{"days": 7}},
		{"/api/dashboard/logs", map[string]interface{}{}},
		{"/api/dashboard/widgets", map[string]interface{}{}},
	} {
		if w := post(tc.path, tc.body, false); w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated POST %s = %d, want 401: %s", tc.path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/dashboard/sysinfo", "/api/console/info"} {
		if w := get(path, false); w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated GET %s = %d, want 401: %s", path, w.Code, w.Body.String())
		}
	}

	// 2) 参数边界：pageSize=500 / days=5 都必须 400
	if w := post("/api/dashboard/logs", map[string]interface{}{"pageSize": 500}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("logs pageSize=500 = %d, want 400: %s", w.Code, w.Body.String())
	}
	if w := post("/api/dashboard/trend", map[string]interface{}{"days": 5}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("trend days=5 = %d, want 400: %s", w.Code, w.Body.String())
	}
	if w := post("/api/dashboard/trend", map[string]interface{}{}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("trend without days = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 2.5) 空 body 允许：卡片接口不带 body 等价于默认值（全库 / 7 天 / 第一页 50 条）
	if w := postNoBody("/api/dashboard/stats", true); w.Code != http.StatusOK {
		t.Fatalf("stats without body = %d, want 200: %s", w.Code, w.Body.String())
	}
	if w := postNoBody("/api/dashboard/widgets", true); w.Code != http.StatusOK {
		t.Fatalf("widgets without body = %d, want 200: %s", w.Code, w.Body.String())
	}
	wEmpty := postNoBody("/api/dashboard/logs", true)
	if wEmpty.Code != http.StatusOK {
		t.Fatalf("logs without body = %d, want 200: %s", wEmpty.Code, wEmpty.Body.String())
	}
	// 默认每页 50 条（service 里的 defaultLogPageSize）
	if pageSize := numOf(dataOf(wEmpty), "pageSize"); pageSize != 50 {
		t.Fatalf("logs default pageSize = %d, want 50", pageSize)
	}

	// 3) stats：7 类数字按 project 收窄
	w := post("/api/dashboard/stats", map[string]interface{}{"project": project}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("stats = %d: %s", w.Code, w.Body.String())
	}
	stats := dataOf(w)
	if got := numOf(stats, "asset"); got != 2 {
		t.Fatalf("stats.asset = %d, want 2", got)
	}
	if got := numOf(stats, "subdomain"); got != 1 {
		t.Fatalf("stats.subdomain = %d, want 1", got)
	}
	if got := numOf(stats, "url"); got != 1 {
		t.Fatalf("stats.url = %d, want 1", got)
	}
	if got := numOf(stats, "vulnerability"); got != 1 {
		t.Fatalf("stats.vulnerability = %d, want 1 raw candidate", got)
	}
	if got := numOf(stats, "vulnPending"); got != 1 {
		t.Fatalf("stats.vulnPending = %d, want 1", got)
	}
	if got := numOf(stats, "vulnConfirmed"); got != 0 {
		t.Fatalf("stats.vulnConfirmed = %d, want 0 for a pending result", got)
	}
	if got := numOf(stats, "sensitive"); got != 1 {
		t.Fatalf("stats.sensitive = %d, want 1", got)
	}
	if got := numOf(stats, "task"); got != 2 {
		t.Fatalf("stats.task = %d, want 2", got)
	}
	if got := numOf(stats, "taskFinished"); got != 1 {
		t.Fatalf("stats.taskFinished = %d, want 1", got)
	}
	if got := numOf(stats, "taskRunning"); got != 1 {
		t.Fatalf("stats.taskRunning = %d, want 1", got)
	}
	if _, ok := stats["node"]; !ok {
		t.Fatalf("stats.node missing: %v", stats)
	}

	// 不带 project：走集合元数据（EstimatedDocumentCount）的那条路径，
	// 数字只能做下界断言（库里还有其它用例的数据）
	globalStats := dataOf(post("/api/dashboard/stats", map[string]interface{}{}, true))
	if got := numOf(globalStats, "asset"); got < 2 {
		t.Fatalf("global stats.asset = %d, want >= 2", got)
	}
	if got := numOf(globalStats, "task"); got < 2 {
		t.Fatalf("global stats.task = %d, want >= 2", got)
	}

	// 4) trend：7 个点、今天 1 个资产 / 1 个漏洞 / 1 个完成任务、昨天 1 个资产
	w = post("/api/dashboard/trend", map[string]interface{}{"days": 7, "project": project}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("trend = %d: %s", w.Code, w.Body.String())
	}
	trend := dataOf(w)
	points, _ := trend["points"].([]interface{})
	if len(points) != 7 {
		t.Fatalf("trend points = %d, want 7 (%s)", len(points), w.Body.String())
	}
	byDate := map[string]map[string]interface{}{}
	for _, raw := range points {
		point, _ := raw.(map[string]interface{})
		date, _ := point["date"].(string)
		byDate[date] = point
	}
	todayKey := today.Format("2006-01-02")
	yesterdayKey := yesterday.Format("2006-01-02")
	todayPoint, ok := byDate[todayKey]
	if !ok {
		t.Fatalf("trend missing today %s: %s", todayKey, w.Body.String())
	}
	if got := numOf(todayPoint, "asset"); got != 1 {
		t.Fatalf("trend[today].asset = %d, want 1", got)
	}
	if got := numOf(todayPoint, "vulnerability"); got != 1 {
		t.Fatalf("trend[today].vulnerability = %d, want 1", got)
	}
	if got := numOf(todayPoint, "task"); got != 1 {
		t.Fatalf("trend[today].task = %d, want 1", got)
	}
	if got := numOf(byDate[yesterdayKey], "asset"); got != 1 {
		t.Fatalf("trend[yesterday].asset = %d, want 1", got)
	}
	totals, _ := trend["totals"].(map[string]interface{})
	if got := numOf(totals, "asset"); got != 2 {
		t.Fatalf("trend.totals.asset = %d, want 2", got)
	}

	// 列式视图与 points 对齐（ARL-Next 的 days/assets/vulns 同形状）
	labels, _ := trend["labels"].([]interface{})
	assets, _ := trend["assets"].([]interface{})
	vulns, _ := trend["vulns"].([]interface{})
	tasks, _ := trend["tasks"].([]interface{})
	if len(labels) != 7 || len(assets) != 7 || len(vulns) != 7 || len(tasks) != 7 {
		t.Fatalf("trend series lengths = %d/%d/%d/%d, want 7: %s",
			len(labels), len(assets), len(vulns), len(tasks), w.Body.String())
	}
	if label, _ := labels[6].(string); label != todayKey {
		t.Fatalf("trend.labels[6] = %q, want today %q", label, todayKey)
	}
	if got, _ := assets[6].(float64); int64(got) != 1 {
		t.Fatalf("trend.assets[today] = %v, want 1", assets[6])
	}
	if tz, _ := trend["timezone"].(string); tz == "" {
		t.Fatalf("trend.timezone is empty: %s", w.Body.String())
	}

	// 5) logs：memory 源、关键字/级别过滤、分页顺序（新的在前）
	marker := "dash-logs-" + suffix
	logger.Info(marker + "-info-1")
	logger.Info(marker + "-info-2")
	logger.Error(marker + "-error-1")

	w = post("/api/dashboard/logs", map[string]interface{}{"keyword": marker, "pageIndex": 1, "pageSize": 2}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("logs = %d: %s", w.Code, w.Body.String())
	}
	logs := dataOf(w)
	if source, _ := logs["source"].(string); source != "memory" {
		t.Fatalf("logs.source = %q, want memory", source)
	}
	if total := numOf(logs, "total"); total < 3 {
		t.Fatalf("logs.total = %d, want >= 3", total)
	}
	list, _ := logs["list"].([]interface{})
	if len(list) != 2 {
		t.Fatalf("logs page1 size = %d, want 2", len(list))
	}
	first, _ := list[0].(map[string]interface{})
	if msg, _ := first["message"].(string); !strings.Contains(msg, "-error-1") {
		t.Fatalf("logs page1[0] = %q, want newest (error) first", msg)
	}

	// 第 2 页应有剩下的一条 info
	w = post("/api/dashboard/logs", map[string]interface{}{"keyword": marker, "pageIndex": 2, "pageSize": 2}, true)
	list2, _ := dataOf(w)["list"].([]interface{})
	if len(list2) != 1 {
		t.Fatalf("logs page2 size = %d, want 1: %s", len(list2), w.Body.String())
	}
	second, _ := list2[0].(map[string]interface{})
	if msg, _ := second["message"].(string); !strings.Contains(msg, "-info-1") {
		t.Fatalf("logs page2[0] = %q, want the oldest info entry", msg)
	}

	// 级别过滤：只剩 error
	w = post("/api/dashboard/logs", map[string]interface{}{"keyword": marker, "level": "error"}, true)
	filtered := dataOf(w)
	if total := numOf(filtered, "total"); total != 1 {
		t.Fatalf("logs(level=error).total = %d, want 1: %s", total, w.Body.String())
	}

	// 6) sysinfo：Linux 必须有 /proc 数据，其它平台必须说明降级原因
	w = get("/api/dashboard/sysinfo", true)
	if w.Code != http.StatusOK {
		t.Fatalf("sysinfo = %d: %s", w.Code, w.Body.String())
	}
	sysinfoData := dataOf(w)
	runtimeInfo, _ := sysinfoData["runtime"].(map[string]interface{})
	if goVersion, _ := runtimeInfo["goVersion"].(string); goVersion != runtime.Version() {
		t.Fatalf("sysinfo.runtime.goVersion = %q, want %q", goVersion, runtime.Version())
	}
	if goroutines, _ := runtimeInfo["goroutines"].(float64); goroutines <= 0 {
		t.Fatalf("sysinfo.runtime.goroutines = %v, want > 0", runtimeInfo["goroutines"])
	}
	available, _ := sysinfoData["available"].(bool)
	if runtime.GOOS == "linux" {
		if !available {
			t.Fatalf("linux sysinfo should be available: %s", w.Body.String())
		}
		memory, _ := sysinfoData["memory"].(map[string]interface{})
		if total, _ := memory["total"].(float64); total <= 0 {
			t.Fatalf("sysinfo.memory.total = %v, want > 0", memory["total"])
		}
	} else {
		// macOS 本地：/proc 不存在，允许降级，但必须带 reason（不算失败）
		t.Logf("sysinfo on %s is degraded as expected: available=%v reason=%v",
			runtime.GOOS, available, sysinfoData["reason"])
		if available {
			t.Fatalf("non-linux sysinfo should be marked unavailable")
		}
		if reason, _ := sysinfoData["reason"].(string); reason == "" {
			t.Fatalf("degraded sysinfo must explain why: %s", w.Body.String())
		}
	}

	// 7) widgets：一次拿到 stats+trend+sysinfo；includeSysInfo=false 时不返回 sysinfo
	w = post("/api/dashboard/widgets", map[string]interface{}{"days": 7, "project": project}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("widgets = %d: %s", w.Code, w.Body.String())
	}
	widgets := dataOf(w)
	wStats, _ := widgets["stats"].(map[string]interface{})
	if got := numOf(wStats, "asset"); got != 2 {
		t.Fatalf("widgets.stats.asset = %d, want 2", got)
	}
	wTrend, _ := widgets["trend"].(map[string]interface{})
	if wPoints, _ := wTrend["points"].([]interface{}); len(wPoints) != 7 {
		t.Fatalf("widgets.trend.points = %d, want 7", len(wPoints))
	}
	if widgets["sysInfo"] == nil {
		t.Fatalf("widgets.sysInfo missing by default: %s", w.Body.String())
	}

	w = post("/api/dashboard/widgets", map[string]interface{}{"project": project, "includeSysInfo": false}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("widgets(no sysinfo) = %d: %s", w.Code, w.Body.String())
	}
	if _, ok := dataOf(w)["sysInfo"]; ok {
		t.Fatalf("widgets.sysInfo should be omitted when includeSysInfo=false: %s", w.Body.String())
	}

	// 8) console/info：版本 / 启动时间 / 节点 / 任务 / 依赖状态
	w = get("/api/console/info", true)
	if w.Code != http.StatusOK {
		t.Fatalf("console/info = %d: %s", w.Code, w.Body.String())
	}
	consoleInfo := dataOf(w)
	if version, _ := consoleInfo["version"].(string); version == "" {
		t.Fatalf("console.version is empty: %s", w.Body.String())
	}
	if startTime, _ := consoleInfo["startTime"].(string); startTime == "" {
		t.Fatalf("console.startTime is empty: %s", w.Body.String())
	}
	if _, ok := consoleInfo["uptimeSeconds"]; !ok {
		t.Fatalf("console.uptimeSeconds missing: %s", w.Body.String())
	}
	if _, ok := consoleInfo["nodeCount"]; !ok {
		t.Fatalf("console.nodeCount missing: %s", w.Body.String())
	}
	if taskCount := numOf(consoleInfo, "taskCount"); taskCount < 2 {
		t.Fatalf("console.taskCount = %d, want >= 2", taskCount)
	}
	mongoStatus, _ := consoleInfo["mongo"].(map[string]interface{})
	if connected, _ := mongoStatus["connected"].(bool); !connected {
		t.Fatalf("console.mongo.connected = false: %s", w.Body.String())
	}
	redisStatus, _ := consoleInfo["redis"].(map[string]interface{})
	if connected, _ := redisStatus["connected"].(bool); !connected {
		t.Fatalf("console.redis.connected = false: %s", w.Body.String())
	}
}
