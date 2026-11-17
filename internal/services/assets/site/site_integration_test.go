//go:build integration

package site

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// seedSiteAssets 插入测试资产并注册清理；host 全部带本次运行的唯一前缀，
// 清理用 ^prefix 正则，不会碰其他测试/真实数据。
func seedSiteAssets(t *testing.T, prefix string, docs []interface{}) {
	t.Helper()
	coll := mongodb.DB.Collection("asset")
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{
			"host": bson.M{"$regex": "^" + regexp.QuoteMeta(prefix)},
		})
	})
	if len(docs) == 0 {
		return
	}
	if _, err := coll.InsertMany(context.Background(), docs); err != nil {
		t.Fatalf("seed assets: %v", err)
	}
}

// 站点列表要按 host+port 聚合。asset 集合上有 unique(host, port) 索引
// （internal/database/mongodb/initdb.go 创建），所以真实数据里同一 host+port
// 只有一条文档、assetCount 恒为 1；聚合本身不假设这条唯一性——真丢了索引
// （历史库/回填中的库）assetCount 就是真实条数，$top 也仍然取最新一条。
// 因此这里断言的是「分组维度正确」：同 host 不同 port 是两行、同 port 不同 host
// 也是两行、行内展示字段来自正确的文档。
func TestSiteListAggregation(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	prefix := fmt.Sprintf("siteip-%d", time.Now().UnixNano())
	hostA := prefix + "-a.example.com"
	hostB := prefix + "-b.example.com"

	seedSiteAssets(t, prefix, []interface{}{
		bson.M{"host": hostA, "port": "443", "ip": "198.18.10.1", "title": "Alpha", "statuscode": 200,
			"project": "proj-siteip", "time": "2026-11-18 12:00:00", "url": "https://alpha.example.com"},
		bson.M{"host": hostA, "port": "80", "ip": "198.18.10.1", "title": "Alpha Plain", "statuscode": 301,
			"project": "proj-siteip", "time": "2026-11-18 09:00:00"},
		bson.M{"host": hostB, "port": "8443", "ip": "198.18.10.3", "title": "Bravo", "statuscode": 403,
			"project": "other-proj", "time": "2026-11-18 08:00:00"},
		bson.M{"host": hostB, "port": "443", "ip": "198.18.10.3", "title": "Bravo TLS", "statuscode": 200,
			"project": "other-proj", "time": "2026-11-18 07:00:00"},
	})

	svc := NewService()

	result, err := svc.ListSites(context.Background(), models.SiteListRequest{
		Keyword:   prefix,
		PageIndex: 1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("ListSites: %v", err)
	}
	if result.Total != 4 {
		t.Fatalf("total = %d, want 4 (hostA:443 / hostA:80 / hostB:8443 / hostB:443)", result.Total)
	}
	if len(result.List) != 4 {
		t.Fatalf("rows = %d, want 4", len(result.List))
	}

	// 同 host 不同端口 -> 两行；同端口不同 host -> 两行
	seen := map[string]string{}
	for _, row := range result.List {
		key := row.Host + "|" + row.Port
		seen[key] = row.Title
		if row.AssetCount != 1 {
			t.Errorf("%s assetCount = %d, want 1（unique(host,port) 下每个分组只有一条）", key, row.AssetCount)
		}
	}
	want := map[string]string{
		hostA + "|443":  "Alpha",
		hostA + "|80":   "Alpha Plain",
		hostB + "|8443": "Bravo",
		hostB + "|443":  "Bravo TLS",
	}
	if len(seen) != len(want) {
		t.Fatalf("rows = %v, want %v", seen, want)
	}
	for k, title := range want {
		if seen[k] != title {
			t.Errorf("row %s title = %q, want %q", k, seen[k], title)
		}
	}

	// 默认按 lastSeen 倒序
	times := []string{result.List[0].LastSeen, result.List[1].LastSeen, result.List[2].LastSeen, result.List[3].LastSeen}
	for i := 1; i < len(times); i++ {
		if times[i-1] < times[i] {
			t.Fatalf("site rows not sorted by lastSeen desc: %v", times)
		}
	}
	first := result.List[0]
	if first.Host != hostA || first.Port != "443" {
		t.Fatalf("first row = %s:%s, want %s:443", first.Host, first.Port, hostA)
	}
	if first.Title != "Alpha" || first.StatusCode != 200 || first.IP != "198.18.10.1" ||
		first.Project != "proj-siteip" || first.LastSeen != "2026-11-18 12:00:00" ||
		first.URL != "https://alpha.example.com" {
		t.Errorf("first row fields = %+v, want Alpha/200/198.18.10.1/proj-siteip/12:00/url", first)
	}

	// project 过滤
	byProject, err := svc.ListSites(context.Background(), models.SiteListRequest{
		Project:   "other-proj",
		PageIndex: 1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("ListSites(project): %v", err)
	}
	if byProject.Total != 2 || len(byProject.List) != 2 {
		t.Fatalf("project filter total = %d rows = %d, want 2/2", byProject.Total, len(byProject.List))
	}
	for _, row := range byProject.List {
		if row.Host != hostB {
			t.Errorf("project filter row host = %s, want %s", row.Host, hostB)
		}
	}

	// 分页：pageSize=3 -> 第 1 页 3 条、第 2 页 1 条，total 始终是 4
	page1, err := svc.ListSites(context.Background(), models.SiteListRequest{Keyword: prefix, PageIndex: 1, PageSize: 3})
	if err != nil {
		t.Fatalf("ListSites(page1): %v", err)
	}
	page2, err := svc.ListSites(context.Background(), models.SiteListRequest{Keyword: prefix, PageIndex: 2, PageSize: 3})
	if err != nil {
		t.Fatalf("ListSites(page2): %v", err)
	}
	if page1.Total != 4 || len(page1.List) != 3 {
		t.Errorf("page1 = total %d rows %d, want 4/3", page1.Total, len(page1.List))
	}
	if page2.Total != 4 || len(page2.List) != 1 {
		t.Errorf("page2 = total %d rows %d, want 4/1", page2.Total, len(page2.List))
	}
	overlap := 0
	for _, a := range page1.List {
		for _, b := range page2.List {
			if a.Host == b.Host && a.Port == b.Port {
				overlap++
			}
		}
	}
	if overlap != 0 {
		t.Errorf("page1/page2 overlap on %d rows", overlap)
	}

	// 关键字不命中 -> 空列表（而不是 nil，前端拿 null 会崩）
	empty, err := svc.ListSites(context.Background(), models.SiteListRequest{Keyword: prefix + "-no-such-keyword"})
	if err != nil {
		t.Fatalf("ListSites(empty): %v", err)
	}
	if empty.Total != 0 || len(empty.List) != 0 {
		t.Errorf("empty keyword = total %d rows %d, want 0/0", empty.Total, len(empty.List))
	}
	if empty.List == nil {
		t.Errorf("empty list is nil, want empty slice")
	}

	// 关键字命中 title（不是 host）也要能筛出来
	byTitle, err := svc.ListSites(context.Background(), models.SiteListRequest{Keyword: "Bravo TLS", PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListSites(title): %v", err)
	}
	if byTitle.Total != 1 || len(byTitle.List) != 1 || byTitle.List[0].Port != "443" || byTitle.List[0].Host != hostB {
		t.Errorf("title keyword = total %d rows %+v, want only %s:443", byTitle.Total, byTitle.List, hostB)
	}
}

// 子域链路要按时间倒序、最多 500 条，并且 total 报的是截断前的真实命中数。
// 链路范围 = 该 host 自身 + 它所属根域下的整棵子树（传根域时就是整棵树），
// 不相干根域的记录必须被排除。
func TestSiteChainOrderAndLimit(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	prefix := fmt.Sprintf("siteip-chain-%d", time.Now().UnixNano())
	rootHost := prefix + ".example.com"
	chainHost := "a." + rootHost
	unrelatedHost := prefix + "-other.example.com"

	coll := mongodb.DB.Collection("subdomain")
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{
			"$or": bson.A{
				bson.M{"host": bson.M{"$regex": "^" + regexp.QuoteMeta(prefix)}},
				bson.M{"rootDomain": bson.M{"$regex": "^" + regexp.QuoteMeta(prefix)}},
			},
		})
	})

	// 505 条同 host 的记录，time 递增，最新的必然是 i=504
	times := make([]string, 0, 505)
	docs := make([]interface{}, 0, 508)
	for i := 0; i < 505; i++ {
		ts := fmt.Sprintf("2026-11-18 10:%02d:%02d", i/60, i%60)
		times = append(times, ts)
		docs = append(docs, bson.M{
			"host":       chainHost,
			"type":       "A",
			"ip":         []string{fmt.Sprintf("198.18.11.%d", i%250)},
			"time":       ts,
			"project":    "proj-siteip-chain",
			"rootDomain": rootHost,
		})
	}
	// 2 条兄弟子域：同根域、不同 host，必须一起出现在链路里
	for i := 0; i < 2; i++ {
		docs = append(docs, bson.M{
			"host":       fmt.Sprintf("sib%d.%s", i, rootHost),
			"type":       "CNAME",
			"ip":         []string{"198.18.11.250"},
			"time":       fmt.Sprintf("2026-11-18 09:0%d:00", i),
			"project":    "proj-siteip-chain",
			"rootDomain": rootHost,
		})
	}
	// 1 条时间最新的无关记录：根域不同，不能被链路带出来
	docs = append(docs, bson.M{
		"host":       unrelatedHost,
		"type":       "A",
		"ip":         []string{"198.18.11.251"},
		"time":       "2026-11-18 23:00:00",
		"project":    "proj-siteip-chain",
		"rootDomain": unrelatedHost,
	})
	if _, err := coll.InsertMany(context.Background(), docs); err != nil {
		t.Fatalf("seed subdomains: %v", err)
	}

	svc := NewService()
	result, err := svc.GetChain(context.Background(), chainHost)
	if err != nil {
		t.Fatalf("GetChain: %v", err)
	}

	if result.Total != 507 {
		t.Errorf("total = %d, want 507 (505 host + 2 同根域兄弟)", result.Total)
	}
	if result.Limit != 500 {
		t.Errorf("limit = %d, want 500", result.Limit)
	}
	if len(result.List) != 500 {
		t.Fatalf("chain length = %d, want capped 500", len(result.List))
	}
	// 最新一条（i=504）排第一；时间更新的无关记录不能被带出来
	if result.List[0].Time != times[504] {
		t.Errorf("chain[0].time = %q, want %q", result.List[0].Time, times[504])
	}
	for _, item := range result.List {
		if item.Host == unrelatedHost {
			t.Fatalf("unrelated rootDomain record leaked into chain: %s", item.Host)
		}
	}
	// 严格倒序
	for i := 1; i < len(result.List); i++ {
		if result.List[i-1].Time < result.List[i].Time {
			t.Fatalf("chain not sorted desc at %d: %q < %q", i, result.List[i-1].Time, result.List[i].Time)
		}
	}
	// 截断点：505 条里取最新的 500 条，最后一条应是 i=5
	if result.List[len(result.List)-1].Time != times[5] {
		t.Errorf("chain[last].time = %q, want %q", result.List[len(result.List)-1].Time, times[5])
	}
	// 兄弟子域时间更早，被截断在 500 之外
	for _, item := range result.List {
		if item.Time == "2026-11-18 09:00:00" || item.Time == "2026-11-18 09:01:00" {
			t.Errorf("older sibling %s leaked into the capped window", item.Host)
		}
	}

	// 直接传根域：整棵树（505 + 2）都算命中
	byRoot, err := svc.GetChain(context.Background(), rootHost)
	if err != nil {
		t.Fatalf("GetChain(root): %v", err)
	}
	if byRoot.Total != 507 || len(byRoot.List) != 500 {
		t.Errorf("root chain = total %d rows %d, want 507/500", byRoot.Total, len(byRoot.List))
	}

	// 不存在的 host -> 空列表（不是 nil）
	none, err := svc.GetChain(context.Background(), prefix+"-no-such-host.example.com")
	if err != nil {
		t.Fatalf("GetChain(none): %v", err)
	}
	if none.Total != 0 || len(none.List) != 0 {
		t.Errorf("unknown host = total %d rows %d, want 0/0", none.Total, len(none.List))
	}
	if none.List == nil {
		t.Errorf("empty chain is nil, want empty slice")
	}

	// 空 host 走哨兵错误，handler 才能映射成 400
	if _, err := svc.GetChain(context.Background(), "   "); !errors.Is(err, ErrEmptyHost) {
		t.Errorf("empty host err = %v, want ErrEmptyHost", err)
	}
}
