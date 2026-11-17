//go:build integration

package nucleiresult

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	nucleirepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/nucleiresult"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// seedNucleiResults 插入漏洞记录并注册清理（按 project 精确删，只删本次运行的数据）
func seedNucleiResults(t *testing.T, project string, docs []interface{}) {
	t.Helper()
	coll := mongodb.DB.Collection("vulnerability")
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"project": project})
	})
	if len(docs) == 0 {
		return
	}
	if _, err := coll.InsertMany(context.Background(), docs); err != nil {
		t.Fatalf("seed vulnerability: %v", err)
	}
}

// buildNucleiDocs 10 条漏洞：severity 2 critical / 3 high / 3 medium / 2 low，
// 前 5 条 templateId 是 cve-2021-44228（用来验子串 + 大小写不敏感），
// 后 5 条是 tech-detect。time 逐分钟递增，用来验倒序与分页。
func buildNucleiDocs(project, hostPrefix string) []interface{} {
	severities := []string{"critical", "critical", "high", "high", "high", "medium", "medium", "medium", "low", "low"}
	docs := make([]interface{}, 0, 10)
	for i := 0; i < 10; i++ {
		templateID := "tech-detect"
		name := "Tech Detect"
		if i < 5 {
			templateID = "cve-2021-44228"
			name = "Apache Log4j RCE"
		}
		host := fmt.Sprintf("%s-%02d.example.com", hostPrefix, i)
		doc := bson.M{
			"vulnid":     templateID,
			"vulnname":   name,
			"level":      severities[i],
			"url":        "https://" + host + "/admin",
			"matched":    fmt.Sprintf("matched-%d", i),
			"project":    project,
			"rootDomain": hostPrefix + ".example.com",
			"taskName":   "task-nuclei",
			"tags":       []string{"nuclei"},
			"status":     1,
			"time":       fmt.Sprintf("2026-11-18 10:%02d:00", i),
			"hash":       fmt.Sprintf("%s-hash-%02d", project, i),
		}
		// 0..7 主文档自带 request/response；8、9 只有 vulnerabilityDetail（验回退）
		if i < 8 {
			doc["request"] = fmt.Sprintf("req-%d", i)
			doc["response"] = fmt.Sprintf("res-%d", i)
		}
		docs = append(docs, doc)
	}
	return docs
}

func TestNucleiResultListAndDetail(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-nuclei-" + suffix
	hostPrefix := "nuc-" + suffix
	docs := buildNucleiDocs(project, hostPrefix)
	seedNucleiResults(t, project, docs)

	// detail 回退数据：8 号、9 号主文档没有 request/response
	detailColl := mongodb.DB.Collection("vulnerabilityDetail")
	detailDocs := []interface{}{
		bson.M{"hash": fmt.Sprintf("%s-hash-08", project), "req": "detail-req-8", "res": "detail-res-8"},
		bson.M{"hash": fmt.Sprintf("%s-hash-09", project), "req": "detail-req-9", "res": "detail-res-9"},
	}
	if _, err := detailColl.InsertMany(context.Background(), detailDocs); err != nil {
		t.Fatalf("seed vulnerabilityDetail: %v", err)
	}
	t.Cleanup(func() {
		_, _ = detailColl.DeleteMany(context.Background(), bson.M{"hash": bson.M{
			"$regex": "^" + regexp.QuoteMeta(project) + "-hash-",
		}})
	})

	// 其它项目的同模板漏洞：不能混进来
	seedNucleiResults(t, project+"-other", []interface{}{
		bson.M{"vulnid": "cve-2021-44228", "vulnname": "other", "level": "critical",
			"url": "https://other.example.com/", "project": project + "-other",
			"time": "2026-11-18 23:59:00", "hash": project + "-other-hash"},
	})

	svc := NewService()
	ctx := context.Background()

	// 全量（限定本项目）：按 time 倒序
	all, err := svc.List(ctx, models.NucleiResultListRequest{Project: project, PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if all.Total != 10 || len(all.List) != 10 {
		t.Fatalf("all = total %d rows %d, want 10/10", all.Total, len(all.List))
	}
	if all.List[0].MatchedAt != "2026-11-18 10:09:00" {
		t.Errorf("first matchedAt = %q, want newest 10:09", all.List[0].MatchedAt)
	}
	if all.List[0].Host != fmt.Sprintf("%s-09.example.com", hostPrefix) {
		t.Errorf("first host = %q, want %s-09", all.List[0].Host, hostPrefix)
	}
	if all.List[0].TemplateID != "tech-detect" || all.List[0].Severity != "low" {
		t.Errorf("first row = %+v, want templateId tech-detect severity low", all.List[0])
	}
	if all.List[0].Name != "Tech Detect" {
		t.Errorf("first row name = %q, want Tech Detect（vulnname 必须映射到 name）", all.List[0].Name)
	}

	// severity 过滤（小写）
	high, err := svc.List(ctx, models.NucleiResultListRequest{Project: project, Severity: "high", PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(severity=high): %v", err)
	}
	if high.Total != 3 || len(high.List) != 3 {
		t.Fatalf("severity high = %d/%d, want 3/3", high.Total, len(high.List))
	}
	for _, row := range high.List {
		if row.Severity != "high" {
			t.Errorf("severity high result contains %q", row.Severity)
		}
	}

	// severity 过滤（首字母大写也要能命中）
	critical, err := svc.List(ctx, models.NucleiResultListRequest{Project: project, Severity: "Critical", PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(severity=Critical): %v", err)
	}
	if critical.Total != 2 {
		t.Errorf("severity Critical = %d, want 2 (case-insensitive)", critical.Total)
	}

	// severity 逗号多选
	multi, err := svc.List(ctx, models.NucleiResultListRequest{Project: project, Severity: "critical,low", PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(severity=critical,low): %v", err)
	}
	if multi.Total != 4 {
		t.Errorf("severity critical,low = %d, want 4", multi.Total)
	}

	// templateId 子串 + 大小写不敏感（库里是小写 cve-2021-44228，查大写）
	byTemplate, err := svc.List(ctx, models.NucleiResultListRequest{Project: project, TemplateID: "CVE-2021", PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(templateId): %v", err)
	}
	if byTemplate.Total != 5 {
		t.Errorf("templateId CVE-2021 = %d, want 5", byTemplate.Total)
	}

	// keyword 命中漏洞名
	byName, err := svc.List(ctx, models.NucleiResultListRequest{Project: project, Keyword: "log4j", PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(keyword=log4j): %v", err)
	}
	if byName.Total != 5 {
		t.Errorf("keyword log4j = %d, want 5", byName.Total)
	}

	// keyword 命中 URL 里的主机序号
	byURL, err := svc.List(ctx, models.NucleiResultListRequest{Project: project, Keyword: "-07.", PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(keyword=-07.): %v", err)
	}
	if byURL.Total != 1 || len(byURL.List) != 1 || byURL.List[0].Matched != "matched-7" {
		t.Errorf("keyword -07. = %+v, want single matched-7", byURL.List)
	}

	// 分页：pageSize=3 -> 3/3/3/1，total 恒为 10
	seen := 0
	for page := 1; page <= 4; page++ {
		res, err := svc.List(ctx, models.NucleiResultListRequest{Project: project, PageIndex: page, PageSize: 3})
		if err != nil {
			t.Fatalf("List(page=%d): %v", page, err)
		}
		if res.Total != 10 {
			t.Errorf("page %d total = %d, want 10", page, res.Total)
		}
		want := 3
		if page == 4 {
			want = 1
		}
		if len(res.List) != want {
			t.Errorf("page %d rows = %d, want %d", page, len(res.List), want)
		}
		seen += len(res.List)
	}
	if seen != 10 {
		t.Errorf("paged rows sum = %d, want 10", seen)
	}

	// 空结果：total 0、list 是 []（不是 null）
	empty, err := svc.List(ctx, models.NucleiResultListRequest{Project: project, Keyword: "no-such-keyword-xyz"})
	if err != nil {
		t.Fatalf("List(empty): %v", err)
	}
	if empty.Total != 0 || empty.List == nil || len(empty.List) != 0 {
		t.Fatalf("empty = total %d list %v, want 0/[]", empty.Total, empty.List)
	}

	// detail：主文档自带 request/response
	var zero bson.M
	if err := mongodb.DB.Collection("vulnerability").FindOne(ctx, bson.M{"project": project, "matched": "matched-0"}).Decode(&zero); err != nil {
		t.Fatalf("find seeded doc: %v", err)
	}
	id0 := zero["_id"].(primitive.ObjectID)
	detail, err := svc.GetDetail(ctx, id0.Hex())
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if detail.Request != "req-0" || detail.Response != "res-0" {
		t.Errorf("detail req/res = %q/%q, want req-0/res-0", detail.Request, detail.Response)
	}
	if detail.TemplateID != "cve-2021-44228" || detail.Severity != "critical" {
		t.Errorf("detail = %+v, want cve-2021-44228/critical", detail)
	}
	if detail.Name != "Apache Log4j RCE" {
		t.Errorf("detail name = %q, want Apache Log4j RCE", detail.Name)
	}

	// detail：主文档没有 request/response 时回退 vulnerabilityDetail
	var eight bson.M
	if err := mongodb.DB.Collection("vulnerability").FindOne(ctx, bson.M{"project": project, "matched": "matched-8"}).Decode(&eight); err != nil {
		t.Fatalf("find seeded doc 8: %v", err)
	}
	id8 := eight["_id"].(primitive.ObjectID)
	detail8, err := svc.GetDetail(ctx, id8.Hex())
	if err != nil {
		t.Fatalf("GetDetail(fallback): %v", err)
	}
	if detail8.Request != "detail-req-8" || detail8.Response != "detail-res-8" {
		t.Errorf("detail fallback = %q/%q, want detail-req-8/detail-res-8", detail8.Request, detail8.Response)
	}

	// detail：id 不存在 -> ErrNotFound
	if _, err := svc.GetDetail(ctx, primitive.NewObjectID().Hex()); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDetail(missing) err = %v, want ErrNotFound", err)
	}
	// detail：id 格式错 -> ErrInvalidID
	if _, err := svc.GetDetail(ctx, "not-an-object-id"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("GetDetail(bad id) err = %v, want ErrInvalidID", err)
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

// assertPlanUsesIndex winningPlan 里必须出现 IXSCAN，且索引名包含 wantIndex 片段。
func assertPlanUsesIndex(t *testing.T, name string, plan bson.M, wantIndex string) {
	t.Helper()
	stages, indexes := explainWinningPlan(t, name, plan)
	if _, ok := stages["IXSCAN"]; !ok {
		t.Fatalf("%s: winning plan has no IXSCAN, stages=%v indexes=%v", name, sortedKeys(stages), sortedKeys(indexes))
	}
	for got := range indexes {
		if wantIndex == "" || strings.Contains(got, wantIndex) {
			t.Logf("%s: winning plan uses index %q", name, got)
			return
		}
	}
	t.Fatalf("%s: winning plan does not use index %q, indexes=%v", name, wantIndex, sortedKeys(indexes))
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// 索引验证：project / severity / project+severity / 纯时间倒序 四种形状都要走索引
func TestNucleiResultQueriesUseIndexes(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-nuclei-explain-" + suffix
	hostPrefix := "nuce-" + suffix
	seedNucleiResults(t, project, buildNucleiDocs(project, hostPrefix))

	repo := nucleirepo.NewRepository()
	ctx := context.Background()

	cases := []struct {
		name  string
		req   models.NucleiResultListRequest
		index string
	}{
		{"project", models.NucleiResultListRequest{Project: project}, "project"},
		{"severity", models.NucleiResultListRequest{Severity: "high"}, "level"},
		{"project+severity", models.NucleiResultListRequest{Project: project, Severity: "high"}, "project_1_level_1_time_-1"},
		{"no filter (time sort)", models.NucleiResultListRequest{}, "time"},
		// keyword/templateId 是子串正则（本身没有索引边界），但和 project 组合时仍然走索引
		{"project+keyword", models.NucleiResultListRequest{Project: project, Keyword: "log4j"}, "project"},
		{"project+templateId", models.NucleiResultListRequest{Project: project, TemplateID: "CVE-2021"}, "project"},
	}
	for _, tc := range cases {
		plan, err := repo.ExplainList(ctx, BuildListFilter(tc.req), 1, 20)
		if err != nil {
			t.Fatalf("%s: ExplainList: %v", tc.name, err)
		}
		assertPlanUsesIndex(t, "nuclei "+tc.name, plan, tc.index)
	}
}
