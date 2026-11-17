//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"go.mongodb.org/mongo-driver/bson"
)

// 站点 / IP 读模型新接口要在真实路由链上验一遍：
// 鉴权中间件挂没挂、GET 的 query 绑定、POST 的 JSON 绑定、
// 400 分支、以及响应结构（data.list / data.total）都要对得上，
// 否则前端按 /api/assets/site/list 一调就是 404 或 401。
func TestSiteIPReadModelRoutes(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	prefix := fmt.Sprintf("siteip-routes-%d", time.Now().UnixNano())
	host := prefix + "-a.example.com"
	ip := fmt.Sprintf("198.18.%d.7", time.Now().UnixNano()%200+20)

	assetColl := mongodb.DB.Collection("asset")
	subColl := mongodb.DB.Collection("subdomain")
	t.Cleanup(func() {
		_, _ = assetColl.DeleteMany(context.Background(), bson.M{"$or": bson.A{
			bson.M{"host": bson.M{"$regex": "^" + regexp.QuoteMeta(prefix)}},
			bson.M{"ip": bson.M{"$regex": "^" + regexp.QuoteMeta(ip)}},
		}})
		_, _ = subColl.DeleteMany(context.Background(), bson.M{
			"host": bson.M{"$regex": "^" + regexp.QuoteMeta(prefix)},
		})
	})
	if _, err := assetColl.InsertMany(context.Background(), []interface{}{
		bson.M{"host": host, "port": "443", "ip": ip, "title": "Routes Title", "statuscode": 200,
			"project": "proj-siteip-routes", "time": "2026-11-18 12:00:00", "url": "https://" + host},
	}); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	if _, err := subColl.InsertMany(context.Background(), []interface{}{
		bson.M{"host": host, "type": "A", "ip": []string{ip}, "time": "2026-11-18 12:00:00",
			"project": "proj-siteip-routes", "rootDomain": prefix + ".internal"},
	}); err != nil {
		t.Fatalf("seed subdomain: %v", err)
	}

	do := func(method, path string, body interface{}, withToken bool) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			reader = bytes.NewReader(payload)
		} else {
			reader = bytes.NewReader(nil)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		if withToken {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, req)
		return w
	}

	decode := func(w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		var payload map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %s: %v (%s)", w.Body.String(), err, w.Body.String())
		}
		return payload
	}

	// 1) 未认证 -> 401（WithAuth 真的挂上了）
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/assets/site/list"},
		{http.MethodPost, "/api/assets/site/list"},
		{http.MethodPost, "/api/assets/site/chain"},
		{http.MethodPost, "/api/assets/ip/list"},
		{http.MethodPost, "/api/assets/ip/domains"},
	} {
		w := do(tc.method, tc.path, map[string]interface{}{"host": host, "ip": ip}, false)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s %s = %d, want 401: %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	// 2) GET /api/assets/site/list?keyword= 走 query 绑定
	w := do(http.MethodGet, "/api/assets/site/list?keyword="+prefix+"&pageIndex=1&pageSize=5", nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("GET site/list = %d: %s", w.Code, w.Body.String())
	}
	siteData, _ := decode(w)["data"].(map[string]interface{})
	if siteData == nil {
		t.Fatalf("GET site/list missing data: %s", w.Body.String())
	}
	if total, _ := siteData["total"].(float64); total != 1 {
		t.Errorf("GET site/list total = %v, want 1", siteData["total"])
	}
	list, _ := siteData["list"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("GET site/list rows = %d, want 1: %s", len(list), w.Body.String())
	}
	row, _ := list[0].(map[string]interface{})
	if row["host"] != host || row["port"] != "443" || row["title"] != "Routes Title" {
		t.Errorf("GET site/list row = %v, want %s:443 Routes Title", row, host)
	}
	if acc, _ := row["assetCount"].(float64); acc != 1 {
		t.Errorf("GET site/list assetCount = %v, want 1", row["assetCount"])
	}
	if row["lastSeen"] != "2026-11-18 12:00:00" {
		t.Errorf("GET site/list lastSeen = %v, want 2026-11-18 12:00:00", row["lastSeen"])
	}

	// 3) POST /api/assets/site/list 走 JSON 绑定
	w = do(http.MethodPost, "/api/assets/site/list", map[string]interface{}{
		"keyword": prefix, "project": "proj-siteip-routes", "pageIndex": 1, "pageSize": 5,
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("POST site/list = %d: %s", w.Code, w.Body.String())
	}
	if data, _ := decode(w)["data"].(map[string]interface{}); data == nil {
		t.Errorf("POST site/list missing data: %s", w.Body.String())
	} else if total, _ := data["total"].(float64); total != 1 {
		t.Errorf("POST site/list total = %v, want 1", data["total"])
	}

	// 4) POST /api/assets/site/chain：正常返回链路 + limit=500
	w = do(http.MethodPost, "/api/assets/site/chain", map[string]interface{}{"host": host}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("POST site/chain = %d: %s", w.Code, w.Body.String())
	}
	chainData, _ := decode(w)["data"].(map[string]interface{})
	if chainData == nil {
		t.Fatalf("POST site/chain missing data: %s", w.Body.String())
	}
	if limit, _ := chainData["limit"].(float64); limit != 500 {
		t.Errorf("POST site/chain limit = %v, want 500", chainData["limit"])
	}
	if total, _ := chainData["total"].(float64); total < 1 {
		t.Errorf("POST site/chain total = %v, want >= 1", chainData["total"])
	}
	if chain, _ := chainData["list"].([]interface{}); len(chain) == 0 {
		t.Errorf("POST site/chain returned empty chain: %s", w.Body.String())
	}

	// 5) POST /api/assets/site/chain 缺 host -> 400（binding:"required" 生效）
	w = do(http.MethodPost, "/api/assets/site/chain", map[string]interface{}{}, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST site/chain without host = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 6) POST /api/assets/ip/list：聚合出端口/域名摘要
	w = do(http.MethodPost, "/api/assets/ip/list", map[string]interface{}{"ip": ip, "pageIndex": 1, "pageSize": 10}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("POST ip/list = %d: %s", w.Code, w.Body.String())
	}
	ipData, _ := decode(w)["data"].(map[string]interface{})
	if ipData == nil {
		t.Fatalf("POST ip/list missing data: %s", w.Body.String())
	}
	if total, _ := ipData["total"].(float64); total != 1 {
		t.Fatalf("POST ip/list total = %v, want 1: %s", ipData["total"], w.Body.String())
	}
	ipList, _ := ipData["list"].([]interface{})
	ipRow, _ := ipList[0].(map[string]interface{})
	if ipRow["ip"] != ip {
		t.Errorf("POST ip/list ip = %v, want %s", ipRow["ip"], ip)
	}
	if pc, _ := ipRow["portCount"].(float64); pc != 1 {
		t.Errorf("POST ip/list portCount = %v, want 1", ipRow["portCount"])
	}
	ports, _ := ipRow["ports"].([]interface{})
	if len(ports) != 1 {
		t.Fatalf("POST ip/list ports = %v, want 1 entry", ipRow["ports"])
	}
	port0, _ := ports[0].(map[string]interface{})
	if port0["port"] != "443" {
		t.Errorf("POST ip/list ports[0].port = %v, want 443", port0["port"])
	}
	if dc, _ := ipRow["domainCount"].(float64); dc != 1 {
		t.Errorf("POST ip/list domainCount = %v, want 1", ipRow["domainCount"])
	}
	domains, _ := ipRow["domains"].([]interface{})
	// asset.host 与 subdomain.host 同名，去重后应为 1 条
	if len(domains) != 1 || domains[0] != host {
		t.Errorf("POST ip/list domains = %v, want [%s]", ipRow["domains"], host)
	}

	// 7) POST /api/assets/ip/domains：asset 与 subdomain 两个来源合并
	w = do(http.MethodPost, "/api/assets/ip/domains", map[string]interface{}{"ip": ip}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("POST ip/domains = %d: %s", w.Code, w.Body.String())
	}
	domData, _ := decode(w)["data"].(map[string]interface{})
	if domData == nil {
		t.Fatalf("POST ip/domains missing data: %s", w.Body.String())
	}
	if total, _ := domData["total"].(float64); total != 1 {
		t.Fatalf("POST ip/domains total = %v, want 1: %s", domData["total"], w.Body.String())
	}
	domList, _ := domData["list"].([]interface{})
	domRow, _ := domList[0].(map[string]interface{})
	if domRow["domain"] != host {
		t.Errorf("POST ip/domains domain = %v, want %s", domRow["domain"], host)
	}
	sources, _ := domRow["sources"].([]interface{})
	if len(sources) != 2 || sources[0] != "asset" || sources[1] != "subdomain" {
		t.Errorf("POST ip/domains sources = %v, want [asset subdomain]", domRow["sources"])
	}

	// 8) POST /api/assets/ip/domains 缺 ip -> 400
	w = do(http.MethodPost, "/api/assets/ip/domains", map[string]interface{}{}, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST ip/domains without ip = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 9) 参数非法（pageSize 超上限）-> 400（binding tag 生效）
	w = do(http.MethodPost, "/api/assets/site/list", map[string]interface{}{"pageSize": 100000}, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST site/list pageSize=100000 = %d, want 400: %s", w.Code, w.Body.String())
	}
}
