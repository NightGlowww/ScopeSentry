# 扫描策略（scan_policy）消费端接线

- 对象：`/Users/night/Workspaces/security_workspace/_tools/ScopeSentry`（Go + Gin + Vue3）
- 背景：`docs/audit/EMPTY_COLLECTIONS.md` §2 `scan_policy` 行判定为「A 生产者存在 + **无消费方**」——`internal/services/task/`、`internal/api/handlers/task/` 里检索 `policyService` / `Apply` 均 0 命中，`policy.Apply` 只做合并预览、不落任务。
- 约束：不部署、不改库数据；改动集中在 `internal/services/task/**`、`docs/audit/SCAN_POLICY.md`（`internal/services/policy/**` 无需改动），另加 1 处 4 行的模型字段（见 §3.1，必要且向后兼容）。
- 时间：2026-11-19 06:05 — 06:30 (+08)

---

## 0. 结论摘要

**生效点判定：策略在「建任务 → 组装节点扫描载荷」这一步生效，不是目标过滤，不是去重，也不是节点侧限速。**

扫描载荷 `models.ScanTemplate` 里真正能被策略驱动的只有两件事，接线就落在这两件事上：

| 策略字段 | 落点 | 状态 |
| --- | --- | --- |
| `config.templateId` | `models.Task.Template`（任务没填模板时兜底，随任务落库） | ✅ 生效 |
| `config.plugins` | `models.ScanTemplate` 的 14 个分阶段插件清单 + 同名 `Parameters[phase]` 参数表 | ✅ 生效 |
| `threadNum` / `rateLimit` / `timeoutSeconds` | 载荷里没有全局执行字段 | ⚠️ 仅记录（显式 warning，不臆造旗标） |
| `dicts.dir` / `dicts.subdomain` / `dicts.brute` | 模板参数用 `{dict.<类别>.<名称>}` 占位符，字典名没有可写位置 | ⚠️ 仅记录 |
| `options` | 模板参数没有自由键值槽位 | ⚠️ 仅记录 |

为什么后四类不硬塞：模板的 `Parameters` 是**每个插件自己的 CLI 字符串**（`GetScanTemplate` 只在 `VulList` 非空时追加 `-t`，其余原样下发；见 `docs/audit/VULN_LINK.md` §参数实测）。并发/速率的旗标名由各插件自己定义（nuclei 的 `-t` 是模板路径，不是并发），服务端按插件猜旗标就是把 bug 写进产品。

---

## 1. 设计意图（从代码读出来的，不是推测）

1. `internal/models/scan_policy.go` 头注释：「一组可复用的扫描参数，建任务时可选策略覆盖模板默认值」。
2. `policy.Service.Apply`（`internal/services/policy/policy.go`）是**纯预览**：读策略 + 可选读模板基线 → `MergeConfig` → 返回 merged config + warnings + appliedFields，全程不写库。它的 `taskTemplate` 入参支持两种形态（模板 ID 字符串读模板分阶段清单生成 baseline，或对象直接当默认值）——这正是「建任务表单把当前状态带上来预览」的形状。
3. `policy/merge.go` 的合并语义：指针区分「未设置」与「显式 0」；`plugins` 非空才覆盖；`dicts` 三个子项独立判断；`options` 按 key 合并；冲突只产 warning 不阻断。
4. 前端 `ScanPolicy.vue` 是策略 CRUD + test + 合并预览；`AddTask.vue` 早已有「一键套用」下拉（`policyId` / `onPolicyChange`），但它只把 `merged.templateId` 填进表单，注释原文：**「其余参数（插件/并发/速率/超时/字典/options）没有对应表单字段，只做展示」**。

→ 设计意图是「策略 = 模板默认值来源 + 执行参数预设」，而执行参数只能通过节点扫描载荷送到节点，所以唯一合理的接入点就是载荷组装处。

---

## 2. 接入点判定（候选逐个否掉）

| 候选接入点 | 依据 | 判定 |
| --- | --- | --- |
| 目标过滤（`task.Target` / `Ignore`） | 策略模型没有任何 target / ignore / 域名维度字段；`ScanPolicyConfig` 只有模板/插件/并发/速率/超时/字典/options | ❌ 无字段支撑，硬加就是臆造 |
| 去重（`task.Duplicates`） | 去重是任务级字段（`None`/`subdomain`），策略里没有对应项 | ❌ 不属于策略语义 |
| 扫描参数（模板/插件） | `templateId` → 读哪张模板；`plugins` → 各阶段跑哪些插件 | ✅ **接入点 A/B** |
| 限速 / 并发 / 超时 | 载荷无全局字段，且插件旗标不统一 | ⚠️ 记录 + warning（§0） |
| 节点侧执行期限速 | 节点是独立组件，改协议属 node 侧工作，本仓库单方面无法落地 | ⛔ 超出本次范围（写进遗留） |

---

## 3. 实现

### 3.1 为什么要动一个模型字段（唯一越界改动）

`AddTask` 直接 `c.ShouldBindJSON(&models.Task)`，Gin 用 `encoding/json` 解码时**丢弃未声明字段**。所以策略选择必须有一个能跨越 HTTP → 服务 → Mongo → 定时轮次回读的载体，否则：

- 放 `task.Filter`（`map[string][]interface{}`）→ 会被 `Insert` 原样带进资产检索条件，污染查询；
- 放 `task.Search` / `task.Duplicates` → 改变既有语义，属臆造；
- 不落库、只在请求内传递 → 重测（`RetestTask`）、`StartTasks`、定时轮次全部丢失策略。

因此最小改动是给 `models.Task` 加一个可选字段（`internal/models/task.go` 末尾，独立分组，不影响既有字段对齐）：

```go
// PolicyID 建任务时选中的扫描策略（scan_policy._id）；空 = 不套用策略。
// 只作「选哪个策略」的引用，参数消费在 services/task/taskpolicy。
PolicyID string `bson:"policyId,omitempty" json:"policyId,omitempty"`
```

`omitempty` + 空串语义 = 不选策略时文档形状与之前一致；`json:"policyId"` 让 `/api/task/add` 自动接受该字段，**handler 无需改动**。

### 3.2 新包 `internal/services/task/taskpolicy`

| 文件 | 内容 | 是否碰 DB |
| --- | --- | --- |
| `apply.go` | `ApplyToTask`（模板兜底）、`ApplyToTemplate`（插件白名单）、`UnsupportedWarnings`（无承载字段告警） | 否，纯函数 |
| `resolve.go` | `Loader` 接口 + `ServiceConfig`（默认走 `policy.Service.Detail`，`WithLoader` 可注入假实现） | 只在默认实现里读 `scan_policy` |

**模板优先级（`ApplyToTask`）**

```
task.Template 为空  → 用 cfg.TemplateID 填上，并随任务落库
task.Template 已填且 ≠ cfg.TemplateID → 保留 task.Template，产 template_conflict warning
task.Template 已填且 = cfg.TemplateID  → 无动作
```

选「用户选择优先」而不是「策略强制覆盖」，是因为 UI 里策略只**预填**表单（`applyPolicyToTaskForm` 只在模板为空时写值）；如果服务端无条件覆盖，用户随后手改的模板会被静默改回去，表单就在骗人。

**插件白名单（`ApplyToTemplate`）**

```
for 每个阶段（14 个）:
    若该阶段清单里命中 ≥1 个策略插件 → 只保留策略插件（保持模板原顺序），
                                         并删除被移除插件在 Parameters[phase] 里的参数条目
    若 0 命中                        → 该阶段原样保留（策略没管这个阶段）
整张模板都 0 命中 → 清单不动 + plugins_not_applied warning
```

两个字段必须成对裁剪：阶段切片决定跑哪些插件，`Parameters[phase]` 决定每个插件的 CLI 参数，只裁一边会让节点看到不一致的数据。

### 3.3 两个接线点（`internal/services/task/common/common.go`）

```
POST /api/task/add
  └─ handlers/task.AddTask → bind models.Task（含 policyId）
      └─ common.Insert
          ├─ applyTaskPolicy(ctx, task)      ← 接线 A：任务没填模板时用策略模板兜底（落库）
          └─ taskRepo.Insert → task.policyId 持久化
              └─ go CreateTaskScan
                  └─ GetScanTemplate        ← 接线 B：读策略 → ApplyToTemplate 收窄插件清单
                      └─ RPushNodeTask(NodeTask:<node>)  → 节点拿到的就是裁剪后的载荷

重测 / 继续：RetestTask / StartTasks → CreateTaskScan → GetScanTemplate（同一路径，策略继续生效）
定时轮次：schedulerRepo.Insert(ctx, req) 是 InsertOne(models.Task) → ScheduledTasks 文档里就带 policyId；
          jobs/scan.go Scan() 反解成 models.Task → common.Insert → 策略继续生效
```

向后兼容：`policyId` 为空时 `taskPolicyConfig` 立即返回 `(nil, nil)`，**零次 DB 读、零个分支变化**；策略被删/停用时只记 `logger.Warn` 并按模板默认值继续，建任务不会失败。

### 3.4 顺带修掉的一个 panic

`repositories/task/task.FindTemplateByID` 找不到文档时返回 `(nil, nil)`，旧 `GetScanTemplate` 紧接着 `scanTemplate.VulList` 会 nil 解引用 panic。策略会引入「策略模板已被删除」的新路径，所以补了显式判断并返回 `scan template not found: <id>`。

### 3.5 前端

建任务表单**已经有**策略下拉（前次提交），缺的是「提交」这一半。本次补：

1. `api/task/index.ts`：`addTaskApi` / `addScheduledTaskApi` 增加可选尾参 `policyId`，只在非空时进 body（`policyId: policyId || undefined`）。
2. `AddTask.vue`：普通任务与计划任务两个提交分支都把 `policyId.value` 带上；`loadTaskData` 回显 `res.data.policyId`。
3. `api/task/types.ts`：`TaskDetail` 增加可选 `policyId`。
4. 生效范围提示（中/英/韩三语 `policy.pickEffectHint`）：明确「执行期只生效模板与插件清单，并发/速率/超时/字典/附加参数只做记录」。
5. `policyId` 的 `ref` 上移到 `props.taskid` 的 immediate watch **之前**：编辑任务时该 watch 会同步调 `loadTaskData` 回显 `policyId`，晚声明会命中 TDZ（`Cannot access before initialization`）。

---

## 4. 改动清单

| 文件 | 类型 | 说明 |
| --- | --- | --- |
| `internal/services/task/taskpolicy/apply.go` | 新增 | 纯函数：模板兜底 / 插件白名单 / 无承载字段告警 |
| `internal/services/task/taskpolicy/resolve.go` | 新增 | `Loader` + `Config`（默认走 policy.Service.Detail） |
| `internal/services/task/taskpolicy/apply_test.go` | 新增 | 12 个纯函数单测 |
| `internal/services/task/taskpolicy/resolve_test.go` | 新增 | 6 个装载/停用/未找到单测 |
| `internal/services/task/common/common.go` | 修改 | 接线 A（`applyTaskPolicy`）、接线 B（`GetScanTemplate`）、`taskPolicyConfig`、nil 模板保护 |
| `internal/services/task/common/policy_integration_test.go` | 新增 | `//go:build integration`：真实 Mongo 下验证「策略换模板 + 收窄插件」与「不选策略不变」 |
| `internal/models/task.go` | 修改（4 行） | `Task.PolicyID`（见 §3.1） |
| `frontend/src/api/task/index.ts` | 修改 | 两个建任务 API 增加可选 `policyId` |
| `frontend/src/api/task/types.ts` | 修改 | `TaskDetail.policyId?` |
| `frontend/src/views/Task/components/AddTask.vue` | 修改 | 提交带 `policyId`、编辑回显、生效范围提示 |
| `frontend/src/locales/{zh-CN,en,ko-KR}.ts` | 修改 | `policy.pickEffectHint` |

`internal/services/policy/**` 未改动：`Apply` 的预览语义（merged config + warnings）保持不变，策略服务仍是单一职责。

---

## 5. 验证

```
$ go build ./...
BUILD_EXIT=0

$ go test ./internal/services/task/... ./internal/services/policy/...
ok  	github.com/Autumn-27/ScopeSentry/internal/services/task/audit	0.633s
?   	github.com/Autumn-27/ScopeSentry/internal/services/task/common	[no test files]
?   	github.com/Autumn-27/ScopeSentry/internal/services/task/scheduler	[no test files]
?   	github.com/Autumn-27/ScopeSentry/internal/services/task/task	[no test files]
ok  	github.com/Autumn-27/ScopeSentry/internal/services/task/taskmsg	(cached)
ok  	github.com/Autumn-27/ScopeSentry/internal/services/task/taskpolicy	(cached)
ok  	github.com/Autumn-27/ScopeSentry/internal/services/task/tasksnapshot	(cached)
ok  	github.com/Autumn-27/ScopeSentry/internal/services/task/taskstate	(cached)
ok  	github.com/Autumn-27/ScopeSentry/internal/services/task/tasksync	(cached)
?   	github.com/Autumn-27/ScopeSentry/internal/services/task/template	[no test files]
ok  	github.com/Autumn-27/ScopeSentry/internal/services/policy	10.724s
TEST_EXIT=0

$ go vet -tags integration ./internal/services/task/...
VET_INTEGRATION_EXIT=0
```

单测明细（18 个全通过，`go test ./internal/services/task/taskpolicy/ -v`）：

- 模板优先级：`EmptyConfigKeepsTask` / `KeepsUserTemplateOnConflict` / `SameTemplateNoWarning` / `FillsEmptyTemplate`
- 插件白名单：`RestrictsMatchedPhases`（含参数表同步裁剪）/ `NoMatchKeepsLists` / `NormalizesPluginRefs` / `EmptyPluginsKeepsTemplate` / `Nil` / `DoesNotMutatePolicyConfig`
- 无承载字段告警：`UnsupportedWarningsCoversAllKnobs`（顺序固定 threadNum→rateLimit→timeoutSeconds→dicts→options）/ `SkipsBlankDicts`
- 策略装载：`ConfigReturnsEnabledPolicyConfig` / `RejectsDisabledPolicy` / `LoadRejectsEmptyID` / `LoadNotFoundWhenLoaderReturnsNil` / `LoadPropagatesLoaderError` / `NewServiceWithoutLoader`

集成用例（`//go:build integration`，需要 Mongo/Redis 已初始化，未起库时自动 `t.Skip`）：`TestGetScanTemplateAppliesPolicy`（策略模板覆盖 + 白名单裁到 `dir-b2`、`dir-b1` 参数条目被删）、`TestGetScanTemplateWithoutPolicyKeepsTemplate`（不选策略 = 原样 2 个插件）。

前端类型检查（`git worktree add` 取 HEAD + 符号链接 node_modules 做基线，两边都直接跑 `./node_modules/.bin/vue-tsc --noEmit --skipLibCheck`）：

```
baseline errors: 191        (HEAD)
current  errors: 191        (改动后)
diff（去掉行号/列号后）: 0 行
```

即本次前端改动**未新增任何类型错误**；仓库既有的 191 个 `vue-tsc` 错误与本次无关（未使用导入、`ElInput v-model.number` 类型等历史项），故 `pnpm ts:check` 在改动前就是 exit 2，不能当作本次回归门禁；以「错误集合零差异」为准。

---

## 6. 风险与遗留

1. **并发/速率/超时/字典/options 仍不真正约束执行**。要让它真生效，需要在节点扫描载荷里新增分阶段执行字段（例如每阶段的 concurrency / rate / timeout），这是协议变更 + node 侧改动，不是服务端单方面能落地的；当前用 `exec_limits_unsupported` / `dicts_unsupported` / `options_unsupported` 三条 warning + UI 文案把边界说清楚，避免把「填了」误读成「生效了」。
2. **插件白名单只按 hash 精确匹配**。`policy` 的引用校验允许 hash 或插件 name；模板阶段里存的是 hash，所以策略里写 name 不会命中，结果是 `plugins_not_applied`（清单不动）。后续可在 `taskpolicy` 里加 name→hash 解析（需要读 `plugins` 集合）。
3. **「白名单只约束策略点名过的阶段」是语法糖也是边界**：策略只列 DirScan 插件时，PortScan 阶段不会被清空。反过来，如果本意是「只跑这几个插件、其余全停」，当前语义做不到——需要新增一个显式开关字段（属产品决策，未臆造）。
4. **策略停用/删除**：任务照常创建，策略静默回退为模板默认值，只有服务端日志（`task policy <id> ignored: ...`）。没有面向用户的历史任务失效提示。
5. **`GetScanTemplate` 行为变化**：模板 ID 不存在时，旧代码 panic（500），现在返回 error；`CreateTaskScan` 只打日志，任务停在待开始，由 `SweepStaleTasks` 兜底标 failed。这是从「崩溃」到「明确失败」的改善，但确实与旧行为不同。
6. **定时任务依赖文档形状**：`policyId` 能跨定时轮次保留，是因为 `schedulerRepo.Insert` 直接 `InsertOne(models.Task)`、`jobs/scan.go` 又反解成 `models.Task`。若将来改成手工构造 `bson.M`，必须同步带上 `policyId`。
7. **引用计数仍不可用**：`policy/detail` 的 `usage.tracked=false` 保持不变；现在任务里真有 `policyId` 了，但历史任务没有，做反查会得到「部分计数」比「诚实说明不可用」更容易误导，故本次不实现。

---

## 7. 复现 / 手工验证

1. 建策略（选一张模板，插件填 2 个 hash），启用。
2. 建任务：模板留空 + 选该策略 → 提交。
3. 服务端日志应出现：`task policy <policyId> applied on task <name>: [templateId]` 与 `task policy <policyId> applied on template <templateId>: [plugins]`。
4. 查任务文档确认 `policyId` 已落库、`template` 已填策略模板：
   `db.task.findOne({name:"<任务名>"},{policyId:1,template:1,name:1})`
5. 查节点载荷确认插件清单被收窄（节点在线时）：
   `redis-cli LRANGE NodeTask:<node> -1` → 看 `DirScan` / `VulnerabilityScan` 数组与 `Parameters` 同名键。
6. 反向验证：同一表单不选策略，重复 4/5 —— `policyId` 字段不存在，载荷与改动前完全一致。

---

## 8. 与 EMPTY_COLLECTIONS 的对应关系

`scan_policy` 从「A（生产者存在，无消费方）」升级为「A（生产者存在，消费方已接线）」：

- 写入方不变：`repositories/policy.Create` + `/api/policy/create` + MCP `create_scan_policy`；
- **新增读取方**：`services/task/taskpolicy.Service.Config`（`policy.Service.Detail`）← `common.Insert` / `common.GetScanTemplate`；
- 空表原因从此只剩「还没人建策略」，不再是「建了也没人用」。
