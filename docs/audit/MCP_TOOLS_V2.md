# MCP 工具面补齐 v2 —— 资产组 / 地契 / ICP / 审计（只读）

范围：只改 `internal/mcp`、`internal/services/mcpguard`、`docs/MCP.md`。未部署、未改库、未改前端。
依据：`docs/audit/MCP_GAPS.md` §3.1 的 P0/P1 只读缺口（M1 资产组、M2 地契、M3 ICP 任务、M12 审计日志）。

---

## 1. 结论

| # | 工具 | scope | 后端等价 | 覆盖缺口 |
| --- | --- | --- | --- | --- |
| 1 | `list_asset_groups` | `asset:read` | POST /api/assets/asset_group/list | M1 |
| 2 | `list_asset_scopes` | `asset:read` | POST /api/assets/asset_scope/list | M2（读侧） |
| 3 | `list_icp_tasks` | `asset:read` | POST /api/icp/task/list | M3（读侧） |
| 4 | `list_audit_logs` | `asset:read` | POST /api/audit/list | M12（审计读） |

- 工具面 **49 → 53**；`mcpguard.ToolScopes` 53 条与 `internal/mcp` 注册 53 个工具**双向零差集**（脚本比对，见 §6.4）。
- 四项**全部复用 `asset:read`**，未新增 scope 常量 → 前端 `ApiKey.vue` 的 `SCOPE_OPTIONS` 与 `docs/MCP.md` 的 scope 表**不需要**新增选项，生产受限 key `{asset:read, task:read, plugin:read, node:read}` 补完即可直接调用。
- 新增参数归一层 `internal/services/mcpguard/params.go`（纯函数、无 Mongo），配套 8 个单测；`go build ./...` 与 `go test ./internal/services/mcpguard/...` 均通过。
- `internal/mcp` 的漂移测试（`-tags integration`）在本机**无法运行**（`internal/mcp` 间接 import `internal/bootstrap` / `internal/database/mongodb`，init 要连真实 Mongo，无库时测试二进制以 `MongoDB client is not initialized` 退出），故用等价静态比对补上（§6.4）。

---

## 2. 工具定义（入参 = handler 字段，逐字对齐）

### 2.1 `list_asset_groups`（asset:read）

- 入参：无（handler `ListGroups` 也不接参数）。
- 调用：`assetgroup.Service.ListGroups(ctx)` → `repo.ListGroups` + `repo.GroupScopeCounts`（填充 `scopeCount`）。
- 返回：`{"items":[{id,name,description,sortOrder,createdAt,updatedAt,scopeCount}], "total":N}`，与 HTTP handler 的响应体同形（空列表返回 `[]` 而非 `null`）。

### 2.2 `list_asset_scopes`（asset:read）

入参对照 `internal/api/handlers/assets/assetgroup/handler.go` 的 `ListScopes` 匿名结构：

| 工具字段 | handler 字段 | repo 语义 |
| --- | --- | --- |
| `groupId` | `groupId` | `filter["group_id"]`；`unassigned` → `$in:["","0"]` |
| `search` | `search` | 对 `name` / `scope` 做 `QuoteMeta` + `i` 正则 |
| `pageIndex` | `pageIndex` | skip=(idx-1)*size，<1 归 1 |
| `pageSize` | `pageSize` | 默认 20，**>500 → repo 静默回落 20** |

MCP 侧把 repo 的「>500 静默回落」改成**显式报错**（见 §4），返回体 `{"list":[...],"total":N,"pageIndex":n,"pageSize":n}` 与 handler 一致。

### 2.3 `list_icp_tasks`（asset:read）

入参对照 `internal/api/handlers/icp/handler.go` 的 `taskListRequest` → `models.IcpTaskListQuery`：

| 工具字段 | handler 字段 | 备注 |
| --- | --- | --- |
| `state`（`*int`） | `state *int` | 0 待开始 / 1 运行中 / 2 已暂停 / 3 已完成；nil = 不过滤 |
| `type` | `type` | `company` / `domain` |
| `keyword` | `keyword` | repo 对 `name` 与 `queries` 做不区分大小写正则 |
| `provider` | `provider` | `manual` / `none` / `http` |
| `pageIndex` / `pageSize` | 同名 | `parsePageParams`：pageSize > 200 → 400 |

调用 `icpsvc.DefaultService().ListTasks`（与服务层单例，和 HTTP handler 同一实例；构造只建仓库句柄，不碰库）。

### 2.4 `list_audit_logs`（asset:read）

handler 直接绑 `models.AuditLogQuery`，工具字段与之逐字一致：
`user` / `path` / `method` / `statusMin` / `pageIndex` / `pageSize`。
repo 行为：`user`/`path` 用 `QuoteMeta`+i 正则，`method` 走 `ToUpper` 等值，`statusMin>0` 才加 `$gte`，排序 `at` 倒序。pageSize > 200 → handler 400。
调用 `auditrepo.NewRepository().List(ctx, models.AuditLogQuery{...})`。

---

## 3. scope 决策：为什么四项都挂 `asset:read`

1. **可用性**：生产受限 key 的授予集是 `{asset:read, task:read, plugin:read, node:read}`。若新增 `icp:read` / `audit:read`，这些 key 对新工具一律被 `Check()` 拒绝（未登记/scope 不覆盖 = 默认拒绝），补了等于没补。
2. **授权面成本**：新 scope 家族必须同步前端 `frontend/src/views/Configuration/ApiKey.vue` 的 `SCOPE_OPTIONS`（当前 13 项：12 细分 + `*`）与 `docs/MCP.md` scope 表，属授权面变更。本次任务边界只允许改 `internal/mcp`、`internal/services/mcpguard`、`docs/MCP.md`，改前端会越界。
3. **语义成立**：四项都是「资产模型（集团/地契）、资产来源（ICP 主体）、资产操作痕迹（操作日志）」，归 asset 面比新开家族更贴近既有划分。

**遗留治理建议（未做）**：`list_audit_logs` 能读到**其他账号**的请求路径、状态码、来源 IP 与 UA，严格说比 `asset:read` 更敏感。若后续要做最小权限，路径是：
`mcpguard` 加 `ScopeAuditRead = "audit:read"` → `ToolScopes["list_audit_logs"] = ScopeAuditRead` → `ApiKey.vue` 的 `SCOPE_OPTIONS` 补一项 → `docs/MCP.md` scope 表补一行 → 现有 `asset:read` key 需重新授权。四步必须一起做，否则 UI 里选不到新 scope。

---

## 4. 参数归一（补回 handler 层校验，避免「成功 + 空列表」）

MCP 直接调 service/repo，**不经 gin 的 `ShouldBindJSON`**，handler 里的校验分支不会生效。若不补，异常入参会穿过到 Mongo 过滤条件，表现为「调用成功、返回 0 条」，与「确实没有数据」不可区分——这正是 `MCP_GAPS.md` §5.1 记录的同类静默失败。

| 归一项 | 函数 | 规则 | 不补的后果 |
| --- | --- | --- | --- |
| 分页 | `NormalizePaging` | pageIndex<1→1；pageSize≤0→默认 20；pageSize>上限→**报错** | `pageSize=100000` 把整个集合拉进内存 |
| ICP 状态 | `NormalizeICPTaskState` | `nil`→不过滤；只接受 0/1/2/3 | `state=9` → 恒 0 条 |
| ICP 类型 | `NormalizeICPTaskType` | 空 / company / domain（大小写不敏感） | `type=Company` 字面量在 `icp_task.type` 里不存在 → 恒 0 条 |
| ICP provider | `NormalizeICPProvider` | 空 / manual / none / http | 拼错 provider → 恒 0 条 |
| 审计方法 | `NormalizeAuditMethod` | `ToUpper` + GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS 闭集 | `method=get` 在 repo 里被 ToUpper 成 GET（可用），但 `method=fetch` 恒 0 条 |
| 审计状态下界 | `NormalizeAuditStatusMin` | 0（不过滤）或 100-599 | `statusMin=5000` → `$gte:5000` 恒 0 条 |
| 地契 groupId | `NormalizeScopeGroupID` | 空 / `0` / `unassigned` 放行；其余必须 24 位 ObjectID | 传集团**名称**而非 id → 恒 0 条（最容易踩） |

上限取值分别对齐后端：ICP/审计 200（handler 400 口径）、地契 500（repo 硬上限）。超限选择**报错**而不是静默截断，与 REST 400 语义一致。

---

## 5. 代码位置

| 内容 | 位置 |
| --- | --- |
| 工具注册（4 个 addTool） | `internal/mcp/tools.go:373-395` |
| deps 字段 `assetGroupService` / `icpService` / `auditRepo` | `internal/mcp/tools.go:79-84`（初始化 `:114-117`） |
| `listAssetGroups` | `internal/mcp/tools.go:1899` |
| `listAssetScopes` + `listAssetScopesInput` | `internal/mcp/tools.go:1914-1947` |
| `listICPTasks` + `listICPTasksInput` | `internal/mcp/tools.go:1949-1999` |
| `listAuditLogs` + `listAuditLogsInput` | `internal/mcp/tools.go:2001-2047` |
| 工具长描述 | `internal/mcp/schema.go:186-260`（`listAssetGroupsToolDesc` / `listAssetScopesToolDesc` / `listICPTasksToolDesc` / `listAuditLogsToolDesc`） |
| scope 登记（4 条 + 决策说明） | `internal/services/mcpguard/scope.go:97-111` |
| 参数归一层 | `internal/services/mcpguard/params.go`（`NormalizePaging:66`、`NormalizeICPTaskState:86`、`NormalizeICPTaskType:103`、`NormalizeICPProvider:119`、`NormalizeAuditMethod:140`、`NormalizeAuditStatusMin:156`、`NormalizeScopeGroupID:179`） |
| 归一层单测（8 个） | `internal/services/mcpguard/params_test.go` |
| 文档 | `docs/MCP.md:158-187`（新增章节 + 工具总数 53 + 权威口径说明） |

改动文件（仅本任务）：`internal/mcp/tools.go`(+200)、`internal/mcp/schema.go`(+77)、`internal/services/mcpguard/scope.go`(+14)、`internal/services/mcpguard/params.go`(新)、`internal/services/mcpguard/params_test.go`(新)、`docs/MCP.md`(+33)、本文件。
> `git status` 里其它 dirty 文件（poc/node/Makefile/assetpacks 等）来自其它工作流，非本次改动。

---

## 6. 验证

### 6.1 编译

```
$ go build ./...
(无输出 = 通过)
```

### 6.2 单测（本任务硬要求）

```
$ go test ./internal/services/mcpguard/... -count=1
ok  	github.com/Autumn-27/ScopeSentry/internal/services/mcpguard	10.332s
```

（10s 是包级 init 连 Mongo/Redis 超时，既有行为，与本次改动无关。）

逐个用例：

```
$ go test ./internal/services/mcpguard/ -run 'TestNormalize|TestReadOnlyV2' -count=1 -v
=== RUN   TestNormalizePaging
--- PASS: TestNormalizePaging (0.00s)          # 9 子用例：缺省/透传/负数/上限/超限报错/maxSize=0
--- PASS: TestNormalizeICPTaskState (0.00s)    # nil 不等于 state=0，且返回副本
--- PASS: TestNormalizeICPTaskType (0.00s)
--- PASS: TestNormalizeICPProvider (0.00s)
--- PASS: TestNormalizeAuditMethod (0.00s)
--- PASS: TestNormalizeAuditStatusMin (0.00s)
--- PASS: TestNormalizeScopeGroupID (0.00s)    # unassigned / 0 / 空 / ObjectID 放行；名称、23 位 hex 拒绝
--- PASS: TestReadOnlyV2ToolsScopes (0.00s)    # 4 个工具已登记且 scope=asset:read，受限 key 可调用
ok  	github.com/Autumn-27/ScopeSentry/internal/services/mcpguard	10.171s
```

### 6.3 生成 schema 自检（用 go-sdk 的 in-memory transport 打 `tools/list`）

```
list_asset_groups  => {"additionalProperties":false,"type":"object"}
list_asset_scopes  => {"properties":{"groupId":{...},"pageIndex":{"type":"integer"},"pageSize":{"type":"integer"},"search":{...}},"type":"object"}
list_icp_tasks     => {"properties":{"keyword":{...},"pageIndex":{...},"pageSize":{...},"provider":{...},"state":{"type":["null","integer"]},"type":{...}},"type":"object"}
list_audit_logs    => {"properties":{"method":{...},"pageIndex":{...},"pageSize":{...},"path":{...},"statusMin":{"type":"integer"},"user":{...}},"type":"object"}
```

字段名与 handler 完全一致（`groupId`/`statusMin` 等驼峰），`state` 为可空整数（`*int` → `["null","integer"]`），不传等价于不过滤。

### 6.4 治理表 ↔ 注册表零差集（替代跑不了的 integration 测试）

```
registered: 53
scoped    : 53
only registered: []
only scoped    : []
```

`TestEveryRegisteredToolHasScope` / `TestNoOrphanScopeEntries`（`internal/mcp/tools_registry_integration_test.go`，`-tags integration`）需要真实 Mongo（该包 init 会连库，无库时 `Error during startup events: MongoDB client is not initialized` 直接 FAIL），本次在本机用上表静态比对覆盖了同一断言；有库环境下应补跑一次：

```
go test -tags integration ./internal/mcp/ -run 'TestEveryRegisteredToolHasScope|TestNoOrphanScopeEntries'
```

### 6.5 格式 / vet

```
$ gofmt -l internal/mcp internal/services/mcpguard   # 无输出
$ go vet ./internal/mcp/ ./internal/services/mcpguard/   # 无输出
```

### 6.6 生产只读验收（建议，本次未执行——不部署）

```bash
# 工具是否出现在 tools/list（总数应为 53）
curl -s http://<host>:8082/mcp -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | python3 -c \
  'import json,sys; d=json.load(sys.stdin); print(len(d["result"]["tools"]))'

# 资产组（生产 asset_group 目前 0 条，预期 items: [] total: 0）
curl -s http://<host>:8082/mcp ... -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_asset_groups","arguments":{}}}'

# 地契未分组
curl -s ... -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_asset_scopes","arguments":{"groupId":"unassigned","pageSize":5}}}'

# ICP 任务
curl -s ... -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_icp_tasks","arguments":{"pageSize":5}}}'

# 审计：只看失败的写请求
curl -s ... -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"list_audit_logs","arguments":{"path":"/api/","method":"POST","statusMin":400,"pageSize":5}}}'

# 归一化拒绝路径（应回 isError + "pageSize 最大 500"）
curl -s ... -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list_asset_scopes","arguments":{"pageSize":100000}}}'
```

---

## 7. 未做 / 后续

1. **写侧**：资产组/地契的 add·edit·delete·reorder·batch_move_group·bind_enterprise、ICP 的 create·stop·resume·delete、ICP 结果入库（`/api/icp/{asset,sync,backfill_details}`）均未暴露。`sync` 会写 `asset_scope`，属写权限面，需先定 scope（`asset:write` 或新 `icp:write`）。
2. **同族只读缺口**（MCP_GAPS §3.2）：S2 资产读模型（cert/cip/http_status/service/stat_finger/nuclei_result/wih/fileleak/npoc，建议合成单工具 `list_asset_read_model(kind,...)`）、S3 统计分布、M10 备份只读、M11 GitHub 情报。
3. **M5 站点聚合**（`/api/assets/site/{list,chain}`）：`docs/MCP.md` 旧文声称 `list_assets` 支持 `site`，实际 `normalizeAssetIndex` 的合法值里没有 `site`（传了会报「不支持的 asset_type」）。本次未改，属文档与实现的另一处偏差（MCP_GAPS §5.8）。
4. **审计日志权限隔离**：见 §3 的四步改造路径。
5. **历史无 scope key**：生产存在 `ssk_nSt64HuF...` 之类空 scope 的 key，按 `ScopeAllowed` 空列表=全权处理，对本次 4 个新工具同样全放行。属既有治理项（`MCP_GAPS` §0.5），未在本次处理。

---

## 8. 风险与注意

- `list_audit_logs` 与 `list_icp_tasks` 在 `asset:read` 下即可读，属**扩权到信息面**；对外发 key 时若只需资产查询，可在 key 级 `tools` 白名单里排除这两个工具（`ToolAllowed` 支持精确名与 `list_*` 前缀通配）。
- `icpService` 用 `icp.DefaultService()` 共享单例：MCP 只调用只读的 `ListTasks`，不会触发 worker 或状态抢占（`Claim` 才是写路径）。
- `auditRepo` / `assetGroupService` 在 `mongodb.DB == nil` 时表现不一致：assetgroup repo 有 `db == nil` 早退（返回 `database is not initialized`），audit repo 的 `coll()` 会因 `mongodb.DB.Collection` panic。生产 DB 必然存在，故未改（改仓库层超出本任务文件边界）；若要在无库环境跑 MCP，应给 audit repo 补同样的早退。
