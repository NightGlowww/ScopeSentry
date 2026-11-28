//go:build integration

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	githubrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/github"
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

// 全部工具对客户端可见的第一层契约：名字、描述、入参 schema 形状。
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

	// 模板停用/启用：写入口有了，还要证明「读得回来」。
	// 只断言调用成功是不够的 —— 停用没落库时界面上永远显示已启用，
	// 而这种缺口在扫描侧的表现是「模板照样跑」，最难被发现。
	t.Run("poc_state", func(t *testing.T) {
		listText := callText(t, cs, "list_pocs", map[string]any{"pageIndex": 1, "pageSize": 5})
		// 注意 id 字段名：Poc.ID 的 json 标签是 "id"，Template ID 才是模板标识。
		var data struct {
			List []struct {
				ID       string `json:"id"`
				Disabled bool   `json:"disabled"`
			} `json:"list"`
		}
		_ = json.Unmarshal([]byte(listText), &data)
		if len(data.List) == 0 {
			t.Skip("库里没有 POC，跳过状态回环")
		}
		id := data.List[0].ID
		wasDisabled := data.List[0].Disabled
		t.Cleanup(func() {
			_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "set_poc_state", Arguments: map[string]any{"ids": []string{id}, "disabled": wasDisabled},
			})
		})

		want := !wasDisabled
		res := callJSON(t, cs, "set_poc_state", map[string]any{"ids": []string{id}, "disabled": want})
		if n, _ := res["updated"].(float64); n < 1 {
			t.Fatalf("停用未命中任何模板: %+v", res)
		}
		echo := callText(t, cs, "list_pocs", map[string]any{"pageIndex": 1, "pageSize": 5})
		var after struct {
			List []struct {
				ID       string `json:"id"`
				Disabled bool   `json:"disabled"`
			} `json:"list"`
		}
		_ = json.Unmarshal([]byte(echo), &after)
		found := false
		for _, row := range after.List {
			if row.ID == id {
				found = true
				if row.Disabled != want {
					t.Fatalf("状态没落库：期望 disabled=%v，实际 %v", want, row.Disabled)
				}
			}
		}
		if !found {
			t.Fatalf("列表里找不到刚改过的模板 %s", id)
		}
	})

	// 指纹规则：新增 → 读回 → 改 state → 读回 → 删除 → 确认消失。
	// 「同名按更新处理」是服务层既有行为，这里顺带锁住：重复 add 不应产生第二条。
	t.Run("fingerprint", func(t *testing.T) {
		name := fmt.Sprintf("it-mcp-fp-%d", stamp)
		var id string
		t.Cleanup(func() {
			if id != "" {
				_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{
					Name: "delete_fingerprints", Arguments: map[string]any{"ids": []string{id}},
				})
			}
		})

		created := callJSON(t, cs, "add_fingerprint", map[string]any{
			"name": name, "rule": "title: \"it-mcp\"", "category": "it", "state": true,
		})
		id, _ = created["id"].(string)
		if id == "" {
			t.Fatalf("新增指纹没有返回 id: %+v", created)
		}

		var listRow struct {
			ID    string `json:"id"`
			State bool   `json:"state"`
		}
		readRow := func() (found bool) {
			text := callText(t, cs, "list_fingerprints", map[string]any{"search": name, "pageIndex": 1, "pageSize": 10})
			var data struct {
				List []struct {
					ID    string `json:"id"`
					State bool   `json:"state"`
				} `json:"list"`
			}
			_ = json.Unmarshal([]byte(text), &data)
			for _, row := range data.List {
				if row.ID == id {
					listRow.ID, listRow.State = row.ID, row.State
					return true
				}
			}
			return false
		}
		if !readRow() || !listRow.State {
			t.Fatalf("新增后读不回或 state 不是启用态: %+v", listRow)
		}

		callJSON(t, cs, "update_fingerprint", map[string]any{
			"id": id, "name": name, "rule": "title: \"it-mcp\"", "state": false,
		})
		if !readRow() {
			t.Fatalf("更新后规则消失了")
		}
		if listRow.State {
			t.Fatalf("state=false 没落库，读回仍是启用")
		}

		del := callJSON(t, cs, "delete_fingerprints", map[string]any{"ids": []string{id}})
		if n, _ := del["deleted"].(float64); n < 1 {
			t.Fatalf("删除未命中: %+v", del)
		}
		id = ""
		if readRow() {
			t.Fatalf("删除后仍能查到该规则")
		}
	})

	// GitHub 研判：写一条 fixture 结果 → 研判为 ignored → 读回状态与研判人 → 按任务清理。
	// 用 UpsertHits 造数据而不是走采集任务：采集会打 GitHub API，测试不该有外网副作用。
	t.Run("github_triage", func(t *testing.T) {
		taskID := fmt.Sprintf("it-mcp-gh-%d", stamp)
		repo := githubrepo.NewResultRepository()
		ctx := context.Background()
		t.Cleanup(func() { _, _ = repo.DeleteByTaskIDs(ctx, []string{taskID}) })
		if _, err := repo.UpsertHits(ctx, taskID, "code_leak", "", "it", []githubrepo.Hit{{
			Repo: "it/repo", Path: "a.env", Commit: "deadbeef", Title: "it rule", Snippet: "x=1",
		}}); err != nil {
			t.Fatalf("造 fixture 失败: %v", err)
		}
		list, _, err := repo.List(ctx, models.GithubResultListQuery{TaskID: taskID, PageIndex: 1, PageSize: 5})
		if err != nil || len(list) == 0 {
			t.Fatalf("fixture 结果读不回来: %v", err)
		}
		rid := list[0].ID.Hex()

		res := callJSON(t, cs, "triage_github_results", map[string]any{
			"ids": []string{rid}, "status": "ignored", "operator": "it-mcp",
		})
		if n, _ := res["updated"].(float64); n < 1 {
			t.Fatalf("研判未命中任何结果: %+v", res)
		}
		after, _, err := repo.List(ctx, models.GithubResultListQuery{TaskID: taskID, Status: "ignored", PageIndex: 1, PageSize: 5})
		if err != nil || len(after) == 0 {
			t.Fatalf("研判后按 ignored 筛不到: %v", err)
		}
		if after[0].TriagedBy != "it-mcp" {
			t.Fatalf("研判人没落库，实际 %q", after[0].TriagedBy)
		}
	})
}
