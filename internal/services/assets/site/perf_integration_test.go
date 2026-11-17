//go:build integration

package site

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	ipsvc "github.com/Autumn-27/ScopeSentry/internal/services/assets/ip"
	"go.mongodb.org/mongo-driver/bson"
)

// 性能回归：ARL-Next 对齐新增的读模型必须在大集合上可用。
// 默认跳过，CI 不跑；本地/发版前用 PERF=1 跑一次，结果记录进 docs。
func TestSiteIPReadModelPerformance(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if os.Getenv("PERF") == "" {
		t.Skip("set PERF=1 to run the 20k-document performance check")
	}
	ctx := context.Background()
	prefix := fmt.Sprintf("perf-%d", time.Now().UnixNano())
	const hosts = 4000
	const assets = 20000
	const subs = 6000

	cleanup := func() {
		_, _ = mongodb.DB.Collection("asset").DeleteMany(context.Background(), bson.M{"project": prefix})
		_, _ = mongodb.DB.Collection("subdomain").DeleteMany(context.Background(), bson.M{"project": prefix})
	}
	cleanup()
	t.Cleanup(cleanup)

	now := time.Now()
	batch := make([]interface{}, 0, 2000)
	flush := func(coll string, force bool) {
		if len(batch) == 0 || (!force && len(batch) < 2000) {
			return
		}
		if _, err := mongodb.DB.Collection(coll).InsertMany(ctx, batch); err != nil {
			t.Fatalf("seed %s: %v", coll, err)
		}
		batch = batch[:0]
	}

	for i := 0; i < assets; i++ {
		host := fmt.Sprintf("h%d.%s.example.com", i%hosts, prefix)
		batch = append(batch, bson.M{
			"host": host, "ip": fmt.Sprintf("198.18.%d.%d", (i/hosts)%250, i%250),
			// asset 有 unique(host,port)：每个 host 分到 5 个不同端口
			"port": fmt.Sprintf("%d", 80+(i/hosts)*100), "service": "https",
			"title": "perf site", "statuscode": 200, "url": "https://" + host + "/",
			"project": prefix, "time": now.Add(-time.Duration(i) * time.Second).Format("2006-01-02 15:04:05"),
			"rootDomain": prefix + ".example.com",
		})
		flush("asset", false)
	}
	flush("asset", true)

	for i := 0; i < subs; i++ {
		batch = append(batch, bson.M{
			"host": fmt.Sprintf("h%d.%s.example.com", i%hosts, prefix),
			"type": "A", "value": []string{fmt.Sprintf("198.18.0.%d", i%250)},
			"ip":      []string{fmt.Sprintf("198.18.0.%d", i%250)},
			"time":    now.Add(-time.Duration(i) * time.Second).Format("2006-01-02 15:04:05"),
			"project": prefix, "rootDomain": prefix + ".example.com", "taskName": "perf",
		})
		flush("subdomain", false)
	}
	flush("subdomain", true)

	siteSvc := NewService()
	ipService := ipsvc.NewReadModelService()

	start := time.Now()
	res, err := siteSvc.ListSites(ctx, models.SiteListRequest{Project: prefix, PageIndex: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListSites: %v", err)
	}
	siteCost := time.Since(start)
	if len(res.List) != 20 || res.Total == 0 {
		t.Fatalf("unexpected ListSites result: rows=%d total=%d", len(res.List), res.Total)
	}

	start = time.Now()
	ipRes, err := ipService.ListIPs(ctx, models.IPListRequest{Keyword: "198.18.", PageIndex: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListIPs: %v", err)
	}
	ipCost := time.Since(start)
	if len(ipRes.List) != 20 {
		t.Fatalf("unexpected ListIPs rows: %d", len(ipRes.List))
	}

	start = time.Now()
	chain, err := siteSvc.GetChain(ctx, fmt.Sprintf("h1.%s.example.com", prefix))
	if err != nil {
		t.Fatalf("GetChain: %v", err)
	}
	chainCost := time.Since(start)
	if len(chain.List) == 0 || len(chain.List) > 500 {
		t.Fatalf("unexpected chain size: %d", len(chain.List))
	}

	t.Logf("20k assets / 6k subdomains: site list=%s (total=%d), ip list=%s (total=%d), chain=%s (rows=%d)",
		siteCost, res.Total, ipCost, ipRes.Total, chainCost, len(chain.List))

	if siteCost > 800*time.Millisecond {
		t.Fatalf("site list too slow: %s", siteCost)
	}
	if ipCost > 800*time.Millisecond {
		t.Fatalf("ip list too slow: %s", ipCost)
	}
	if chainCost > 400*time.Millisecond {
		t.Fatalf("chain too slow: %s", chainCost)
	}
}
