package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// MaxJSONPageSize 是 JSON 列表请求里 pageSize 的硬上限。
//
// 依据（2026-11-28 实测 /api/assets/asset）：
//   pageSize=5000   →  5000 行 /  4.2MB / 2.7s
//   pageSize=100000 → 45363 行 / 22.1MB / 15.9s
// 单个请求就能把一个核顶住十几秒、堆一次性涨上百 MB；而前端最大只用到 1000，
// 导出走的是独立的游标/流式路径，不经过这里。
const MaxJSONPageSize = 5000

// maxClampBodyBytes 超过这个体量的 JSON 不做重写：那是导入类请求，不是列表查询。
const maxClampBodyBytes = 1 << 20

// PageSizeLimit 把 JSON body 里的 pageSize 收敛到 max。
//
// 选择中间件而不是逐个 service 改：分页散落在 84 处 SetLimit、21 个用 SearchRequest 的 service，
// 漏改一处就等于没改；这里一处覆盖全部 /api 列表接口，且对 multipart 与超大 body 直接放行。
func PageSizeLimit(max int) gin.HandlerFunc {
	if max <= 0 {
		max = MaxJSONPageSize
	}
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		ct := c.GetHeader("Content-Type")
		if !strings.Contains(ct, "application/json") {
			c.Next()
			return
		}
		if c.Request.ContentLength <= 0 || c.Request.ContentLength > maxClampBodyBytes || c.Request.Body == nil {
			c.Next()
			return
		}

		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxClampBodyBytes+1))
		if err != nil {
			c.Next()
			return
		}
		if len(body) > maxClampBodyBytes {
			// body 超限：原样放回，交给 handler 自己处理
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			c.Next()
			return
		}

		out, from, ok := clampPageSizeJSON(body, max)
		if ok {
			c.Request.Body = io.NopCloser(bytes.NewReader(out))
			c.Request.ContentLength = int64(len(out))
			c.Header("X-PageSize-Clamped", strconv.Itoa(from)+"->"+strconv.Itoa(max))
			logger.Warn("api: pageSize clamped",
				zap.String("path", c.Request.URL.Path),
				zap.Int("from", from),
				zap.Int("to", max))
		} else {
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
		}
		c.Next()
	}
}

// clampPageSizeJSON 把 body 里超过 max 的 pageSize 改小，返回新 body 与原值。
// 只在确实需要改的时候返回 ok=true，避免无谓的重新序列化。
func clampPageSizeJSON(body []byte, max int) (out []byte, from int, ok bool) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil || m == nil {
		return nil, 0, false
	}
	v, exists := m["pageSize"]
	if !exists {
		return nil, 0, false
	}
	f, isNum := v.(float64)
	if !isNum || f <= float64(max) {
		return nil, 0, false
	}
	m["pageSize"] = max
	nb, err := json.Marshal(m)
	if err != nil {
		return nil, 0, false
	}
	return nb, int(f), true
}
