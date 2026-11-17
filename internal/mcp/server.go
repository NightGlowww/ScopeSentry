package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newServer 建一个 MCP server。
// allowedTools 为 nil 表示不限制；非 nil 时只注册集合内的工具，
// 这样 tools/list 不会被治理中间件拦下却又把无权工具列给客户端看。
func newServer(allowedTools map[string]bool) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "scopesentry",
		Version: constants.Version,
	}, nil)

	registerTools(server, allowedTools)
	return server
}

func jsonToolResult(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

func errorResult(msg string, err error) (*mcp.CallToolResult, any, error) {
	text := msg
	if err != nil {
		text = fmt.Sprintf("%s: %v", msg, err)
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, nil, nil
}
