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

## 数据导出（v2.0.82 新增，共 4 个）

`list_export_records`（asset:read）历史导出记录：文件名、类型、状态、大小
`get_export_fields`（asset:read）某类数据可导出的字段列表
`create_export_task`（asset:write）异步创建导出任务，返回 `fileName`
`delete_export_files`（asset:write）删除导出文件与记录

> `fileType` 只支持 `xlsx` / `json`：导出文件名校验（`IsValidFileName`）不接受 `csv`，传 csv 会在删除阶段被拒。
> `quantity` 是导出条数上限（`SetLimit`），默认 1000，最大 200000；`fields` 必填，可先用 `get_export_fields` 查。

## 工具总数

42 + 4 = 46。

## 插件安装 / 重检 / 卸载（v2.0.83 新增，共 3 个，`plugin:write`）

`reinstall_plugin` 在节点上重新安装插件
`recheck_plugin` 让节点重新检查插件可用性（不动文件，最安全的验证手段）
`uninstall_plugin` 从节点卸载插件

入参统一：`node` + `hash` + `module`（`recheck/uninstall` 三个都必填，`reinstall` 的 module 可空）。

## 工具总数

46 + 3 = 49。

## 在 UI 里创建带 scope 的 Key（v2.0.84）

「配置 → API Key」页面已切到 MCP 治理接口（`/api/mcp/keys`）：

- 列表新增 **Scope / 限速/分 / 过期** 三列，并支持启用/禁用；
- 新建弹窗可选 13 个 scope（12 个细分 + `*`），并设置每分钟限速（0 = 跟随全局）；
- 明文 key 仍然只在创建后显示一次；列表只存 `keyPrefix` 与哈希。

> 旧的 `/api/apikey/create` 仍然存在但固定 `scope = *`，需要用 scope 控制权限时请走新接口/新 UI。
