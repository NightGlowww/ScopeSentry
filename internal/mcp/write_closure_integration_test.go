//go:build integration

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 走真实的 MCP 协议栈（server + in-memory transport），不直接调 handler。
//
// 直接调 handler 只证明「函数能跑」；走协议才覆盖 schema 校验、参数反序列化、
// 结果封装这几层——客户端看到的行为就是这几层决定的。
func newTestClient(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := newServer(nil)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "it-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// callText 调工具并返回正文；工具回错误直接让用例失败。
//
// 注意区分两件事：正文（成功时的 JSON 数据）和错误（IsError）。
// 把「正文非空」当错误会让所有成功调用看起来都失败。
func callText(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s 调用失败: %v", name, err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if res.IsError {
		t.Fatalf("%s 返回错误: %s", name, text)
	}
	return text
}

// callJSON 在 callText 基础上解析成对象（数组结果包成 items，纯文本包成 raw）。
func callJSON(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	text := callText(t, cs, name, args)
	if strings.TrimSpace(text) == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err == nil {
		return out
	}
	var arr []any
	if err := json.Unmarshal([]byte(text), &arr); err == nil {
		return map[string]any{"items": arr}
	}
	return map[string]any{"raw": text}
}

// 82 个工具对客户端可见的第一层契约：名字、描述、入参 schema 形状。
// 描述为空的工具在客户端里只剩一个名字，调用方无从判断该不该用。
func TestToolsListContract(t *testing.T) {
	cs := newTestClient(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("tools/list 为空")
	}
	seen := map[string]bool{}
	for _, tool := range res.Tools {
		if tool.Name == "" {
			t.Fatal("存在无名工具")
		}
		if seen[tool.Name] {
			t.Fatalf("工具名重复: %s", tool.Name)
		}
		seen[tool.Name] = true
		if strings.TrimSpace(tool.Description) == "" {
			t.Fatalf("工具 %s 没有描述", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("工具 %s 没有入参 schema", tool.Name)
		}
	}
	if len(seen) != len(ToolNames()) {
		t.Fatalf("tools/list %d 个，注册表 %d 个", len(seen), len(ToolNames()))
	}
}

// 写工具闭环：MCP 建 → MCP 读回 → MCP 删 → 确认消失。
//
// 只看「调用成功」不够：写工具回成功但没落库，客户端以为建好了，
// REST/界面上却什么都没有——这正是最难被发现的一类闭环缺口。
func TestWriteToolClosure(t *testing.T) {
	cs := newTestClient(t)
	stamp := time.Now().UnixNano()

	t.Run("dictionary", func(t *testing.T) {
		name := fmt.Sprintf("it-mcp-dict-%d", stamp)
		t.Cleanup(func() {
			_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "delete_dictionary", Arguments: map[string]any{"kind": "dir", "name": name},
			})
		})

		callJSON(t, cs, "create_dictionary", map[string]any{
			"kind": "dir", "name": name, "lines": []string{"alpha", "beta"},
		})
		callJSON(t, cs, "append_dictionary_lines", map[string]any{
			"kind": "dir", "name": name, "lines": []string{"gamma"},
		})
		preview := callText(t, cs, "preview_dictionary", map[string]any{
			"kind": "dir", "name": name, "limit": 50,
		})
		if !strings.Contains(preview, "gamma") {
			t.Fatalf("追加的行读不回来: %s", preview)
		}

		callJSON(t, cs, "delete_dictionary", map[string]any{"kind": "dir", "name": name})
		listText := callText(t, cs, "list_dictionaries", map[string]any{
			"kind": "dir", "search": name, "pageIndex": 1, "pageSize": 20,
		})
		if strings.Contains(listText, name) {
			t.Fatalf("删除后字典仍在列表里: %s", listText)
		}
	})

	t.Run("asset_group", func(t *testing.T) {
		name := fmt.Sprintf("it-mcp-group-%d", stamp)
		id := ""
		t.Cleanup(func() {
			if id != "" {
				_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{
					Name: "delete_asset_groups", Arguments: map[string]any{"ids": []string{id}, "confirm": true},
				})
			}
		})

		callJSON(t, cs, "create_asset_group", map[string]any{"name": name})
		listText := callText(t, cs, "list_asset_groups", map[string]any{})
		var data struct {
			Items []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"items"`
			List []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"list"`
		}
		_ = json.Unmarshal([]byte(listText), &data)
		rows := data.Items
		if len(rows) == 0 {
			rows = data.List
		}
		for _, row := range rows {
			if row.Name == name {
				id = row.ID
			}
		}
		if id == "" {
			t.Fatalf("新建的资产组在列表里查不到: %s", listText)
		}

		callJSON(t, cs, "delete_asset_groups", map[string]any{"ids": []string{id}, "confirm": true})
		id = ""
		afterText := callText(t, cs, "list_asset_groups", map[string]any{})
		if strings.Contains(afterText, name) {
			t.Fatalf("删除后资产组仍在列表里: %s", afterText)
		}
	})

	t.Run("scan_policy", func(t *testing.T) {
		name := fmt.Sprintf("it-mcp-policy-%d", stamp)
		id := ""
		t.Cleanup(func() {
			if id != "" {
				_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{
					Name: "delete_scan_policies", Arguments: map[string]any{"ids": []string{id}},
				})
			}
		})

		created := callJSON(t, cs, "create_scan_policy", map[string]any{"name": name})
		id, _ = created["id"].(string)
		if id == "" {
			listText := callText(t, cs, "list_scan_policies", map[string]any{"pageIndex": 1, "pageSize": 50})
			var data struct {
				List []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"list"`
			}
			_ = json.Unmarshal([]byte(listText), &data)
			for _, row := range data.List {
				if row.Name == name {
					id = row.ID
				}
			}
			if id == "" {
				t.Fatalf("新建的扫描策略查不到: %s", listText)
			}
		}

		callJSON(t, cs, "delete_scan_policies", map[string]any{"ids": []string{id}})
		id = ""
		afterText := callText(t, cs, "list_scan_policies", map[string]any{"pageIndex": 1, "pageSize": 50})
		if strings.Contains(afterText, name) {
			t.Fatalf("删除后扫描策略仍在列表里: %s", afterText)
		}
	})
}
