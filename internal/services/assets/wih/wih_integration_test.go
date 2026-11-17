//go:build integration

package wih

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	wihrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/wih"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// seedWIHResults 插入 SensitiveResult 并注册清理（按 project 精确删）
func seedWIHResults(t *testing.T, project string, docs []interface{}) {
	t.Helper()
	coll := mongodb.DB.Collection("SensitiveResult")
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"project": project})
	})
	if len(docs) == 0 {
		return
	}
	if _, err := coll.InsertMany(context.Background(), docs); err != nil {
		t.Fatalf("seed SensitiveResult: %v", err)
	}
}

// buildWIHDocs 10 条敏感信息命中：email 3 / phone 3 / jwt 2 / apikey 2，
// time 逐分钟递增，match 里带可检索的片段（keyword 要能命中数组内容）。
func buildWIHDocs(project, hostPrefix string) []interface{} {
	sids := []string{"email", "email", "email", "phone", "phone", "phone", "jwt", "jwt", "apikey", "apikey"}
	docs := make([]interface{}, 0, 10)
	for i := 0; i < 10; i++ {
		host := fmt.Sprintf("%s-%02d.example.com", hostPrefix, i)
		docs = append(docs, bson.M{
			"sid":        sids[i],
			"url":        "https://" + host + "/static/app.js",
			"match":      []string{fmt.Sprintf("secret-%02d", i), sids[i] + "-value"},
			"md5":        fmt.Sprintf("md5-%02d", i),
			"color":      "danger",
			"tags":       []string{"sensitive"},
			"status":     1,
			"project":    project,
			"rootDomain": hostPrefix + ".example.com",
			"taskName":   "task-wih",
			"time":       fmt.Sprintf("2026-11-18 10:%02d:00", i),
		})
	}
	return docs
}

func TestWIHListAndDetail(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-wih-" + suffix
	hostPrefix := "wih-" + suffix
	seedWIHResults(t, project, buildWIHDocs(project, hostPrefix))
	// 其它项目：不能混进来
	seedWIHResults(t, project+"-other", []interface{}{
		bson.M{"sid": "email", "url": "https://other.example.com/", "match": []string{"x"},
			"project": project + "-other", "time": "2026-11-18 23:59:00", "md5": "md5-other"},
	})

	svc := NewService()
	ctx := context.Background()

	// 全量（限定本项目）：time 倒序
	all, err := svc.List(ctx, models.WIHListRequest{Project: project, PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if all.Total != 10 || len(all.List) != 10 {
		t.Fatalf("all = total %d rows %d, want 10/10", all.Total, len(all.List))
	}
	if all.List[0].Time != "2026-11-18 10:09:00" {
		t.Errorf("first time = %q, want newest 10:09", all.List[0].Time)
	}
	if all.List[0].Type != "apikey" {
		t.Errorf("first type = %q, want apikey", all.List[0].Type)
	}
	if all.List[0].Host != fmt.Sprintf("%s-09.example.com", hostPrefix) {
		t.Errorf("first host = %q, want %s-09", all.List[0].Host, hostPrefix)
	}
	if all.List[0].BodyID != "md5-09" {
		t.Errorf("first bodyId = %q, want md5-09", all.List[0].BodyID)
	}
	if len(all.List[0].Match) != 2 {
		t.Errorf("first match = %v, want 2 fragments", all.List[0].Match)
	}

	// type 过滤（单值）
	emails, err := svc.List(ctx, models.WIHListRequest{Project: project, Type: "email", PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(type=email): %v", err)
	}
	if emails.Total != 3 || len(emails.List) != 3 {
		t.Fatalf("type email = %d/%d, want 3/3", emails.Total, len(emails.List))
	}
	for _, row := range emails.List {
		if row.Type != "email" {
			t.Errorf("type email result contains %q", row.Type)
		}
	}

	// type 多选
	multi, err := svc.List(ctx, models.WIHListRequest{Project: project, Type: "email,phone", PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(type=email,phone): %v", err)
	}
	if multi.Total != 6 {
		t.Errorf("type email,phone = %d, want 6", multi.Total)
	}

	// keyword 命中 match 数组内容
	byMatch, err := svc.List(ctx, models.WIHListRequest{Project: project, Keyword: "secret-05", PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(keyword=secret-05): %v", err)
	}
	if byMatch.Total != 1 || len(byMatch.List) != 1 || byMatch.List[0].Type != "phone" {
		t.Errorf("keyword secret-05 = %+v, want single phone row", byMatch.List)
	}

	// keyword 命中 url
	byURL, err := svc.List(ctx, models.WIHListRequest{Project: project, Keyword: "-07.", PageIndex: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List(keyword=-07.): %v", err)
	}
	if byURL.Total != 1 || len(byURL.List) != 1 || byURL.List[0].BodyID != "md5-07" {
		t.Errorf("keyword -07. = %+v, want md5-07", byURL.List)
	}

	// 分页：pageSize=4 -> 4/4/2
	seen := 0
	for page := 1; page <= 3; page++ {
		res, err := svc.List(ctx, models.WIHListRequest{Project: project, PageIndex: page, PageSize: 4})
		if err != nil {
			t.Fatalf("List(page=%d): %v", page, err)
		}
		if res.Total != 10 {
			t.Errorf("page %d total = %d, want 10", page, res.Total)
		}
		want := 4
		if page == 3 {
			want = 2
		}
		if len(res.List) != want {
			t.Errorf("page %d rows = %d, want %d", page, len(res.List), want)
		}
		seen += len(res.List)
	}
	if seen != 10 {
		t.Errorf("paged rows sum = %d, want 10", seen)
	}

	// 空结果 -> []
	empty, err := svc.List(ctx, models.WIHListRequest{Project: project, Keyword: "no-such-keyword-xyz"})
	if err != nil {
		t.Fatalf("List(empty): %v", err)
	}
	if empty.Total != 0 || empty.List == nil || len(empty.List) != 0 {
		t.Fatalf("empty = total %d list %v, want 0/[]", empty.Total, empty.List)
	}

	// detail
	var doc bson.M
	if err := mongodb.DB.Collection("SensitiveResult").FindOne(ctx, bson.M{"project": project, "md5": "md5-03"}).Decode(&doc); err != nil {
		t.Fatalf("find seeded doc: %v", err)
	}
	id := doc["_id"].(primitive.ObjectID)
	detail, err := svc.GetDetail(ctx, id.Hex())
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if detail.Type != "phone" || detail.BodyID != "md5-03" {
		t.Errorf("detail = %+v, want phone/md5-03", detail.WIHRow)
	}
	if len(detail.Match) != 2 || detail.Match[0] != "secret-03" {
		t.Errorf("detail match = %v, want [secret-03 phone-value]", detail.Match)
	}

	// detail：不存在 / 格式错
	if _, err := svc.GetDetail(ctx, primitive.NewObjectID().Hex()); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDetail(missing) err = %v, want ErrNotFound", err)
	}
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

// 索引验证：type / project / type+project / 纯时间倒序 四种形状都要走索引
func TestWIHQueriesUseIndexes(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-wih-explain-" + suffix
	hostPrefix := "wihe-" + suffix
	seedWIHResults(t, project, buildWIHDocs(project, hostPrefix))

	repo := wihrepo.NewRepository()
	ctx := context.Background()

	cases := []struct {
		name  string
		req   models.WIHListRequest
		index string
	}{
		{"type", models.WIHListRequest{Type: "email"}, "sid_1_time_-1"},
		{"project", models.WIHListRequest{Project: project}, "project_1_time_-1"},
		{"type+project", models.WIHListRequest{Type: "email", Project: project}, "project_1_sid_1_time_-1"},
		// keyword 是子串正则，和 project 组合时仍然走索引
		{"project+keyword", models.WIHListRequest{Project: project, Keyword: "secret"}, "project"},
	}
	for _, tc := range cases {
		plan, err := repo.ExplainList(ctx, BuildListFilter(tc.req), 1, 20)
		if err != nil {
			t.Fatalf("%s: ExplainList: %v", tc.name, err)
		}
		assertPlanUsesIndex(t, "wih "+tc.name, plan, tc.index)
	}
}
