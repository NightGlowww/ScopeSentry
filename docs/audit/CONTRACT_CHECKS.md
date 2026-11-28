# 契约核对（verify_contract.sh）

冒烟脚本 `scripts/ci/smoke.sh` 只回答「服务活着吗」（healthz / 版本 / Mongo / 节点数）。
它管不到「行为有没有悄悄退化」——例如分页上限被绕过、删除接口又开始假成功、
静态资源的 404 又被缓存一年。这些都是在 2026-11-27~28 的真实事故里逐条踩出来的，
所以固化成一份可重复执行的核对表。

## 用法

```bash
SS_BASE=http://<host>:8082 SS_TOKEN=<JWT> [MCP_KEY 或 SS_MCP_KEY=<ssk_...>] bash scripts/ci/verify_contract.sh
```

`SS_TOKEN` 用管理员登录后的 `access_token`（`POST /api/user/login` 的 `data.access_token`）。
脚本只读 + 只用不存在的 id 触发写接口的拒绝分支，不会改动任何真实数据。

## 核对项

| # | 不变量 | 为什么 |
|---|--------|--------|
| 1 | `/assets/<入口 chunk>` 200 时 `Cache-Control` 含 `immutable` | 带 hash 的资源该长缓存 |
| 2 | `/assets/<不存在>` 404 时 `Cache-Control: no-store` | 曾经 404 也被 immutable，浏览器缓存一年 → 页面永久白屏 |
| 3 | `POST /api/task/delete` 传不存在 id → 404 | 曾经回 200 且 `deleted=1` |
| 4 | `POST /api/node/restart` 传不存在节点 → 404 | 同上，且错误里应列出当前节点 |
| 5 | `POST /api/assets/asset` 传 `pageSize=100000` → 行数 ≤ 5000 且带 `X-Pagesize-Clamped` | 曾经单请求 45363 行 / 22.1MB / 15.9s |
| 6 | `GET /api/mcp/tools` 工具数 ≥ 60 且每条都有 scope | scope 漏登记会让受限 key 被默认拒绝 |
| 7 | `GET /api/export/records` 的 `list` 是数组 | 空集曾经返回 `null`，表格拿不到数组 |
| 8 | 持 `asset:read` 的 key 调 `tools/list`，可见工具数 == `/api/mcp/tools` 里登记为该 scope 的工具数 | 看不到权限外的工具，agent 才不会反复撞 403（脚本临时建 key，跑完立即撤；数字从接口算，不写死） |
| 9 | `count_assets` 的 total == 仪表盘 stats 的 asset | 同一个事实源被两处各算一遍，最容易一处悄悄归零 |
| 10 | `/api/node` 的节点数/在线数 == `/api/console/info` 的 nodeCount/nodeOnline | 2026-11-28 实测：SCAN node:* 带回字符串键导致 pipeline 失败，控制台一直显示 0/0，而节点在线 |
| 11 | `POST /api/icp/task/stop` 传不存在 id → 404 | 曾经回 200，还会给这个不存在的 id 往 `icp_task_log` 写一条「收到停止请求」——幻影日志（2026-11-28 实测） |
| 12 | `POST /api/icp/task/delete` 传不存在 id → 404 | 同一批接口，曾经回 200 + 全零；而幻影日志会让它报 `deletedLogs:1`，像是删掉了真东西 |
| 13 | `POST /api/github/task/delete` 传不存在 id → 404 | GitHub 采集面同款假成功 |
| 14 | 两次无效删除后 ICP 任务总数不变 | 兜住任何「拒绝分支顺手改了状态/写了日志」的回归 |
| 15 | MCP `get_asset_detail` 传未知 id → 工具级错误，且调用前后 `uptimeSeconds` 不减 | 2026-11-28 实测：asset 服务对仓库层的 `(nil,nil)` 没判空，MCP 工具跑在 jsonrpc2 自己的 goroutine（`gin.Recovery` 管不到），一次调用就让进程 SIGSEGV，容器重启 5 次 |
| 16 | `/api/policy/delete`、`/api/poc/detail` 传不存在 id → 404 | 前者 200 + `deleted:0`，后者 500 且把 `mongo: no documents in result` 透给前端 |
| 17 | 受限 key 的可见工具数 == `/api/mcp/tools` 里该 scope 的工具数 | 写死数字的断言每加一个工具就失效（加 `list_port_dict` 时踩到） |
| 18 | `GET /api/system/quality-report` 的 `data` 含 alerts/evidence/llm/quarantine/tasks/vulnerabilities 六块 | 少一块状态页就少一栏，且不报错——静默空白最难发现 |
| 19 | `GET /api/mcp/logs` 的 `data.list` 是数组 | 同一批接口里 `export/records` 曾经回 `null`，表格 `.map()` 直接崩 |
| 20 | asset / crawler / url / sensitive / dirscan 五个集合的列表 `data.total` 都存在，且 == `/api/assets/common/total` 同筛选下的 `total` | 列表原本只有部分集合回 total，前端另打一次总数接口；那次调用只在「搜索/筛选变化」时触发，时间筛选变了 total 停在旧值（2026-11-28 实测 45363 → 切「近 7 天」仍显示 45363） |
| 21 | 备份恢复三重把门：缺 `confirm` / `confirm` 大小写不符 → 400；`confirm=RESTORE` 但归档不存在 → 404；名字含 `../` → 400；且无效请求后备份份数不变；`download` 404 的 `data` 不含服务器绝对路径 | 恢复会覆盖线上库，是唯一「点错就回不去」的接口。而 `download` 曾经把 `stat /data/scopesentry-backups/...` 原样透给客户端，等于顺手暴露部署目录（2026-11-28 实测） |
| 22 | `POST /api/export/preview`（type=domain）的 `collection` 是 `subdomain`、字段非空、总数 > 0；`/api/export/batch` 传非法字段或非法 type → 400 且把可用字段/类型列出来 | 导出链路此前从未被验证过（records 一直是空的）。批量导出是先 `writeDownloadHeaders` 再流式写，入参校验一旦晚一步，用户就只能拿到半截 CSV 而状态码已改不了（2026-11-28 实测：214765 行 / 16.2MB，表头带 BOM，`Content-Disposition` 正常） |
| 23 | （需 `SS_EXPORT_ROUNDTRIP=1`）导出记录链路：创建 → 列表可见 → 下载是有效 xlsx（zip + worksheet）→ 删除 → 清单归零 | 这条链路和流式 batch 是两套代码，`records` 一直是空的意味着从没跑过。默认跳过是因为它会真的落一个文件 |
| 24 | MCP 治理三件套：① 建 key 传未知字段（如 `allowed_tools`）→ 400；② 白名单 key 调未授权工具 → `tool not allowed`，授权工具正常；③ `rateLimitPerMin=1` 的 key 第二次调用 → 429（-32029） | 这三条都是「看起来配了、实际没生效」的高危面。① 尤其危险：Go 默认忽略未知字段，写错字段名会拿到 scope=["*"] 的全权 key（2026-11-28 实测）；脚本自建 key 并在 finally 撤销 |
| 25 | 项目名称为空（`{}` / `""` / 纯空白）→ 400，且项目列表里不存在空标题卡片 | `AddProject` 直接绑定 `models.Project` 没有必填校验，`POST {}` 会真的建出一张没有标题的卡片；重名原本也只回 500「api.error」。注意自动化里**只测空名字**，重名用例会真的建项目 |
| 26 | 端口字典 `add` 传空 `name` 或空 `value`（含纯空白）→ 400，且字典行数不变 | `PortService.Add` 只校验了 `value`，空 `name` 照样插入 —— 端口字典会通过 `{dict.port.*}` 进入扫描参数，无名行会一路带到节点（2026-11-28 扫描「绑持久化模型无 binding 校验」的接口时发现） |
| 27 | （需 `MCP_KEY`/`SS_MCP_KEY`）MCP `get_task` / `get_project` / `get_poc` 返回的 `name` 必须与 REST 列表/详情一致 | 只断言 `isError=false` 是不够的：工具可以「成功返回一份旧数据」。同源比对才证明读模型没自带一份缓存（2026-11-28 实测三者一致） |
| 31 | 批量删除的筛选条件必须与界面所见一致（`buildDeleteFilter` 含 node；无条件 → 400） | 少一个条件就是「比你以为的多删」：节点条件曾在 api 函数签名处被截断（2026-11-28 修） |
| 30 | 大字段瘦身默认 dry-run，写入需 `confirm=SLIM` 且集合白名单（`POST /api/system/storage/slim`） | 不可逆操作不能靠调用方自觉：默认只统计，真跑要双条件，集合写错直接 400（2026-11-28） |
| 29 | 每个 MCP 工具都登记了 scope（`mcp_scope_drift.py`，静态） | 未登记的工具运行期默认拒绝，构建和 tools/list 都发现不了，只有受限 key 实际调用才 403（2026-11-28 新增 4 个工具时同批核对） |
| 28 | 变更操作有反馈、失败路径收 loading（`feedback_guard.py`，静态） | 「点了没反应」和「按钮永久转圈」都不会报错，只会在用户手上变成不可用的界面（2026-11-28 实测：页面监控保存无提示、新增 URL 失败后保存键死锁） |

## MCP 工具矩阵（mcp_sweep.py）

契约脚本管的是 REST 侧的横切口；MCP 有 78 个工具，逐个手工验不现实，所以单独一个脚本：

```bash
SS_BASE=http://<host>:8082 SS_TOKEN=<JWT> [MCP_KEY 或 SS_MCP_KEY=<ssk_...>] bash scripts/ci/verify_contract.sh
  python3 scripts/ci/mcp_sweep.py
```

它做四件事（当前 43 条读 + 45 条守卫，78 个工具全覆盖；另含 4 条口径断言：list↔count 一致、过滤字段真的生效、产出口径 has_asset < 全量、pageSize 夹紧）：

1. 用真实参数调**读类**工具，断言 `isError=false`（顺带看响应大小）；
2. 用不存在的 id / 非法参数调**写类**工具，断言 `isError=true` —— 这条才是重点：
   MCP 侧的「假成功」曾经让删除/停止类工具对任意 id 回 `ok:true`；
3. 用 `pageSize=100000` 调一次列表，断言行数被夹到 ≤500；
4. 口径一致：`list_assets.total` == `count_assets.total`。

**9 个 `create_*` 怎么覆盖而不建出东西**：传一个 schema 里不存在的字段（`{"__invalid__": true}`），
断言拿到的是 JSON-RPC 的 `invalid params: unexpected additional properties`。
校验发生在 SDK 的入参绑定阶段，handler 根本没执行 —— 所以既能证明「写类工具在落库前先过 schema」，
又保证不会真的建出任务/项目/字典（2026-11-28 实测：调用前后 task 仍 235、asset group 仍 0）。

覆盖度可以用 `python3 scripts/ci/mcp_sweep.py --coverage` 自查：它把 `/api/mcp/tools` 的工具清单
和脚本里的 READ/GUARD 名字做差集，新增工具没跟进会直接列出来。

结论看最后一行 `MCP_SWEEP=PASS|FAIL`。


另外加了两条**口径一致**断言（`list_assets` 的 `total` == `count_assets` 的 `total`，asset 与 subdomain 各一条）：
同一个事实在两处各算一遍，是最容易一处悄悄漂移的地方——REST 侧就真的把 `total` 丢了（见不变量 #20）。

脚本自带三次重试：本地代理偶发把连接掐断时，空响应会被重试掉，
不会被误判成「工具坏了」（2026-11-28 实测，不加重试会连报 57 条假失败）。

## 何时跑

- 动过分页、删除类接口、静态缓存、MCP 治理之后；
- 发版后（脚本对线上实例只读，可直接打生产）；
- 结论看最后一行 `CONTRACT_RESULT=PASS|FAIL`。

新增不变量时：先想清楚「它失败时用户会看到什么」，再按同样的 `ok/bad` 风格追加一节。
新增 MCP 工具时：同时在 `scripts/ci/mcp_sweep.py` 的 READ/GUARD 表里加一行，否则它不会被任何回归覆盖。

## 界面文案不许硬编码中文（i18n_guard.py）

英文界面走查只能覆盖「渲染出来看得见」的部分。真正的漏网在其它地方：
`aria-label`、`ElTooltip content`、`ElMessageBox` 的确认文案、删除确认、
单位后缀（`N 条` / `N 台已暂停` / `核`）、标签与值之间的全角冒号——
只有摸到那条路径才会暴露，而且一暴露就是「英文界面里蹦出中文」。

所以把这套判据做成静态检查（2026-11-28 一轮扫出 24 个文件、60+ 处）：

```bash
python3 scripts/ci/i18n_guard.py            # 扫描 frontend/src 的路由页面与公共组件
python3 scripts/ci/i18n_guard.py --demo     # 连未挂路由的 views/Components 一起扫
```

判据：

1. `<template>` 块内的裸文本、以及 `aria-label`/`title`/`placeholder`/`content` 属性值出现 CJK；
2. `<script>` 里引号内出现 CJK（先按引号状态切掉行尾 `//` 注释，`console.*` 整行跳过）；
3. `<!-- -->` 注释（含跨行）先整段抹成空格，行号保持不变——注释里的中文不算文案。

例外只有两条，写在脚本 `ALLOW` 里并注明理由：
`Task.vue` 用 `排队超过` 回落匹配老任务的 `failReason`（没有 `failCode` 的历史数据）、
`plugin.vue` 的 `tags.includes('内置')` 比对的是后端 tag 值本身。

同一轮还改掉两处真问题，不是文案问题：

- APP / 小程序搜索帮助里，关键字写的是 `name`，示例却给 `domain="百度"`——照示例输入查不到数据；已改成 `name="百度"` 并随语言切换（`Baidu` / `바이두`）。
- 标签与值的分隔符原来是写死的全角冒号，英文界面也照输出；改为 `common.labelSep`（中文 `：` / 英文与韩文 `: `）。

结论看最后一行 `I18N_GUARD=PASS|FAIL`。

## 后端 data:null 与前端 res.data.* 的契约（frontend_payload_contract.py）

这类 bug 不报编译错、不报接口错：后端 `response.Success(c, nil, ...)`，前端却写 `res.data.message`，
运行时 `Cannot read properties of null` —— 用户看到的是「点了没反应」。2026-11-28 修改密码就是这么坏的
（改成功了、弹窗关了、一句提示都没有）。

```bash
python3 scripts/ci/frontend_payload_contract.py
```

判定链（纯静态，不需要起服务）：

1. 扫 Go handler，找出**函数体内**（不跨 `func` 边界）真的调用 `response.Success(c, nil, ...)` 的函数，按「目录 + 函数名」记；
2. 解析路由文件，用 `group Path + 子 Path` 拼完整 URL，handler 别名经 import 反查成目录，避免 `backup.Status` 与 `plugin.Status` 撞名；
3. 扫 `frontend/src/api/*.ts`，把 `export const fn = ... url: '/api/...'` 映射成 `fn → url`，用**完整 URL 等值**匹配（子 Path 形如 `status`/`run` 会让后缀匹配误命中）；
4. 扫前端调用点：`= await fn(...)` 之后 5 行内出现 `<var>.data.<字段>` 即违规，注释行先剥掉。

当前结果：**51 个 data:null 接口 / 20 个前端 api 函数 / 0 处违规 → `PAYLOAD_CONTRACT=PASS`**。
脚本做过正向对照：临时放一个 `res.data.message` 的探针文件，能精确报到 `user/ChangePassword`，删掉后回到 PASS。

新增写接口时：如果它只回 `nil`，就别让前端去读 `data`；直接读顶层 `message` 或用本地文案。

## 表格列字段 vs 接口返回（column_field_check.py）

同一个 bug 出现了三次，形态完全一样：**列定义里的 field 名，接口根本不回**。

- 计划任务「状态」列读 `row.state`，接口回的是 `scheduledTasks`（模型的 bson 名）→ 整列空白；
- npoc「插入时间」列读 `row.insertTime`，接口回 `insert_time` → 整列空白；
- 任务「策略提示」列读 `policyWarnings`，后端写了但列表投影漏了 → 整列永远是「-」。

```bash
SS_BASE=http://<host>:8082 SS_TOKEN=<JWT> python3 scripts/ci/column_field_check.py
```

做法：把每个表格页的列 `field:` 名，和它 `fetchDataApi` 调的那个接口**实际返回的键**比对（取 5 行键的并集，
避免 `omitempty` 字段只在有值时才出现造成误判）。输出分两档：

- `缺失`（该列没有 formatter）→ 直接判 FAIL；
- `待人工`（有 formatter，自己算值）→ 只提示，因为 formatter 允许不依赖该字段；上一轮那个 `state` 列就落在这档。

当前结果：**25 个表格页通过、0 缺失、4 条待人工**（GitHub 调度间隔读 `row.config.intervalMinutes` 属嵌套、
`ScheduledTask.state` 是修复时保留的兼容读法、`policyWarnings` 待有数据的任务出现、`insertTime` 已修）。

已知限制：一个文件挂多张表时列 schema 与接口无法可靠配对（子表多走详情接口），脚本**明确跳过并计数**（当前 61 个），
不硬配、不产噪声。要覆盖这部分得按 `<Table>` ↔ `useTable` 逐表建映射，留作后续。

## 变更操作必须给反馈、失败必须收 loading（feedback_guard.py）

这一栏来自一次实测：计划任务的页面监控配置点「保存」后，弹窗不关、列表数字不变、没有任何提示，
用户无法判断到底存没存；而同一个文件里的「新增监控 URL」更糟 —— 成功后不刷新列表，接口一失败
`addPageMonitSaveLoading` 永远停在 `true`，保存按钮变成一个按不动的死按钮。

```bash
python3 scripts/ci/feedback_guard.py          # 本地静态扫
python3 scripts/ci/feedback_guard.py --strict # SILENT 也算失败
```

两级判定：

- **STUCK（必须为 0）**：函数把 `xxxLoading.value = true` 打开，之后 `await` 一个调用，再把 false 写在成功路径上。
  没有 `finally`、也没有失败分支里的重置 → 判卡死。纯计时/纯本地（set 和 reset 之间没有 `await`）不算。
- **SILENT（WARN）**：变更型接口（delete/update/add/save/toggle/restore/run/sync…）调用后既没 `ElMessage`，
  也没刷新/关闭弹窗。读接口（`get*/list*/fetch*/query*/load*`）一律不算，避免把查询当写入。

当前结果：**STUCK 0、SILENT 3**（ThreatTab 增删目标用接口返回值直接替换列表、AddTask 策略应用是读操作），
三项都在白名单合理范围内，`FEEDBACK_GUARD=PASS`。

本轮修掉的：PageMonit 新增 URL、计划任务页面监控配置保存、通知开关保存、新建/编辑计划任务、
系统更新提交、资产/敏感信息/端口字典编辑弹窗、项目信息与统计加载、项目搜索、项目卡片删除后刷新。

## MCP 工具 ↔ scope 漂移（mcp_scope_drift.py）

未登记 scope 的工具在运行期会被「默认拒绝」——只有持 `*` 的 key 能调，普通受限 key 一律 403，
而这种缺口单测、`go build`、tools/list 都看不出来，只有真机调一次才暴露。所以做成静态检查：

```bash
python3 scripts/ci/mcp_scope_drift.py
```

核对 `internal/mcp/tools.go` 里每个 `addTool(... Name: "x")` 与 `mcpguard.ToolScopes` 是否一一对应，
三个方向都算失败：重复注册、未登记、以及 scope 表里凭空多出没有工具认领的条目。
当前 **82 个工具 / 82 条 scope，MCP_SCOPE_DRIFT=PASS**（MCP_COVERAGE 同步 82/82：源码声明数 = 服务端 tools/list 数）。

顺带记录一个容易被忽略的联动：新增 scope 只改后端时，前端「API Key」页的 scope 清单是硬编码的，
这个 scope 在界面上根本选不到（只能建 `*` 全权 key 或用接口建）。本轮已把该清单改成
「硬编码打底 + `/api/mcp/tools` 返回的 scopes 自动补齐」，后端再加 scope 时界面不会掉队。

## 前端类型门禁（typecheck.sh）

```bash
bash scripts/ci/typecheck.sh      # TYPECHECK=PASS / FAIL
```

`build:pro` 走 vite，不做类型检查，所以 `pnpm ts:check` 长期是红的（143 条，从没人跑）。
2026-11-28 清零到 0 之后固化成门禁。**这不是洁癖** —— 那 143 条里埋着四个真问题，
其中最贵的一个是：`src/axios/index.ts` 只转发 6 个字段，`timeout` 被静默丢弃，
于是备份创建/恢复（分钟级）一直按 `REQUEST_TIMEOUT=60s` 超时失败。类型报错的原始信息是
「`'timeout' does not exist in type 'AxiosConfig'`」，看起来像「类型定义缺字段」，
实际是「这个参数从来没被转发过」。

## 本轮新增的两条不变量

- **单请求超时必须真的能生效**：`AxiosConfig.timeout` 声明 + `axios/index.ts` 显式转发。
  回归方式：任何分钟级接口（备份创建/恢复、导出）传 `timeout` 后，前端不应在 60s 报超时。
- **`change` 事件必须带值**：`TimeRangeFilter` 的 `change` 要把「本次选中的值」带出去，
  父组件靠它算 `timeFrom`（`v-model` 更新是异步的，父组件立刻读 `modelValue` 会拿到旧值）。
  `Asset/IP`、`Asset/RootDomain`、`Asset/Subfinder` 三处都依赖这个行为。

注：`build:test`（`ts:check && vite build --mode test`）的 ts:check 段已通过，但 vite 段在 Vite 5.0.6 的 `generateBundle` 里 `String.replace` 爆栈（`RangeError: Maximum call stack size exceeded`），与类型治理无关，另案处理；发布走 `build:pro`，不受影响。

## MCP 闭环矩阵（mcp_closure.py）

```bash
python3 scripts/ci/mcp_closure.py --write --strict   # 重新生成 docs/audit/MCP_CLOSURE.md
```

tools/list 里有、scope 表里有，都不等于能用。这条把 82 个工具的验证程度做成账本，三个维度：

- **读**：`mcp_sweep.py` 用真实 id 调通（48 个）；
- **拒绝分支**：喂不存在/非法参数必须 `isError=true`（46 个，防「假成功」）；
- **写往返**：合法参数下真的改到库、复核、再还原（5 个：`assign_tasks_project`、`set_scheduled_task_state`、
  `create_scan_policy`、`set_port_dict_entry`、`set_sensitive_rule_state`）。

其余工具不是坏的，是没有可安全验证的前置样本（比如 `create_scan_task` 会真的起扫描、
`pause_task_targets` 需要「运行中且目标未全部领取」的任务），逐个写在矩阵的「说明」列里。
`--strict` 在有工具既没验证也没说明时退出码 1，可以接 CI。

## 一键本地门禁（verify_all.sh）与 i18n key 闭环（i18n_keys.js）

```bash
bash scripts/ci/verify_all.sh        # 全部本地门禁，末行 VERIFY_ALL=PASS/FAIL
node scripts/ci/i18n_keys.js         # 单跑 i18n key 检查
```

`verify_all.sh` 跑 7 道本地门禁（typecheck / i18n_keys / i18n_guard / feedback / payload /
scope_drift / mcp_closure）+ `go build` + `go vet`；`column_field` 需要线上凭据，没给就 SKIP。
判定契约只有一条：每条门禁的输出里必须出现 `=PASS`，否则打印尾部并以 1 退出 ——
避免「脚本跑失败但汇总看着是绿的」。

`i18n_keys.js` 补的是 i18n 的反方向：`i18n_guard.py` 只查「.vue 里有没有硬编码中文」，
查不了「`t('x.y')` 写错一个字母」或「只在一个语言里加了 key」。做法是把两个 locale 文件
当数据求值（`export default` → `module.exports`）后 flatten 成点路径，再扫所有引用。

本轮结果：zh/en 各 **2201** 个 key 且**集合完全一致**；437 个文件引用 1623 个 key；
真实缺失 **0**（只有 `views/Components/` 下 26 个未挂路由的 demo 页、以及 `task.module.` 这个
`t('task.module.' + key)` 动态前缀）。

## API 集成测试（integration.sh）

```bash
bash scripts/ci/integration.sh     # 需要真实 Mongo + Redis，末行 INTEGRATION=PASS/FAIL
```

2026-11-28 之前这套测试**根本跑不起来**，两个原因都真实存在：

1. `//go:build integration` 的包编译不过：`assetgroup.Service.DeleteScopes` 早就改成返回 `(int64, error)`，
   而 `icp_routes_integration_test.go` 与 `services/icp/icp_integration_test.go` 还按单返回值调用
   → `go vet -tags integration ./...` 直接报错，等于整套集成测试（含 314 条路由的清单守卫）不存在。
2. 就算编译过，`SetupRouter()` 会触发 bootstrap 连接 Mongo/Redis，连不上就 `log.Fatalf`，
   所以「路由清单守卫不需要数据库」这句注释是过时的。

修完之后（本地起 mongo:7 + redis:7 容器实测）：

| 项 | 结果 |
| --- | --- |
| 路由总数 | **314**（其中 `/api` 下 313） |
| 集成用例 | **24 个全绿**（--- PASS 计数 24，整包 ok） |
| 修复的用例 | `TestIcpRoutesGuardsAndFlow`：建任务后后台 goroutine 立刻进 running，此时 `restart` 返回 400 `icp task is running` 是**正确行为**；用例改成先 stop 再 restart，消除时序假失败 |

覆盖到的面：登录与鉴权白名单、路由清单、用户管理（含最后一个超管保护）、策略 CRUD/apply/test、
MCP 治理端到端、导出、ICP 全流程、GitHub 流转、FOFA、资产组、读模型、项目聚合、字典索引与回填、
以及一批「新接口守卫」（未认证、坏 ObjectID、超长文案、空筛选的全量删除保护等）。

每次跑用独立数据库（`ss_it_<时间戳>`），不污染任何既有库。

## REST 接口引用索引（rest_closure.py）

    python3 scripts/ci/rest_closure.py --write     # 生成 docs/audit/REST_CLOSURE.md

MCP 有闭环账本，REST 这边一直没有「哪条接口被谁验过」的索引。这条按路由快照（docs/audit/routes.snapshot.tsv，
314 条，从 SS_ROUTE_DUMP=1 的清单守卫导出）比对测试/契约脚本里出现过的路径字面量，输出按模块的未点名数量，
以及写操作里完全没被点名的清单。

当前数字：314 条路由，被点名 157（50%）；读 73/115、写 47/113、无法判读 37/86；写操作未点名 66 条。

**它是索引，不是覆盖率裁决**：路径可能是拼出来的（只做字面量 + 前缀匹配），而且这个项目大量用 POST 查列表，
读写分类是名字启发式。真值只会比这个高。

这一轮靠它挑出 /api/assets/common/* 整族没人验，补了 TestCommonAssetOpsGuards（14 个子用例），并因此发现
一个真 bug：update_status 传不存在的 id 会回 **200 成功**（repo 的 UpdateOne 吞掉了 MatchedCount=0）。
修复是把「匹配 0 条」在 repo 层转成 mongo.ErrNoDocuments，三个通用写操作统一走 errors.Is 映射到 404，
顺带替掉了原来靠 strings.Contains(err, "no documents in result") 判存在性的写法。

## 「假成功」这一类：仓库层不许吞计数

现象统一是：**mutation 对不存在的目标回 200，界面说成功，库里一条没动。** 根因也统一：
mongo 驱动的 `UpdateOne`/`UpdateMany`/`DeleteMany` 对「没有文档命中」不报错，
仓库层写成 `_, err := coll.UpdateOne(...)` 就把 `MatchedCount`/`DeletedCount` 丢了，
service 拿到 `nil` 一路回成功。

2026-11-28 这一轮共修掉 7 处（全部线上实测过）：

| 接口 | 之前 | 现在 |
| --- | --- | --- |
| `POST /api/assets/common/update_status` | 200 | 404 |
| `POST /api/task/template/delete` | 200 | 404 |
| `POST /api/assets/fileleak/delete` | 200 `{"deleted":0}` | 404 |
| `POST /api/assets/npoc_service/delete` | 200 `{"deleted":0}` | 404 |
| `POST /api/assets/nuclei_result/status` | 200 | 404 |
| `POST /api/project/update` | 200（还会继续对着不存在的项目起 goroutine） | 404 |
| `POST /api/sensitive/update/state` | 200 | 404 |

另有一处不是假成功而是崩溃：`POST /api/task/scheduled/delete` 在调度器未初始化时
`GetGlobalScheduler()` 返回 nil，`RemoveJob` 里对 nil 取锁 panic（Gin Recovery 兜住变 500）。
改成惰性初始化后，10 处同样的解引用风险一起消失。

**固定下来的规则**（写在这里，改代码时对着看）：

1. 仓库层的 `Update*`/`Delete*` 不许写 `_, err :=`；要么返回计数，要么匹配 0 时返回 `mongo.ErrNoDocuments`。
2. 判「存在性」用 `MatchedCount`，不要用 `ModifiedCount` —— 值本来就是这样时后者是 0，会被误判成「id 不存在」。
3. service 负责把驱动错误翻译成业务哨兵（如 `ErrNotFound`），handler 只做 `errors.Is` 映射到 4xx；
   不要用 `strings.Contains(err.Error(), "no documents in result")`，驱动换文案就失效。
4. 集成用例对「不存在的目标」既断言不回 200，也断言不回 500 —— 500 会漏掉 panic（Recovery 兜住后就是 500）。

仓库里还有 **62 处** `_, err :=` 形式的调用（`grep -rn '_, err := .*\.(UpdateOne|UpdateMany|DeleteOne|DeleteMany)(' internal/repositories/`），
不是每一处都需要计数（例如后台巡检的幂等写入、统计字段刷新），但每新增一个用户可点的写接口，
就该按上面 4 条过一遍，并补一条集成用例。

## MCP 工具「静默消失」的一个固定原因：jsonschema tag 里的等号

`addTool(... &mcp.Tool{...}, handler)` 的输入结构体用 `jsonschema:"..."` 描述参数。解析器把
`WORD=VALUE` 当成「默认值标记」，遇到就 panic——而 `addTool` 外层有 recover，**工具不会报错，只是从
`tools/list` 里消失**。

2026-11-29 实测：给 `create_scan_task` 加 `scanProfile` 时写了「空=按模板；recon=只做资产测绘」，
部署后 `tools/list` 从 82 掉到 81、`create_scan_task` 不见了；把等号去掉立刻恢复。

排查口径：`SS_BASE=... SS_MCP_KEY=... python3 scripts/ci/mcp_sweep.py --coverage` 会对比
「源码声明的工具数」与「服务端实际返回的工具数」，不一致就是这类问题；`mcp_closure.py` 另外做静态扫描，
对含等号的 jsonschema 片段给出告警。

## 2026-11-29 扫描策略三条不变量

这三条都是「看起来在跑，其实没在按预期跑」的类型，各自钉一条断言：

1. **nuclei 必须显式选择才跑**：`VulnerabilityScan` 已从模板的硬性核心模块里移除；模板没勾 nuclei 时
   下发副本里既没有插件也没有参数（`TestGetScanTemplateAppliesScanProfile` 覆盖）。
2. **任务档位必须真的改下发内容**：`scanProfile=recon` 时下发副本摘掉 `VulnerabilityScan` 与 `DirScan`，
   其余模块原样保留；同时任务 `policyWarnings` 里要留下「跳过了什么」的可见提示
   （`TestTaskScanProfileReconPersisted` + `scanprofile` 单测）。
3. **待开始任务也要有排队位次**：`TaskList` 对 status 0/1 都回 `queued`/`queuePos`，
   前端据此显示「排队中 / 前面还有 N 个」（`TestTaskListQueueInfoForPendingTask`）。
   线上实测：5 条任务排队 30 小时，改之前界面只写「待开始 0%」。

另外补了第四条巡检 `stalled_running`（节点活着但任务长时间没有进度 → 判失败 + 告警），
验证方式见 `docs/audit/SCAN_STRATEGY.md`；不自动重启节点。
