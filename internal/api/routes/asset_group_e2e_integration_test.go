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
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

// ARL-Next 对齐 P0 的端到端验收：集团/地契走真实路由 + 中间件 + Mongo。
// 覆盖：建集团 → 建地契（混合解析）→ 列表 → 批量移动 → transfer 删除 → dissolve 删除 → 未分组可见。
func TestAssetGroupScopeEndToEnd(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	ctx := context.Background()
	prefix := fmt.Sprintf("e2e-ag-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("asset_group").DeleteMany(ctx, bson.M{"name": bson.M{"$regex": "^" + prefix}})
		_, _ = mongodb.DB.Collection("asset_scope").DeleteMany(ctx, bson.M{"name": bson.M{"$regex": "^" + prefix}})
	})

	status, data := doJSON(t, router, http.MethodPost, "/api/assets/asset_group/add",
		map[string]interface{}{"name": prefix + "-A", "description": "e2e"}, token)
	if status != http.StatusOK {
		t.Fatalf("add group A = %d %v", status, data)
	}
	groupA, _ := data["id"].(string)

	status, data = doJSON(t, router, http.MethodPost, "/api/assets/asset_group/add",
		map[string]interface{}{"name": prefix + "-B"}, token)
	if status != http.StatusOK {
		t.Fatalf("add group B = %d %v", status, data)
	}
	groupB, _ := data["id"].(string)

	// 地契：域名+IP 混合
	status, data = doJSON(t, router, http.MethodPost, "/api/assets/asset_scope/add",
		map[string]interface{}{
			"name": prefix + "-scope", "scope": "a.example.com\nb.example.com\n10.1.2.3", "groupId": groupA,
		}, token)
	if status != http.StatusOK {
		t.Fatalf("add scope = %d %v", status, data)
	}
	scopeID, _ := data["id"].(string)

	status, data = doJSON(t, router, http.MethodPost, "/api/assets/asset_scope/list",
		map[string]interface{}{"groupId": groupA, "pageIndex": 1, "pageSize": 20}, token)
	if status != http.StatusOK {
		t.Fatalf("list scopes = %d %v", status, data)
	}
	if total, _ := data["total"].(float64); total != 1 {
		t.Fatalf("scope total in group A = %v, want 1 (%v)", data["total"], data)
	}
	list, _ := data["list"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("scope list = %v", data["list"])
	}
	row, _ := list[0].(map[string]interface{})
	if domains, _ := row["domainArray"].([]interface{}); len(domains) != 2 {
		t.Fatalf("domainArray = %v, want 2", row["domainArray"])
	}
	if ips, _ := row["ipArray"].([]interface{}); len(ips) != 1 {
		t.Fatalf("ipArray = %v, want 1", row["ipArray"])
	}

	// 批量移动 → B
	status, data = doJSON(t, router, http.MethodPost, "/api/assets/asset_scope/batch_move_group",
		map[string]interface{}{"ids": []string{scopeID}, "groupId": groupB}, token)
	if status != http.StatusOK {
		t.Fatalf("move scope = %d %v", status, data)
	}

	// 删除 B：transfer 到 A
	status, data = doJSON(t, router, http.MethodPost, "/api/assets/asset_group/delete",
		map[string]interface{}{"id": groupB, "action": "transfer", "targetGroupId": groupA}, token)
	if status != http.StatusOK {
		t.Fatalf("delete group B = %d %v", status, data)
	}
	_, data = doJSON(t, router, http.MethodPost, "/api/assets/asset_scope/list",
		map[string]interface{}{"groupId": groupA}, token)
	if total, _ := data["total"].(float64); total != 1 {
		t.Fatalf("after transfer, group A total = %v, want 1", data["total"])
	}

	// 删除 A：dissolve → 未分组
	status, data = doJSON(t, router, http.MethodPost, "/api/assets/asset_group/delete",
		map[string]interface{}{"id": groupA, "action": "dissolve"}, token)
	if status != http.StatusOK {
		t.Fatalf("delete group A = %d %v", status, data)
	}
	_, data = doJSON(t, router, http.MethodPost, "/api/assets/asset_scope/list",
		map[string]interface{}{"groupId": "unassigned"}, token)
	if total, _ := data["total"].(float64); total < 1 {
		t.Fatalf("unassigned total = %v, want >=1", data["total"])
	}
}

func doJSON(t *testing.T, router *gin.Engine, method, path string, body map[string]interface{}, token string) (int, map[string]interface{}) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].(map[string]interface{})
	return w.Code, data
}
