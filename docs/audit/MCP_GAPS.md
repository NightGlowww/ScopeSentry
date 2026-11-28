# MCP 工具面 vs 产品能力 对齐审计（只读）

审计对象：本仓库 HEAD（Go+Gin+Vue3），生产实例 `prod-tx-sh-app-02`（隧道 `http://127.0.0.1:18082`）。
审计方式：源码静态核对 + 生产只读查询（Mongo `--eval` 只读 / REST 只读 GET·查询类 POST）。未改任何产品代码、未重启服务、未改数据。

---

## 0. 结论摘要

1. **工具面核实为 49 个，与 scope 表 49 条一一对应，无漂移**（脚本比对通过）。但 `docs/MCP.md` 对工具总数、asset_type 取值、scope 表三处与实现不一致（见 §7）。
2. **后端有 280 条 REST 路由条目，MCP 只覆盖约 40 条等价能力**；缺失集中在：资产组/企业、ICP 主体、GitHub 泄露与威胁情报、FOFA、备份恢复、通知与全局配置、资产读模型（site/cert/cip/wih/nuclei/http_status/stat_finger/fileleak）、计划任务、任务重测/同步、导出批量与下载。
3. **发现 12 处 schema/语义不一致**，其中 6 处会造成「调用成功但结果错/为空」的静默失败（不是报错，最难排查）：数值型 filter 恒 0 命中（§5.1）、`get_dashboard_stats` 传项目名恒 0（§5.3）、`create_scan_task` 计划任务被忽略（§5.5）、`create_export_task` 无时间范围（§5.6）、`list_pocs` 搜不到模板 ID（§5.2）、`list_tasks` 状态可能停留 pending（§5.4）。
4. **最低成本补齐路径**：优先「扩一个已有工具的入参枚举」（`list_assets.asset_type` 加 site/service/读模型）+ 「加一批只读工具」（资产组、ICP、计划任务、备份、GitHub、系统/趋势、审计），全部复用现有 scope，受限 key 立即可用；写类工具第二批。
5. **治理注意**：生产存在一条无 scope 的历史 key（`ssk_nSt64HuF...`），按 `ScopeAllowed` 空列表=全权（scope.go:150-162），等价于 `*`；新增工具对它是全放行的。

---

## 1. 当前 MCP 工具面（49）

来源：`internal/mcp/tools.go:105-360`（注册）+ `internal/services/mcpguard/scope.go:42-97`（scope 唯一真相源）。

### 1.1 项目 / 任务 / 模板 / 插件（20）

| 工具 | scope | 后端等价 |
| --- | --- | --- |
| list_projects | project:read | GET /api/project/all |
| list_projects_data | project:read | POST /api/project/data |
| get_project | project:read | POST /api/project/content |
| create_project | project:write | POST /api/project/add（实际走 ApplyProjectScope，语义不同，见 §5.10） |
| list_tasks | task:read | POST /api/task |
| get_task | task:read | POST /api/task/detail |
| create_scan_task | task:write | POST /api/task/add（缺计划任务分支，见 §5.5） |
| list_scan_templates | template:read | POST /api/task/template |
| get_scan_template | template:read | POST /api/task/template/detail |
| create_scan_template | template:write | POST /api/task/template/save |
| list_plugin_modules | plugin:read | 常量 internal/constants（无路由） |
| list_plugins | plugin:read | POST /api/plugin、POST /api/plugin/module |
| list_nodes | node:read | GET /api/node、GET /api/node/online |
| list_node_plugins | node:read | POST /api/node/plugin |
| get_node_logs | node:read | POST /api/node/log |
| restart_node | node:write | POST /api/node/restart |
| delete_nodes | node:write | POST /api/node/delete |
| reinstall_plugin | plugin:write | POST /api/plugin/reinstall |
| recheck_plugin | plugin:write | POST /api/plugin/recheck |
| uninstall_plugin | plugin:write | POST /api/plugin/uninstall |

### 1.2 资产 / POC / 指纹 / 字典 / 看板（13）

| 工具 | scope | 后端等价 |
| --- | --- | --- |
| list_assets | asset:read | POST /api/assets/{asset,root_domain,subdomain,app,mp,url,ip,sensitive,dirscan,crawler,vulnerability,page-monitoring,subdomain/taker} |
| count_assets | asset:read | POST /api/assets/common/total |
| get_asset_detail | asset:read | POST /api/assets/asset/detail、POST /api/assets/vulnerability/detail |
| add_asset_tag | asset:write | POST /api/assets/common/add_tag |
| list_pocs | asset:read | POST /api/poc |
| get_poc | asset:read | POST /api/poc/detail |
| list_fingerprints | asset:read | POST /api/fingerprint/data |
| list_dictionaries | asset:read | POST /api/dictionary/list（parity 接口） |
| preview_dictionary | asset:read | POST /api/dictionary/preview |
| search_dictionary_lines | asset:read | POST /api/dictionary/search |
| get_dashboard_stats | asset:read | POST /api/dashboard/stats |
| get_console_info | asset:read | GET /api/console/info |
| get_task_progress | task:read | POST /api/task/progress/info |

### 1.3 写/控：任务 / 策略 / 敏感规则（8）

| 工具 | scope | 后端等价 |
| --- | --- | --- |
| start_task | task:write | POST /api/task/start |
| stop_task | task:write | POST /api/task/stop |
| delete_task | task:write | POST /api/task/delete |
| list_scan_policies | task:read | POST /api/policy/list |
| create_scan_policy | task:write | POST /api/policy/create |
| delete_scan_policies | task:write | POST /api/policy/delete |
| list_sensitive_rules | asset:read | POST /api/sensitive/data |
| set_sensitive_rule_state | asset:write | POST /api/sensitive/update/state |

### 1.4 字典维护 / 导出（8）

| 工具 | scope | 后端等价 |
| --- | --- | --- |
| create_dictionary | asset:write | POST /api/dictionary/create |
| append_dictionary_lines | asset:write | POST /api/dictionary/append |
| delete_dictionary | asset:write | POST /api/dictionary/delete_file |
| delete_dictionary_entries | asset:write | POST /api/dictionary/delete_entries |
| list_export_records | asset:read | GET /api/export/records |
| get_export_fields | asset:read | POST /api/export/fields |
| create_export_task | asset:write | POST /api/export |
| delete_export_files | asset:write | POST /api/export/delete |

> 计数核对：20 + 13 + 8 + 8 = 49；`mcpguard.ToolScopes` 亦为 49 条，双向差集为空（脚本比对 `Name:` 提取 vs `"tool": ScopeX` 提取）。

---

## 2. 后端能力总览（280 条路由条目）

按模块统计（`internal/api/routes/**`，剔除 `_test.go` 与注释掉的 role 路由）：

| 模块 | 条目 | MCP 覆盖 | 缺口概述 |
| --- | --- | --- | --- |
| assets | 75 | ~15 | 资产组/企业 12 条全缺；site/cert/cip/http_status/service/stat_finger/nuclei_result/wih/fileleak/npoc/statistics/tag_color 全缺；写操作（delete/delete_by_search/update_status/delete_tag/remark/add(_batch)/card/changelog/hosts/by-host）全缺 |
| github | 26 | 0 | 泄露任务、调度、结果、监控、CVE/黑客/工具情报全缺 |
| task | 25（task 12 + scheduled 9 + template 4） | 11 | retest/sync/target pause·resume/names 缺；scheduled 9 条全缺；template delete 缺 |
| dictionary | 23 | 7 | port 字典、cdn/brute 字典、upload_large/status、download 缺 |
| plugin | 17 | 4 | detail/save/delete/import(+data)/log/log_clean/status/run/key_check/remote_search 缺 |
| configuration | 14 | 0 | subfinder/rad/system/dedup 配置 + 通知（增删改查）全缺 |
| icp | 14 | 0 | 主体任务 CRUD/日志/统计/资产/同步/回填/导出 全缺 |
| mcp | 9 | —（治理面，不面向工具） | keys/logs/config 不可由 MCP 自读 |
| user | 8 | 0 | 用户管理（不建议暴露写） |
| export | 8 | 4 | download/batch/task/:taskId/preview 缺 |
| poc | 7 | 2 | add/update/delete/data_all/data_import 缺 |
| node | 7 | 4 | config/update 缺（online 已由 list_nodes 覆盖） |
| backup | 7 | 0 | create/list/status/download/delete/restore/upload 全缺 |
| policy | 7 | 3 | update/detail/apply/test 缺 |
| fingerprint | 6 | 1 | add/update/delete/batch_add/version 缺 |
| project | 6 | 4 | update/delete 缺 |
| fofa | 5 | 0 | status/search/task/task list/config 全缺 |
| sensitive_rule | 5 | 2 | add/update/delete 缺 |
| dashboard | 5 | 1 | trend/logs/sysinfo/widgets 缺 |
| apikey | 3 | 0 | 旧 key 管理（不建议暴露） |
| system / audit / console | 1/1/1 | 1 | version、audit/list 缺 |

> 生产数据量（只读统计，用于判断补齐后是否立刻有价值）：asset_group 0、asset_scope 0、icp_task 0、icp_result 0、fofa_import 0、github_result 0、github_threat_history 0、notification 0、export 0；asset 122、subdomain 198、PocList 14338、FingerprintRules 6863、task 6、ScanTemplates 4。即：**ICP/FOFA/GitHub/资产组属于「已实现但尚未投产使用」的 ARL-Next parity 面**，补齐是按产品能力而非按现网数据。

---

## 3. 「产品能做但 MCP 调不到」清单

### 3.1 必须补（P0/P1，能力缺口会造成 Agent 无法完成闭环任务）

| # | 能力 | 后端端点 | 缺 MCP 后果 |
| --- | --- | --- | --- |
| M1 | 资产组（集团）增删改查 + 重排 | `/api/assets/asset_group/{list,add,edit,delete,reorder}` | Agent 无法回答「这个集团覆盖了哪些域名」，也无法把新资产归组 |
| M2 | 资产地契（scope）增删改/批量移动/绑定企业 | `/api/assets/asset_group/{list,add,update,delete,batch_move_group,bind_enterprise,clear_update_error}`（同前缀，第二组路由） | 无法做资产归属纠偏（这是 ARL-Next 的核心资产模型） |
| M3 | ICP 主体任务（创建/停止/恢复/删除/日志/统计/总览） | `/api/icp/task/*`（11 条） | 无法用「企业名 → 域名 → 资产」的链路，只能靠外部 OSINT |
| M4 | ICP 结果资产 + 同步 + 回填 | `/api/icp/{asset,sync,backfill_details,export}` | 主体查到的域名无法落库/回填，Agent 只能出报告不能进资产库 |
| M5 | 站点聚合视图 | `/api/assets/site/{list,chain}` | `list_assets` 只能按 host 逐行，拿不到「站点」聚合（assetCount/lastSeen），MCP 文档却声称支持 site（见 §5.8） |
| M6 | 任务重测 | `POST /api/task/retest` | **暂不补**：重测等于新建一批扫描并投给节点，属于「会真的打流量」的动作，需要有意的操作者决策点，不放给自动化调用（与「不擅自加扫描任务」的约定一致）。Agent 可用 `create_scan_task` 显式建新任务代替 |
| M7 | 任务同步到项目 | `POST /api/task/sync` | **部分已补（2026-11-28）**：`assign_tasks_project` 给已有任务补录/解除项目归属（更新 0 条按失败返回）。仍缺「顺便新建项目」这条（对应 `/api/task/sync` 的 option=new），建项目请用 `create_project` 再 assign |
| M8 | 目标级暂停/恢复 | `POST /api/task/target/{pause,resume}` | **已补（2026-11-28）**：`pause_task_targets` / `resume_task_targets`，按目标回执，单个目标失败不影响其余 |
| M9 | 计划任务 CRUD | `/api/scheduled/{add,detail,update,delete}` + `/pagemonit/*` | **部分已补（2026-11-28）**：新增 `list_scheduled_tasks` / `get_scheduled_task` / `set_scheduled_task_state`，且 `create_scan_task(scheduledTasks=true)` 现在真的走 `CreateScheduledTask`（§5.5 已修）。仍缺：改周期参数、删除计划任务（删除可复用 `delete_task`？两者同一集合，但语义不同，未合） |
| M10 | 备份只读面 | `GET /api/backup/{list,status}` | **已补（2026-11-28）**：`list_backups`（scope `system:read`）返回文件清单 + 当前任务状态；创建/恢复/删除仍不给 MCP 入口 |
| M11 | GitHub 泄露/威胁情报只读 | `/api/github/result/{list,overview,stats}`、`/api/github/threat/{cve_history,token_status}` | 情报面完全黑盒（26 条路由 0 覆盖） |
| M12 | 系统/看板趋势 + 审计日志 | `GET /api/system/version`、`POST /api/dashboard/{trend,logs}`、`GET /api/dashboard/sysinfo`、`POST /api/audit/list` | Agent 无法自证「我改了什么/系统是否健康」 |

### 3.2 建议补（P2/P3，提升体验与自动化深度）

| # | 能力 | 端点 | 备注 |
| --- | --- | --- | --- |
| S1 | FOFA 检索与批量建任务 | `/api/fofa/{status,search,task,task/list,config}` | 建议只读 `fofa_status`+`fofa_search`；`fofa_config`（写 key）P3 |
| S2 | 资产读模型 | `/api/assets/{cert,cip,http_status,service,stat_finger,nuclei_result,wih,fileleak,npoc}` | 建议合并为 `list_asset_read_model(kind, ...)` 单工具，避免 9 个碎片工具 |
| S3 | 资产统计分布 | `/api/assets/statistics/{port,service,app,icon,title}`、`/api/assets/statistics` | **已补（2026-11-28）**：`asset_distribution(dimension=port\|service\|app\|title\|icon)`，输出统一成 `[{value, count, iconHash?}]`，并标出聚合上限（port/service ≤200 桶、title ≤50 桶时 `truncated=true`）。实测 port/service Top5 与 REST 完全一致 |
| S4 | 导出下载/预览/批量 | `GET /api/export/download`、`POST /api/export/{preview,batch}`、`GET /api/export/task/:taskId` | MCP 传不了二进制，建议返回可下载路径/URL 而非文件流 |
| S5 | 模板更新/删除 | `POST /api/task/template/{save,delete}` | MCP 只能建不能改，建错的模板永久堆积 |
| S6 | POC 增改删/导入 | `/api/poc/{add,update,delete,data/import,data/all}` | 导入会触发节点同步，属写操作，P2 |
| S7 | 指纹增改删/批量 | `/api/fingerprint/{add,update,delete,batch-add,version}` | P2 |
| S8 | 敏感规则增改删 | `/api/sensitive/{add,update,delete}` | P2 |
| S9 | 字典传输 | `GET /api/dictionary/download`、`POST /upload_large`、`GET /upload_status`、port/cdn/brute 系列 | 大字典上传建议走 upload_large + 状态轮询 |
| S10 | 插件详情/日志/运行/导入 | `/api/plugin/{detail,log,log/clean,status,run,import,import/data,key/check,remote/search}` | `run`/`import` 影响节点执行，P3 |
| S11 | 节点配置更新 | `POST /api/node/config/update` | P3（改节点并发会影响全量扫描） |
| S12 | 通知配置 | `/api/configuration{,/data,/add,/update,/delete}`（notification 两组） | 读 P2、写 P3；写通知等于给出外发通道 |
| S13 | 全局配置读取 | `/api/configuration/{data}`（subfinder/rad/system/dedup） | 只读 P2，写 P3 |
| S14 | 项目更新/删除 | `POST /api/project/{update,delete}` | P2（删除可带删资产，危险） |
| S15 | 策略详情/更新/应用/试算 | `/api/policy/{detail,update,apply,test}` | **部分已补（2026-11-28）**：`get_scan_policy`（详情+用量）、`apply_scan_policy`（把策略合并到模板的纯计算试算，返回生效配置/应用字段/冲突提示）。仍缺：`update`（改策略）与 `test`（独立校验一份配置） |
| S16 | 备份写 | `POST /api/backup/{create,restore,upload}`、`DELETE /api/backup/:name` | P3；restore 需 `confirm:"RESTORE"`（backup handler:140） |
| S17 | MCP 自身治理只读 | `GET /api/mcp/{keys,logs,config}` | P2，便于 Agent 自审计（注意：会暴露 key 前缀与调用轨迹） |

---

## 4. scope 体系与新增工具挂载

现有 12 个细分 scope + `*`（scope.go:20-34；前端 `ApiKey.vue:47-61` 13 个选项一致）。**建议第一批新增工具全部复用现有 scope**，理由：生产受限 key 已是 `{asset:read, task:read, plugin:read, node:read}`，复用即刻可用、前端零改动；只有出现「只给情报不给资产」的诉求时再扩家族。

| 建议新增 scope | 何时才需要 | 涉及工具 |
| --- | --- | --- |
| `icp:read` / `icp:write` | 需要把「主体查询」单独授权给 OSINT 用途 key | M3/M4、S2 之外 |
| `github:read` | 需要把泄露监控单独授权（不含资产库） | M11 |
| `fofa:read` / `fofa:write` | 测绘额度需要单独限额 | S1 |
| `backup:read` / `backup:write` | 备份属高危运维，需与 asset 分离 | M10/S16 |
| `config:read` / `config:write` | 全局配置/通知 | S12/S13 |
| `audit:read` | 审计日志单独授权 | M12 |

治理要点（按现状复述，供补齐时一并处理）：

- 未在 `ToolScopes` 登记的工具对受限 key **默认拒绝**（scope.go:223-230），且 `TestEveryRegisteredToolHasScope`（`-tags integration`）会失败；新增工具必须同步登记 + 跑该测试。
- `ScopeAllowed` 对空 scope 列表返回 true（scope.go:152-157），生产 `api_key` 里存在无 scope 的历史 key（`ssk_nSt64HuF...`，2026-11-18 前创建）。它不是漏洞，但意味着「新增工具只对受限 key 生效」的前提不成立，建议补一次 scope 回填（治理动作，本次未做）。

---

## 5. 输入 schema 与后端真实字段的核对（抽查 5 个 + 旁证 7 个）

方法：读 MCP 入参结构体 → 追踪到 service/handler 的绑定结构体 → 用生产 REST（只读查询类）与 Mongo 类型统计做实证。MCP 直接调 service（不经 handler），所以 **handler 层的校验/副作用一律缺失**，这是 §5.4/§5.5/§5.6 的根因。

### 5.1 list_assets：filter 值只能字符串 → 数值型字段恒 0 命中（严重）

- MCP 定义：`Filter map[string][]string`（tools.go:781），转换为 `map[string][]interface{}`（tools.go:843-853）。
- 后端：`helper.GetSearchQuery` 把 filter 值 **原样** 作为比较值（search.go:518-540），不做类型转换。
- 生产实测（只读）：

```
POST /api/assets/common/total {"index":"asset","filter":{"statuscode":["200"]}}  -> {"total":0}
POST /api/assets/common/total {"index":"asset","filter":{"statuscode":[200]}}    -> {"total":4}
POST /api/assets/common/total {"index":"SensitiveResult","filter":{"status":["5"]}} -> {"total":0}
POST /api/assets/common/total {"index":"SensitiveResult","filter":{"status":[1]}}   -> {"total":207}
```

- Mongo 类型（只读）：`asset.statuscode -> int`（7 条，115 条 missing）、`asset.port -> string`（122 条）、`SensitiveResult.status -> int`、`DirScanResult.status -> int`（[200,403]）。
- 结论：`filter.statuscode`、`filter.status`（SensitiveResult/DirScanResult/UrlScan）经 MCP **永远返回空**；而 `filter.port`（string）正常（`{"port":["443"]}` → 61，与 DB 61 一致）。
- 修复方向（不改后端语义）：MCP 入参改 `map[string][]any`（或 `json.RawMessage` + 按 key 做类型归一：`statuscode/status/port_number` 尝试转 int）；同时在 schema.go 增加「数值型 key 请传 JSON 数字」说明。

### 5.2 list_pocs：search 实际只匹配 name，描述声称含模板 ID（轻微，易误导）

- MCP：`Search string` jsonschema 写「POC 名称或模板 ID 模糊搜索」（tools.go:1099）。
- 后端：`GetPocList` 只对 `name` 做正则（poc.go:60-65），`id`（模板 ID）字段未参与。
- 实测：`POST /api/poc {"search":"ack-cluster-api-public"}` → `total 0`；`{"search":"ACK Cluster"}` → 命中。
- 另：`get_poc` 只能传 ObjectID。Poc 模型 `json:"id"` 是 ObjectID，模板 ID 在 `json:"Template ID"`（models/poc.go:9-16）；实测传模板 ID → 500 `invalid id format`。列表返回字段 `id` 与 `Template ID` 并列，Agent 极易取错。
- 修复：二选一——后端把 `id` 加入搜索正则（1 行），或改 MCP 描述为「仅按名称搜索」，并在返回里显式标注 `_id`/`Template ID`。

### 5.3 list_tasks：字段一致，但缺 handler 的状态刷新副作用（中等）

- 入参 `search/pageIndex/pageSize` 与 `models.ListRequest`（task.go:112-116）一致；MCP 直连 `taskService.List`（tools.go:522）绕开 `binding:"required,min=1"`（无影响，因为 MCP 自己补默认值）。
- 差异：REST `List` 每次都 `go taskService.TaskProgress(...)`（handlers/task/task.go:46-51），而任务入库时状态固定写 `TaskStatusPending`，靠 TaskProgress 转运行中（task/common/common.go:149-155 注释）。MCP 连续调用 `list_tasks`/`get_task` 不会触发刷新 → **可能长时间看到 pending**。
- 修复：MCP `list_tasks`/`get_task` 前调用一次 `TaskProgress`（异步、best-effort），或在 description 注明「状态可能滞后，需配合 get_task_progress」。

### 5.4 list_scan_policies：字段一致，边界语义不同（轻微）

- MCP：`keyword/enabled/pageIndex/pageSize`（tools.go:1342-1347）→ `ScanPolicyListQuery`（models/scan_policy.go:239-244），与 handler 完全对应（handler 额外支持 `search` 作 keyword 别名，MCP 不需要）。
- 差异：`pageSize>200` 时 REST 返回 400（policy/handler.go:52-61），MCP 静默截断为 200（tools.go:1360-1362）。建议文档化，或与 REST 一致返回错误避免「少拿了数据还以为拿全了」。

### 5.5 create_scan_task：计划任务参数被静默忽略（严重）——**已修（2026-11-28）**

> 修法：`scheduledTasks=true` 时改调 `schedulerService.CreateScheduledTask`（只写一条周期任务并注册 cron），
> 不再先 `Insert` 一条一次性任务——原来的 REST 路径会同时留下「立刻跑一次」和「周期跑」两条文档，
> 对 MCP 调用方是意外的目标消耗。默认值（cycleType=nhours/hour=1/day=1/week=1）由服务层补。
> 回归：`mcp_sweep.py` 覆盖 `list_scheduled_tasks`/`get_scheduled_task`（读）与
> `set_scheduled_task_state`（不存在的 id 必须报错）。

- MCP 接受 `scheduledTasks/hour/minute/day/week/cycleType`（tools.go:710-715）并写入 `models.Task`（tools.go:751-756），然后只调 `taskCommonService.Insert`（tools.go:769）。
- REST `AddTask` 在 Insert 之后还有一段：`if req.ScheduledTasks { SchedulerService.CreateScheduledTask(c,&req) }`（handlers/task/task.go:148-155）；全仓库仅此一处调用（`grep CreateScheduledTask`）。
- 后果：MCP 建「计划任务」得到的是**一次性任务**，返回 `success:true`，无任何告警。
- 其余字段核对（正确）：`targetSource` 分支与 common.go:86-138 完全对应；`targetNumber`=0 表示不限（asset GetTaskTarget 只在 `PageSize!=0` 时 SetLimit，asset.go:415-417）；`bindProject` 由 tasksync 在任务完成时消费（tasksync.go:108-114），MCP 传参有效。
- 另一处软失配：`targetSource` 非法值不会报错，会落到「不解析目标」分支，最终以 `Target is null`（common.go:146-148）报出，对 Agent 是误导性错误。建议 MCP 侧做枚举校验。

### 5.6 create_export_task：缺时间范围与 PageMonitoring 特例（中等）

- MCP：`index/fields/fileType/quantity/searchExpression/filter`（tools.go:1661-1668）；无 `startTime/endTime`。
- REST：`ExportRequest` 有 `startTime/endTime`，且 `exportfilter.ApplyTimeRange` 会改写查询（handlers/export/export.go:83，issue #261）；另有 `index=="PageMonitoring"` 时强制 `hash.$size=2`（export.go:77-79）。
- 后果：MCP **无法按时间窗导出**；PageMonitoring 导出与 Web 结果集不一致（MCP 会导出未完成两次快照的行）。
- 修复：补 `startTime/endTime` 字段并复用 `exportfilter`；同步补 PageMonitoring 的 hash 条件。

### 5.7 get_dashboard_stats：project 实际要 ObjectID，描述写「项目名」；「七类」实为 5 类（中等）

- MCP jsonschema：`project：可选的项目名，空表示全部项目`（tools.go:1252）。
- 后端：`bsonProjectFilter` 直接 `{project: project}` 字符串比较（dashboard.go:435-440），而 asset.project 存的是项目 ObjectID 字符串。
- 实测：`{"project":"artex-own-infra-scan1"}` → 全 0；`{"project":"6afad734e3460b716c5723f7"}` → asset 1/subdomain 2/url 9。**静默全 0**。
- 同时 `DashboardStats` 只有 asset/subdomain/url/vulnerability/sensitive 5 类资产 + task 三计数 + node 两计数（models/dashboard.go:18-31），描述「七类资产总量」不实。

### 5.8 list_assets 的 asset_type 白名单缺 site/service（中等）

- `normalizeAssetIndex` 别名表（tools.go:1042-1089）只有 13 个值；`site`/`service` 不在其中 → 直接报「不支持的 asset_type」。
- 但产品有 `GET·POST /api/assets/site/list`、`POST /api/assets/site/chain`、`POST /api/assets/service/list`（routes/assets/site.go、asset_service.go），实测 site/list 正常返回聚合行（host+port+assetCount）。
- 且 `docs/MCP.md:36` 明确宣传「type 支持 site/.../service/...」→ 文档承诺了实现没有的东西。

### 5.9 IPAsset 的 filter.port/service/app 恒 0（产品侧既有缺陷，MCP 文档宣称可用）

- 文档（schema.go:94）：「IPAsset: project, port, service, app（嵌套 ports 字段，走聚合查询）」。
- 实现：`$unwind ports` 后 `expandNestedFilter` 保持 key 原样（ip.go:369-395），但数据路径是 `ports.port`；filterKeyCache 把 `port` 映射为 `"port"`（search.go:130）。
- 实测：`POST /api/assets/ip {filter:{port:["443"]}}` → `total 0`；无过滤 → `total 16`（且返回行里确有 port=443）。
- 建议：作为独立产品缺陷修（把 filterKeyCache 的 IPAsset 分支改写为 `ports.port` 等），MCP 文档在修复前标注不支持。

### 5.10 create_project 语义与 Web 不同（轻微）

- MCP 调 `ApplyProjectScope`（tools.go:488）：同名项目存在且新 target 非空时**只替换范围**，返回 `replaced:true`。
- Web `POST /api/project/add` 调 `AddProject`：重名直接 `name already exists` 报错（project.go:234-241）。
- 影响：同一操作用 MCP 与用 Web 结果不同（MCP 更宽松，符合幂等 Agent 需求）。建议保留 MCP 行为但在 description 里显式写明「同名即替换范围」，避免两套语义无人知晓。

### 5.11 count_assets 与 list_assets 的口径差异（轻微，仅 IPAsset）

- 列表：有嵌套条件时走聚合 pipeline（ip.go:50-70）；计数：`TotalData` 一律 `CountDocuments`（common.go:207-226）。
- 后果：IPAsset 在嵌套过滤下，`count_assets` 与 `list_assets` 返回的 total 可能不一致。修 §5.9 时一并统一。

### 5.12 MCP 绕过 handler 的通用风险（结构性）

MCP 全部工具都是「service 直调」（tools.go 全文），因此 handler 层的以下东西**统统一致性缺位**：分页上限校验（400 vs 静默截断）、`binding` 必填校验、写后副作用（计划任务、TaskProgress 刷新、bin 文件校验）、错误码语义（404/400/500 分层）。建议在 `internal/mcp` 里加一个薄 `guard` 层（枚举校验 + 分页上限 + 副作用调用），否则每加一个工具就多一处静默失配。

---

## 6. 新增工具建议（名称 / 作用 / scope / 优先级）

约定：`P0`=本迭代必须，`P1`=下迭代，`P2`=可排期，`P3`=谨慎/需审批。所有新增工具必须在 `mcpguard.ToolScopes` 登记，否则受限 key 默认拒绝。

### 6.1 必须补（P0/P1，15 项建议 / 16 个工具）

| 工具名 | 作用 | 入参要点 | scope | 优先 |
| --- | --- | --- | --- | --- |
| `list_asset_groups` | 集团列表（含 scopeCount） | `keyword/pageIndex/pageSize` | asset:read | P0 |
| `list_asset_scopes` | 地契（域/IP 覆盖）列表，可按集团过滤 | `groupId/keyword/tag/pageIndex/pageSize` | asset:read | P0 |
| `bind_asset_scope_enterprise` | 把地契绑定到 ICP 企业主体 | `scopeIds[],enterprise` | asset:write | P1 |
| `list_icp_tasks` | ICP 主体任务列表 | `state/type/keyword/provider/pageIndex/pageSize` | asset:read | P0 |
| `get_icp_overview` | 主体/域名/资产总览计数 | 无 | asset:read | P1 |
| `create_icp_task` | 创建主体检索任务 | `keyword/type/provider` | asset:write | P1 |
| `list_icp_assets` | 主体查出的域名/资产 | `taskId/pageIndex/pageSize` | asset:read | P1 |
| `retest_task` | 重测任务（重置进度重跑） | `id` | task:write | P0 |
| `sync_task_to_project` | 把任务产出同步到项目 | `taskId,projectId` | task:write | P1 |
| `list_scheduled_tasks` | 计划任务列表 | `pageIndex/pageSize` | task:read | P0 |
| `delete_scheduled_task` | 删除计划任务 | `id` | task:write | P1 |
| `list_backups` + `get_backup_status` | 备份记录/进行中状态（只读） | `pageIndex/pageSize` / 无 | asset:read（或 backup:read） | P1 |
| `list_github_results` | 泄露结果列表 | `keyword/taskId/pageIndex/pageSize` | asset:read（或 github:read） | P1 |
| `get_system_info` | 版本/运行时长/依赖状态（含 sysinfo） | `includeSysInfo` | asset:read（或 system:read） | P1 |
| `list_audit_logs` | 操作审计 | `username/tool/status/startTime/endTime/pageIndex/pageSize` | asset:read（或 audit:read） | P2 |

> 其中「`create_scan_task` 计划分支」「`list_assets` 数值 filter」「`get_dashboard_stats` project 语义」「`list_pocs` 说明」「`create_export_task` 时间范围」是**修现有工具**，不新增工具名，列在 §5 与 §8 的整改清单。

### 6.2 建议补（按投入产出排序）

| 工具名 | 作用 | scope | 优先 |
| --- | --- | --- | --- |
| `list_asset_read_model` | 统一读模型入口：`kind=site\|cert\|cip\|http_status\|service\|stat_finger\|nuclei_result\|wih\|fileleak\|npoc` | asset:read | P2 |
| `get_asset_statistics` | port/service/app/icon TopN 分布 | asset:read | P2 |
| `download_export` | 返回导出文件的可下载路径/URL（不传二进制） | asset:read | P2 |
| `preview_export` | 导出前预览前 N 行 | asset:read | P2 |
| `update_scan_template` / `delete_scan_template` | 模板改/删 | template:write | P2 |
| `update_scan_policy` / `test_scan_policy` | 策略改、试算校验 | task:write | P2 |
| `pause_target` / `resume_target` | 目标级暂停/恢复 | task:write | P1 |
| `fofa_status` / `fofa_search` | FOFA 配额状态、测绘检索 | asset:read / fofa:read | P2 |
| `fofa_create_tasks` | 按 FOFA 查询批量建任务 | asset:write / fofa:write | P3 |
| `delete_assets` / `update_asset_status` | 批量删资产（可按 search）、改状态 | asset:write | P2 |
| `set_asset_remark` | 资产备注 | asset:write | P2 |
| `add_assets_batch` | 手工导入资产 | asset:write | P3 |
| `list_notifications` / `get_notification_config` | 通知渠道与配置（只读） | asset:read / config:read | P2 |
| `get_global_config` | subfinder/rad/system/dedup 配置只读 | config:read | P2 |
| `add_poc` / `update_poc` / `delete_poc` / `import_poc` | POC 维护 | asset:write | P2 |
| `add_fingerprint` / `update_fingerprint` / `delete_fingerprint` | 指纹维护 | asset:write | P2 |
| `add_sensitive_rule` / `update_sensitive_rule` / `delete_sensitive_rule` | 规则维护 | asset:write | P2 |
| `upload_dictionary` / `download_dictionary` | 大字典上传（upload_large+轮询）/下载 | asset:write / asset:read | P3 |
| `get_plugin_detail` / `get_plugin_logs` | 插件详情、运行日志 | plugin:read | P2 |
| `run_plugin` / `import_plugin` | 节点上运行/导入插件 | plugin:write | P3 |
| `update_node_config` | 节点并发等配置 | node:write | P3 |
| `create_backup` / `restore_backup` | 备份/恢复（restore 要求 `confirm:"RESTORE"`） | backup:write | P3 |
| `get_mcp_config` / `list_mcp_call_logs` | 自审计：全局配置与调用日志 | asset:read（或 audit:read） | P2 |

### 6.3 落地骨架（以 `list_asset_groups` 为例）

```go
// 1) tools.go: registerTools 内追加
addTool(server, allowedTools, &names, &mcp.Tool{
    Name:        "list_asset_groups",
    Description: "资产集团（企业维度）列表，含每个集团下地契数量。用 list_asset_scopes 取具体覆盖范围。",
}, listAssetGroups)

// 2) 入参与 handler
type listAssetGroupsInput struct {
    Keyword   string `json:"keyword,omitempty" jsonschema:"集团名称关键字"`
    PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
    PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listAssetGroups(ctx context.Context, _ *mcp.CallToolRequest, in listAssetGroupsInput) (*mcp.CallToolResult, any, error) {
    c := ginContext(ctx)
    list, total, err := d.assetGroupService.ListGroups(c, in.Keyword, in.PageIndex, in.PageSize)
    if err != nil {
        return errorResult("获取资产集团失败", err)
    }
    return jsonToolResult(ginH{"list": list, "total": total})
}

// 3) mcpguard/scope.go: ToolScopes 追加（否则受限 key 默认拒绝，集成测试会红）
"list_asset_groups": ScopeAssetRead,
```

- 依赖装配：`deps` 里加 `assetGroupService assetgroup.Service`（`internal/services/assets/assetgroup`）。
- 提交前跑：`go test -tags integration ./internal/mcp/ ./internal/api/routes/` —— 两个漂移测试（工具↔scope 双向）会兜住漏登记。
- 前端无需改动（复用 `asset:read`）；仅当新增 scope 家族时才改 `ApiKey.vue` 的 `SCOPE_OPTIONS`。

---

## 7. `docs/MCP.md` 需要同步修改的位置

| 位置 | 现状 | 应改为 |
| --- | --- | --- |
| L24「工具清单（25）」 | 用累计口径（25→34→42→46→49）手工递增 | 改为「当前 49（以 `mcpguard.ToolScopes` 为准）」，并在 CI 里跑 `tools_registry_integration_test.go` 断言数量，避免每版手算 |
| L9-20 scope 表 | 只有 6 行 + `*`，缺 `template:write/plugin:write/node:write` 等 | 补全 12 个真实 scope（与 scope.go:20-34、ApiKey.vue:47-61 三方一致） |
| L36 asset_type 列表 | 写 `site/subdomain/url/ip/service/vulnerability/sensitive/dirscan/crawler/app/mp/page-monitoring` | 改为实现的 13 个值：`asset / RootDomain / subdomain / app / mp / UrlScan / SensitiveResult / DirScanResult / crawler / vulnerability / PageMonitoring / IPAsset / SubdomainTakerResult`；若同步实现 site/service，再补回（§5.8） |
| 新增章节「已知语义边界」 | 无 | 写清：数值型 filter 目前只支持字符串（§5.1）；`project` 参数是 ObjectID 不是名字（§5.7）；`list_pocs.search` 只搜名称（§5.2）；`create_scan_task.scheduledTasks` 待修（§5.5）；`create_export_task` 无时间范围（§5.6）；IPAsset 嵌套过滤待修（§5.9） |
| 新增章节「MCP 未覆盖的产品面」 | 无 | 放本报告 §3.1/§3.2 的矩阵摘要（资产组、ICP、GitHub、FOFA、备份、通知、读模型、计划任务、重测/同步） |
| L46-60 调用示例 | 硬编码 `http://127.0.0.1:8082` | 改 `$SCOPE_SENTRY_ADDR`（生产实际经隧道 `18082`，避免误导） |
| L129-137 API Key UI 段 | 写「13 个 scope」 | 保留，但注明「新增 scope 家族时需同步 `ApiKey.vue:SCOPE_OPTIONS` + 本表」 |
| L110 导出 fileType 说明 | 只说 xlsx/json | 补「csv 会被 `IsValidFileName` 拒绝；导出无时间范围参数（待补）」 |
| 文末 | 无漂移测试说明 | 补：`go test -tags integration ./internal/mcp/` + `./internal/api/routes/`，以及「新增工具三件套：addTool / ToolScopes / 文档」 |

---

## 8. 整改清单（可直接开 issue）

P0（静默失败，优先级最高）
1. `create_scan_task`：`ScheduledTasks=true` 时调用 `schedulerService.CreateScheduledTask`（对齐 handlers/task/task.go:148-155），并对非法 `targetSource` 前置校验。
2. `list_assets`/`count_assets`：`Filter` 改 `map[string][]any`（或按 key 归一化类型），修复 `statuscode/status` 恒 0。
3. `get_dashboard_stats`：描述与实现对齐——`project` 改收 ObjectID（并在描述里给出「用 list_projects_data 取 id」），或服务层支持项目名解析。
4. `list_pocs`：要么后端 search 覆盖 `id`，要么改描述为「仅名称」；`get_poc` 错误里区分「模板 ID ≠ ObjectID」。
5. `list_tasks`/`get_task`：补 TaskProgress 刷新或明确「状态可能滞后」。

P1（能力缺口）
6. 新增 `list_asset_groups`/`list_asset_scopes`（资产组+地契）。
7. 新增 `list_icp_tasks`/`get_icp_overview`/`list_icp_assets`。
8. 新增 `retest_task`/`sync_task_to_project`/`pause_target`/`resume_target`。
9. 新增 `list_scheduled_tasks`/`delete_scheduled_task`。
10. 新增 `list_backups`/`get_backup_status`（只读）。
11. 新增 `list_github_results`（只读威胁情报最小集）。
12. `list_assets.asset_type` 扩 `site`/`service`（或新增 `list_sites`）。

P2/P3
13. `list_asset_read_model`（cert/cip/wih/nuclei/http_status/stat_finger/fileleak/npoc 合并入口）。
14. `create_export_task` 补 `startTime/endTime` + PageMonitoring hash 条件；补 `download_export`/`preview_export`。
15. 模板/策略/POC/指纹/敏感规则/字典/插件/节点/通知的写操作按 §6.2 排期；备份 restore 与插件 run 需人工确认。
16. 修 IPAsset 嵌套过滤（产品侧，§5.9），修完再更新 MCP 文档。

---

## 9. 证据附录（可复现）

环境：`ssh prod-tx-sh-app-02` + `docker exec scopesentry-mongodb mongosh -u admin -p *** --authenticationDatabase admin ScopeSentry --quiet --eval '...'`（只读）；REST 用 `POST /api/user/login` 取 JWT 后调用只读查询接口。

```bash
# 1) 工具数与 scope 数（本地仓库，纯静态）
grep -c 'Name:        "' internal/mcp/tools.go            # -> 49
grep -c '"\w*":\s*Scope' internal/services/mcpguard/scope.go  # -> 49（脚本比对双向差集为空）

# 2) filter 类型实证
curl -s -X POST $ADDR/api/assets/common/total -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
  -d '{"index":"asset","filter":{"statuscode":["200"]}}'     # -> total 0
curl -s -X POST $ADDR/api/assets/common/total -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
  -d '{"index":"asset","filter":{"statuscode":[200]}}'       # -> total 4

# 3) dashboard project 语义
curl -s -X POST $ADDR/api/dashboard/stats ... -d '{"project":"artex-own-infra-scan1"}'        # -> 全 0
curl -s -X POST $ADDR/api/dashboard/stats ... -d '{"project":"6afad734e3460b716c5723f7"}'      # -> asset 1/subdomain 2/url 9

# 4) POC 搜索只匹配 name
curl -s -X POST $ADDR/api/poc ... -d '{"search":"ack-cluster-api-public"}'   # -> total 0
curl -s -X POST $ADDR/api/poc ... -d '{"search":"ACK Cluster"}'              # -> 命中
curl -s -X POST $ADDR/api/poc/detail ... -d '{"id":"ack-cluster-api-public"}' # -> 500 invalid id format

# 5) IPAsset 嵌套过滤
curl -s -X POST $ADDR/api/assets/ip ... -d '{"filter":{"port":["443"]}}'     # -> total 0
curl -s -X POST $ADDR/api/assets/ip ... -d '{}'                              # -> total 16

# 6) Mongo 字段类型（只读）
db.asset.aggregate([{$group:{_id:{$type:"$statuscode"},n:{$sum:1}}}])        # int / missing
db.asset.aggregate([{$group:{_id:{$type:"$port"},n:{$sum:1}}}])              # string
db.SensitiveResult.aggregate([{$group:{_id:{$type:"$status"},n:{$sum:1}}}])  # int
db.DirScanResult.distinct("status")                                          # [200,403]

# 7) MCP 现网使用情况（只读）
db.mcp_call_logs.aggregate([{$group:{_id:{t:"$tool",s:"$status"},n:{$sum:1}}},{$sort:{n:-1}}])
# -> tools/list 1217 / count_assets 34 / list_plugins 20 / start_task 2(denied: need task:write) ...
db.api_key.find({},{keyPrefix:1,scope:1})   # 一条无 scope（=全权）、一条 {asset,task,plugin,node}:read
```

MCP 调用日志里 `start_task` 两次 `denied: insufficient scope: need task:write` 说明：现网确实存在「用只读 key 试图控任务」的自动化，补齐写类工具时必须保持 scope 严判，不要为了「好用」把 task:write 合并进 task:read。

---

## 10. 未做/未覆盖（边界说明）

- 未真实调用 `POST /mcp`（需要 API key 明文，库里只有哈希与前缀；本次不新建 key，避免写入）。
- 未逐一核对 49 个工具的返回体结构（只做入参 schema 与语义核对）；返回体差异（如 `list_assets` 各资产类型返回字段不统一）建议单独做一轮「输出契约」审计。
- 未评估新增工具对 `mcp_call_logs` 保留策略、限速（`rateLimitPerMin`）的影响；批量只读工具（如遍历 14k POC）建议在工具内做 pageSize 上限。

---

## 11. 输出契约审计（2026-11-29 补上 §10 的第一条遗留项）

`scripts/ci/mcp_output_contract.py`：32 项断言，MCP_OUTPUT=PASS 才算过。
它补的是 `mcp_sweep.py` 拦不住的一类假成功——**工具返回了东西，但东西和库里的真相不一致**。

| 断言 | 拦的是哪种假成功 |
| --- | --- |
| `rows == min(pageSize, total)` | total 说有 4935 条，rows 却是空 |
| `filter.port=443` 的每一行 port 都是 443 | filter 只影响 total，不影响 rows |
| `count_assets == list_assets.total` | 两个工具各算一套口径 |
| 分布返回条数 ≤ limit、分桶有 value+count | limit 被忽略、分桶是空壳 |
| `search_assets` 每条都真的含关键词 | 搜索退化成「返回全部」 |
| `get_*` 回显请求的 id | 拿错文档也返回成功 |
| 节点行带 `queueLen`，有在跑任务必须给 `idleMinutes` | 界面字段悄悄消失 |
| 越界 `pageIndex` 返回空列表 | 越界复读第一页（比报错更糟） |

### 审计发现的真问题：filter 键与行字段不同名

| 场景 | 之前 | 现在 |
| --- | --- | --- |
| 按状态过滤 | `filter.statuscode=["200"]` 命中 4935 条，但行里只有 `status` | 行补 `statuscode` 别名 |
| 跨工具合并 | `search_assets` 返回 `host`，`list_assets` 返回 `domain` | 行补 `host`（=domain）别名 |

实现是只读别名（`aliasAssetRowFields`，只补缺不改值，脏数据跳过），配 2 个单测。
没做「把 filter 键改成 status」这种破坏性重命名：现有调用方（含 sweep 脚本）用的是 `statuscode`。

门禁：`verify_all.sh` 增加 `mcp_output`，缺 `SS_BASE`/`SS_MCP_KEY` 时 SKIP（与 column_field 一致）。

## 12. 参数与写链路的静态审计（第 97 轮）

两类问题在工具面上都验不到「客户端看到的」这一层（当时 82 个，第 99 轮加了 run_scheduled_task 后是 83 个），所以补了两个检查点：

**一、声明了但 handler 从不读取的入参。**
go-sdk 用 handler 泛型入参结构体生成 JSON Schema，Schema 与类型不一致在编译期就没了；
剩下的风险是另一种：字段声明了（甚至 `required`），handler 里一次都没读。调用方按 Schema
传值，工具照常成功，只是那个值被静静丢掉——表现是「筛选项不生效」「分页不起作用」，且无报错。

`scripts/ci/mcp_param_audit.py` 按 Go 名字匹配 `input.Xxx` 统计：82 个工具里 69 个可静态判定，
8 个把整个入参对象往下传（在下游消费，整条跳过），5 个声明为 `_ struct{}`（确实无参）。
当前结论：**没有「必填但从不读取」的参数**。

**二、写工具是否真的有人验过。**
同一个脚本反向扫 `mcpguard.ToolScopes` 里所有 `*:write` 工具，检查工具名是否出现在
`internal/mcp/*_test.go`、`internal/api/routes/*_test.go` 或 `scripts/ci/mcp_sweep.py` 里。
只登记 scope 不写用例的写工具等于「接口存在但没人验」：MCP 写路径没有 gin 的 binding 兜底，
问题直接落到数据上。当前 33 个写工具全部被点名。

**三、写链路闭环用例（`internal/mcp/write_closure_integration_test.go`）。**
走真实 MCP 协议栈（server + in-memory transport），不直接调 handler——直接调只证明函数能跑，
走协议才覆盖 schema 校验、参数反序列化、结果封装这几层。三个子用例各自「MCP 建 → MCP 读回 →
MCP 删 → 确认消失」：字典（含 append 后 preview 能读回追加行）、资产组、扫描策略。
另外 `TestToolsListContract` 断言 82 个工具名字唯一、描述非空、入参 schema 存在，且数量与注册表一致。

门禁：`verify_all.sh` 增加 `mcp_param`（无库也能跑）。
## 13. 模板治理写入口补齐（第 104 轮）

`/api/poc/state` 从第 103 轮起就能批量停用/启用 nuclei 模板，停用后显式 vulList 里引用到的模板
也会从扫描载荷里剔除（`task/common.GetScanTemplate`）。但 MCP 侧只有 `list_pocs` / `get_poc`
两条读工具：自动化能看见「哪些模板还在跑」，改不了。上游 ARL-Next 的 nuclei toggle/blacklist
干的正是这件事，所以这是对齐 + 闭环同时缺的一条。

补 `set_poc_state(ids[], disabled)`，scope 用 `template:write`：副作用是「改变后续扫描的载荷」，
不是改资产，挂在 `asset:write` 下会让只想打资产标签的 key 顺带获得关掉漏洞扫描的能力。

闭环证据链（三层都验，缺一层就可能「回成功但没落库」）：

- 拒绝分支：`mcp_sweep.py` 的 GUARD 加 `('set_poc_state', {'ids': [NOPE]})` —— 不存在的 id 必须报错，
  否则「停用了 0 条」会被当成成功，调用方以为模板已经不会被引用；
- 写往返：`TestWriteToolClosure/poc_state` 停用一枚模板 → `list_pocs` 回读 `disabled=true` → `t.Cleanup` 还原；
- 账本：`mcp_closure.py` 的 WRITE_ROUNDTRIP 登记该往返，工具总数 84、未归类 0、scope 缺失 0。

顺带把「id 字段名」这个坑记下来：`Poc.ID` 的 json 标签是 `id`，`Template ID` 才是模板标识。
第一版用例按 `_id` 反序列化，拿到空串，`set_poc_state` 直接回「没有匹配的模板」——这正是
「调用成功与否」和「改到的是不是那一行」必须分开验的原因。

