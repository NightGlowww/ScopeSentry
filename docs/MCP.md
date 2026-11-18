# MCP 接口

ScopeSentry 暴露一个 MCP（Model Context Protocol）Server，走 **Streamable HTTP**，与 REST 同一进程同一端口：

- 端点：`POST /mcp`（`/mcp/*` 同样挂在该组下）
- 认证：`Authorization: Bearer <API Key>`，key 在「配置 → API Key」创建（明文只显示一次）
- 治理：`internal/api/middleware/apikey.go` 负责 key 状态/过期/scope/工具白名单/限速/审计，审计落 `mcp_call_logs`

## 权限面（scope）

| scope | 覆盖 |
| --- | --- |
| `asset:read` | 资产、POC、指纹、字典、看板、资产组/地契、ICP 任务、服务端操作日志等只读查询 |
| `asset:write` | 资产打标签、导出、敏感规则启停、字典维护，以及**资产组/地契的写操作**（建组、删组、建地契） |
| `icp:write` | ICP 主体任务写操作：创建任务（会对外部 ICP 源发起查询并后台跑）、停止任务（可暂停可恢复） |
| `task:read` / `task:write` | 任务查询 / 创建 |
| `project:read` / `project:write` | 项目查询 / 创建 |
| `template:read` / `template:write` | 扫描模板查询 / 创建 |
| `plugin:read` / `plugin:write` | 插件与模块查询 / 节点上安装·重检·卸载 |
| `node:read` / `node:write` | 节点查询 / 重启·删除节点 |
| `*` | 全部 |

> 共 13 个细分 scope + `*`，与 `internal/services/mcpguard/scope.go` 的常量、前端 `ApiKey.vue` 的 `SCOPE_OPTIONS` 三方一致（新增 scope 必须三处同改，否则 UI 里选不到）。

工具名 → scope 的映射在 `internal/services/mcpguard/scope.go`，新增工具必须同步登记（有漂移测试守着）。

## 工具清单（只读面，按版本累加；总数以文末 `ToolScopes` 口径为准，当前 58）

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
- 新建弹窗可选 14 个 scope（13 个细分 + `*`：asset/task/project/template/plugin/node 各 read·write，外加 icp:write），并设置每分钟限速（0 = 跟随全局）；
- 明文 key 仍然只在创建后显示一次；列表只存 `keyPrefix` 与哈希。

> 旧的 `/api/apikey/create` 仍然存在但固定 `scope = *`，需要用 scope 控制权限时请走新接口/新 UI。

## 调用日志页面（v2.0.85）

「配置 → MCP 调用日志」（`#/configuration/mcp-audit`）：

- 列：时间 / 工具 / 状态(success·denied·error) / 耗时(ms) / Key 前缀 / 客户端 IP / 错误；
- 筛选：Key ID、工具名、状态、日期范围；
- 导出：`GET /api/mcp/logs/export`，CSV 流式输出（上限 10 万行）。

数据来自 `mcp_call_logs`，由 `apikey` 中间件异步批量写入，主请求不等 Mongo。

## 全局配置（v2.0.86）

「配置 → MCP 调用日志」顶部新增「全局配置」卡片，对应 `GET/POST /api/mcp/config`：

- **启用 MCP**：关掉后所有 `/mcp` 调用返回 `503 {"code":-32000,"message":"mcp is disabled"}`；
- **全局限速/分**：0 = 不限；
- **日志保留条数**：`mcp_call_logs` 保留上限（默认 10 万）；
- **工具白名单**：留空 = 全部；填了就只放行列表内的工具（可与 key 级 `tools` 白名单叠加）。

## 资产组 / 地契 / ICP / 审计（v2.0.87 新增，共 4 个，只读）

补齐 `docs/audit/MCP_GAPS.md` 的 P0/P1 只读缺口（M1 / M2 / M3 / M12）。四项**全部复用 `asset:read`**，不新增 scope 家族 —— 生产受限 key 的授予集是 `{asset:read, task:read, plugin:read, node:read}`，新增 `icp:read` / `audit:read` 会让这些 key 立刻调不到新工具，且要同步改前端 `ApiKey.vue` 的 `SCOPE_OPTIONS`。

| 工具 | scope | 后端等价 | 入参 |
| --- | --- | --- | --- |
| `list_asset_groups` | asset:read | POST /api/assets/asset_group/list | 无 |
| `list_asset_scopes` | asset:read | POST /api/assets/asset_scope/list | groupId / search / pageIndex / pageSize（≤500） |
| `list_icp_tasks` | asset:read | POST /api/icp/task/list | state / type / keyword / provider / pageIndex / pageSize（≤200） |
| `list_audit_logs` | asset:read | POST /api/audit/list | user / path / method / statusMin / pageIndex / pageSize（≤200） |

入参字段与后端 handler **同名同义**（直接读 handler 绑定结构与 service/repo 签名对齐，不是猜的）：

- `list_asset_scopes` → `internal/api/handlers/assets/assetgroup/handler.go` 的 ListScopes 匿名结构 + `assetgroup.ListScopes(groupID, search, pageIndex, pageSize)`
- `list_icp_tasks` → `internal/api/handlers/icp/handler.go` 的 `taskListRequest` → `models.IcpTaskListQuery` → `icpsvc.Service.ListTasks`
- `list_audit_logs` → `models.AuditLogQuery`（handler 直接绑它）→ `auditrepo.Repository.List`

**与 REST 的差异（只有一处，且是收紧）**：MCP 不经 gin `ShouldBindJSON`，handler 里的校验分支不会自动生效，所以由 `internal/services/mcpguard/params.go` 的 `Normalize*` 补回：
分页上限（ICP/审计 200、地契 500）、ICP `state` 只能 0-3、`type` 只能 company/domain、`provider` 只能 manual/none/http、审计 `method` 闭集、`statusMin` 只能 0 或 100-599、地契 `groupId` 必须 24 位 ObjectID 或 `unassigned`。
越界一律返回错误，而不是让后端过滤恒不命中、返回「成功 + 空列表」（这是 `MCP_GAPS.md` §5.1 记录的同类静默失败）。

典型链路：`list_asset_groups` 找集团 → `list_asset_scopes` 看覆盖的域名/IP → `list_assets` 查这些域名的资产；`list_icp_tasks` 看「企业名 → 域名」任务的进度；`list_audit_logs`（`path=/api/task/add&method=POST`）自证写操作是否落到服务端。

> 写侧第一批已补 5 个（见下节）；仍未暴露：资产组编辑/重排、地契编辑/删除/批量移动/绑定企业/清理同步错误、ICP 任务重启/恢复/删除、ICP 结果入库与地契同步（`/api/icp/{sync,backfill_details}`）、备份、通知配置。

## 写侧第一批：资产组 / 地契 / ICP 任务（v2.0.88 新增，共 5 个）

按 `docs/audit/MCP_GAPS.md` §3.1 的 M1/M2/M3 补写侧最小闭环，只做 5 个，其余写操作仍在 Web 端（见 `docs/audit/MCP_WRITE_TOOLS.md`）。

| 工具 | scope | 后端等价 | 入参 |
| --- | --- | --- | --- |
| `create_asset_group` | asset:write | POST /api/assets/asset_group/add | name（1-64，全局唯一）/ description / sortOrder |
| `delete_asset_groups` | asset:write | POST /api/assets/asset_group/delete（逐 id） | ids（1-20，**必填**）/ action（dissolve·transfer）/ targetGroupId / **confirm=true** |
| `create_asset_scope` | asset:write | POST /api/assets/asset_scope/add | name（1-128）/ scope（域名·IP 文本）/ scopeType（domain·ip·mixed·留空推断）/ groupId |
| `create_icp_task` | icp:write | POST /api/icp/task/create | type（company·domain，默认 domain）/ queries / target / name / provider（manual·none·http） |
| `stop_icp_tasks` | icp:write | POST /api/icp/task/stop | ids（1-50） |

**高风险防护（删除类）**：`delete_asset_groups` 是唯一不可逆工具（集团记录物理删除，无回滚接口），**必须显式传 `confirm: true`**，缺省或传 `false` 直接返回 `isError`（`confirm is required`），不会「默认执行」——MCP 调用方是 Agent，没有 Web 端那种二次确认弹窗。`stop_icp_tasks` 是把状态改成 2（已暂停）、可用 REST `resume` 续跑，因此不要求 confirm。

**两处比 REST 更严**（MCP 侧收紧，都在 `mcpguard.Normalize*` 里）：

- `action=dissolve` 时传 `targetGroupId` 直接报错（REST 静默忽略，容易让人以为地契被划转了）；
- `targetGroupId` 不允许是本批要删的集团之一（REST 只挡 self-transfer，挡不住「A 转给同批的 B、B 转给 A」然后一起删）。

**入参校验一律显式报错，不静默**：ids 去空白/转小写/去重后再判上限（20 / 50），非法 ObjectID 直接拒；名称按 rune 计长（中文按字符不按字节）；`create_icp_task` 的 `queries` 与 `target` 都为空时报错而不是建空任务；`create_asset_scope` 的 `groupId` 只接受 24 位 ObjectID 或留空（`unassigned` / `0` 是只读过滤标记，写不进去）。

**典型闭环**：`create_icp_task`（企业名 → 备案域名）→ `list_icp_tasks` 轮询 state/stats → `create_asset_group` 建集团 → `create_asset_scope` 把域名落成地契 → `list_assets` 查覆盖资产。

## 工具总数

53 + 5 = 58。

> 权威口径是 `internal/services/mcpguard/scope.go` 的 `ToolScopes` 条目数（当前 58），与 `internal/mcp` 实际注册的工具名一一对应（漂移测试 `TestEveryRegisteredToolHasScope` / `TestNoOrphanScopeEntries`，`-tags integration`）。上面按版本累加的数字只是历史，**对不上时以 `ToolScopes` 为准**。
