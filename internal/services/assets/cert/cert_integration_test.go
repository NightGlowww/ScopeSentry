//go:build integration

package cert

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

// tlsCertDoc 构造扫描端真实写入的 tls.certificateresponse 形状。
// tlsx 的 Response/CertificateResponse 没有 bson tag，嵌入指针结构体被
// mongo-driver 编码成 certificateresponse 子文档，字段名是 Go 字段名小写。
func tlsCertDoc(subject, issuer, serial, fingerprint string, notBefore, notAfter time.Time, san ...string) bson.M {
	return bson.M{
		"subjectdn": subject,
		"subjectcn": subject,
		"issuerdn":  issuer,
		"issuercn":  issuer,
		"serial":    serial,
		"notbefore": notBefore,
		"notafter":  notAfter,
		"subjectan": san,
		"fingerprinthash": bson.M{
			"sha256": fingerprint,
			"sha1":   fingerprint + "-sha1",
			"md5":    fingerprint + "-md5",
		},
	}
}

func tlsAssetDoc(project, host, ip, port string, cert bson.M) bson.M {
	return bson.M{
		"host":    host,
		"ip":      ip,
		"port":    port,
		"project": project,
		"time":    "2026-11-18 10:00:00",
		"tls": bson.M{
			"host":                host,
			"port":                port,
			"probestatus":         true,
			"certificateresponse": cert,
		},
	}
}

// seedCertAssets 插入测试资产，按 project/主机前缀清理，只删本次运行的数据。
func seedCertAssets(t *testing.T, project, hostPrefix string, docs []interface{}) {
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

func findRowBySubject(t *testing.T, rows []models.CertRow, subject string) *models.CertRow {
	t.Helper()
	for i := range rows {
		if rows[i].Subject == subject {
			return &rows[i]
		}
	}
	return nil
}

// 合并计数 / 过期过滤 / 排序 / keyword / 详情 / 项目隔离 / legacy tlsdata 兼容
func TestCertListAggregation(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-cert-" + suffix
	otherProject := project + "-other"
	hostPrefix := "cert-" + suffix + "-"

	now := time.Now().UTC()
	sharedNotAfter := now.Add(365 * 24 * time.Hour)
	expiredNotAfter := now.Add(-48 * time.Hour)
	legacyNotAfter := now.Add(100 * time.Hour)

	sharedCert := tlsCertDoc(
		"CN=shared.example.com", "CN=Test CA", "01AB", "fp-shared",
		now.Add(-24*time.Hour), sharedNotAfter,
		"shared.example.com", "www.shared.example.com",
	)
	expiredCert := tlsCertDoc(
		"CN=expired.example.com", "CN=Old CA", "02CD", "fp-expired",
		now.Add(-2*365*24*time.Hour), expiredNotAfter,
		"expired.example.com",
	)
	legacyNotAfterText := legacyNotAfter.Format(time.RFC3339)
	legacyBeforeText := now.Add(-24 * time.Hour).Format(time.RFC3339)

	docs := []interface{}{
		tlsAssetDoc(project, hostPrefix+"a.shared.example.com", "10.10.0.1", "443", sharedCert),
		tlsAssetDoc(project, hostPrefix+"b.shared.example.com", "10.10.0.2", "8443", sharedCert),
		tlsAssetDoc(project, hostPrefix+"expired.example.com", "10.10.0.3", "443", expiredCert),
		// 服务端 legacy 模型形状：tlsdata 是 map[string]string，日期是字符串
		bson.M{
			"host": hostPrefix + "legacy.example.com", "ip": "10.10.0.4", "port": "443",
			"project": project, "time": "2026-11-18 10:00:00",
			"tlsdata": bson.M{
				"subject_dn":         "CN=legacy.example.com",
				"subject_cn":         "legacy.example.com",
				"issuer_dn":          "CN=Legacy CA",
				"issuer_cn":          "Legacy CA",
				"serial":             "LEG-1",
				"not_before":         legacyBeforeText,
				"not_after":          legacyNotAfterText,
				"subject_an":         "legacy.example.com, www.legacy.example.com",
				"fingerprint_sha256": "fp-legacy",
			},
		},
		// 没有 TLS 字段的资产不能造出空证书
		bson.M{"host": hostPrefix + "plain.example.com", "ip": "10.10.0.5", "port": "80",
			"project": project, "time": "2026-11-18 10:00:00"},
	}
	// 另一个项目的同一张证书：不能混进本次统计
	docs = append(docs, tlsAssetDoc(otherProject, hostPrefix+"other.example.com", "10.10.9.9", "443", sharedCert))
	seedCertAssets(t, project, hostPrefix, docs)
	seedCertAssets(t, otherProject, hostPrefix, nil)

	svc := NewService()
	ctx := context.Background()

	res, err := svc.List(ctx, models.CertListRequest{Project: project, PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list certificates: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("total = %d, want 3 (%+v)", res.Total, res.List)
	}
	if len(res.List) != 3 {
		t.Fatalf("rows = %d, want 3", len(res.List))
	}
	// notAfter 升序：expired(-48h) -> legacy(+100h) -> shared(+365d)
	if res.List[0].Subject != "CN=expired.example.com" ||
		res.List[1].Subject != "CN=legacy.example.com" ||
		res.List[2].Subject != "CN=shared.example.com" {
		t.Fatalf("unexpected order: %q, %q, %q", res.List[0].Subject, res.List[1].Subject, res.List[2].Subject)
	}

	shared := findRowBySubject(t, res.List, "CN=shared.example.com")
	if shared == nil {
		t.Fatalf("shared cert not found: %+v", res.List)
	}
	if shared.HostCount != 2 || len(shared.Hosts) != 2 {
		t.Fatalf("shared hosts = %d/%d, want 2/2: %+v", shared.HostCount, len(shared.Hosts), shared.Hosts)
	}
	if shared.Hosts[0].Host != hostPrefix+"a.shared.example.com" || shared.Hosts[0].IP != "10.10.0.1" || shared.Hosts[0].Port != "443" {
		t.Fatalf("unexpected first host: %+v", shared.Hosts[0])
	}
	if len(shared.SAN) != 2 || shared.SAN[0] != "shared.example.com" || shared.SAN[1] != "www.shared.example.com" {
		t.Fatalf("shared san = %v", shared.SAN)
	}
	if shared.Expired {
		t.Fatalf("shared cert marked expired")
	}
	if shared.NotAfter == nil || shared.Fingerprint != "fp-shared" {
		t.Fatalf("shared cert fields: %+v", shared)
	}
	if shared.ID != "fp:fp-shared" {
		t.Fatalf("shared id = %q, want fp:fp-shared", shared.ID)
	}

	legacy := findRowBySubject(t, res.List, "CN=legacy.example.com")
	if legacy == nil {
		t.Fatalf("legacy cert not found: %+v", res.List)
	}
	if legacy.ID != "fp:fp-legacy" || legacy.Serial != "LEG-1" || legacy.Expired {
		t.Fatalf("legacy cert fields: %+v", legacy)
	}
	if len(legacy.SAN) != 2 {
		t.Fatalf("legacy san = %v, want 2 entries from comma string", legacy.SAN)
	}

	// 过期过滤
	expiredTrue := true
	onlyExpired, err := svc.List(ctx, models.CertListRequest{Project: project, Expired: &expiredTrue, PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	if onlyExpired.Total != 1 || onlyExpired.List[0].Subject != "CN=expired.example.com" || !onlyExpired.List[0].Expired {
		t.Fatalf("expired filter = %+v", onlyExpired)
	}

	expiredFalse := false
	onlyValid, err := svc.List(ctx, models.CertListRequest{Project: project, Expired: &expiredFalse, PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list valid: %v", err)
	}
	if onlyValid.Total != 2 {
		t.Fatalf("not-expired total = %d, want 2", onlyValid.Total)
	}

	// keyword：issuer / host / SAN
	byIssuer, err := svc.List(ctx, models.CertListRequest{Project: project, Keyword: "legacy ca", PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list by issuer: %v", err)
	}
	if byIssuer.Total != 1 || byIssuer.List[0].Subject != "CN=legacy.example.com" {
		t.Fatalf("keyword issuer = %+v", byIssuer)
	}
	byHost, err := svc.List(ctx, models.CertListRequest{Project: project, Keyword: hostPrefix + "b.shared", PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list by host: %v", err)
	}
	if byHost.Total != 1 || byHost.List[0].HostCount != 2 {
		t.Fatalf("keyword host = %+v", byHost)
	}
	bySAN, err := svc.List(ctx, models.CertListRequest{Project: project, Keyword: "www.legacy.example.com", PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list by san: %v", err)
	}
	if bySAN.Total != 1 || bySAN.List[0].Subject != "CN=legacy.example.com" {
		t.Fatalf("keyword san = %+v", bySAN)
	}

	// 分页：pageSize=1 第二页拿到第二张证书
	page2, err := svc.List(ctx, models.CertListRequest{Project: project, PageIndex: 2, PageSize: 1})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if page2.Total != 3 || len(page2.List) != 1 || page2.List[0].Subject != "CN=legacy.example.com" {
		t.Fatalf("page2 = %+v", page2)
	}

	// 详情：带 project 时与列表口径一致（2 个 host）
	detail, err := svc.Detail(ctx, models.CertDetailRequest{ID: shared.ID, Project: project})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail == nil || detail.Subject != "CN=shared.example.com" || detail.HostCount != 2 {
		t.Fatalf("detail = %+v", detail)
	}
	// 不带 project：同一张证书在全部项目里的关联（测试里另有 1 个 other project 的 host）
	globalDetail, err := svc.Detail(ctx, models.CertDetailRequest{ID: shared.ID})
	if err != nil {
		t.Fatalf("detail global: %v", err)
	}
	if globalDetail == nil || globalDetail.HostCount != 3 {
		t.Fatalf("global detail = %+v", globalDetail)
	}

	// 不存在的 id
	missing, err := svc.Detail(ctx, models.CertDetailRequest{ID: "fp:no-such-fingerprint"})
	if err != nil {
		t.Fatalf("detail missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("detail missing = %+v, want nil", missing)
	}
}

// hosts 数组最多 50 个、hostCount 是完整计数
func TestCertHostSampleCap(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-cert-cap-" + suffix
	hostPrefix := "cert-cap-" + suffix + "-"

	now := time.Now().UTC()
	cert := tlsCertDoc("CN=many.example.com", "CN=Cap CA", "CAP-1", "fp-many",
		now.Add(-time.Hour), now.Add(24*time.Hour), "many.example.com")

	docs := make([]interface{}, 0, 55)
	for i := 0; i < 55; i++ {
		docs = append(docs, tlsAssetDoc(project,
			fmt.Sprintf("%sh%02d.example.com", hostPrefix, i),
			fmt.Sprintf("10.20.0.%d", i+1), "443", cert))
	}
	seedCertAssets(t, project, hostPrefix, docs)

	svc := NewService()
	res, err := svc.List(context.Background(), models.CertListRequest{Project: project, PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list certificates: %v", err)
	}
	if res.Total != 1 || len(res.List) != 1 {
		t.Fatalf("total/rows = %d/%d, want 1/1", res.Total, len(res.List))
	}
	if res.List[0].HostCount != 55 {
		t.Fatalf("hostCount = %d, want 55", res.List[0].HostCount)
	}
	if len(res.List[0].Hosts) != 50 {
		t.Fatalf("hosts len = %d, want 50", len(res.List[0].Hosts))
	}
}

// serial+issuer 合并：没有指纹时同一张证书也要按 serial+issuer 合并
func TestCertSerialIssuerMerge(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-cert-sn-" + suffix
	hostPrefix := "cert-sn-" + suffix + "-"

	now := time.Now().UTC()
	cert := tlsCertDoc("CN=serial.example.com", "CN=Serial CA", "SN-42", "",
		now.Add(-time.Hour), now.Add(48*time.Hour), "serial.example.com")

	seedCertAssets(t, project, hostPrefix, []interface{}{
		tlsAssetDoc(project, hostPrefix+"s1.example.com", "10.40.0.1", "443", cert),
		tlsAssetDoc(project, hostPrefix+"s2.example.com", "10.40.0.2", "8443", cert),
	})

	svc := NewService()
	res, err := svc.List(context.Background(), models.CertListRequest{Project: project, PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list certificates: %v", err)
	}
	if res.Total != 1 || len(res.List) != 1 {
		t.Fatalf("total/rows = %d/%d, want 1/1: %+v", res.Total, len(res.List), res.List)
	}
	row := res.List[0]
	if row.ID != "sn:CN=Serial CA|SN-42" {
		t.Fatalf("id = %q, want sn:CN=Serial CA|SN-42", row.ID)
	}
	if row.HostCount != 2 {
		t.Fatalf("hostCount = %d, want 2", row.HostCount)
	}

	detail, err := svc.Detail(context.Background(), models.CertDetailRequest{ID: row.ID, Project: project})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail == nil || detail.HostCount != 2 {
		t.Fatalf("detail = %+v", detail)
	}
}

// 新增的 TLS / company 索引要真的能在 createIndexes 里建出来
func TestCertCIPIndexes(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	assertIndex := func(collection string, want []string) {
		cursor, err := mongodb.DB.Collection(collection).Indexes().List(context.Background())
		if err != nil {
			t.Fatalf("list %s indexes: %v", collection, err)
		}
		defer cursor.Close(context.Background())
		found := map[string]bool{}
		for cursor.Next(context.Background()) {
			var spec struct {
				Key bson.D `bson:"key"`
			}
			if err := cursor.Decode(&spec); err != nil {
				t.Fatalf("decode %s index: %v", collection, err)
			}
			parts := make([]string, 0, len(spec.Key))
			for _, elem := range spec.Key {
				parts = append(parts, fmt.Sprintf("%s:%v", elem.Key, elem.Value))
			}
			found[joinIndexKey(parts)] = true
		}
		for _, key := range want {
			if !found[key] {
				t.Fatalf("%s missing index %s (have %v)", collection, key, found)
			}
		}
	}

	assertIndex("asset", []string{
		"tls.certificateresponse.fingerprinthash.sha256:1",
		"tls.certificateresponse.notafter:1",
		"tls.certificateresponse.subjectan:1",
		"tlsdata.not_after:1",
	})
	assertIndex("RootDomain", []string{"company:1,time:-1"})
}

func joinIndexKey(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += ","
		}
		out += part
	}
	return out
}
