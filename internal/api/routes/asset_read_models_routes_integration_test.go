//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 三个资产读模型（stat_finger / nuclei_result / wih）要在真实路由链上验一遍：
// 鉴权中间件挂没挂、JSON 绑定对不对、pageSize>200 是不是 400、
// detail 的 400/404 分支、以及响应结构（data.list / data.total）能不能被前端直接用。
func TestAssetReadModelRoutes(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-readmodels-routes-" + suffix
	host := fmt.Sprintf("rmr-%s.example.com", suffix)
	assetColl := mongodb.DB.Collection("asset")
	vulnColl := mongodb.DB.Collection("vulnerability")
	sensColl := mongodb.DB.Collection("SensitiveResult")
	t.Cleanup(func() {
		_, _ = assetColl.DeleteMany(context.Background(), bson.M{"project": project})
		_, _ = vulnColl.DeleteMany(context.Background(), bson.M{"project": project})
		_, _ = sensColl.DeleteMany(context.Background(), bson.M{"project": project})
	})

	if _, err := assetColl.InsertOne(context.Background(), bson.M{
		"host": host, "port": "443", "ip": "198.21.7.7", "url": "https://" + host,
		"title": "read model", "statuscode": 200, "project": project,
		"time": "2026-11-18 12:00:00", "technologies": []string{"nginx-probe", "php-probe"},
	}); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	vulnRes, err := vulnColl.InsertOne(context.Background(), bson.M{
		"vulnid": "cve-2021-44228", "vulnname": "Log4j RCE", "level": "critical",
		"url": "https://" + host + "/admin", "matched": "jndi:ldap://x",
		"project": project, "rootDomain": host, "taskName": "task-routes",
		"tags": []string{"nuclei"}, "status": 1, "time": "2026-11-18 12:00:00",
		"hash": project + "-hash", "request": "GET /admin", "response": "HTTP/1.1 200",
	})
	if err != nil {
		t.Fatalf("seed vulnerability: %v", err)
	}
	vulnID := vulnRes.InsertedID.(primitive.ObjectID).Hex()
	sensRes, err := sensColl.InsertOne(context.Background(), bson.M{
		"sid": "apikey", "url": "https://" + host + "/app.js", "match": []string{"sk-live-xxx"},
		"md5": project + "-md5", "color": "danger", "tags": []string{"sensitive"},
		"status": 1, "project": project, "rootDomain": host,
		"time": "2026-11-18 12:00:00",
	})
	if err != nil {
		t.Fatalf("seed SensitiveResult: %v", err)
	}
	sensID := sensRes.InsertedID.(primitive.ObjectID).Hex()

	post := func(path string, body interface{}, withToken bool) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
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
	dataOf := func(w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		data, _ := decode(w)["data"].(map[string]interface{})
		if data == nil {
			t.Fatalf("response missing data: %s", w.Body.String())
		}
		return data
	}

	// 1) 未认证：6 个端点都必须 401（WithAuth 真的挂上了）
	unauth := []struct {
		path string
		body map[string]interface{}
	}{
		{"/api/assets/stat_finger/list", map[string]interface{}{}},
		{"/api/assets/stat_finger/assets", map[string]interface{}{"technology": "nginx-probe"}},
		{"/api/assets/nuclei_result/list", map[string]interface{}{}},
		{"/api/assets/nuclei_result/detail", map[string]interface{}{"id": vulnID}},
		{"/api/assets/wih/list", map[string]interface{}{}},
		{"/api/assets/wih/detail", map[string]interface{}{"id": sensID}},
	}
	for _, tc := range unauth {
		w := post(tc.path, tc.body, false)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s = %d, want 401: %s", tc.path, w.Code, w.Body.String())
		}
	}

	// 2) pageSize 超上限（binding max=200）-> 400
	for _, tc := range []struct {
		path string
		body map[string]interface{}
	}{
		{"/api/assets/stat_finger/list", map[string]interface{}{"pageSize": 500}},
		{"/api/assets/stat_finger/assets", map[string]interface{}{"technology": "nginx-probe", "pageSize": 500}},
		{"/api/assets/nuclei_result/list", map[string]interface{}{"pageSize": 500}},
		{"/api/assets/wih/list", map[string]interface{}{"pageSize": 500}},
	} {
		w := post(tc.path, tc.body, true)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s pageSize=500 = %d, want 400: %s", tc.path, w.Code, w.Body.String())
		}
	}

	// 3) 指纹统计：技术名/次数/样例 host
	w := post("/api/assets/stat_finger/list", map[string]interface{}{
		"project": project, "pageIndex": 1, "pageSize": 20,
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("stat_finger/list = %d: %s", w.Code, w.Body.String())
	}
	statData := dataOf(w)
	if total, _ := statData["total"].(float64); total != 2 {
		t.Errorf("stat_finger total = %v, want 2", statData["total"])
	}
	rows, _ := statData["list"].([]interface{})
	if len(rows) != 2 {
		t.Fatalf("stat_finger rows = %d, want 2: %s", len(rows), w.Body.String())
	}
	row0, _ := rows[0].(map[string]interface{})
	if row0["name"] == nil || row0["count"] == nil || row0["assetCount"] == nil || row0["lastSeen"] == nil || row0["samples"] == nil {
		t.Errorf("stat_finger row missing fields: %v", row0)
	}
	samples, _ := row0["samples"].([]interface{})
	if len(samples) != 1 || samples[0] != host {
		t.Errorf("stat_finger samples = %v, want [%s]", row0["samples"], host)
	}

	// 4) 按技术反查资产
	w = post("/api/assets/stat_finger/assets", map[string]interface{}{
		"technology": "nginx-probe", "project": project, "pageIndex": 1, "pageSize": 10,
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("stat_finger/assets = %d: %s", w.Code, w.Body.String())
	}
	assetData := dataOf(w)
	if total, _ := assetData["total"].(float64); total != 1 {
		t.Fatalf("stat_finger/assets total = %v, want 1", assetData["total"])
	}
	assetRows, _ := assetData["list"].([]interface{})
	assetRow, _ := assetRows[0].(map[string]interface{})
	if assetRow["host"] != host || assetRow["port"] != "443" {
		t.Errorf("stat_finger/assets row = %v, want %s:443", assetRow, host)
	}

	// 5) Nuclei 列表 + 详情
	w = post("/api/assets/nuclei_result/list", map[string]interface{}{
		"project": project, "severity": "critical", "templateId": "CVE-2021",
		"keyword": "log4j", "pageIndex": 1, "pageSize": 10,
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("nuclei_result/list = %d: %s", w.Code, w.Body.String())
	}
	nucData := dataOf(w)
	if total, _ := nucData["total"].(float64); total != 1 {
		t.Fatalf("nuclei_result total = %v, want 1: %s", nucData["total"], w.Body.String())
	}
	nucRows, _ := nucData["list"].([]interface{})
	nucRow, _ := nucRows[0].(map[string]interface{})
	if nucRow["templateId"] != "cve-2021-44228" || nucRow["severity"] != "critical" || nucRow["host"] != host {
		t.Errorf("nuclei_result row = %v, want templateId/severity/host mapped", nucRow)
	}
	if nucRow["name"] != "Log4j RCE" {
		t.Errorf("nuclei_result name = %v, want Log4j RCE（vulnname -> name）", nucRow["name"])
	}

	w = post("/api/assets/nuclei_result/detail", map[string]interface{}{"id": vulnID}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("nuclei_result/detail = %d: %s", w.Code, w.Body.String())
	}
	nucDetail := dataOf(w)
	if nucDetail["request"] != "GET /admin" || nucDetail["response"] != "HTTP/1.1 200" {
		t.Errorf("nuclei_result/detail req/res = %v/%v", nucDetail["request"], nucDetail["response"])
	}

	// 6) WIH 列表 + 详情
	w = post("/api/assets/wih/list", map[string]interface{}{
		"project": project, "type": "apikey", "keyword": "sk-live", "pageIndex": 1, "pageSize": 10,
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("wih/list = %d: %s", w.Code, w.Body.String())
	}
	wihData := dataOf(w)
	if total, _ := wihData["total"].(float64); total != 1 {
		t.Fatalf("wih total = %v, want 1: %s", wihData["total"], w.Body.String())
	}
	wihRows, _ := wihData["list"].([]interface{})
	wihRow, _ := wihRows[0].(map[string]interface{})
	if wihRow["type"] != "apikey" || wihRow["host"] != host || wihRow["bodyId"] != project+"-md5" {
		t.Errorf("wih row = %v, want type/host/bodyId mapped", wihRow)
	}

	w = post("/api/assets/wih/detail", map[string]interface{}{"id": sensID}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("wih/detail = %d: %s", w.Code, w.Body.String())
	}
	wihDetail := dataOf(w)
	if wihDetail["type"] != "apikey" {
		t.Errorf("wih/detail type = %v, want apikey", wihDetail["type"])
	}
	match, _ := wihDetail["match"].([]interface{})
	if len(match) != 1 || match[0] != "sk-live-xxx" {
		t.Errorf("wih/detail match = %v, want [sk-live-xxx]", wihDetail["match"])
	}

	// 7) detail 的 400 / 404 分支
	for _, tc := range []struct {
		name string
		path string
		body map[string]interface{}
		want int
	}{
		{"nuclei detail bad id", "/api/assets/nuclei_result/detail", map[string]interface{}{"id": "not-an-object-id"}, http.StatusBadRequest},
		{"nuclei detail empty id", "/api/assets/nuclei_result/detail", map[string]interface{}{}, http.StatusBadRequest},
		{"nuclei detail missing", "/api/assets/nuclei_result/detail", map[string]interface{}{"id": primitive.NewObjectID().Hex()}, http.StatusNotFound},
		{"wih detail bad id", "/api/assets/wih/detail", map[string]interface{}{"id": "not-an-object-id"}, http.StatusBadRequest},
		{"wih detail missing", "/api/assets/wih/detail", map[string]interface{}{"id": primitive.NewObjectID().Hex()}, http.StatusNotFound},
		{"stat_finger assets missing technology", "/api/assets/stat_finger/assets", map[string]interface{}{}, http.StatusBadRequest},
	} {
		w := post(tc.path, tc.body, true)
		if w.Code != tc.want {
			t.Fatalf("%s = %d, want %d: %s", tc.name, w.Code, tc.want, w.Body.String())
		}
	}
}
