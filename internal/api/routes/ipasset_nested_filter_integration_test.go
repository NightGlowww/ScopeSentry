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
	"go.mongodb.org/mongo-driver/bson"
)

// IP 资产的 port/service/app 在文档里是 ports[] 数组的嵌套字段。
// 过滤器却按顶级字段查（filterKeyCache 只有 port/service/app），于是
// filter.port=443 恒空——库里明明有一堆 443，界面/自动化看到的是「没有」。
// 这条 2026-11-28 记在 MCP_GAPS §9 证据里，本轮修并钉住。
func TestIPAssetFilterByNestedPort(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	ctx := context.Background()
	if mongodb.DB == nil {
		t.Skip("mongo 未连接，跳过 IP 资产嵌套过滤用例")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ipA := "203.0.113.10"
	ipB := "203.0.113.11"
	coll := mongodb.DB.Collection("IPAsset")
	if _, err := coll.InsertMany(ctx, []interface{}{
		bson.M{
			"ip": ipA, "time": time.Now().Format("2006-01-02 15:04:05"),
			"ports": []bson.M{{
				"port": "443",
				// server 是数组（models.IPAsset.Ports[].Server []PortServer），写成一个文档会解码失败
				"server": []bson.M{{"domain": "it-ip-" + suffix + ".example.com", "service": "nginx", "technologies": []string{"nginx"}}},
			}},
		},
		bson.M{
			"ip": ipB, "time": time.Now().Format("2006-01-02 15:04:05"),
			"ports": []bson.M{{
				"port":   "8080",
				"server": []bson.M{{"domain": "it-ip2-" + suffix + ".example.com", "service": "tomcat", "technologies": []string{"tomcat"}}},
			}},
		},
	}); err != nil {
		t.Fatalf("seed IPAsset: %v", err)
	}
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(ctx, bson.M{"ip": bson.M{"$in": []string{ipA, ipB}}})
	})

	post := func(body map[string]interface{}) (int, int, []map[string]interface{}) {
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/assets/ip", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		var resp struct {
			Data struct {
				Total int                      `json:"total"`
				List  []map[string]interface{} `json:"list"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if w.Code != http.StatusOK {
			t.Logf("ip list body: %s", w.Body.String())
		}
		return w.Code, resp.Data.Total, resp.Data.List
	}

	code, total, rows := post(map[string]interface{}{
		"pageIndex": 1, "pageSize": 10,
		"filter": map[string][]string{"port": {"443"}},
	})
	if code != http.StatusOK {
		t.Fatalf("ip list = %d", code)
	}
	if total < 1 || len(rows) < 1 {
		t.Fatalf("filter.port=443 返回 0 条（嵌套字段没被映射）: total=%d rows=%d", total, len(rows))
	}
	for _, r := range rows {
		if got := fmt.Sprintf("%v", r["port"]); got != "443" {
			t.Fatalf("返回了非 443 的行: port=%v ip=%v", r["port"], r["ip"])
		}
	}

	// 反向对照：不存在的端口必须真的空（否则就是「过滤器被忽略」而不是「映射生效」）
	_, totalNone, _ := post(map[string]interface{}{
		"pageIndex": 1, "pageSize": 10,
		"filter": map[string][]string{"port": {"65530"}},
	})
	if totalNone != 0 {
		t.Fatalf("不存在的端口应返回 0 条，实际 %d（过滤器被忽略）", totalNone)
	}
}
