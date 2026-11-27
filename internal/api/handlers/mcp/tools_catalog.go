package mcp

import (
	"sort"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/services/mcpguard"
	"github.com/gin-gonic/gin"
)

// ToolsCatalog 返回 MCP 工具与所需 scope 的对照表。
//
// 为什么需要它：建 key 时要选 scope，但界面上只有一句手写的「asset:read（资产/…只读）」——
// 用户没法知道某个 scope 到底解锁哪些工具，也不知道某个工具该给什么 scope。
// 真相源是 mcpguard.ToolScopes（新增工具忘登记会在启动日志里报 ERROR），
// 这里只做投影，不做第二份清单。
func ToolsCatalog(c *gin.Context) {
	type toolEntry struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	type scopeEntry struct {
		Scope string   `json:"scope"`
		Tools int      `json:"tools"`
		Names []string `json:"names"`
	}

	names := mcpguard.ToolNames()
	sort.Strings(names)

	list := make([]toolEntry, 0, len(names))
	byScope := make(map[string][]string, 16)
	for _, name := range names {
		scope, _ := mcpguard.RequiredScope(name)
		if scope == "" {
			scope = "*"
		}
		list = append(list, toolEntry{Name: name, Scope: scope})
		byScope[scope] = append(byScope[scope], name)
	}

	scopes := make([]scopeEntry, 0, len(byScope))
	for scope, toolNames := range byScope {
		sort.Strings(toolNames)
		scopes = append(scopes, scopeEntry{Scope: scope, Tools: len(toolNames), Names: toolNames})
	}
	sort.Slice(scopes, func(i, j int) bool {
		if scopes[i].Scope == "*" {
			return false
		}
		if scopes[j].Scope == "*" {
			return true
		}
		return scopes[i].Scope < scopes[j].Scope
	})

	response.Success(c, gin.H{"list": list, "scopes": scopes, "total": len(list)}, "api.success")
}
