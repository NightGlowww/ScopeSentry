//go:build integration

package assetoverview

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
)

// TestAssetOverviewCount 验证总览接口把各资产族数量一次返回，且与种子数据一致。
//
// 物理集合用 EstimatedDocumentCount，天然是整库计数；测试库可能同时跑其它包，
// 所以断言用「调用前快照 + 本次种子数 <= 返回值」的单调口径，而不是硬编码绝对值。
func TestAssetOverviewCount(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-overview-" + suffix
	hostPrefix := "overview-" + suffix + "-"

	// 每个集合种 2 条，字段尽量贴近真实读模型（按 project 精确计数，避免并行包的干扰）
	seed := func(collection string, docs []interface{}) {
		coll := mongodb.DB.Collection(collection)
		t.Cleanup(func() {
			_, _ = coll.DeleteMany(context.Background(), bson.M{"project": project})
		})
		if len(docs) == 0 {
			return
		}
		if _, err := coll.InsertMany(context.Background(), docs); err != nil {
			t.Fatalf("seed %s: %v", collection, err)
		}
	}

	assetDocs := []interface{}{
		bson.M{"host": hostPrefix + "a.example.com", "ip": "10.30.0.1", "port": "443", "project": project, "time": "2026-11-18 10:00:00"},
		bson.M{"host": hostPrefix + "b.example.com", "ip": "10.30.0.2", "port": "443", "project": project, "time": "2026-11-18 10:00:01"},
	}
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		assetDocs = append(assetDocs, bson.M{
			"host": fmt.Sprintf("%scert%d.example.com", hostPrefix, i), "ip": fmt.Sprintf("10.30.1.%d", i+1), "port": "443",
			"project": project, "time": "2026-11-18 10:00:02",
			"tls": bson.M{
				"probestatus": true,
				"certificateresponse": bson.M{
					"subjectdn":       fmt.Sprintf("CN=cert%d.example.com", i),
					"issuerdn":        "CN=Overview CA",
					"serial":          fmt.Sprintf("OV-%d", i),
					"notbefore":       now.Add(-time.Hour),
					"notafter":        now.Add(24 * time.Hour),
					"subjectan":       []string{fmt.Sprintf("cert%d.example.com", i)},
					"fingerprinthash": bson.M{"sha256": fmt.Sprintf("fp-overview-%d", i)},
				},
			},
		})
	}
	seed("asset", assetDocs)
	seed("subdomain", []interface{}{
		bson.M{"host": hostPrefix + "sub1.example.com", "project": project, "time": "2026-11-18 10:00:00"},
		bson.M{"host": hostPrefix + "sub2.example.com", "project": project, "time": "2026-11-18 10:00:01"},
	})
	seed("UrlScan", []interface{}{
		bson.M{"url": "https://" + hostPrefix + "u1", "project": project, "time": "2026-11-18 10:00:00"},
		bson.M{"url": "https://" + hostPrefix + "u2", "project": project, "time": "2026-11-18 10:00:01"},
	})
	seed("app", []interface{}{
		bson.M{"name": hostPrefix + "app1", "project": project, "time": "2026-11-18 10:00:00"},
		bson.M{"name": hostPrefix + "app2", "project": project, "time": "2026-11-18 10:00:01"},
	})
	seed("mp", []interface{}{
		bson.M{"name": hostPrefix + "mp1", "project": project, "time": "2026-11-18 10:00:00"},
		bson.M{"name": hostPrefix + "mp2", "project": project, "time": "2026-11-18 10:00:01"},
	})
	seed("vulnerability", []interface{}{
		bson.M{"name": hostPrefix + "vuln1", "project": project, "time": "2026-11-18 10:00:00"},
		bson.M{"name": hostPrefix + "vuln2", "project": project, "time": "2026-11-18 10:00:01"},
	})
	seed("SensitiveResult", []interface{}{
		bson.M{"url": "https://" + hostPrefix + "s1", "project": project, "time": "2026-11-18 10:00:00"},
		bson.M{"url": "https://" + hostPrefix + "s2", "project": project, "time": "2026-11-18 10:00:01"},
	})

	companyA := "OverviewAcme-" + suffix
	companyB := "OverviewBeta-" + suffix
	rootDocs := []interface{}{
		bson.M{"domain": hostPrefix + "a1.acme.test", "company": companyA, "icp": "京ICP备" + suffix + "号", "project": project, "time": "2026-11-18 10:00:00"},
		bson.M{"domain": hostPrefix + "b1.beta.test", "company": companyB, "icp": "沪ICP备" + suffix + "号", "project": project, "time": "2026-11-18 10:00:01"},
	}
	rootColl := mongodb.DB.Collection("RootDomain")
	t.Cleanup(func() {
		_, _ = rootColl.DeleteMany(context.Background(), bson.M{"$or": bson.A{
			bson.M{"project": project},
			bson.M{"domain": bson.M{"$regex": "^" + regexp.QuoteMeta(hostPrefix)}},
		}})
	})
	if _, err := rootColl.InsertMany(context.Background(), rootDocs); err != nil {
		t.Fatalf("seed RootDomain: %v", err)
	}

	result, err := NewService().Count(context.Background(), project)
	if err != nil {
		t.Fatalf("count overview: %v", err)
	}
	if result == nil {
		t.Fatal("nil overview result")
	}

	want := map[string]int64{
		"asset":         4,
		"subdomain":     2,
		"url":           2,
		"app":           2,
		"mp":            2,
		"vulnerability": 2,
		"sensitive":     2,
	}
	got := map[string]int64{
		"asset": result.Asset, "subdomain": result.Subdomain, "url": result.URL,
		"app": result.App, "mp": result.MP, "vulnerability": result.Vulnerability,
		"sensitive": result.Sensitive,
	}
	for family, expected := range want {
		if got[family] != expected {
			t.Fatalf("%s = %d, want %d (project-scoped exact count)", family, got[family], expected)
		}
	}
	// cert/cip 是全局读模型（没有 project 维度），只保证覆盖本次种子
	if result.Cert < 2 {
		t.Fatalf("cert = %d, want >= 2", result.Cert)
	}
	if result.CIP < 2 {
		t.Fatalf("cip = %d, want >= 2", result.CIP)
	}

	// 空 project 走估算口径：不报错且不小于本次种子
	global, err := NewService().Count(context.Background(), "")
	if err != nil {
		t.Fatalf("global count: %v", err)
	}
	if global.Asset < 4 || global.Subdomain < 2 {
		t.Fatalf("global counts too small: %+v", global)
	}
}
