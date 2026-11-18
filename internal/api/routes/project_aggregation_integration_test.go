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

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 项目详情页 4 个 Tab 的 HTTP 层：真实路由 + 鉴权中间件 + Mongo。
//
// 回归的是 docs/audit/ISLANDS.md 第 4.1 节那个孤岛：
//
//	POST /api/project_aggregation/project/{info,asset/count,vul/statistics,vul/data,
//	     subdomain/data,port/data,service/data}
//
// 曾经 7 条全部未注册 → 项目详情页 4 个 Tab 全 404。
func TestProjectAggregationRoutes(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	ctx := context.Background()
	oid := primitive.NewObjectID()
	pid := oid.Hex()
	projectFilter := bson.M{"project": pid}

	projectColl := mongodb.DB.Collection("project")
	assetColl := mongodb.DB.Collection("asset")
	subColl := mongodb.DB.Collection("subdomain")
	vulnColl := mongodb.DB.Collection("vulnerability")
	urlColl := mongodb.DB.Collection("UrlScan")
	t.Cleanup(func() {
		_, _ = projectColl.DeleteMany(ctx, bson.M{"_id": oid})
		_, _ = assetColl.DeleteMany(ctx, projectFilter)
		_, _ = subColl.DeleteMany(ctx, projectFilter)
		_, _ = vulnColl.DeleteMany(ctx, projectFilter)
		_, _ = urlColl.DeleteMany(ctx, projectFilter)
	})

	if _, err := projectColl.InsertOne(ctx, bson.M{
		"_id": oid, "name": "pd-routes", "tag": "pd", "scheduledTasks": false,
		"hour": 0, "AssetCount": int64(3), "root_domains": []string{"pd.example.test"},
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := assetColl.InsertMany(ctx, []interface{}{
		bson.M{"host": "a.pd.example.test", "ip": "10.1.0.1", "port": "80", "service": "http", "type": "http", "project": pid, "time": "2026-11-18 10:00:00"},
		bson.M{"host": "b.pd.example.test", "ip": "10.1.0.2", "port": "443", "service": "https", "type": "http", "project": pid, "time": "2026-11-18 10:01:00"},
		bson.M{"host": "c.pd.example.test", "ip": "10.1.0.3", "port": "80", "service": "http", "type": "http", "project": pid, "time": "2026-11-18 10:02:00"},
	}); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	if _, err := subColl.InsertMany(ctx, []interface{}{
		bson.M{"host": "a.pd.example.test", "type": "A", "value": nil, "ip": []string{"10.1.0.1"}, "project": pid, "rootDomain": "pd.example.test", "time": "2026-11-18 10:00:00"},
		bson.M{"host": "b.pd.example.test", "type": "CNAME", "value": []string{"a.pd.example.test"}, "ip": nil, "project": pid, "rootDomain": "pd.example.test", "time": "2026-11-18 10:01:00"},
	}); err != nil {
		t.Fatalf("seed subdomain: %v", err)
	}
	if _, err := vulnColl.InsertMany(ctx, []interface{}{
		bson.M{"vulname": "XSS", "url": "https://a.pd.example.test/x", "level": "high", "matched": "alert(1)", "project": pid, "time": "2026-11-18 10:05:00"},
	}); err != nil {
		t.Fatalf("seed vulnerability: %v", err)
	}
	if _, err := urlColl.InsertMany(ctx, []interface{}{
		bson.M{"input": "https://a.pd.example.test", "output": "https://a.pd.example.test/x", "outputtype": "katana", "project": pid, "time": "2026-11-18 10:00:00"},
	}); err != nil {
		t.Fatalf("seed UrlScan: %v", err)
	}

	post := func(path string, body map[string]interface{}, auth bool) (int, map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if auth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, req)
		var out map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	t.Run("未认证被拦下", func(t *testing.T) {
		code, _ := post("/api/project_aggregation/project/info", map[string]interface{}{"id": pid}, false)
		if code != http.StatusUnauthorized {
			t.Fatalf("未认证 status = %d，期望 401", code)
		}
	})

	t.Run("project info", func(t *testing.T) {
		code, out := post("/api/project_aggregation/project/info", map[string]interface{}{"id": pid}, true)
		if code != http.StatusOK {
			t.Fatalf("status = %d body = %v", code, out)
		}
		data, _ := out["data"].(map[string]interface{})
		if data["name"] != "pd-routes" {
			t.Fatalf("data = %v", data)
		}
		if roots, _ := data["root_domains"].([]interface{}); len(roots) != 1 || roots[0] != "pd.example.test" {
			t.Fatalf("root_domains = %v", data["root_domains"])
		}
	})

	t.Run("asset count", func(t *testing.T) {
		code, out := post("/api/project_aggregation/project/asset/count", map[string]interface{}{"id": pid}, true)
		if code != http.StatusOK {
			t.Fatalf("status = %d body = %v", code, out)
		}
		data, _ := out["data"].(map[string]interface{})
		if num(data["assetCount"]) != 3 || num(data["subdomainCount"]) != 2 ||
			num(data["vulCount"]) != 1 || num(data["urlCount"]) != 1 {
			t.Fatalf("count = %v", data)
		}
	})

	t.Run("vul statistics", func(t *testing.T) {
		code, out := post("/api/project_aggregation/project/vul/statistics", map[string]interface{}{"id": pid}, true)
		if code != http.StatusOK {
			t.Fatalf("status = %d body = %v", code, out)
		}
		list, _ := out["data"].([]interface{})
		if len(list) != 1 {
			t.Fatalf("data = %v", out["data"])
		}
		row, _ := list[0].(map[string]interface{})
		if row["_id"] != "high" || num(row["count"]) != 1 {
			t.Fatalf("row = %v", row)
		}
	})

	t.Run("vul data", func(t *testing.T) {
		code, out := post("/api/project_aggregation/project/vul/data", map[string]interface{}{"id": pid, "pageSize": 10}, true)
		if code != http.StatusOK {
			t.Fatalf("status = %d body = %v", code, out)
		}
		data, _ := out["data"].(map[string]interface{})
		list, _ := data["list"].([]interface{})
		if len(list) != 1 || num(data["total"]) != 1 {
			t.Fatalf("data = %v", data)
		}
		row, _ := list[0].(map[string]interface{})
		if row["vulname"] != "XSS" || row["id"] == "" {
			t.Fatalf("row = %v", row)
		}
	})

	t.Run("subdomain data", func(t *testing.T) {
		code, out := post("/api/project_aggregation/project/subdomain/data", map[string]interface{}{
			"search": "", "filter": map[string][]interface{}{"project": {pid}}, "fq": map[string]string{},
		}, true)
		if code != http.StatusOK {
			t.Fatalf("status = %d body = %v", code, out)
		}
		data, _ := out["data"].(map[string]interface{})
		list, _ := data["list"].([]interface{})
		if len(list) != 2 || num(data["total"]) != 2 {
			t.Fatalf("data = %v", data)
		}
		for _, item := range list {
			row, _ := item.(map[string]interface{})
			// value / ip 必须是数组（前端 formatter 直接 forEach）
			if _, ok := row["value"].([]interface{}); !ok {
				t.Fatalf("value 不是数组: %v", row["value"])
			}
			if _, ok := row["ip"].([]interface{}); !ok {
				t.Fatalf("ip 不是数组: %v", row["ip"])
			}
		}
	})

	t.Run("port data", func(t *testing.T) {
		code, out := post("/api/project_aggregation/project/port/data", map[string]interface{}{
			"search": "", "filter": map[string][]interface{}{"project": {pid}}, "fq": map[string]string{},
		}, true)
		if code != http.StatusOK {
			t.Fatalf("status = %d body = %v", code, out)
		}
		data, _ := out["data"].(map[string]interface{})
		list, _ := data["list"].([]interface{})
		if len(list) != 2 || num(data["total"]) != 2 {
			t.Fatalf("data = %v", data)
		}
		byPort := map[string]map[string]interface{}{}
		for _, item := range list {
			row, _ := item.(map[string]interface{})
			byPort[fmt.Sprint(row["port"])] = row
		}
		if num(byPort["80"]["count"]) != 2 {
			t.Fatalf("port 80 = %v", byPort["80"])
		}
	})

	t.Run("service data", func(t *testing.T) {
		code, out := post("/api/project_aggregation/project/service/data", map[string]interface{}{
			"search": "", "filter": map[string][]interface{}{"project": {pid}}, "fq": map[string]string{},
		}, true)
		if code != http.StatusOK {
			t.Fatalf("status = %d body = %v", code, out)
		}
		data, _ := out["data"].(map[string]interface{})
		list, _ := data["list"].([]interface{})
		if len(list) != 2 {
			t.Fatalf("data = %v", data)
		}
	})

	t.Run("参数与不存在项目", func(t *testing.T) {
		code, out := post("/api/project_aggregation/project/port/data", map[string]interface{}{
			"id": pid, "pageSize": 201,
		}, true)
		if code != http.StatusBadRequest {
			t.Fatalf("pageSize=201 status = %d body = %v", code, out)
		}
		code, out = post("/api/project_aggregation/project/info", map[string]interface{}{"id": "not-an-id"}, true)
		if code != http.StatusBadRequest {
			t.Fatalf("非法 id status = %d body = %v", code, out)
		}
		missing := primitive.NewObjectID().Hex()
		code, out = post("/api/project_aggregation/project/info", map[string]interface{}{"id": missing}, true)
		if code != http.StatusNotFound {
			t.Fatalf("缺失项目 status = %d body = %v", code, out)
		}
	})

	t.Run("时间范围", func(t *testing.T) {
		code, out := post("/api/project_aggregation/project/subdomain/data", map[string]interface{}{
			"id": pid, "startTime": "2026-11-18 10:00:30", "endTime": "2026-11-18 10:05:00",
		}, true)
		if code != http.StatusOK {
			t.Fatalf("status = %d body = %v", code, out)
		}
		data, _ := out["data"].(map[string]interface{})
		if num(data["total"]) != 1 {
			t.Fatalf("时间范围内 total = %v，期望 1", data["total"])
		}
	})

}

// num 把 JSON 数字（float64 int64 json.Number）统一成 int64 比较
func num(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return -1
}
