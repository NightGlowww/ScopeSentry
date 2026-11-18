# MCP 写侧第一批 —— 资产组 / 地契 / ICP 任务

范围：只改 `internal/mcp`、`internal/services/mcpguard`、`frontend/src/views/Configuration/ApiKey.vue`、`docs/MCP.md`。
未部署、未改库、未改服务层（`internal/services/{assets/assetgroup,icp}` 一行未动）。

依据：`docs/audit/MCP_GAPS.md` §3.1（M1 资产组 / M2 地契 / M3 ICP 任务）与 `docs/audit/MCP_TOOLS_V2.md` §7「写侧未做」。
只实现 5 个工具，其余写操作仍未暴露（清单见 §9）。

---

## 1. 结论

| # | 工具 | scope | 后端等价（handler → service） | 不可逆 | 需 confirm |
| --- | --- | --- | --- | --- | --- |
| 1 | `create_asset_group` | `asset:write` | POST /api/assets/asset_group/add → `assetgroup.CreateGroup` | 否 | 否 |
| 2 | `delete_asset_groups` | `asset:write` | POST /api/assets/asset_group/delete → `assetgroup.DeleteGroup`（逐 id） | **是** | **是** |
| 3 | `create_asset_scope` | `asset:write` | POST /api/assets/asset_scope/add → `assetgroup.CreateScope` | 否 | 否 |
| 4 | `create_icp_task` | `icp:write` | POST /api/icp/task/create → `icp.Service.CreateTask` | 否 | 否 |
| 5 | `stop_icp_tasks` | `icp:write` | POST /api/icp/task/stop → `icp.Service.Stop` | 否（可 resume） | 否 |

- 工具面 **53 → 58**；`mcpguard.ToolScopes` 58 条与 `internal/mcp` 实际注册 58 个工具**双向零差集**（§7.5 脚本比对）。
- 新增 scope 常量 1 个：`ScopeICPWrite = "icp:write"`。`ScopeAssetWrite = "asset:write"` **本来就存在**（`scope.go:25`，`add_asset_tag` / 导出 / 字典等 9 个工具在用），本批只是新增挂载点，没有重复定义。
- 单测新增 10 个（`internal/services/mcpguard/write_test.go`，337 行），覆盖 scope 登记、confirm 闸、5 类参数归一。
- `go build ./...`、`go test ./internal/services/mcpguard/...`、`cd frontend && pnpm build:pro` 全部通过（§7）。

---

## 2. scope 决策

| 决策 | 内容 | 理由 |
| --- | --- | --- |
| 复用 `asset:write` | 建组 / 删组 / 建地契 | 这三者改的是「资产模型（集团/地契）的归属结构」，与 `add_asset_tag` 同属资产写面；单独开 `group:write` 会让已有 `asset:write` key 全部失效，收益只是命名更细 |
| 新增 `icp:write` | 建 / 停 ICP 任务 | ICP 的副作用与资产写不同面：**对外部 ICP 数据源发起真实查询**、落 `icp_result`、后台 goroutine 跑全流程，且按 provider 消耗配额。只想给自动化开「资产写」的 key 不该顺带获得对外查询能力 |
| **不复用** `asset:read` | 5 个工具全部不挂只读 scope | `MCP_GAPS.md` §9 记录过现网只读 key 调 `start_task` 被 `insufficient scope: need task:write` 拦下的案例，那次拦截是对的。写工具若挂只读 scope，等于把「只读 key」一次性升级成「可删除 key」 |

家族通配语义（`ScopeMatches`）：`asset:*` 覆盖 `asset:write`、`icp:*` 覆盖 `icp:write`，但**两者互不跨界**，且 `asset:*` 不覆盖 `icp:write`（有单测断言）。

三方一致性：`scope.go` 常量 → `ApiKey.vue:SCOPE_OPTIONS` → `docs/MCP.md` scope 表。本批三处同改（`icp:write`），UI 现在可选 **13 个细分 + `*`**。

---

## 3. 工具定义（入参逐字对齐 handler / models）

### 3.1 `create_asset_group`（asset:write）

`models.AssetGroupAddRequest`：`name`（binding:"required"）、`description`、`sortOrder`。

| 工具字段 | 类型 | 校验（MCP 侧） | service 行为 |
| --- | --- | --- | --- |
| `name` | string，必填 | 去空白、1-64 rune（`NormalizeWriteName`） | 再判一次；`FindGroupByName` 重名 → `ErrGroupExists`（不覆盖） |
| `description` | string | 去首尾空白 | 落库 |
| `sortOrder` | int | 无（任意整数，与 UI 一致） | 落库 |

返回 `{"ok":true,"id":"<ObjectID>","name":"..."}`。`id` 可直接喂 `create_asset_scope.groupId`。

### 3.2 `delete_asset_groups`（asset:write，高危）

`models.AssetGroupDeleteRequest` = `{id, action, targetGroupId}`，服务层**没有批量接口**，因此工具按 id 逐个调用。

| 工具字段 | 类型 | 规则 |
| --- | --- | --- |
| `ids` | []string，必填 | 去空白 → 小写 → 去重 → 1-20 个（`MaxDeleteAssetGroupIDs`）；每个必须 24 位 hex |
| `action` | string | `dissolve`（默认；地契移入未分组）/ `transfer`（先划转到 targetGroupId） |
| `targetGroupId` | string | `action=transfer` 时必填，24 位 ObjectID，且**不能是本批被删的集团之一** |
| `confirm` | bool | **必须 true**，否则 `ErrConfirmRequired` |

返回 `{"ok":bool,"action":...,"targetGroupId":...,"deleted":[...],"failed":[{id,error}],"total":n}`；
逐个执行，单条失败不中断整批；**全部失败**时返回 `isError`（错误文本含每个 id 的原因），避免「返回 ok 但什么都没删」。

### 3.3 `create_asset_scope`（asset:write）

`models.AssetScopeAddRequest`：`Name`(required)、`Scope`(required)、`ScopeType`、`GroupID`。

| 工具字段 | 规则 |
| --- | --- |
| `name` | 去空白，1-128 rune（`MaxAssetScopeNameRunes`） |
| `scope` | 非空且至少一个条目（`NormalizeAssetScopeRange`，分隔符 `\n \r \t , ; 空格`）；逐条域名/IP 合法性由 `service.buildScope` 判，错误带具体条目 |
| `scopeType` | `domain` / `ip` / `mixed`，留空=按内容推断（`NormalizeAssetScopeType`） |
| `groupId` | 24 位 ObjectID 或留空（=未分组）；**不接受 `unassigned` / `0`**（那是只读过滤标记，见 §5） |

**关于「url」**：任务描述里提到 `url`，但 handler 的 `AssetScopeAddRequest` 只有 `name/scope/scopeType/groupId`，**没有 url 字段**；地契存的是域名/IP 文本（解析成 `domainArray` / `ipArray`）。本实现严格对齐 handler，不虚构 `url` —— 给单个 URL 时传它的 host 即可。这一点已写进工具 description，避免 Agent 反复试错。

返回 `{"ok":true,"id":...,"name":...,"scopeType":...,"groupId":...}`（`scopeType` 为空表示由服务端推断，要看推断结果用 `list_asset_scopes`）。

### 3.4 `create_icp_task`（icp:write）

`models.IcpTaskCreateRequest`：`Name / Type / Queries / Target / Provider`。

| 工具字段 | 规则 | service 语义（复用，不重写） |
| --- | --- | --- |
| `type` | 大小写不敏感；空 → `domain`（`NormalizeICPTypeForCreate`） | `company`=企业名查域名；`domain`=域名查主体；其余 `ErrInvalidType` |
| `queries` | 去空去重；domain 类型小写去尾点（复用 `models.NormalizeIcpQueries`） | 同上 |
| `target` | 仅当 `queries` 为空时作为唯一查询（与 `CreateTask` 同规则） | 同上 |
| `name` | 显式给才校验 1-128 rune；留空 → 用 `queries[0]` | `maxTaskNameRunes=128` |
| `provider` | `manual` / `none` / `http`，空 = 按当前配置解析（`NormalizeICPProvider`） | 注册表里没有该 provider → `ErrInvalidProvider` |

返回 `{"ok":true,"task":{...}}`（与 REST handler `response.Success(c, task)` 同形）。

**副作用**：`CreateTask` 内部已 `Claim` 并 `launch` 后台 goroutine，本请求不同步等待；返回的 `state` 通常已是 1（运行中），进度用 `list_icp_tasks` 轮询 `stats`。

### 3.5 `stop_icp_tasks`（icp:write）

`models.IcpTaskIDRequest.All()` 的多别名合并语义在 MCP 侧简化为 `ids []string`（Agent 不需要 id/taskId/_id 三种写法）。

| 工具字段 | 规则 |
| --- | --- |
| `ids` | 必填，去空白/小写/去重后 1-50 个（`MaxStopICPIDs`），每个 24 位 hex |

返回 `{"ok":true,"updated":n,"ids":[...]}`。`updated` 是 `ModifiedCount`：已完成(3)/已暂停(2) 的任务不会被改，所以 `updated < len(ids)` 是正常结果（description 已写明）。

---

## 4. 高风险防护（删除类 confirm）

**规则**：`delete_asset_groups` 必须显式 `confirm: true`，否则在**任何服务调用之前**返回 `isError`：

```
confirm is required: delete_asset_groups 是不可逆操作，确认执行请在参数里显式传 "confirm": true
```

实现要点：

1. **校验顺序**：`confirm` → `ids` → `action`。confirm 放最前面，因为它是唯一「什么都没发生」的确定性信号；若先报 ids 超限，调用方容易误以为「差一点就删了」。
2. **不可逆写进 description**：`deleteAssetGroupsToolDesc` 首行即「【不可逆】…集团记录会被物理删除，没有回滚接口；恢复只能重新创建（id 会变，地契归属需要重新挂）」。MCP 客户端把 description 展示给模型，这是唯一能提前劝阻的通道。
3. **两处比 REST 更严**（都在 `NormalizeAssetGroupAction`）：
   - `action=dissolve` 却传了 `targetGroupId` → 报错（REST 静默忽略，调用方会以为地契被划转了）；
   - `targetGroupId` 属于本批被删集合 → 报错（REST 只挡 `targetGroupID == req.ID`，挡不住「A 转给同批的 B、B 转给 A」然后一起删）。
4. **`stop_icp_tasks` 不要求 confirm**：它是「暂停」（`state=2`），任务/日志/`icp_result` 全保留，REST `/api/icp/task/resume` 可从 `cursor` 续跑，属可逆操作。把 confirm 滥用到可逆操作上只会训练调用方无脑传 true，反而削弱真正高危工具的那道闸。

---

## 5. 参数归一（`mcpguard` 纯函数层）

MCP 直接调 service，不经 gin `ShouldBindJSON`，handler 的 `binding:"required"` / 长度上限 / 闭集全部失效。写操作的静默失败比读更贵（少一个参数就建出空壳地契，ids 混一个非法值就半删），所以这一批**一律先归一再落库**。

| 归一项 | 函数 | 规则 | 不补的后果 |
| --- | --- | --- | --- |
| confirm | `RequireConfirm` | false → `ErrConfirmRequired`（含工具名与正确写法） | Agent 漏传即真删 |
| 批量 ids | `NormalizeBatchIDs` → `NormalizeDeleteAssetGroupIDs` / `NormalizeStopICPIDs` | 去空白→小写→去重→判上限（20/50）→逐个 24 位 hex | 半删；仓库层只报 `invalid id` 无字段名；重复 id 白占额度 |
| 名称 | `NormalizeWriteName` | 去空白、非空、**按 rune** 计长（64/128） | 中文集团名按字节算被误判超长 |
| 删除策略 | `NormalizeAssetGroupAction` | dissolve/transfer 互斥；transfer 的 target 必填且不在 ids 内 | 静默丢归类；同批互转后一起删 |
| 地契归属 | `NormalizeScopeOwnerGroupID` | 空或 24 位 hex；**拒绝 `unassigned` / `0`** | 把只读过滤标记写进库；`ensureGroup` 在建库后才报 `invalid group id` |
| 地契类型 | `NormalizeAssetScopeType` | 空 / domain / ip / mixed | `host` 这类值到 `buildScope` 才报错 |
| 地契范围 | `NormalizeAssetScopeRange` | 非空且至少一个条目 | 建出空地契（服务层会拒，但错误无字段名） |
| ICP 类型 | `NormalizeICPTypeForCreate` | 复用 `NormalizeICPTaskType`；空 → domain | — |
| ICP 查询 | `NormalizeICPQueriesForCreate` | 复用 `models.NormalizeIcpQueries`；target 回落；空 → `ErrEmptyQueries` | 建出 0 查询的任务，只有服务层报错且不带参数名 |

上限取值对齐后端：集团名 64 / 地契名 128 / ICP 任务名 128 来自各 service 自己的校验；ids 20（删除）/ 50（停止）是本批新增的业务上限，超限**报错而非静默截断**。

---

## 6. 代码位置

| 内容 | 位置 |
| --- | --- |
| `ScopeICPWrite` 常量 | `internal/services/mcpguard/scope.go:39` |
| 5 条工具登记 | `internal/services/mcpguard/scope.go:124-128` |
| 写侧归一层（常量/错误/11 个函数） | `internal/services/mcpguard/params.go:193-448`（`MaxDeleteAssetGroupIDs:204`、`RequireConfirm:259`、`NormalizeBatchIDs:269`、`NormalizeDeleteAssetGroupIDs:298`、`NormalizeStopICPIDs:303`、`NormalizeWriteName:310`、`NormalizeAssetGroupAction:329`、`NormalizeScopeOwnerGroupID:366`、`NormalizeAssetScopeType:381`、`NormalizeAssetScopeRange:398`、`NormalizeICPTypeForCreate:417`、`NormalizeICPQueriesForCreate:435`） |
| 工具注册（5 个 addTool） | `internal/mcp/tools.go:396-421` |
| `createAssetGroup` | `internal/mcp/tools.go:2095` |
| `deleteAssetGroups` | `internal/mcp/tools.go:2127` |
| `createAssetScope` | `internal/mcp/tools.go:2186` |
| `createICPTask` | `internal/mcp/tools.go:2234` |
| `stopICPTasks` | `internal/mcp/tools.go:2276` |
| 工具长描述（含「不可逆」与 confirm 说明） | `internal/mcp/schema.go:271-395` |
| `SCOPE_OPTIONS`（asset:write 标签更新 + icp:write 新增） | `frontend/src/views/Configuration/ApiKey.vue:47-56` |
| 单测（10 个） | `internal/services/mcpguard/write_test.go`（337 行） |
| 文档 | `docs/MCP.md`（scope 表 9-23、写侧章节 186-207、工具总数 58 在第 209 行） |

改动文件（仅本任务）：`internal/mcp/tools.go`(+244)、`internal/mcp/schema.go`(+129)、`internal/services/mcpguard/scope.go`(+18)、`internal/services/mcpguard/params.go`(+258)、`internal/services/mcpguard/write_test.go`(新)、`frontend/src/views/Configuration/ApiKey.vue`(+8/-2)、`docs/MCP.md`(+42)、本文件。
> `git status` 里的其它 dirty 文件（Makefile / poc / node / assetpacks / tools 等）来自其它工作流，非本次改动。

---

## 7. 验证

### 7.1 编译

```
$ go build ./...
build OK
```

### 7.2 单测（本任务硬要求）

```
$ go test ./internal/services/mcpguard/... -count=1
ok  	github.com/Autumn-27/ScopeSentry/internal/services/mcpguard	10.193s
```

（10s 是包级 init 连 Mongo/Redis 超时，既有行为，与本次改动无关。）

逐个用例：

```
=== RUN   TestWriteToolsScopes
--- PASS: TestWriteToolsScopes (0.00s)           # 5 工具已登记 + scope 正确 + 只读 key 不可达 + asset:*/icp:* 不跨界
=== RUN   TestCheckWriteToolsScopeDenied
--- PASS: TestCheckWriteToolsScopeDenied (0.00s) # Check() 端到端：只读 key → ErrScopeDenied；写 key → 放行
=== RUN   TestRequireConfirm
--- PASS: TestRequireConfirm (0.00s)             # confirm=false → ErrConfirmRequired（含工具名与 confirm 字样）
=== RUN   TestNormalizeDeleteAssetGroupIDs
--- PASS: TestNormalizeDeleteAssetGroupIDs (0.00s) # 去空白/小写/去重；空→ErrIDsRequired；非法→ErrInvalidObjectID；20 放行/21 拒绝/重复不占额度
=== RUN   TestNormalizeStopICPIDs
--- PASS: TestNormalizeStopICPIDs (0.00s)        # 上限 50 独立于删除的 20
=== RUN   TestNormalizeAssetGroupAction
--- PASS: TestNormalizeAssetGroupAction (0.00s)  # 默认 dissolve；transfer 必填 target；target 不在 ids；未知 action 拒绝
=== RUN   TestNormalizeWriteName
--- PASS: TestNormalizeWriteName (0.00s)         # 64 个中文字符放行（rune 计长）、65 拒绝、空拒绝
=== RUN   TestNormalizeScopeOwnerGroupID
--- PASS: TestNormalizeScopeOwnerGroupID (0.00s) # unassigned/0 在写侧拒绝；只读侧仍接受
=== RUN   TestNormalizeAssetScopeTypeAndRange
--- PASS: TestNormalizeAssetScopeTypeAndRange (0.00s)
=== RUN   TestNormalizeICPForCreate
--- PASS: TestNormalizeICPForCreate (0.00s)      # type 回落 domain；queries/target 取一；domain 归一；企业名保原样；空报错
ok  	github.com/Autumn-27/ScopeSentry/internal/services/mcpguard	10.275s
```

### 7.3 格式 / vet

```
$ go vet ./internal/mcp/ ./internal/services/mcpguard/
vet OK
$ gofmt -l internal/mcp internal/services/mcpguard
gofmt clean
```

### 7.4 前端构建（本任务硬要求）

```
$ cd frontend && pnpm build:pro
> vue-element-plus-admin@2.5.2 build:pro
> pnpm vite build --mode pro
Build successful. Please see dist-pro directory
```

### 7.5 治理表 ↔ 注册表零差集

```
registered: 58
scoped    : 58
only registered: []
only scoped    : []
dupes reg: []
write tools: ['create_asset_group', 'delete_asset_groups', 'create_asset_scope', 'create_icp_task', 'stop_icp_tasks']
```

### 7.6 未执行：integration 漂移测试

`internal/mcp` 的 `TestEveryRegisteredToolHasScope` / `TestNoOrphanScopeEntries`（`-tags integration`）需要真实 Mongo（该包 init 会连库），本机无库，用 §7.5 的等价静态比对覆盖了同一断言。有库环境应补跑：

```
go test -tags integration ./internal/mcp/ -run 'TestEveryRegisteredToolHasScope|TestNoOrphanScopeEntries'
```

### 7.7 生产验收（建议，本次未执行 —— 不部署）

```bash
# 1) tools/list 总数应为 58
curl -s http://<host>:8082/mcp -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["result"]["tools"]))'

# 2) 只读 key 调写工具 → 应被 scope 拒绝（不是 500）
curl -s ... -d '{"jsonrpc":"2.0","id":2,"method":"tools/call",
  "params":{"name":"create_asset_group","arguments":{"name":"验证组"}}}'
# 期望: isError, 消息含 "insufficient scope: need asset:write"

# 3) 写 key 但缺 confirm → 应拒绝
curl -s ... -d '{"jsonrpc":"2.0","id":3,"method":"tools/call",
  "params":{"name":"delete_asset_groups","arguments":{"ids":["<oid>"]}}}'
# 期望: isError, 消息含 "confirm is required"

# 4) 归一拒绝路径：ids 超过 20
curl -s ... -d '{"jsonrpc":"2.0","id":4,"method":"tools/call",
  "params":{"name":"delete_asset_groups","arguments":{"ids":[...21 个...],"confirm":true}}}'
# 期望: isError, 消息含 "单次最多 20 个"

# 5) 建组 → 建地契 → 列表回读（唯一会写库的验证，建议在测试实例做）
curl -s ... '{"name":"create_asset_group","arguments":{"name":"验收组A"}}'
curl -s ... '{"name":"create_asset_scope","arguments":{"name":"验收地契","scope":"example.com\nexample.org","groupId":"<上一步 id>"}}'
curl -s ... '{"name":"list_asset_scopes","arguments":{"groupId":"<上一步 id>"}}'
```

---

## 8. 风险与注意

1. **历史空 scope key 全放行**：`ScopeAllowed` 对空 scope 列表返回 true（`scope.go:150-162`），生产存在 `ssk_nSt64HuF...` 这类无 scope 的历史 key，它对本次 5 个写工具**同样全放行**（包括 `delete_asset_groups`，只需补 `confirm`）。这是既有治理项（`MCP_GAPS` §0.5），建议尽快做一次 scope 回填；回填之前，`confirm` 是删除类唯一的闸。
2. **建组无唯一索引**：`CreateGroup` 是「先 `FindGroupByName` 再插」，并发下仍可能插入同名集团（repo 无 unique index）。MCP 侧不做额外补偿（补了也只是缩小窗口），需要严格幂等请先 `list_asset_groups` 查重。
3. **删除是逐条 + 部分成功**：`deleted` / `failed` 分开返回，`ok` 仅在零失败时为 true。调用方**必须读 `failed` 数组**，不能只看 `ok` 就认为整批完成。
4. **`create_icp_task` 有对外副作用**：会启动后台 goroutine 并按 provider 调外部 ICP 源（`http` 模式下产生真实请求与配额消耗），且 `state` 从 1 开始。批量建任务前请确认 provider 配额与 `icp.provider_url` 配置。
5. **`stop_icp_tasks` 会写日志**：`service.Stop` 对每个 id 追加一行 `icp_task_log`（warn 级），50 个 id = 50 行日志，属预期行为。
6. **`create_asset_scope` 不做域名格式预校验**：避免与 `service.buildScope` 的规则双写导致漂移；错误信息由服务层给（含具体条目），但**不带** MCP 的「参数不合法」前缀 —— Agent 需要能读懂 `"x" is not a valid domain or ip` 这类英文错误。
7. **`create_asset_scope` 会原样存 `scope` 文本**：`AssetScope.Scope` 存用户原文（只 trim），解析结果另存数组。因此同一批域名用不同分隔符创建会产生不同 `scope` 字段，而 `search` 对它的正则会命中原文而不是归一后的值。
8. **前端标签是硬编码中文**：`SCOPE_OPTIONS` 的 `label` 一直是中文字面量（本次沿用既有风格），所以 `icp:write` 只在 `ApiKey.vue` 加了一行、没有动 `locales/{zh-CN,en,ko-KR}.ts`（任务文件边界只含该 `.vue`）。若要迁 i18n，三处需同步加 key（建议 `apiKey.scopeAssetWrite` / `apiKey.scopeICPWrite`）；scope 值与 key 名已在本文对齐，迁移时不会产生语义漂移。
9. **MCP 写工具不受 Web 端二次确认保护**：REST 侧删组有前端 `ElMessageBox.confirm`，MCP 侧只有 `confirm: true`。这是设计取舍：Agent 场景无法做 UI 确认，所以把「确认」变成协议参数，并让错误信息直接给出正确写法。

---

## 9. 未做 / 后续

**仍不暴露的写操作**（本批明确不做）：

- 资产组：`edit` / `reorder`（`/api/assets/asset_group/{edit,reorder}`）
- 地契：`update` / `delete` / `batch_move_group` / `bind_enterprise` / `clear_update_error`
- ICP：`restart` / `resume` / `delete`、结果入库与地契同步（`/api/icp/{asset,sync,backfill_details,export}`）
- 备份（`create/restore/upload`，restore 需 `confirm:"RESTORE"`）、通知与全局配置写、POC/指纹/敏感规则的增改删、插件 `run` / `import`、节点配置更新、项目删除

**后续建议**：

1. 地契 `delete` / `batch_move_group` 是下一批的自然延伸，但都要挂 `confirm: true`；`batch_move_group` 的 `groupId` 应复用本批的 `NormalizeScopeOwnerGroupID`（空=未分组）。
2. `icp sync`（结果写入地契）是「查询 → 落库」闭环的最后一段，但它同时写 `asset_scope`，scope 归属需要先定：挂 `icp:write` 会让 ICP 权限能改资产模型，挂 `asset:write` 则要求同时持两个 scope。建议后者（双 scope 交集），并在 description 写明。
3. 治理：回填历史 key 的 scope（§8.1），并把 `docs/MCP.md` 的「按版本累加」改为 CI 断言 `len(ToolScopes) == len(ToolNames())`。
