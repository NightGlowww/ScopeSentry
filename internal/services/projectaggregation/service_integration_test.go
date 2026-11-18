//go:build integration

package projectaggregation

import (
	"context"
	"errors"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/repositories/projectaggregation"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 该测试验证项目详情页 4 个 Tab 的聚合语义：$facet 拿 total+分页、分组计数、
// value/ip 为 null 时归一成 []、空项目返回空数组而不是 nil、参数错误分类。
// 需要在有 Mongo 的环境跑：
//
//	MONGODB_IP=127.0.0.1 MONGODB_PORT=27111 MONGODB_DATABASE=projectagg_test \
//	  go test -tags integration ./internal/services/projectaggregation/
func requireDB(t *testing.T) {
	t.Helper()
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
}

func seed(t *testing.T, coll string, docs []interface{}) {
	t.Helper()
	if len(docs) == 0 {
		return
	}
	if _, err := mongodb.DB.Collection(coll).InsertMany(context.Background(), docs); err != nil {
		t.Fatalf("seed %s: %v", coll, err)
	}
}

func cleanup(t *testing.T, coll string, filter bson.M) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection(coll).DeleteMany(context.Background(), filter)
	})
}

func TestProjectAggregationService(t *testing.T) {
	requireDB(t)

	ctx := context.Background()
	oid := primitive.NewObjectID()
	pid := oid.Hex()
	projectFilter := bson.M{"project": pid}

	// 项目文档
	seed(t, projectaggregation.CollProject, []interface{}{bson.M{
		"_id":            oid,
		"name":           "agg-test-project",
		"tag":            "agg",
		"scheduledTasks": false,
		"hour":           0,
		"AssetCount":     int64(4),
		"root_domains":   []string{"example.test"},
	}})
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection(projectaggregation.CollProject).DeleteMany(ctx, bson.M{"_id": oid})
	})

	// asset：80 端口 2 条（验证 count 聚合 + 组内取最近样例），443 端口 2 条，空端口 1 条（不应进端口聚合）
	seed(t, projectaggregation.CollAsset, []interface{}{
		bson.M{"host": "a.example.test", "ip": "10.0.0.1", "port": "80", "service": "http", "type": "http", "project": pid, "time": "2026-11-18 10:00:00"},
		bson.M{"host": "b.example.test", "ip": "10.0.0.2", "port": "80", "service": "http", "type": "http", "project": pid, "time": "2026-11-18 10:05:00"},
		bson.M{"host": "c.example.test", "ip": "10.0.0.3", "port": "443", "service": "https", "type": "http", "project": pid, "time": "2026-11-18 10:01:00"},
		bson.M{"host": "d.example.test", "ip": "10.0.0.4", "port": "443", "service": "unknown", "type": "other", "project": pid, "time": "2026-11-18 10:02:00"},
		bson.M{"host": "e.example.test", "ip": "10.0.0.5", "port": "", "service": "", "type": "other", "project": pid, "time": "2026-11-18 10:03:00"},
	})
	cleanup(t, projectaggregation.CollAsset, projectFilter)

	// subdomain：3 条，其中一条 value 为 null（验证归一成 []）
	seed(t, projectaggregation.CollSubdomain, []interface{}{
		bson.M{"host": "www.example.test", "type": "CNAME", "value": []string{"a.example.test"}, "ip": []string{"10.0.0.1"}, "project": pid, "rootDomain": "example.test", "time": "2026-11-18 10:00:00"},
		bson.M{"host": "api.example.test", "type": "A", "value": nil, "ip": []string{"10.0.0.2"}, "project": pid, "rootDomain": "example.test", "time": "2026-11-18 10:06:00"},
		bson.M{"host": "example.test", "type": "SOA", "value": nil, "ip": nil, "project": pid, "rootDomain": "example.test", "time": "2026-11-18 09:00:00"},
	})
	cleanup(t, projectaggregation.CollSubdomain, projectFilter)

	// vulnerability：high 2 条 + critical 1 条（验证等级分布与时间倒序分页）
	seed(t, projectaggregation.CollVuln, []interface{}{
		bson.M{"vulname": "SQL Injection", "url": "https://a.example.test/x?id=1", "level": "high", "matched": "sqlmap", "project": pid, "time": "2026-11-18 10:00:00"},
		bson.M{"vulname": "XSS", "url": "https://b.example.test/s", "level": "high", "matched": "alert(1)", "project": pid, "time": "2026-11-18 10:10:00"},
		bson.M{"vulname": "RCE", "url": "https://c.example.test/r", "level": "critical", "matched": "id", "project": pid, "time": "2026-11-18 10:20:00"},
	})
	cleanup(t, projectaggregation.CollVuln, projectFilter)

	svc := NewService()

	t.Run("info", func(t *testing.T) {
		info, err := svc.Info(ctx, models.ProjectAggregationRequest{ID: pid})
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if info.Name != "agg-test-project" || info.Tag != "agg" {
			t.Fatalf("info = %+v", info)
		}
		if len(info.RootDomains) != 1 || info.RootDomains[0] != "example.test" {
			t.Fatalf("root_domains = %v", info.RootDomains)
		}
		if info.NextTime != "" {
			t.Fatalf("next_time 应为空串（无定时任务），实际 %q", info.NextTime)
		}
	})

	t.Run("asset count", func(t *testing.T) {
		got, err := svc.AssetCount(ctx, models.ProjectAggregationRequest{ID: pid})
		if err != nil {
			t.Fatalf("AssetCount: %v", err)
		}
		if got.AssetCount != 5 || got.SubdomainCount != 3 || got.VulCount != 3 {
			t.Fatalf("count = %+v", got)
		}
		if got.URLCount != 0 || got.SensitiveCount != 0 || got.DirScanCount != 0 {
			t.Fatalf("空集合应计 0，实际 %+v", got)
		}
	})

	t.Run("vul statistics", func(t *testing.T) {
		stats, err := svc.VulStatistics(ctx, models.ProjectAggregationRequest{ID: pid})
		if err != nil {
			t.Fatalf("VulStatistics: %v", err)
		}
		if len(stats) != 2 {
			t.Fatalf("stats = %+v", stats)
		}
		// count 倒序：high=2 在前
		if stats[0].Level != "high" || stats[0].Count != 2 {
			t.Fatalf("stats[0] = %+v", stats[0])
		}
		if stats[1].Level != "critical" || stats[1].Count != 1 {
			t.Fatalf("stats[1] = %+v", stats[1])
		}
	})

	t.Run("vul data pagination", func(t *testing.T) {
		res, err := svc.VulData(ctx, models.ProjectAggregationRequest{ID: pid, PageIndex: 1, PageSize: 2})
		if err != nil {
			t.Fatalf("VulData: %v", err)
		}
		rows := res.List.([]models.ProjectAggregationVulRow)
		if res.Total != 3 || len(rows) != 2 {
			t.Fatalf("total=%d rows=%d", res.Total, len(rows))
		}
		// time 倒序：10:20 的 RCE 在最前
		if rows[0].VulName != "RCE" || rows[0].Level != "critical" {
			t.Fatalf("rows[0] = %+v", rows[0])
		}
		if rows[0].ID == "" {
			t.Fatalf("行缺少 id（前端 rowKey 依赖）")
		}
		// 第 2 页只剩 1 条
		res2, err := svc.VulData(ctx, models.ProjectAggregationRequest{ID: pid, PageIndex: 2, PageSize: 2})
		if err != nil {
			t.Fatalf("VulData page2: %v", err)
		}
		if rows2 := res2.List.([]models.ProjectAggregationVulRow); len(rows2) != 1 || res2.Total != 3 {
			t.Fatalf("page2 total=%d rows=%d", res2.Total, len(rows2))
		}
	})

	t.Run("subdomain normalize", func(t *testing.T) {
		res, err := svc.SubdomainData(ctx, models.ProjectAggregationRequest{ID: pid, PageSize: 10})
		if err != nil {
			t.Fatalf("SubdomainData: %v", err)
		}
		rows := res.List.([]models.ProjectAggregationSubdomainRow)
		if res.Total != 3 || len(rows) != 3 {
			t.Fatalf("total=%d rows=%d", res.Total, len(rows))
		}
		for _, row := range rows {
			if row.Value == nil || row.IP == nil {
				t.Fatalf("value/ip 必须是数组（前端 forEach），得到 %+v", row)
			}
		}
		// 时间倒序
		if rows[0].Host != "api.example.test" {
			t.Fatalf("rows[0].host = %s", rows[0].Host)
		}
		// 表头模糊搜索
		res2, err := svc.SubdomainData(ctx, models.ProjectAggregationRequest{ID: pid, Fq: map[string]string{"sub_host": "api."}})
		if err != nil {
			t.Fatalf("SubdomainData fq: %v", err)
		}
		if res2.Total != 1 {
			t.Fatalf("fq sub_host 命中 %d，期望 1", res2.Total)
		}
	})

	t.Run("port groups", func(t *testing.T) {
		res, err := svc.PortData(ctx, models.ProjectAggregationRequest{ID: pid})
		if err != nil {
			t.Fatalf("PortData: %v", err)
		}
		rows := res.List.([]models.ProjectAggregationPortRow)
		if res.Total != 2 || len(rows) != 2 {
			t.Fatalf("total=%d rows=%d（空端口不应进聚合）", res.Total, len(rows))
		}
		byPort := map[string]models.ProjectAggregationPortRow{}
		for _, row := range rows {
			byPort[row.Port] = row
		}
		if byPort["80"].Count != 2 {
			t.Fatalf("port 80 count = %d", byPort["80"].Count)
		}
		// 组内样例取最近一条：80 端口 10:05 的 b 主机
		if byPort["80"].Host != "b.example.test" {
			t.Fatalf("port 80 样例 host = %s", byPort["80"].Host)
		}
		// 443 端口最新一条是 10:02 的端口级资产（service=unknown），样例字段取的就是它
		if byPort["443"].Service != "unknown" {
			t.Fatalf("port 443 样例 service = %s", byPort["443"].Service)
		}
	})

	t.Run("service groups", func(t *testing.T) {
		res, err := svc.ServiceData(ctx, models.ProjectAggregationRequest{ID: pid})
		if err != nil {
			t.Fatalf("ServiceData: %v", err)
		}
		rows := res.List.([]models.ProjectAggregationServiceRow)
		byService := map[string]models.ProjectAggregationServiceRow{}
		for _, row := range rows {
			byService[row.Service] = row
		}
		if byService["http"].Count != 2 || byService["https"].Count != 1 || byService["unknown"].Count != 1 {
			t.Fatalf("service groups = %+v", rows)
		}
	})

	t.Run("time range", func(t *testing.T) {
		res, err := svc.SubdomainData(ctx, models.ProjectAggregationRequest{
			ID: pid, StartTime: "2026-11-18 10:00:00", EndTime: "2026-11-18 10:10:00",
		})
		if err != nil {
			t.Fatalf("SubdomainData time range: %v", err)
		}
		if res.Total != 2 {
			t.Fatalf("时间范围内应 2 条，实际 %d", res.Total)
		}
		// 只给日期时按整天补齐
		res2, err := svc.SubdomainData(ctx, models.ProjectAggregationRequest{ID: pid, StartTime: "2026-11-18", EndTime: "2026-11-18"})
		if err != nil {
			t.Fatalf("SubdomainData date only: %v", err)
		}
		if res2.Total != 3 {
			t.Fatalf("整天范围应 3 条，实际 %d", res2.Total)
		}
	})

	t.Run("empty project", func(t *testing.T) {
		other := primitive.NewObjectID()
		seed(t, projectaggregation.CollProject, []interface{}{bson.M{
			"_id": other, "name": "empty-project", "tag": "", "scheduledTasks": false,
			"hour": 0, "AssetCount": int64(0), "root_domains": []string{},
		}})
		t.Cleanup(func() {
			_, _ = mongodb.DB.Collection(projectaggregation.CollProject).DeleteMany(ctx, bson.M{"_id": other})
		})
		res, err := svc.PortData(ctx, models.ProjectAggregationRequest{ID: other.Hex()})
		if err != nil {
			t.Fatalf("PortData empty: %v", err)
		}
		if res.Total != 0 {
			t.Fatalf("空项目 total = %d", res.Total)
		}
		if rows := res.List.([]models.ProjectAggregationPortRow); rows == nil || len(rows) != 0 {
			t.Fatalf("空项目应返回空数组而不是 nil，实际 %#v", rows)
		}
		count, err := svc.AssetCount(ctx, models.ProjectAggregationRequest{ID: other.Hex()})
		if err != nil {
			t.Fatalf("AssetCount empty: %v", err)
		}
		if count.AssetCount != 0 || count.SubdomainCount != 0 {
			t.Fatalf("空项目计数 = %+v", count)
		}
	})

	t.Run("error mapping", func(t *testing.T) {
		if _, err := svc.PortData(ctx, models.ProjectAggregationRequest{ID: "not-an-objectid"}); !errors.Is(err, ErrInvalidProjectID) {
			t.Fatalf("非法 id 错误 = %v", err)
		}
		missing := primitive.NewObjectID().Hex()
		if _, err := svc.Info(ctx, models.ProjectAggregationRequest{ID: missing}); !errors.Is(err, ErrProjectNotFound) {
			t.Fatalf("缺失项目错误 = %v", err)
		}
		if _, err := svc.PortData(ctx, models.ProjectAggregationRequest{ID: pid, PageSize: 201}); !errors.Is(err, ErrPageSizeTooLarge) {
			t.Fatalf("pageSize 上限错误 = %v", err)
		}
		if _, err := svc.VulData(ctx, models.ProjectAggregationRequest{ID: pid, StartTime: "18/11/2026"}); !errors.Is(err, ErrInvalidTimeRange) {
			t.Fatalf("时间格式错误 = %v", err)
		}
	})

	t.Run("filter.project 等价于 id", func(t *testing.T) {
		res, err := svc.SubdomainData(ctx, models.ProjectAggregationRequest{
			Filter: map[string][]interface{}{"project": {pid}},
		})
		if err != nil {
			t.Fatalf("SubdomainData by filter: %v", err)
		}
		if res.Total != 3 {
			t.Fatalf("filter.project 命中 %d，期望 3", res.Total)
		}
	})
}
