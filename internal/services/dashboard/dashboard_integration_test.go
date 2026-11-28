//go:build integration

package dashboard

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	dashboardrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/dashboard"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 趋势/统计要真连 Mongo 验：聚合管道、按天分组、project 过滤都是 Mongo 语义，
// mock 掉就等于什么都没测。

func dashboardTestLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}
	return loc
}

// seedProject 建一条真实项目记录并返回它的 ObjectID 十六进制串。
//
// 两个必须点：
//  1. 项目必须真实存在 —— resolveProject 先按 ObjectID、再按名字解析，都没有就 404；
//  2. 资产/任务里的 project 字段存的是**项目 ObjectID**，不是名字。用名字塞 fixture，
//     查询按 ID 过滤时一条都命中不了，表现成「趋势全是 0」——那是 fixture 错，不是聚合错。
func seedProject(t *testing.T, name string) string {
	t.Helper()
	coll := mongodb.DB.Collection("project")
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"name": name})
	})
	res, err := coll.InsertOne(context.Background(), bson.M{"name": name, "tag": "it"})
	if err != nil {
		t.Fatalf("seed project %s: %v", name, err)
	}
	id, _ := res.InsertedID.(primitive.ObjectID)
	return id.Hex()
}

func dayString(day time.Time, hour, minute int) string {
	return day.Format("2006-01-02") + fmt.Sprintf(" %02d:%02d:00", hour, minute)
}

// TestTrendAggregationWithProject 按天聚合 + 补零 + project 过滤
func TestTrendAggregationWithProject(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-dashboard-trend-" + suffix
	otherProject := "proj-dashboard-trend-other-" + suffix

	projectID := seedProject(t, project)
	otherProjectID := seedProject(t, otherProject)

	assetColl := mongodb.DB.Collection("asset")
	vulnColl := mongodb.DB.Collection("vulnerability")
	taskColl := mongodb.DB.Collection("task")
	t.Cleanup(func() {
		_, _ = assetColl.DeleteMany(context.Background(), bson.M{"project": bson.M{"$in": bson.A{project, otherProject}}})
		_, _ = vulnColl.DeleteMany(context.Background(), bson.M{"project": bson.M{"$in": bson.A{project, otherProject}}})
		_, _ = taskColl.DeleteMany(context.Background(), bson.M{"project": projectID})
	})

	loc := dashboardTestLocation(t)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterday := today.AddDate(0, 0, -1)
	twoDaysAgo := today.AddDate(0, 0, -2)
	threeDaysAgo := today.AddDate(0, 0, -3)

	host := func(tag string) string { return fmt.Sprintf("dash-%s-%s.example.com", suffix, tag) }

	// 资产：今天 2、昨天 1、3 天前 1；另一个项目 1 条（必须被 project 过滤掉）
	assets := []bson.M{
		{"host": host("a1"), "port": "443", "project": projectID, "time": dayString(today, 9, 0)},
		{"host": host("a2"), "port": "8443", "project": projectID, "time": dayString(today, 10, 30)},
		{"host": host("a3"), "port": "80", "project": projectID, "time": dayString(yesterday, 11, 0)},
		{"host": host("a4"), "port": "8080", "project": projectID, "time": dayString(threeDaysAgo, 12, 0)},
		{"host": host("a5"), "port": "443", "project": otherProjectID, "time": dayString(today, 13, 0)},
	}
	if _, err := assetColl.InsertMany(context.Background(), toInterfaces(assets)); err != nil {
		t.Fatalf("seed assets: %v", err)
	}

	// 漏洞：今天 1、2 天前 2
	vulns := []bson.M{
		{"project": projectID, "time": dayString(today, 8, 0), "vulnname": "xss", "level": "high", "hash": project + "-v1"},
		{"project": projectID, "time": dayString(twoDaysAgo, 9, 0), "vulnname": "sqli", "level": "critical", "hash": project + "-v2"},
		{"project": projectID, "time": dayString(twoDaysAgo, 15, 0), "vulnname": "rce", "level": "critical", "hash": project + "-v3"},
	}
	if _, err := vulnColl.InsertMany(context.Background(), toInterfaces(vulns)); err != nil {
		t.Fatalf("seed vulnerabilities: %v", err)
	}

	// 任务：今天完成 1、昨天完成 1；运行中的任务不算「完成」
	tasks := []bson.M{
		{"name": "dash-task-1-" + suffix, "project": []string{projectID}, "status": 3, "endTime": dayString(today, 7, 10), "creatTime": dayString(threeDaysAgo, 7, 0)},
		{"name": "dash-task-2-" + suffix, "project": []string{projectID}, "status": 3, "endTime": dayString(yesterday, 18, 20), "creatTime": dayString(twoDaysAgo, 18, 0)},
		{"name": "dash-task-3-" + suffix, "project": []string{projectID}, "status": 1, "endTime": "", "creatTime": dayString(today, 6, 0)},
	}
	if _, err := taskColl.InsertMany(context.Background(), toInterfaces(tasks)); err != nil {
		t.Fatalf("seed tasks: %v", err)
	}

	svc := NewService()
	res, err := svc.Trend(context.Background(), models.DashboardTrendRequest{Days: 7, Project: project})
	if err != nil {
		t.Fatalf("Trend: %v", err)
	}

	if res.Days != 7 || len(res.Points) != 7 {
		t.Fatalf("trend days=%d points=%d, want 7/7", res.Days, len(res.Points))
	}
	if res.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q, want Asia/Shanghai", res.Timezone)
	}

	byDate := make(map[string]models.DashboardTrendPoint, len(res.Points))
	for i, point := range res.Points {
		if i > 0 && point.Date <= res.Points[i-1].Date {
			t.Fatalf("points not ascending: %s after %s", point.Date, res.Points[i-1].Date)
		}
		byDate[point.Date] = point
	}

	cases := []struct {
		day           time.Time
		wantAsset     int64
		wantVuln      int64
		wantCompleted int64
	}{
		{today, 2, 1, 1},
		{yesterday, 1, 0, 1},
		{twoDaysAgo, 0, 2, 0}, // 补零：这天没有资产，但有 2 个漏洞
		{threeDaysAgo, 1, 0, 0},
	}
	for _, c := range cases {
		key := c.day.Format("2006-01-02")
		point, ok := byDate[key]
		if !ok {
			t.Fatalf("%s missing from trend points", key)
		}
		if point.Asset != c.wantAsset || point.Vulnerability != c.wantVuln || point.Task != c.wantCompleted {
			t.Fatalf("%s = {asset:%d vuln:%d task:%d}, want {asset:%d vuln:%d task:%d}",
				key, point.Asset, point.Vulnerability, point.Task,
				c.wantAsset, c.wantVuln, c.wantCompleted)
		}
	}

	// 窗口内合计
	if res.Totals.Asset != 4 || res.Totals.Vulnerability != 3 || res.Totals.Task != 2 {
		t.Fatalf("totals = %+v, want asset=4 vuln=3 task=2", res.Totals)
	}

	// 30 天窗口同样必须包含上面这些天，且长度是 30
	wide, err := svc.Trend(context.Background(), models.DashboardTrendRequest{Days: 30, Project: project})
	if err != nil {
		t.Fatalf("Trend(30): %v", err)
	}
	if len(wide.Points) != 30 {
		t.Fatalf("30d points = %d, want 30", len(wide.Points))
	}
	if wide.Totals.Asset != 4 {
		t.Fatalf("30d totals.asset = %d, want 4", wide.Totals.Asset)
	}
}

// TestStatsCountsWithProject 总量统计：project 过滤 + 任务状态拆分
func TestStatsCountsWithProject(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-dashboard-stats-" + suffix
	projectID := seedProject(t, project)

	now := time.Now().Format("2006-01-02 15:04:05")
	colls := map[string][]bson.M{
		"asset": {
			{"host": "stats-" + suffix + "-1.example.com", "port": "443", "project": projectID, "time": now},
			{"host": "stats-" + suffix + "-2.example.com", "port": "443", "project": projectID, "time": now},
		},
		"subdomain": {
			{"host": "sub-" + suffix + ".example.com", "project": projectID, "time": now},
		},
		"UrlScan": {
			{"url": "https://stats-" + suffix + ".example.com/a", "project": projectID, "time": now},
			{"url": "https://stats-" + suffix + ".example.com/b", "project": projectID, "time": now},
			{"url": "https://stats-" + suffix + ".example.com/c", "project": projectID, "time": now},
		},
		"vulnerability": {
			{"project": projectID, "time": now, "vulnname": "xss", "hash": project + "-h1"},
		},
		"SensitiveResult": {
			{"project": projectID, "time": now, "sid": "apikey", "md5": project + "-m1"},
			{"project": projectID, "time": now, "sid": "password", "md5": project + "-m2"},
		},
		"task": {
			{"name": "stats-task-1-" + suffix, "project": []string{projectID}, "status": 3, "endTime": now},
			{"name": "stats-task-2-" + suffix, "project": []string{projectID}, "status": 1},
			{"name": "stats-task-3-" + suffix, "project": []string{projectID}, "status": 1},
			{"name": "stats-task-other-" + suffix, "project": []string{"other-" + suffix}, "status": 3},
		},
	}

	for name, docs := range colls {
		coll := mongodb.DB.Collection(name)
		t.Cleanup(func() { _, _ = coll.DeleteMany(context.Background(), bson.M{"project": projectID}) })
		if _, err := coll.InsertMany(context.Background(), toInterfaces(docs)); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	svc := NewService()
	stats, err := svc.Stats(context.Background(), models.DashboardStatsRequest{Project: project})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if stats.Asset != 2 || stats.Subdomain != 1 || stats.URL != 3 ||
		stats.Vulnerability != 1 || stats.Sensitive != 2 {
		t.Fatalf("stats = %+v, want asset=2 subdomain=1 url=3 vuln=1 sensitive=2", stats)
	}
	if stats.Task != 3 || stats.TaskRunning != 2 || stats.TaskFinished != 1 {
		t.Fatalf("task stats = {total:%d running:%d finished:%d}, want {3 2 1}",
			stats.Task, stats.TaskRunning, stats.TaskFinished)
	}
	if stats.Project != project || stats.GeneratedAt == "" {
		t.Fatalf("stats meta = {project:%q generatedAt:%q}", stats.Project, stats.GeneratedAt)
	}
	// 节点来自 Redis：只校验口径（在线数不会超过总数）
	if stats.NodeOnline > stats.Node || stats.Node < 0 {
		t.Fatalf("node counts = {total:%d online:%d}", stats.Node, stats.NodeOnline)
	}
}

// TestTrendAggregatesUseIndexes 趋势聚合必须走索引：
//   - asset/vulnerability：{time: -1}（project 场景是 {project:1, time:-1}）
//   - task：{status:1, endTime:-1}
//
// 否则资产表一大，仪表盘首屏就会变成全集合扫描。
func TestTrendAggregatesUseIndexes(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	// 生产启动路径会建索引；测试库要先补一次，否则测到的是「没索引」而不是查询本身
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	repo := dashboardrepo.NewRepository()
	ctx := context.Background()
	start := "2026-11-12 00:00:00"
	end := "2026-11-18 23:59:59"

	cases := []struct {
		name       string
		collection string
		timeField  string
		base       bson.M
		wantIndex  string
	}{
		{"asset trend (time)", "asset", "time", bson.M{}, "time_-1"},
		{"asset trend (project+time)", "asset", "time", bson.M{"project": "proj-x"}, "project_1_time_-1"},
		{"vulnerability trend (time)", "vulnerability", "time", bson.M{}, "time_-1"},
		{"task trend (status+endTime)", "task", "endTime", bson.M{"status": 3}, "status_1_endTime_-1"},
		// project 场景走的是 withStatus 生成的 {"$and": [project, status]}，
		// trendMatch 会把它摊平，仍然要落到复合索引上
		{"task trend (project+status+endTime)", "task", "endTime",
			bson.M{"$and": bson.A{bson.M{"project": "proj-x"}, bson.M{"status": 3}}}, "status_1_endTime_-1"},
	}

	for _, c := range cases {
		plan, err := repo.ExplainTrend(ctx, c.collection, c.timeField, c.base, start, end)
		if err != nil {
			t.Fatalf("%s: ExplainTrend: %v", c.name, err)
		}
		stages, indexes := explainPlan(t, c.name, plan)
		if _, ok := stages["IXSCAN"]; !ok {
			if _, countScan := stages["COUNT_SCAN"]; !countScan {
				t.Fatalf("%s: winning plan has no IXSCAN, stages=%v indexes=%v", c.name, stages, indexes)
			}
		}
		used := false
		for got := range indexes {
			if strings.Contains(got, c.wantIndex) {
				used = true
			}
		}
		if !used {
			t.Fatalf("%s: winning plan does not use index %q, indexes=%v", c.name, c.wantIndex, indexes)
		}
		t.Logf("%s: stages=%v indexes=%v", c.name, stages, indexes)
	}
}

// explainPlan 递归收集 explain 结果里的 stage 与 indexName
func explainPlan(t *testing.T, name string, plan bson.M) (map[string]struct{}, map[string]struct{}) {
	t.Helper()
	stages := map[string]struct{}{}
	indexes := map[string]struct{}{}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch node := v.(type) {
		case bson.M:
			if s, ok := node["stage"].(string); ok {
				stages[s] = struct{}{}
			}
			if s, ok := node["indexName"].(string); ok {
				indexes[s] = struct{}{}
			}
			for _, child := range node {
				walk(child)
			}
		case bson.D:
			for _, item := range node {
				walk(item.Value)
			}
		case bson.A:
			for _, child := range node {
				walk(child)
			}
		case map[string]interface{}:
			for _, child := range node {
				walk(child)
			}
		case []interface{}:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(plan)
	if len(stages) == 0 {
		t.Fatalf("%s: explain result has no stage at all: %v", name, plan)
	}
	return stages, indexes
}

func toInterfaces(docs []bson.M) []interface{} {
	out := make([]interface{}, len(docs))
	for i, doc := range docs {
		out[i] = doc
	}
	return out
}