//go:build integration

package cip

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// seedRootDomains 插入测试根域名，按 project/域名前缀清理，只删本次运行的数据。
func seedRootDomains(t *testing.T, project, domainPrefix string, docs []interface{}) {
	t.Helper()
	coll := mongodb.DB.Collection("RootDomain")
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"$or": bson.A{
			bson.M{"project": project},
			bson.M{"domain": bson.M{"$regex": "^" + regexp.QuoteMeta(domainPrefix)}},
		}})
	})
	if len(docs) == 0 {
		return
	}
	if _, err := coll.InsertMany(context.Background(), docs); err != nil {
		t.Fatalf("seed root domains: %v", err)
	}
}

func findCIPRow(t *testing.T, rows []models.CIPRow, company string) *models.CIPRow {
	t.Helper()
	for i := range rows {
		if rows[i].Company == company {
			return &rows[i]
		}
	}
	return nil
}

// 分组统计 / 样例 / keyword / company 过滤 / 项目隔离 / 无主体排除 / 详情分页
func TestCIPListAggregation(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-cip-" + suffix
	otherProject := project + "-other"
	domainPrefix := "cip-" + suffix + "-"

	companyA := "Acme-" + suffix
	companyB := "Beta-" + suffix
	icpA := "京ICP备" + suffix + "号-1"
	icpB := "沪ICP备" + suffix + "号-2"

	rootDoc := func(project, domain, company, icp, tm string) bson.M {
		return bson.M{
			"domain": domain, "company": company, "icp": icp,
			"project": project, "time": tm, "tags": []string{"test"},
		}
	}

	docs := []interface{}{
		rootDoc(project, domainPrefix+"a1.acme.test", companyA, icpA, "2026-11-18 10:03:00"),
		rootDoc(project, domainPrefix+"a2.acme.test", companyA, icpA, "2026-11-18 10:02:00"),
		rootDoc(project, domainPrefix+"a3.acme.test", companyA, icpA, "2026-11-18 10:01:00"),
		rootDoc(project, domainPrefix+"b1.beta.test", companyB, icpB, "2026-11-18 10:05:00"),
		rootDoc(project, domainPrefix+"b2.beta.test", companyB, icpB, "2026-11-18 10:04:00"),
		// 空 company 不是可展示的主体
		rootDoc(project, domainPrefix+"orphan.test", "", "", "2026-11-18 10:00:00"),
		// 另一个项目的同名主体：不能混进 project 过滤结果
		rootDoc(otherProject, domainPrefix+"other.acme.test", companyA, icpA, "2026-11-18 10:06:00"),
	}
	seedRootDomains(t, project, domainPrefix, docs)
	seedRootDomains(t, otherProject, domainPrefix, nil)

	svc := NewService()
	ctx := context.Background()

	res, err := svc.List(ctx, models.CIPListRequest{Project: project, PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list cip: %v", err)
	}
	if res.Total != 2 || len(res.List) != 2 {
		t.Fatalf("total/rows = %d/%d, want 2/2: %+v", res.Total, len(res.List), res.List)
	}
	// 域名数倒序：Acme(3) 在 Beta(2) 前面
	acme := findCIPRow(t, res.List, companyA)
	if acme == nil || acme.DomainCount != 3 {
		t.Fatalf("acme row = %+v", acme)
	}
	if acme.ICP != icpA || len(acme.ICPs) != 1 {
		t.Fatalf("acme icp = %q/%v", acme.ICP, acme.ICPs)
	}
	if len(acme.Samples) != 3 {
		t.Fatalf("acme samples = %v, want 3", acme.Samples)
	}
	beta := findCIPRow(t, res.List, companyB)
	if beta == nil || beta.DomainCount != 2 {
		t.Fatalf("beta row = %+v", beta)
	}

	// company 精确过滤
	onlyA, err := svc.List(ctx, models.CIPListRequest{Project: project, Company: companyA, PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list by company: %v", err)
	}
	if onlyA.Total != 1 || onlyA.List[0].Company != companyA {
		t.Fatalf("company filter = %+v", onlyA)
	}

	// keyword 命中主体名 / ICP 号 / 域名
	for _, keyword := range []string{"beta", icpA, "a2.acme"} {
		got, err := svc.List(ctx, models.CIPListRequest{Project: project, Keyword: keyword, PageIndex: 1, PageSize: 200})
		if err != nil {
			t.Fatalf("list keyword %q: %v", keyword, err)
		}
		if got.Total != 1 {
			t.Fatalf("keyword %q total = %d, want 1: %+v", keyword, got.Total, got.List)
		}
	}

	// 详情：按域名反查主体，域名列表分页。
	// 带 project 时只统计该项目（3 个域名），与列表口径一致。
	detail, err := svc.Detail(ctx, models.CIPDetailRequest{Domain: domainPrefix + "a1.acme.test", Project: project, PageIndex: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail == nil || detail.Company != companyA || detail.Total != 3 {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.List) != 2 {
		t.Fatalf("detail page1 rows = %d, want 2", len(detail.List))
	}
	if detail.List[0].Domain != domainPrefix+"a1.acme.test" {
		t.Fatalf("detail order = %+v, want newest first", detail.List)
	}
	if detail.PageIndex != 1 || detail.PageSize != 2 {
		t.Fatalf("detail page = %d/%d", detail.PageIndex, detail.PageSize)
	}

	detail2, err := svc.Detail(ctx, models.CIPDetailRequest{Domain: domainPrefix + "a1.acme.test", Project: project, PageIndex: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("detail page2: %v", err)
	}
	if detail2.Total != 3 || len(detail2.List) != 1 {
		t.Fatalf("detail page2 = %+v", detail2)
	}

	// 不带 project：同一主体在全部项目里的域名（测试里另有 1 个 other project 域名）
	globalDetail, err := svc.Detail(ctx, models.CIPDetailRequest{Domain: domainPrefix + "a1.acme.test", PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("detail global: %v", err)
	}
	if globalDetail == nil || globalDetail.Total != 4 {
		t.Fatalf("global detail = %+v", globalDetail)
	}

	// 无主体：单条返回，不硬凑主体
	orphan, err := svc.Detail(ctx, models.CIPDetailRequest{Domain: domainPrefix + "orphan.test"})
	if err != nil {
		t.Fatalf("detail orphan: %v", err)
	}
	if orphan == nil || orphan.Company != "" || orphan.Total != 1 || len(orphan.List) != 1 {
		t.Fatalf("orphan detail = %+v", orphan)
	}

	// 不存在
	missing, err := svc.Detail(ctx, models.CIPDetailRequest{Domain: "no-such-" + suffix + ".invalid"})
	if err != nil {
		t.Fatalf("detail missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("missing detail = %+v, want nil", missing)
	}
}
