package mcp

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

func bindCase(t *testing.T, body string) (models.McpKeyCreateRequest, error) {
	var req models.McpKeyCreateRequest
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/mcp/keys", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	err := bindStrictKey(c, &req)
	return req, err
}

// 写错字段名（allowed_tools 而不是 tools）必须报错。
// 以前会被静默忽略 → tools 为空、scope 落默认 ["*"]，等于把全权 key 发给了只想读的调用方。
func TestCreateKeyRejectsUnknownField(t *testing.T) {
	_, err := bindCase(t, `{"name":"x","allowed_tools":["list_assets"]}`)
	if err == nil {
		t.Fatal("unknown field allowed_tools must be rejected")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateKeyAcceptsKnownFields(t *testing.T) {
	req, err := bindCase(t, `{"name":"x","tools":["list_assets"],"rateLimitPerMin":5}`)
	if err != nil {
		t.Fatalf("known fields must bind: %v", err)
	}
	if req.Name != "x" || len(req.Tools) != 1 || req.Tools[0] != "list_assets" || req.RateLimitPerMin != 5 {
		t.Fatalf("unexpected bind result: %+v", req)
	}
}
