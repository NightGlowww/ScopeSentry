# MCP 接口

ScopeSentry 暴露一个 MCP（Model Context Protocol）Server，走 **Streamable HTTP**，与 REST 同一进程同一端口：

- 端点：`POST /mcp`（`/mcp/*` 同样挂在该组下）
- 认证：`Authorization: Bearer <API Key>`，key 在「配置 → API Key」创建（明文只显示一次）
- 治理：`internal/api/middleware/apikey.go` 负责 key 状态/过期/scope/工具白名单/限速/审计，审计落 `mcp_call_logs`

## 权限面（scope）

| scope | 覆盖 |
| --- | --- |
| `asset:read` | 资产、POC、指纹、字典、看板等只读查询 |
| `asset:write` | 资产打标签等写操作 |
| `task:read` / `task:write` | 任务查询 / 创建 |
| `project:read` / `project:write` | 项目查询 / 创建 |
| `template:read` / `template:write` | 扫描模板查询 / 创建 |
| `plugin:read` | 插件与模块查询 |
| `node:read` | 节点查询 |
| `*` | 全部 |

工具名 → scope 的映射在 `internal/services/mcpguard/scope.go`，新增工具必须同步登记（有漂移测试守着）。

## 工具清单（25）

**项目**
`list_projects` `list_projects_data` `get_project` `create_project`

**任务 / 模板**
`list_tasks` `get_task` `create_scan_task` `list_scan_templates` `get_scan_template` `create_scan_template`

**插件 / 节点**
`list_plugin_modules` `list_plugins` `list_nodes`

**资产**
`list_assets`（type 支持 site/subdomain/url/ip/service/vulnerability/sensitive/dirscan/crawler/app/mp/page-monitoring 等）`count_assets` `get_asset_detail` `add_asset_tag`

**只读扩展（v2.0.76 新增）**
`list_pocs` `get_poc` —— POC 列表/详情，可搜索、按危险等级过滤
`list_fingerprints` —— Web 指纹规则
`list_dictionaries` `preview_dictionary` `search_dictionary_lines` —— 字典（port/subdomain/dir/brute）分页、预览、行内搜索
`get_dashboard_stats` `get_console_info` —— 看板七类总量与节点/任务计数、控制台版本与依赖状态

## 调用示例

```bash
curl -s http://127.0.0.1:8082/mcp \
  -H "Authorization: Bearer ssk_xxx" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

```bash
curl -s http://127.0.0.1:8082/mcp \
  -H "Authorization: Bearer ssk_xxx" -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call",
       "params":{"name":"list_pocs","arguments":{"search":"wordpress","pageSize":5}}}'
```

## 写/控工具（v2.0.78 新增，共 9 个）

**任务**
`get_task_progress`（task:read）任务目标级分页进度
`start_task` / `stop_task` / `delete_task`（task:write）启动 / 暂停 / 删除（可带 `deleteAssets`）

**扫描策略**
`list_scan_policies`（task:read）
`create_scan_policy`（task:write）`config` 传 JSON 对象，结构与 UI 导出一致
`delete_scan_policies`（task:write）

**敏感信息规则**
`list_sensitive_rules`（asset:read）
`set_sensitive_rule_state`（asset:write）批量启用 / 停用

> 写/控工具同样受 key 的 scope 与 `tools` 白名单约束；受限 key 调不到未授权工具（默认拒绝）。

## 工具总数

25（只读） + 9（写/控） = 34。

## 节点运维 + 字典维护（v2.0.80 新增，共 8 个）

**节点**（`node:read` / `node:write`）
`list_node_plugins` 节点上各插件的 install/check 状态
`get_node_logs` 节点日志
`restart_node` 重启节点
`delete_nodes` 批量删除节点记录

**字典**（`asset:write`）
`create_dictionary` 新建（重名报错），`content` 或 `lines` 二选一
`append_dictionary_lines` 追加行（唯一索引去重）
`delete_dictionary` 按 kind + name 删除整个字典
`delete_dictionary_entries` 删除指定行

> 字典 kind 取值：`subdomain` / `dir` / `brute` / `cdn` / `other`；`port` 属旧版字典接口，不走这套 parity API。默认 kind 为 `subdomain`。

## 工具总数

34 + 8 = 42。
