//go:build integration

package ip

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

// seedIPAssets 插入测试资产并注册清理。
// host 用本次运行的前缀，ip 用本次运行的 198.18.x. 前缀，两边都只删自己的数据。
func seedIPAssets(t *testing.T, hostPrefix, ipPrefix string, docs []interface{}) {
	t.Helper()
	coll := mongodb.DB.Collection("asset")
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.M{"$or": bson.A{
			bson.M{"host": bson.M{"$regex": "^" + regexp.QuoteMeta(hostPrefix)}},
			bson.M{"ip": bson.M{"$regex": "^" + regexp.QuoteMeta(ipPrefix)}},
		}})
	})
	if len(docs) == 0 {
		return
	}
	if _, err := coll.InsertMany(context.Background(), docs); err != nil {
		t.Fatalf("seed assets: %v", err)
	}
}

// IP 列表要按 ip 聚合：端口/域名去重计数、摘要只带前 20 个、
// 计数用的是截断前的完整集合。这里三种情况都断言到。
func TestIPListAggregation(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	hostPrefix := fmt.Sprintf("ipagg-%d", time.Now().UnixNano())
	// 每个 run 独占一个 198.18.x. 网段，避免和别的测试/真实数据撞
	ipPrefix := fmt.Sprintf("198.18.%d.", time.Now().UnixNano()%200+20)
	ip1 := ipPrefix + "1"
	ip2 := ipPrefix + "2"
	ip3 := ipPrefix + "3"

	hostA := hostPrefix + "-a.example.com"
	hostB := hostPrefix + "-b.example.com"
	hostC := hostPrefix + "-c.example.com"

	docs := []interface{}{
		// ip1：两个端口、两个域名、三条资产
		bson.M{"host": hostB, "ip": ip1, "port": "443", "service": "https", "time": "2026-11-18 12:00:00"},
		bson.M{"host": hostA, "ip": ip1, "port": "443", "service": "https", "time": "2026-11-18 11:00:00"},
		bson.M{"host": hostA, "ip": ip1, "port": "80", "service": "http", "time": "2026-11-18 10:00:00"},
		// ip2：单端口单域名
		bson.M{"host": hostC, "ip": ip2, "port": "22", "service": "ssh", "time": "2026-11-18 09:00:00"},
	}
	// ip3：25 个端口，验证「前 20 个 + 全量计数」
	for i := 0; i < 25; i++ {
		docs = append(docs, bson.M{
			"host":    hostA,
			"ip":      ip3,
			"port":    fmt.Sprintf("%d", 1000+i),
			"service": "tcp",
			"time":    fmt.Sprintf("2026-11-18 08:%02d:00", i),
		})
	}
	seedIPAssets(t, hostPrefix, ipPrefix, docs)

	svc := NewReadModelService()

	result, err := svc.ListIPs(context.Background(), models.IPListRequest{
		IP:        ipPrefix,
		PageIndex: 1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("ListIPs: %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("total = %d, want 3", result.Total)
	}
	if len(result.List) != 3 {
		t.Fatalf("rows = %d, want 3", len(result.List))
	}

	byIP := map[string]models.IPRow{}
	for _, row := range result.List {
		byIP[row.IP] = row
	}

	// 倒序：ip1 的 lastSeen(12:00) 最新
	if result.List[0].IP != ip1 {
		t.Errorf("first row = %s, want %s (newest lastSeen)", result.List[0].IP, ip1)
	}

	row1, ok := byIP[ip1]
	if !ok {
		t.Fatalf("ip1 %s missing from result", ip1)
	}
	if row1.AssetCount != 3 {
		t.Errorf("ip1 assetCount = %d, want 3", row1.AssetCount)
	}
	if row1.PortCount != 2 {
		t.Errorf("ip1 portCount = %d, want 2 (443/https, 80/http)", row1.PortCount)
	}
	ports := map[string]string{}
	for _, p := range row1.Ports {
		ports[p.Port] = p.Protocol
	}
	if ports["443"] != "https" || ports["80"] != "http" {
		t.Errorf("ip1 ports = %+v, want 443:https / 80:http", row1.Ports)
	}
	if row1.DomainCount != 2 {
		t.Errorf("ip1 domainCount = %d, want 2", row1.DomainCount)
	}
	if len(row1.Domains) != 2 || row1.Domains[0] != hostA || row1.Domains[1] != hostB {
		t.Errorf("ip1 domains = %v, want [%s %s] (asc)", row1.Domains, hostA, hostB)
	}
	if row1.LastSeen != "2026-11-18 12:00:00" {
		t.Errorf("ip1 lastSeen = %q, want 2026-11-18 12:00:00", row1.LastSeen)
	}

	// ip3：portCount 是全量 25，ports 只带排序后的前 20 个
	row3, ok := byIP[ip3]
	if !ok {
		t.Fatalf("ip3 %s missing from result", ip3)
	}
	if row3.PortCount != 25 {
		t.Errorf("ip3 portCount = %d, want 25", row3.PortCount)
	}
	if len(row3.Ports) != 20 {
		t.Fatalf("ip3 ports len = %d, want capped 20", len(row3.Ports))
	}
	if row3.Ports[0].Port != "1000" || row3.Ports[19].Port != "1019" {
		t.Errorf("ip3 ports window = %s..%s, want 1000..1019", row3.Ports[0].Port, row3.Ports[19].Port)
	}
	if row3.DomainCount != 1 || len(row3.Domains) != 1 || row3.Domains[0] != hostA {
		t.Errorf("ip3 domains = %v (count %d), want [%s]", row3.Domains, row3.DomainCount, hostA)
	}

	// 前缀收窄：只按 ip2 查
	byPrefix, err := svc.ListIPs(context.Background(), models.IPListRequest{
		IP:        ip2,
		PageIndex: 1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("ListIPs(prefix): %v", err)
	}
	if byPrefix.Total != 1 || len(byPrefix.List) != 1 || byPrefix.List[0].IP != ip2 {
		t.Fatalf("prefix filter = total %d rows %+v, want only %s", byPrefix.Total, byPrefix.List, ip2)
	}
	if byPrefix.List[0].Ports[0].Protocol != "ssh" {
		t.Errorf("ip2 protocol = %q, want ssh", byPrefix.List[0].Ports[0].Protocol)
	}

	// keyword 子串匹配 + 分页
	page1, err := svc.ListIPs(context.Background(), models.IPListRequest{IP: ipPrefix, PageIndex: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("ListIPs(page1): %v", err)
	}
	page2, err := svc.ListIPs(context.Background(), models.IPListRequest{IP: ipPrefix, PageIndex: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("ListIPs(page2): %v", err)
	}
	if page1.Total != 3 || len(page1.List) != 2 {
		t.Errorf("page1 = total %d rows %d, want 3/2", page1.Total, len(page1.List))
	}
	if page2.Total != 3 || len(page2.List) != 1 {
		t.Errorf("page2 = total %d rows %d, want 3/1", page2.Total, len(page2.List))
	}
	if page1.List[0].IP == page2.List[0].IP {
		t.Errorf("page1/page2 overlap on %s", page1.List[0].IP)
	}
}

// IP 反查域名要同时走 asset.host 和 subdomain.ip，并标出来源；
// 只查一边会漏掉「只解析过、没扫到」的域名。
func TestIPDomainsByIP(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	hostPrefix := fmt.Sprintf("ipdom-%d", time.Now().UnixNano())
	ipPrefix := fmt.Sprintf("198.19.%d.", time.Now().UnixNano()%200+20)
	targetIP := ipPrefix + "9"

	domainA := hostPrefix + "-a.example.com"
	domainB := hostPrefix + "-b.example.com"

	assetColl := mongodb.DB.Collection("asset")
	subColl := mongodb.DB.Collection("subdomain")
	t.Cleanup(func() {
		_, _ = assetColl.DeleteMany(context.Background(), bson.M{"$or": bson.A{
			bson.M{"host": bson.M{"$regex": "^" + regexp.QuoteMeta(hostPrefix)}},
			bson.M{"ip": bson.M{"$regex": "^" + regexp.QuoteMeta(ipPrefix)}},
		}})
		_, _ = subColl.DeleteMany(context.Background(), bson.M{
			"host": bson.M{"$regex": "^" + regexp.QuoteMeta(hostPrefix)},
		})
	})

	// domainA 同时出现在 asset 和 subdomain（来源要合并成两条）
	if _, err := assetColl.InsertMany(context.Background(), []interface{}{
		bson.M{"host": domainA, "ip": targetIP, "port": "443", "service": "https", "time": "2026-11-18 10:00:00"},
	}); err != nil {
		t.Fatalf("seed assets: %v", err)
	}
	if _, err := subColl.InsertMany(context.Background(), []interface{}{
		bson.M{"host": domainB, "ip": []string{targetIP}, "time": "2026-11-18 11:00:00", "type": "A"},
		bson.M{"host": domainA, "ip": []string{targetIP}, "time": "2026-11-18 12:00:00", "type": "A"},
		// 无关子域：IP 不同，不应出现
		bson.M{"host": hostPrefix + "-other.example.com", "ip": []string{"203.0.113.9"}, "time": "2026-11-18 13:00:00"},
	}); err != nil {
		t.Fatalf("seed subdomains: %v", err)
	}

	svc := NewReadModelService()
	result, err := svc.ListDomainsByIP(context.Background(), targetIP)
	if err != nil {
		t.Fatalf("ListDomainsByIP: %v", err)
	}

	if result.Total != 2 || len(result.List) != 2 {
		t.Fatalf("domains = total %d rows %d, want 2/2: %+v", result.Total, len(result.List), result.List)
	}
	// 按域名升序
	if result.List[0].Domain != domainA || result.List[1].Domain != domainB {
		t.Fatalf("domains order = %s,%s, want %s,%s", result.List[0].Domain, result.List[1].Domain, domainA, domainB)
	}
	if got := result.List[0].Sources; len(got) != 2 || got[0] != "asset" || got[1] != "subdomain" {
		t.Errorf("domainA sources = %v, want [asset subdomain]", got)
	}
	if result.List[0].LastSeen != "2026-11-18 12:00:00" {
		t.Errorf("domainA lastSeen = %q, want 2026-11-18 12:00:00", result.List[0].LastSeen)
	}
	if got := result.List[1].Sources; len(got) != 1 || got[0] != "subdomain" {
		t.Errorf("domainB sources = %v, want [subdomain]", got)
	}

	// 没有任何资产的 IP -> 空列表而不是报错
	none, err := svc.ListDomainsByIP(context.Background(), ipPrefix+"250")
	if err != nil {
		t.Fatalf("ListDomainsByIP(none): %v", err)
	}
	if none.Total != 0 || len(none.List) != 0 {
		t.Errorf("unknown ip = total %d rows %d, want 0/0", none.Total, len(none.List))
	}

	// 空 ip 走哨兵错误
	if _, err := svc.ListDomainsByIP(context.Background(), "  "); !errors.Is(err, ErrEmptyIP) {
		t.Errorf("empty ip err = %v, want ErrEmptyIP", err)
	}
}
