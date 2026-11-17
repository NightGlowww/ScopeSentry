//go:build integration

package statfinger

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	statfingerrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/statfinger"
	"go.mongodb.org/mongo-driver/bson"
)

// seedStatFingerAssets 插入测试资产，按 project/主机前缀清理，只删本次运行的数据。
func seedStatFingerAssets(t *testing.T, project, hostPrefix string, docs []interface{}) {
	t.Helper()
	coll := mongodb.DB.Collection("asset")
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"$or": bson.A{
			bson.M{"project": project},
			bson.M{"host": bson.M{"$regex": "^" + regexp.QuoteMeta(hostPrefix)}},
		}})
	})
	if len(docs) == 0 {
		return
	}
	if _, err := coll.InsertMany(context.Background(), docs); err != nil {
		t.Fatalf("seed assets: %v", err)
	}
}

// buildStatFingerDocs 20 个资产、6 个技术，覆盖三种边界：
//   - 同一资产重复写同一个技术（nginx 在 0 号资产出现两次：count 要 +2、资产数只 +1）
//   - 一个技术只落在一台没有 host 的资产上（cloudflare：样例 hosts 必须被剔成空数组）
//   - count 相同的并列（php / wordpress 都是 5，按技术名升序兜底）
func buildStatFingerDocs(project, hostPrefix string) []interface{} {
	techs := make([][]string, 20)
	add := func(i int, tech string) { techs[i] = append(techs[i], tech) }

	for i := 0; i < 10; i++ {
		add(i, "nginx")
	}
	add(0, "nginx")
	for i := 0; i < 5; i++ {
		add(i, "php")
	}
	for i := 3; i <= 7; i++ {
		add(i, "wordpress")
	}
	for i := 5; i <= 14; i++ {
		add(i, "jquery")
	}
	add(0, "tomcat")
	add(19, "cloudflare")

	ipPrefix := fmt.Sprintf("198.20.%d.", time.Now().UnixNano()%200+20)
	docs := make([]interface{}, 0, 20)
	for i := 0; i < 20; i++ {
		host := fmt.Sprintf("%s%02d.example.com", hostPrefix, i)
		if i == 19 {
			// IP 型资产没有 host：统计里仍要计数，但样例 host 不该出现空串
			host = ""
		}
		docs = append(docs, bson.M{
			"host":         host,
			"port":         fmt.Sprintf("%d", 8000+i),
			"ip":           fmt.Sprintf("%s%d", ipPrefix, i+1),
			"url":          "https://" + host,
			"title":        "statfinger",
			"project":      project,
			"time":         fmt.Sprintf("2026-11-18 10:%02d:00", i),
			"technologies": techs[i],
		})
	}
	return docs
}

// 统计口径：count（出现次数）/ assetCount（涉及资产数）/ 排序 / 样例 / 关键字 / 分页 / 项目隔离
func TestStatFingerAggregation(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-statfinger-" + suffix
	hostPrefix := "sf-" + suffix + "-"

	seedStatFingerAssets(t, project, hostPrefix, buildStatFingerDocs(project, hostPrefix))
	// 另一个项目的同名技术：不能混进本次统计
	seedStatFingerAssets(t, project+"-other", "sf-other-"+suffix+"-", []interface{}{
		bson.M{"host": "sf-other-" + suffix + "-a.example.com", "port": "443",
			"project": project + "-other", "time": "2026-11-18 23:59:00", "technologies": []string{"nginx"}},
	})

	svc := NewService()
	result, err := svc.ListTechnologies(context.Background(), models.StatFingerListRequest{
		Project:   project,
		PageIndex: 1,
		PageSize:  50,
	})
	if err != nil {
		t.Fatalf("ListTechnologies: %v", err)
	}

	if result.Total != 6 {
		t.Fatalf("total = %d, want 6 (nginx/php/wordpress/jquery/tomcat/cloudflare)", result.Total)
	}
	if len(result.List) != 6 {
		t.Fatalf("rows = %d, want 6: %+v", len(result.List), result.List)
	}

	// 排序：count 倒序，同 count 按技术名升序
	// 同 count 按技术名升序兜底：1 这一档 "cloudflare" < "tomcat"
	wantOrder := []string{"nginx", "jquery", "php", "wordpress", "cloudflare", "tomcat"}
	for i, want := range wantOrder {
		if result.List[i].Name != want {
			t.Fatalf("row[%d] = %s, want %s (order %v)", i, result.List[i].Name, want, wantOrder)
		}
	}

	wantCounts := map[string]int64{"nginx": 11, "jquery": 10, "php": 5, "wordpress": 5, "tomcat": 1, "cloudflare": 1}
	wantAssets := map[string]int64{"nginx": 10, "jquery": 10, "php": 5, "wordpress": 5, "tomcat": 1, "cloudflare": 1}
	for _, row := range result.List {
		if row.Count != wantCounts[row.Name] {
			t.Errorf("%s count = %d, want %d", row.Name, row.Count, wantCounts[row.Name])
		}
		if row.AssetCount != wantAssets[row.Name] {
			t.Errorf("%s assetCount = %d, want %d", row.Name, row.AssetCount, wantAssets[row.Name])
		}
	}

	// 样例：字典序最小的 5 个 host；没有 host 的技术返回空数组（不是 null）
	byName := map[string]models.StatFingerRow{}
	for _, row := range result.List {
		byName[row.Name] = row
	}
	nginx := byName["nginx"]
	if len(nginx.Samples) != 5 {
		t.Fatalf("nginx samples = %v, want 5", nginx.Samples)
	}
	for i := 0; i < 5; i++ {
		want := fmt.Sprintf("%s%02d.example.com", hostPrefix, i)
		if nginx.Samples[i] != want {
			t.Errorf("nginx samples[%d] = %s, want %s", i, nginx.Samples[i], want)
		}
	}
	if nginx.LastSeen != "2026-11-18 10:09:00" {
		t.Errorf("nginx lastSeen = %q, want 2026-11-18 10:09:00", nginx.LastSeen)
	}
	cloudflare := byName["cloudflare"]
	if cloudflare.Samples == nil {
		t.Errorf("cloudflare samples = nil, want []")
	}
	if len(cloudflare.Samples) != 0 {
		t.Errorf("cloudflare samples = %v, want [] (empty host filtered)", cloudflare.Samples)
	}

	// 分页：pageSize=2 三页拼起来 = 全量顺序，total 不随分页变化
	var paged []string
	for page := 1; page <= 3; page++ {
		res, err := svc.ListTechnologies(context.Background(), models.StatFingerListRequest{
			Project: project, PageIndex: page, PageSize: 2,
		})
		if err != nil {
			t.Fatalf("ListTechnologies(page=%d): %v", page, err)
		}
		if res.Total != 6 {
			t.Errorf("page %d total = %d, want 6", page, res.Total)
		}
		wantLen := 2
		if page == 3 {
			wantLen = 2
		}
		if len(res.List) != wantLen {
			t.Errorf("page %d rows = %d, want %d", page, len(res.List), wantLen)
		}
		for _, row := range res.List {
			paged = append(paged, row.Name)
		}
	}
	if fmt.Sprint(paged) != fmt.Sprint(wantOrder) {
		t.Errorf("paged order = %v, want %v", paged, wantOrder)
	}

	// 关键字只匹配技术名本身：tomcat 命中时不能把同文档里的 nginx 一起带出来
	kw, err := svc.ListTechnologies(context.Background(), models.StatFingerListRequest{
		Project: project, Keyword: "tomcat", PageIndex: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListTechnologies(keyword=tomcat): %v", err)
	}
	if kw.Total != 1 || len(kw.List) != 1 || kw.List[0].Name != "tomcat" {
		t.Fatalf("keyword tomcat = total %d rows %+v, want only tomcat", kw.Total, kw.List)
	}

	kw, err = svc.ListTechnologies(context.Background(), models.StatFingerListRequest{
		Project: project, Keyword: "PRESS", PageIndex: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListTechnologies(keyword=PRESS): %v", err)
	}
	if kw.Total != 1 || len(kw.List) != 1 || kw.List[0].Name != "wordpress" {
		t.Fatalf("keyword PRESS = total %d rows %+v, want wordpress only", kw.Total, kw.List)
	}

	// 项目过滤：不传 project 时看到的是全库（含本次和其它项目），传了就只看本项目
	other, err := svc.ListTechnologies(context.Background(), models.StatFingerListRequest{
		Project: project + "-other", PageIndex: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListTechnologies(other project): %v", err)
	}
	if other.Total != 1 || len(other.List) != 1 || other.List[0].Name != "nginx" || other.List[0].Count != 1 {
		t.Fatalf("other project = %+v, want single nginx with count 1", other.List)
	}
}

// 按技术反查资产：精确命中 + time 倒序 + 分页；空结果返回 []
func TestStatFingerAssetsByTechnology(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-statfinger-assets-" + suffix
	hostPrefix := "sfa-" + suffix + "-"
	seedStatFingerAssets(t, project, hostPrefix, buildStatFingerDocs(project, hostPrefix))

	svc := NewService()
	res, err := svc.ListAssetsByTechnology(context.Background(), models.StatFingerAssetsRequest{
		Technology: "nginx", Project: project, PageIndex: 1, PageSize: 4,
	})
	if err != nil {
		t.Fatalf("ListAssetsByTechnology: %v", err)
	}
	if res.Total != 10 {
		t.Fatalf("total = %d, want 10", res.Total)
	}
	if len(res.List) != 4 {
		t.Fatalf("rows = %d, want 4", len(res.List))
	}
	// time 倒序：0 号资产 10:00 ... 9 号资产 10:09，第一页第一条应是 09
	if res.List[0].Host != fmt.Sprintf("%s%02d.example.com", hostPrefix, 9) {
		t.Errorf("first host = %s, want -09 (newest)", res.List[0].Host)
	}
	if res.List[0].ID == "" {
		t.Errorf("row id is empty, want ObjectID hex")
	}

	// 第二页接着排
	page2, err := svc.ListAssetsByTechnology(context.Background(), models.StatFingerAssetsRequest{
		Technology: "nginx", Project: project, PageIndex: 2, PageSize: 4,
	})
	if err != nil {
		t.Fatalf("ListAssetsByTechnology(page2): %v", err)
	}
	if page2.Total != 10 || len(page2.List) != 4 {
		t.Fatalf("page2 = total %d rows %d, want 10/4", page2.Total, len(page2.List))
	}
	if page2.List[0].Host != fmt.Sprintf("%s%02d.example.com", hostPrefix, 5) {
		t.Errorf("page2 first host = %s, want -05", page2.List[0].Host)
	}

	// 不存在的技术 -> 空数组，不是 null
	empty, err := svc.ListAssetsByTechnology(context.Background(), models.StatFingerAssetsRequest{
		Technology: "no-such-technology-xyz", Project: project,
	})
	if err != nil {
		t.Fatalf("ListAssetsByTechnology(empty): %v", err)
	}
	if empty.Total != 0 || empty.List == nil || len(empty.List) != 0 {
		t.Fatalf("empty = total %d list %v, want 0/[]", empty.Total, empty.List)
	}
}

// explainWinningPlan 只看 explain 的 winningPlan，收集其中的 stage 与 indexName。
//
// 不能直接在整份 explain JSON 上 grep IXSCAN：rejectedPlans 里同样带索引名，
// 那样连「被否决的计划」都会被当成走索引，等于没验。
func explainWinningPlan(t *testing.T, name string, plan bson.M) (map[string]struct{}, map[string]struct{}) {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("%s: marshal explain: %v", name, err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: decode explain: %v", name, err)
	}

	stages := map[string]struct{}{}
	indexes := map[string]struct{}{}
	var walk func(node interface{}, inWinning bool)
	walk = func(node interface{}, inWinning bool) {
		switch v := node.(type) {
		case map[string]interface{}:
			if wp, ok := v["winningPlan"]; ok {
				walk(wp, true)
			}
			if inWinning {
				if s, ok := v["stage"].(string); ok {
					stages[s] = struct{}{}
				}
				if n, ok := v["indexName"].(string); ok {
					indexes[n] = struct{}{}
				}
			}
			for key, child := range v {
				if key == "winningPlan" {
					continue
				}
				walk(child, inWinning)
			}
		case []interface{}:
			for _, child := range v {
				walk(child, inWinning)
			}
		}
	}
	walk(doc, false)
	return stages, indexes
}

// assertPlanUsesIndex winningPlan 里必须出现 IXSCAN/COUNT_SCAN，且索引名命中 candidates 之一。
//
// 为什么要多个候选：查询规划器会根据数据分布改选等价索引（例如 project+technologies 既能走
// `project_1_technologies_1` 也能走 `technologies_1_time_-1`）。断言「必须走某个索引」是真正的
// 目的，锁死某一个具体索引名会把正常计划变更判成回归。
func assertPlanUsesIndex(t *testing.T, name string, plan bson.M, candidates ...string) {
	t.Helper()
	stages, indexes := explainWinningPlan(t, name, plan)
	// COUNT_SCAN 是 count 查询的索引扫描（只读索引、不回表），和 IXSCAN 一样算走索引
	usedIndex := false
	for _, stage := range []string{"IXSCAN", "COUNT_SCAN"} {
		if _, ok := stages[stage]; ok {
			usedIndex = true
		}
	}
	if !usedIndex {
		t.Fatalf("%s: winning plan has no IXSCAN/COUNT_SCAN, stages=%v indexes=%v", name, sortedKeys(stages), sortedKeys(indexes))
	}
	for got := range indexes {
		for _, want := range candidates {
			if want == "" || strings.Contains(got, want) {
				t.Logf("%s: winning plan uses index %q", name, got)
				return
			}
		}
	}
	t.Fatalf("%s: winning plan uses none of %v, indexes=%v", name, candidates, sortedKeys(indexes))
}

// explainCount 直接对 count 查询取 explain（仓库层的 count 是 CountDocuments，
// 没有单独的 Explain 方法，这里用同一条 filter 复原它的计划）。
func explainCount(t *testing.T, coll string, filter bson.M) bson.M {
	t.Helper()
	cmd := bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "count", Value: coll},
			{Key: "query", Value: filter},
		}},
		{Key: "verbosity", Value: "queryPlanner"},
	}
	var out bson.M
	if err := mongodb.DB.RunCommand(context.Background(), cmd).Decode(&out); err != nil {
		t.Fatalf("explain count on %s: %v", coll, err)
	}
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// 索引验证：统计聚合（带 project）与按技术反查都必须走索引，不能全表扫。
func TestStatFingerQueriesUseIndexes(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-statfinger-explain-" + suffix
	hostPrefix := "sfe-" + suffix + "-"
	seedStatFingerAssets(t, project, hostPrefix, buildStatFingerDocs(project, hostPrefix))

	// 先确保索引真的建出来了（生产启动路径也会跑 createIndexes），
	// 否则这里测到的是「测试库没索引」，不是查询本身的问题。
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	repo := statfingerrepo.NewRepository()
	ctx := context.Background()

	req := models.StatFingerListRequest{Project: project, PageIndex: 1, PageSize: 20}
	plan, err := repo.ExplainTechnologies(ctx, BuildStatFingerPreMatch(req), BuildStatFingerPostMatch(req), 1, 20)
	if err != nil {
		t.Fatalf("ExplainTechnologies: %v", err)
	}
	assertPlanUsesIndex(t, "stat_finger aggregate(project)", plan, "project")

	// 全库统计（不传 project）没有等值条件，计划器只能扫多值索引；
	// 这里把 winning plan 记下来，方便对比「有 project 收窄」时的差异。
	globalPlan, err := repo.ExplainTechnologies(ctx, BuildStatFingerPreMatch(models.StatFingerListRequest{}), bson.M{}, 1, 20)
	if err != nil {
		t.Fatalf("ExplainTechnologies(global): %v", err)
	}
	globalStages, globalIndexes := explainWinningPlan(t, "stat_finger aggregate(no project filter)", globalPlan)
	t.Logf("stat_finger aggregate(no project filter): stages=%v indexes=%v（没有等值条件时计划器按 technologies 多值索引扫）",
		sortedKeys(globalStages), sortedKeys(globalIndexes))

	assetReq := models.StatFingerAssetsRequest{Technology: "nginx", Project: project, PageIndex: 1, PageSize: 20}
	assetPlan, err := repo.ExplainAssetsByTechnology(ctx, BuildTechnologyAssetsFilter(assetReq), 1, 20)
	if err != nil {
		t.Fatalf("ExplainAssetsByTechnology: %v", err)
	}
	// 规划器可能选 technologies 多值索引，也可能在 project 等值条件下选 project 复合索引；
	// 两者都只扫索引/小范围，断言「命中其一」而不是锁死某一个。
	assertPlanUsesIndex(t, "stat_finger assets(technology)", assetPlan,
		"technologies_1_time_-1", "project_1_technologies_1", "technologies_1", "project_1_time_-1")

	// 同一个端点里还有一次 count（拿 total）：它用的是 {project:1, technologies:1} 的 COUNT_SCAN，
	// 只读索引不回表。这里一并验掉，避免「列表走索引、count 全表扫」那种半截优化。
	countPlan := explainCount(t, "asset", BuildTechnologyAssetsFilter(assetReq))
	assertPlanUsesIndex(t, "stat_finger assets(count)", countPlan,
		"project_1_technologies_1", "technologies_1", "project_1_time_-1")
}
