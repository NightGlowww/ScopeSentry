//go:build integration

package routes

import (
	"strings"
	"testing"
)

// 路由清单守卫：SetupRouter 不需要 Mongo/Redis 即可完成注册（服务构造已做 nil 保护）。
// 这里只钉住"路由数量没有意外塌缩"与几个关键前缀存在，避免重构成片丢接口。
func TestSetupRouterInventory(t *testing.T) {
	r := SetupRouter()
	if r == nil {
		t.Fatal("SetupRouter returned nil")
	}
	routes := r.Routes()
	if len(routes) < 200 {
		t.Fatalf("route count = %d, 期望 >= 200（历史基线 281）", len(routes))
	}
	need := []string{"/healthz", "/api/project_aggregation/project/info", "/api/mcp/config", "/api/audit/list"}
	have := make(map[string]bool, len(routes))
	for _, rt := range routes {
		have[rt.Path] = true
	}
	for _, p := range need {
		if !have[p] {
			t.Fatalf("缺少路由 %s", p)
		}
	}
	// 所有 /api 路由都应挂在中间件链上（AllowList 由 route_auth_test 覆盖）
	apiCount := 0
	for _, rt := range routes {
		if strings.HasPrefix(rt.Path, "/api/") {
			apiCount++
		}
	}
	if apiCount < 150 {
		t.Fatalf("/api 路由仅 %d 条", apiCount)
	}
	t.Logf("routes=%d api=%d", len(routes), apiCount)
}
