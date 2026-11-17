//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	scopesentrymcp "github.com/Autumn-27/ScopeSentry/internal/mcp"
	"github.com/Autumn-27/ScopeSentry/internal/services/mcpaudit"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

// mcpRequest 发一次 MCP JSON-RPC 请求（Streamable HTTP，stateless）
func mcpRequest(router *gin.Engine, apiKey string, body map[string]interface{}) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	req.Header.Set("X-API-Key", apiKey)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// mcpCallWith 发一次 tools/call
func mcpCallWith(router *gin.Engine, apiKey, tool string) *httptest.ResponseRecorder {
	return mcpRequest(router, apiKey, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      tool,
			"arguments": map[string]interface{}{},
		},
	})
}

// MCP 治理面端到端：建 key -> 允许工具成功 / 未授权工具被拒 -> 日志可查 ->
// 禁用后调用被拒 -> CSV 导出行数对得上。
//
// 这条链同时覆盖三件容易「各自正确、组合出错」的事：
//  1. /api/mcp/* 挂的是 JWT（WithAuth），/mcp 挂的是 API Key；
//  2. scope / 工具白名单在中间件生效，且拒绝时返回 JSON-RPC error 而不是 panic；
//  3. 审计是异步批量写，查询前必须 Flush 才能看到记录。
func TestMCPGovernanceEndToEnd(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}

	router := SetupRouter()
	scopesentrymcp.RegisterRoutes(router)
	token := seedUserAndLogin(t, router)

	doJSON := func(method, path string, body interface{}, withToken bool) *httptest.ResponseRecorder {
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
			t.Fatalf("decode response: %v (%s)", err, w.Body.String())
		}
		return payload
	}

	// 保证全局开关打开、工具白名单清空（[] = 允许全部）
	if w := doJSON(http.MethodPost, "/api/mcp/config", map[string]interface{}{
		"enabled":      true,
		"allowedTools": []string{},
	}, true); w.Code != http.StatusOK {
		t.Fatalf("reset mcp config = %d: %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() {
		// 全局开关是共享单例文档，失败中途退出也要恢复
		_ = doJSON(http.MethodPost, "/api/mcp/config", map[string]interface{}{"enabled": true}, true)
	})

	keyName := fmt.Sprintf("e2e-mcp-%d", time.Now().UnixNano())
	var createdKeyID string
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("api_key").DeleteMany(context.Background(), bson.M{"name": keyName})
		if createdKeyID != "" {
			_, _ = mongodb.DB.Collection("mcp_call_logs").DeleteMany(context.Background(), bson.M{"keyId": createdKeyID})
		}
	})

	// 1) 未认证访问管理面 -> 401（WithAuth 真的挂上了）
	if w := doJSON(http.MethodGet, "/api/mcp/keys", nil, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /api/mcp/keys = %d, want 401", w.Code)
	}

	// 2) 新建 key：scope 只给 plugin:read，工具白名单只给 list_plugin_modules
	w := doJSON(http.MethodPost, "/api/mcp/keys", map[string]interface{}{
		"name":  keyName,
		"scope": []string{"plugin:read"},
		"tools": []string{"list_plugin_modules"},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("create key = %d: %s", w.Code, w.Body.String())
	}
	createData, _ := decode(w)["data"].(map[string]interface{})
	rawKey, _ := createData["key"].(string)
	createdKeyID, _ = createData["id"].(string)
	if rawKey == "" || createdKeyID == "" {
		t.Fatalf("create key payload missing key/id: %s", w.Body.String())
	}
	if scope, _ := createData["scope"].([]interface{}); len(scope) != 1 || scope[0] != "plugin:read" {
		t.Fatalf("created scope = %v, want [plugin:read]", createData["scope"])
	}

	// 3) MCP 调用
	mcpCall := func(tool string) *httptest.ResponseRecorder {
		t.Helper()
		return mcpCallWith(router, rawKey, tool)
	}

	// 3.1 白名单内工具 -> 成功
	if rec := mcpCall("list_plugin_modules"); rec.Code != http.StatusOK {
		t.Fatalf("allowed tool = %d: %s", rec.Code, rec.Body.String())
	} else if !strings.Contains(rec.Body.String(), "modules") {
		t.Fatalf("allowed tool response missing result: %s", rec.Body.String())
	}

	// 3.2 白名单外工具 -> 403 + JSON-RPC error（不是 panic，也不是空响应）
	denied := mcpCall("create_project")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("denied tool = %d, want 403: %s", denied.Code, denied.Body.String())
	}
	var rpcErr struct {
		JSONRPC string `json:"jsonrpc"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(denied.Body.Bytes(), &rpcErr); err != nil {
		t.Fatalf("denied body is not a JSON-RPC error: %v (%s)", err, denied.Body.String())
	}
	if rpcErr.JSONRPC != "2.0" || rpcErr.Error.Code != -32003 {
		t.Fatalf("denied error = %+v, want code -32003", rpcErr)
	}

	// 4) 审计落库（异步批量写，先 Flush）
	flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := mcpaudit.Flush(flushCtx); err != nil {
		t.Fatalf("flush audit: %v", err)
	}

	logsResp := doJSON(http.MethodGet, "/api/mcp/logs?keyId="+createdKeyID, nil, true)
	if logsResp.Code != http.StatusOK {
		t.Fatalf("list logs = %d: %s", logsResp.Code, logsResp.Body.String())
	}
	logsData, _ := decode(logsResp)["data"].(map[string]interface{})
	if total, _ := logsData["total"].(float64); total != 2 {
		t.Fatalf("logs total = %v, want 2: %s", logsData["total"], logsResp.Body.String())
	}
	statuses := map[string]int{}
	list, _ := logsData["list"].([]interface{})
	for _, item := range list {
		row, _ := item.(map[string]interface{})
		status, _ := row["status"].(string)
		statuses[status]++
		if _, ok := row["durationMs"]; !ok {
			t.Fatalf("log row missing durationMs: %v", row)
		}
	}
	if statuses["success"] != 1 || statuses["denied"] != 1 {
		t.Fatalf("log statuses = %v, want 1 success + 1 denied", statuses)
	}

	// 5) 禁用 key -> 再调用被拒
	if w := doJSON(http.MethodPost, "/api/mcp/keys/"+createdKeyID+"/status",
		map[string]interface{}{"enabled": false}, true); w.Code != http.StatusOK {
		t.Fatalf("disable key = %d: %s", w.Code, w.Body.String())
	}
	disabledCall := mcpCall("list_plugin_modules")
	if disabledCall.Code != http.StatusForbidden {
		t.Fatalf("disabled key call = %d, want 403: %s", disabledCall.Code, disabledCall.Body.String())
	}
	if !strings.Contains(disabledCall.Body.String(), "disabled") {
		t.Fatalf("disabled key call body = %s, want mention of disabled", disabledCall.Body.String())
	}

	if err := mcpaudit.Flush(flushCtx); err != nil {
		t.Fatalf("flush audit after disable: %v", err)
	}

	// 6) CSV 导出：3 条记录（成功 + 工具越权 + key 禁用）
	exportResp := doJSON(http.MethodGet, "/api/mcp/logs/export?keyId="+createdKeyID, nil, true)
	if exportResp.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", exportResp.Code, exportResp.Body.String())
	}
	csvBody := strings.TrimPrefix(exportResp.Body.String(), "\ufeff")
	lines := []string{}
	for _, line := range strings.Split(csvBody, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 4 {
		t.Fatalf("csv rows = %d (with header), want 4: %s", len(lines), csvBody)
	}
	if !strings.HasPrefix(lines[0], "keyId,keyPrefix,tool,status") {
		t.Fatalf("csv header = %q", lines[0])
	}
	if !strings.Contains(csvBody, "list_plugin_modules") || !strings.Contains(csvBody, "create_project") {
		t.Fatalf("csv missing tool names: %s", csvBody)
	}

	// 7) 分页上限：pageSize 超过 200 直接 400
	if w := doJSON(http.MethodGet, "/api/mcp/logs?pageSize=500", nil, true); w.Code != http.StatusBadRequest {
		t.Fatalf("pageSize=500 = %d, want 400: %s", w.Code, w.Body.String())
	}
	if w := doJSON(http.MethodGet, "/api/mcp/keys?pageSize=500", nil, true); w.Code != http.StatusBadRequest {
		t.Fatalf("keys pageSize=500 = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 8) 全局开关关闭 -> 503（JSON-RPC -32000）
	if w := doJSON(http.MethodPost, "/api/mcp/config", map[string]interface{}{"enabled": false}, true); w.Code != http.StatusOK {
		t.Fatalf("disable mcp = %d: %s", w.Code, w.Body.String())
	}
	// 重新启用 key，确保下面测的是全局开关而不是 key 状态
	if w := doJSON(http.MethodPost, "/api/mcp/keys/"+createdKeyID+"/status",
		map[string]interface{}{"enabled": true}, true); w.Code != http.StatusOK {
		t.Fatalf("re-enable key = %d: %s", w.Code, w.Body.String())
	}
	globalOff := mcpCall("list_plugin_modules")
	if globalOff.Code != http.StatusServiceUnavailable {
		t.Fatalf("global disabled call = %d, want 503: %s", globalOff.Code, globalOff.Body.String())
	}
	// 恢复
	if w := doJSON(http.MethodPost, "/api/mcp/config", map[string]interface{}{"enabled": true}, true); w.Code != http.StatusOK {
		t.Fatalf("restore mcp = %d: %s", w.Code, w.Body.String())
	}

	// 9) tools/list 必须放行（工具白名单只约束 tools/call），且只列出该 key 允许的工具
	listRec := mcpRequest(router, rawKey, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("tools/list = %d, want 200: %s", listRec.Code, listRec.Body.String())
	}
	if !strings.Contains(listRec.Body.String(), "list_plugin_modules") {
		t.Fatalf("tools/list missing allowed tool: %s", listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), "create_project") {
		t.Fatalf("tools/list leaked a tool outside the allowlist: %s", listRec.Body.String())
	}

	// 10) 向后兼容：治理字段上线前创建的 key（走老接口 /api/apikey/create，无 scope/tools）
	//    必须仍能调 MCP —— 空 scope 视为 *，空工具白名单视为全部。
	legacyName := keyName + "-legacy"
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("api_key").DeleteMany(context.Background(), bson.M{"name": legacyName})
	})
	legacyResp := doJSON(http.MethodPost, "/api/apikey/create", map[string]interface{}{"name": legacyName}, true)
	if legacyResp.Code != http.StatusOK {
		t.Fatalf("legacy create = %d: %s", legacyResp.Code, legacyResp.Body.String())
	}
	legacyData, _ := decode(legacyResp)["data"].(map[string]interface{})
	legacyKey, _ := legacyData["key"].(string)
	if legacyKey == "" {
		t.Fatalf("legacy create missing key: %s", legacyResp.Body.String())
	}
	legacyCall := mcpCallWith(router, legacyKey, "list_plugin_modules")
	if legacyCall.Code != http.StatusOK {
		t.Fatalf("legacy key call = %d, want 200: %s", legacyCall.Code, legacyCall.Body.String())
	}
	if legacyID, _ := legacyData["id"].(string); legacyID != "" {
		t.Cleanup(func() {
			_, _ = mongodb.DB.Collection("mcp_call_logs").DeleteMany(context.Background(), bson.M{"keyId": legacyID})
		})
	}
}
