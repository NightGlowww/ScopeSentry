package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/apikey"
	"github.com/Autumn-27/ScopeSentry/internal/services/mcpaudit"
	"github.com/Autumn-27/ScopeSentry/internal/services/mcpguard"
	"github.com/gin-gonic/gin"
)

var mcpApiKeyService apikey.Service

func init() {
	mcpApiKeyService = apikey.NewService()
}

// MCP 校验挂载点：
//
//	/mcp 路由组（internal/mcp/register.go）整体挂在 MCPAPIKeyMiddleware 上，
//	SDK 的 StreamableHTTPHandler 在其后。中间件先解析 JSON-RPC 信封，拿到
//	method 与 params.name（tools/call 的工具名），再做 key 状态 / 过期 / scope /
//	工具白名单 / 限速判定；任一不过就直接写 MCP 规范的 JSON-RPC error 并 Abort，
//	SDK handler 不会被调用（不会出现 panic 或半截 SSE）。
//
// 审计：请求前后打点，durationMs 覆盖整段 handler 执行；结果按 HTTP 状态码判定
// success / error，被治理拦下的记为 denied。写日志走 mcpaudit 的异步批量器。
const (
	// mcpMaxRPCParseBytes 只解析 1MB 以内的 JSON-RPC 信封；更大的 body 原样透传，
	// 避免为了取工具名把大参数整体读进内存。
	mcpMaxRPCParseBytes = 1 << 20

	// JSON-RPC 错误码：-32000 ~ -32099 是规范留给服务端自定义的区间
	rpcCodeServerDisabled = -32000
	rpcCodeUnauthorized   = -32001
	rpcCodeForbidden      = -32003
	rpcCodeRateLimited    = -32029
)

// MCPAPIKeyMiddleware MCP 端点 API Key 认证 + 治理中间件
func MCPAPIKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		metas := readJSONRPC(c)
		auditTool := auditToolName(metas)
		rpcID := primaryRPCID(metas)

		keyID := ""
		keyPrefix := ""

		deny := func(httpStatus, code int, reason string) {
			mcpaudit.Record(models.McpCallLog{
				KeyID:      keyID,
				KeyPrefix:  keyPrefix,
				Tool:       auditTool,
				Status:     models.McpLogStatusDenied,
				DurationMS: time.Since(start).Milliseconds(),
				ClientIP:   c.ClientIP(),
				Error:      reason,
				CreatedAt:  time.Now(),
			})
			// 客户端（如 huntdeck）常常只打印 HTTP 状态码，把原因也放进响应头，
			// 这样即使它不解析 JSON-RPC body，日志里也能看出是「key 被停用」而不是笼统 403。
			c.Header("X-ScopeSentry-Auth", reason)
			writeJSONRPCError(c, httpStatus, rpcID, code, reason)
			c.Abort()
		}

		rawKey := extractAPIKey(c)
		if rawKey == "" {
			deny(http.StatusUnauthorized, rpcCodeUnauthorized,
				"API key is required. Use X-API-Key header or Authorization: Bearer <api_key>")
			return
		}

		record, err := mcpApiKeyService.Validate(c.Request.Context(), rawKey)
		if err != nil {
			if record != nil {
				keyID = record.ID.Hex()
				keyPrefix = record.KeyPrefix
			}
			switch {
			case errors.Is(err, apikey.ErrKeyDisabled):
				deny(http.StatusForbidden, rpcCodeForbidden, "API key is disabled")
			case errors.Is(err, apikey.ErrKeyExpired):
				deny(http.StatusForbidden, rpcCodeForbidden, "API key has expired")
			default:
				deny(http.StatusUnauthorized, rpcCodeUnauthorized, "Invalid API key")
			}
			return
		}
		keyID = record.ID.Hex()
		keyPrefix = record.KeyPrefix

		guard := mcpguard.Default()
		cfg, cfgErr := guard.Config(c.Request.Context())
		if cfgErr != nil {
			// 配置读失败只降级为默认策略（开启/不额外限制），不能让 MCP 整体不可用
			cfg = models.DefaultMcpConfig()
		}

		// 逐个 JSON-RPC 信封校验；批量请求里任何一个越权都整体拒绝
		targets := metas
		if len(targets) == 0 {
			targets = []rpcMeta{{}}
		}
		for _, meta := range targets {
			if err := mcpguard.Check(record, cfg, meta.policyTool(), time.Now()); err != nil {
				status, code := mapGuardError(err)
				deny(status, code, err.Error())
				return
			}
		}

		limit := guard.EffectiveRateLimit(record, cfg)
		if !guard.CheckRateLimit(keyID, limit) {
			deny(http.StatusTooManyRequests, rpcCodeRateLimited, "Rate limit exceeded")
			return
		}

		// 把「该 key 可用工具集合」放进请求 context：SDK 建 server 时据此裁剪
		// tools/list，避免客户端看到无权调用的工具。
		permitted := mcpguard.PermittedTools(record, cfg)
		c.Request = c.Request.WithContext(mcpguard.WithPermittedTools(c.Request.Context(), permitted))

		c.Set("apiKeyID", keyID)
		c.Set("apiKeyName", record.Name)

		c.Next()

		status := models.McpLogStatusSuccess
		errMsg := ""
		if c.Writer.Status() >= http.StatusBadRequest {
			status = models.McpLogStatusError
			errMsg = "http status " + http.StatusText(c.Writer.Status())
		}
		mcpaudit.Record(models.McpCallLog{
			KeyID:      keyID,
			KeyPrefix:  keyPrefix,
			Tool:       auditTool,
			Status:     status,
			DurationMS: time.Since(start).Milliseconds(),
			ClientIP:   c.ClientIP(),
			Error:      errMsg,
			CreatedAt:  time.Now(),
		})
	}
}

// mapGuardError 治理错误 -> (HTTP 状态码, JSON-RPC 错误码)
func mapGuardError(err error) (int, int) {
	switch {
	case errors.Is(err, mcpguard.ErrMCPDisabled):
		return http.StatusServiceUnavailable, rpcCodeServerDisabled
	case errors.Is(err, mcpguard.ErrRateLimited):
		return http.StatusTooManyRequests, rpcCodeRateLimited
	case errors.Is(err, mcpguard.ErrInvalidKey),
		errors.Is(err, mcpguard.ErrKeyDisabled),
		errors.Is(err, mcpguard.ErrKeyExpired),
		errors.Is(err, mcpguard.ErrToolNotAllowed),
		errors.Is(err, mcpguard.ErrScopeDenied):
		return http.StatusForbidden, rpcCodeForbidden
	default:
		return http.StatusForbidden, rpcCodeForbidden
	}
}

// rpcMeta 从 JSON-RPC 信封里抽出的治理所需信息
type rpcMeta struct {
	Method string
	Tool   string // 仅 tools/call 有值
	ID     json.RawMessage
}

// toolName 审计用的「工具名」：
// tools/call 取 params.name；其它方法取 JSON-RPC method（initialize / tools/list …）
func (m rpcMeta) toolName() string {
	if m.Method == "tools/call" {
		return m.Tool
	}
	return m.Method
}

// policyTool 治理判定用的工具名：只有 tools/call 才参与工具白名单与 scope 校验。
// tools/list、initialize、ping 这类握手/发现方法必须放行，否则客户端连工具清单都拿不到。
func (m rpcMeta) policyTool() string {
	if m.Method == "tools/call" {
		return m.Tool
	}
	return ""
}

// readJSONRPC 读请求体解析元信息，并把 body 原样还给后续 handler。
// 只读前 1MB+1 字节，剩余部分留在原 reader 上，用 MultiReader 拼回去。
func readJSONRPC(c *gin.Context) []rpcMeta {
	if c.Request == nil || c.Request.Body == nil || c.Request.Method != http.MethodPost {
		return nil
	}
	orig := c.Request.Body
	head, err := io.ReadAll(io.LimitReader(orig, mcpMaxRPCParseBytes+1))
	// 还原 body：head + 尚未读取的剩余部分
	c.Request.Body = &restoredBody{
		Reader: io.MultiReader(bytes.NewReader(head), orig),
		closer: orig,
	}
	if err != nil || len(head) > mcpMaxRPCParseBytes {
		return nil
	}
	return parseJSONRPC(head)
}

// restoredBody 让还原后的 body 关闭时能关掉原始 body
type restoredBody struct {
	io.Reader
	closer io.Closer
}

func (b *restoredBody) Close() error {
	if b.closer != nil {
		return b.closer.Close()
	}
	return nil
}

// parseJSONRPC 解析单请求或批量请求；解析失败返回 nil（交给 SDK 报标准解析错误）
func parseJSONRPC(body []byte) []rpcMeta {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil
		}
		metas := make([]rpcMeta, 0, len(items))
		for _, item := range items {
			if meta, ok := parseRPCEnvelope(item); ok {
				metas = append(metas, meta)
			}
		}
		return metas
	}
	if meta, ok := parseRPCEnvelope(trimmed); ok {
		return []rpcMeta{meta}
	}
	return nil
}

func parseRPCEnvelope(raw []byte) (rpcMeta, bool) {
	var env struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return rpcMeta{}, false
	}
	meta := rpcMeta{Method: env.Method, ID: env.ID}
	if env.Method == "tools/call" {
		meta.Tool = env.Params.Name
	}
	return meta, true
}

// auditToolName 审计记录里的 tool 字段：优先 tools/call 的工具名，其次首个方法名
func auditToolName(metas []rpcMeta) string {
	for _, m := range metas {
		if m.Method == "tools/call" && m.Tool != "" {
			return m.Tool
		}
	}
	for _, m := range metas {
		if m.Method != "" {
			return m.Method
		}
	}
	return ""
}

// primaryRPCID 错误响应里的 id；取不到就是 null
func primaryRPCID(metas []rpcMeta) json.RawMessage {
	for _, m := range metas {
		if len(m.ID) > 0 && string(m.ID) != "null" {
			return m.ID
		}
	}
	return json.RawMessage("null")
}

// writeJSONRPCError 按 MCP / JSON-RPC 2.0 规范返回错误对象
func writeJSONRPCError(c *gin.Context, httpStatus int, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
	if err != nil {
		payload = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`)
	}
	c.Header("Content-Type", "application/json")
	c.Status(httpStatus)
	_, _ = c.Writer.Write(payload)
}

func extractAPIKey(c *gin.Context) string {
	if key := strings.TrimSpace(c.GetHeader("X-API-Key")); key != "" {
		return key
	}

	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	token := strings.TrimSpace(parts[1])
	if apikey.IsAPIKeyFormat(token) {
		return token
	}
	return ""
}
