//go:build integration

package routes

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// 路由清单守卫。注意：SetupRouter() 会触发 bootstrap 连接 Mongo/Redis，连不上直接 log.Fatalf，
// 所以「不需要数据库」这句老注释是错的 —— 跑它必须有真库（本地可用 mongo:7 + redis:7 容器）。
// 这里只钉住"路由数量没有意外塌缩"与几个关键前缀存在，避免重构成片丢接口。
func TestSetupRouterInventory(t *testing.T) {
	r := SetupRouter()
	if r == nil {
		t.Fatal("SetupRouter returned nil")
	}
	routes := r.Routes()
	// SS_ROUTE_DUMP=1 时把清单打出来：scripts/ci/rest_closure.py 靠它算「哪些接口没被任何测试/契约脚本碰过」。
	// 需要真实 Mongo/Redis（SetupRouter 会触发 bootstrap），所以只在显式要求时输出。
	if os.Getenv("SS_ROUTE_DUMP") != "" {
		for _, rt := range routes {
			fmt.Printf("ROUTE\t%s\t%s\n", rt.Method, rt.Path)
		}
	}
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
