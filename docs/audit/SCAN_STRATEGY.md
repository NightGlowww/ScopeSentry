# 扫描策略：nuclei 改为按需开启（2026-11-29）

## 为什么改

节点是 `maxTaskNum=1` 串行跑任务的（node-jp-01，4 核）。原先 `VulnerabilityScan` 在
`coreModules` 里——模板不选 nuclei 插件就**不允许保存**，于是每个模板都挂着 nuclei，
每条任务都要跑一遍模板集。结果是资产测绘任务从分钟级变成小时级，队列永远排不完。

实测（node-jp-01 日志尾部 128KB，按目标统计各模块 `module end` 耗时）：

| 模块 | 次数 | 平均/目标 | 合计 | 占总耗时 |
| --- | --- | --- | --- | --- |
| VulnerabilityScan (nuclei) | 10 | 1134s（≈19 分钟） | 11340s | **38.6%** |
| DirScan | 10 | 1088s（≈18 分钟） | 10882s | **37.1%** |
| URLSecurity | 13 | 68s | 882s | 3.0% |
| WebCrawler | 13 | 60s | 784s | 2.7% |
| AssetMapping | 13 | 57s | 745s | 2.5% |
| URLScan | 13 | 57s | 740s | 2.5% |
| 其余（端口/子域/指纹…） | 13 | ≤55s | ≈3900s | 13.2% |

nuclei + 目录扫描合计接近 **76%**。资产测绘本身（子域 → 端口 → 指纹 → URL → 爬虫）不到 15%。

## 改了什么

1. `coreModules` 去掉 `VulnerabilityScan`；新增 `advisoryModules`。留空可以保存，
   但会在服务端日志留一行「未选择 VulnerabilityScan 插件：本次只做资产测绘，不跑 nuclei」，
   避免以后又靠猜「为什么一个漏洞都没有」。
2. `ScanTemplateDefault.Parameters.VulnerabilityScan` 清空（原来预置了 `-s high,critical`，
   等于新装的默认模板也会跑 nuclei）。
3. 模板编辑页：VulnerabilityScan 未勾选插件时顶部给提示（中英双语）。
4. 线上 7 个模板（default / artex-recon-light/deep/full / jp-*）的 `VulnerabilityScan` 插件与参数已清空。

## 怎么重新开启

nuclei 没有被删、也没有被卸载，只是不再默认挂进模板：

- **按模板开启**：模板编辑页 → `VulnerabilityScan` → 勾选 nuclei 插件（hash `ed93b8af…`）→ 保存。
  该模板下的任务恢复漏洞扫描。
- **按策略开启**：扫描策略里把 `VulnerabilityScan` 写上 nuclei 插件，策略套到任务时会覆盖模板（显式配置才算）。
- **只想扫特定 POC**：在模板里选好 nuclei 插件后，用 POC 选择器限定模板列表（`VulList`），
  服务端会拼成 `-t a.yaml,b.yaml`。

## 还没动但同样贵的一项

`DirScan` 与 nuclei 一个量级（37%）。它产出的是目录/接口面（此前修过一次「122 个 URL 里 0 个 API 端点」
的覆盖缺口），所以这轮**没有**默认关掉。如果资产测绘优先到极致，可以按同一套机制把它也改成可选——
但会牺牲接口面覆盖，需要明确取舍后再动。

## 验证口径

- 模板层：`POST /api/task/template/detail` 里 `VulnerabilityScan=[]`、`Parameters.VulnerabilityScan={}`（7/7 已确认）。
- 任务层：改配置之后**新创建**的任务日志里不应再出现 `VulnerabilityScan module start scanning the target`
  （口径修正见下一节：队列条目是提交时的快照，提交得早的任务会照跑完 nuclei）。
- 队列层：`/api/node` 的 `queueLen` 应随任务变短而下降。

## 扫描档位（scanProfile，2026-11-29 追加）

nuclei 改成模板可选之后，还需要一个「这条任务只要快」的开关——否则要快就得手工改模板、跑完再改回来。
任务上新增 `scanProfile`：

| 取值 | 行为 |
| --- | --- |
| 空（默认） | 按模板完整扫描 |
| `recon` | 下发时把模板副本里的 `VulnerabilityScan` 与 `DirScan` 清空，其余模块原样保留 |

实现要点：

- `services/task/scanprofile`：`Normalize` / `Apply` / `Describe`，纯函数 + 4 个单测
  （非重模块必须保留、未知档位按完整处理、nil 模板安全）
- 只在**下发副本**上裁剪，不写库、不改模板；同一模板可以今天跑 recon、明天跑完整
- 入库时把「跳过 VulnerabilityScan、DirScan」写进任务 `policyWarnings`，任务列表名字后带「仅资产测绘（快）」
- MCP `create_scan_task` 增加 `scanProfile` 参数；建任务弹窗有对应下拉

典型用法：先用 recon 档位把 100 个目标的面铺开（每目标几分钟），再挑真正值得的下发完整档位。

## 关掉 nuclei 的真正机制：插件数量就是开关（2026-11-29 追加）

前面只验证了「模板里的插件清单空了」，但**空清单到底意味着跳过还是按默认跑全量**，当时是猜的。
翻节点源码（`Autumn-27/ScopeSentry-Scan`）确认了实现：

- `modules/vulnerabilityscan/module.go`：`ProgressStart(r.GetName(), target, id, len(r.Option.VulnerabilityScan))`
- `internal/handler/task.go`：`ProgressStart(..., flag)` 第一件事是 `if flag == 0 { return }` —— 不记进度、不跑模块

所以 **flag = 插件个数，0 即整段跳过**。空清单 = 模块不执行，模板层关掉是成立的
（这条结论比「看起来没跑」重要：反过来说，只要清单里还留着一个插件，模块就会跑满，
不存在「留个占位但参数为空所以不跑」这种中间态）。

## 队列条目是快照，不是引用（2026-11-29 追加，解释「改了模板却没生效」）

`CreateTaskScan` 建任务时就把**解析后的模板 JSON** 推进了节点的待领取队列：

```
template := GetScanTemplate(...)      // 策略 → 档位 → 参数解析，全在这里定稿
taskRepo.RPushNodeTask(ctx, node, template)   // 序列化后进 NodeTask:<node>
```

节点 `LRange` 取到的是一份**写死的载荷**。模板、策略、扫描档位后来怎么改，都改不动已经在队列里的条目。

现场证据（部署了「nuclei 默认关闭」之后仍在跑 nuclei）：

| 观测 | 值 |
| --- | --- |
| 模板 `6afaa875…` 的 `VulnerabilityScan` | `[]`（确实是空的） |
| 节点收到的载荷 `VulnerabilityScan` | `["ed93b8af…"]`（还带着 nuclei 插件） |
| 任务名 | `huntdeck-intigriti-superdrug-20261126-214221` —— **11-26 提交的** |
| 结果 | 01:01:29 起跑 `VulnerabilityScan`，按老模板跑满约 20 分钟/目标 |

结论：不是开关没生效，是 backlog 里的旧快照。要让它们立刻变快只有取消后重新提交
（重新提交会重新解析模板，拿到空清单 → 模块整段跳过）。

界面上补了这条信息：任务列表的排队行会标「队列快照含 Nuclei」（`queuedNuclei`），
判定直接解析 `NodeTask:<node>` 里的载荷本身（`repositories/task.TaskQueueSnapshot`），
不看模板现值——否则永远解释不了「为什么这条还在跑 nuclei」。

## 队列可见性（2026-11-29 追加）

以前任务列表只在 `status=1` 时计算 `queuePos/queued`，**待开始（status=0）的任务永远拿不到排队位次**。
线上实测：5 条任务在节点队列里排了 30 小时，界面一直显示「待开始 0%」，分不清是排队还是卡死。

现在 status 0/1 都会回 `queued` 与 `queuePos`（「正在做什么」仍只给运行中），前端 progress 列显示
「排队中 / 前面还有 N 个」。部署后线上 5 条待开始任务的位次依次为 1、2、2、3、5 —— 说明它们在队列里，
只是排在唯一那个跑不完的任务后面。

## 仍然慢在哪：单任务可以卡住（观测记录）

节点日志显示 2026-11-29 00:26 起某目标进入 VulnerabilityScan 后，直到 00:51 没有任何新日志 —— nuclei
在这个目标上没有时间上限，任务就停在 92%。后台巡检最终会按「超过 1h30m 无进展」把它判失败并重投，
但那是 90 分钟之后的事了。

两个可选的收紧手段（都需要定调后再做）：

1. 给 nuclei 参数加时间上限（节点侧 wrapper 支持哪些参数要先确认，当前 help 里没有 `et`）。
2. 既然 nuclei 默认已关，直接把「需要漏洞面」的目标挑出来单独跑，避免它挂在主流程里。

## 第四条巡检：节点活着但模块卡住（2026-11-29 追加）

`watchdog` 原有三条判定都漏掉同一类现场：**节点心跳正常、`currentTask` 指着它、但长时间没有进度写入**。

- `tasks_stalled`：要求「没有任何节点在跑」
- `phantom_running`：要求「既非 currentTask 也不在队列」
- `node_zombie`：要求心跳停更

实测的那条任务从 11-24 跑到 11-29，停在 85%、`currentModule=VulnerabilityScan`、最后进度写入在当天 00:26；
节点一直活着，于是队列被它堵了 5 天。

新增 `stalled_running`：对 `node:*:currentTask` 指向的任务查最近一次进度写入，超过
`WATCHDOG_STALLED_AFTER_MIN`（默认 60 分钟）即判失败：`failCode=stalledNoProgress`，
`failReason` 写明节点名、已卡时长与「重启该节点释放队列后 retest」。

**不自动重启节点**：节点还卡在模块里，重启是不可逆操作（in-flight 的产出会丢），
交给人或外部重启策略。要手动释放：

    curl -X POST $BASE/api/node/restart -H 'Content-Type: application/json' \
      -H "Authorization: Bearer $TOKEN" -d '{"name":"node-jp-01"}'

判失败之后不会再被「复活」：进度轮询只处理 status 0/1，失败任务不在其中。

验证方式（不依赖等 60 分钟）：集成测试 `TestWatchdogFailsStalledRunningTask` 造出完整现场
（status=1 任务 + `currentTask` + 24 小时前的进度时间戳 + 窗口设成 1 分钟），跑一轮 `watchdog.Check()`，
断言任务落库为 status=4 / `stalledNoProgress` / 原因里含节点名与重启建议。

## 节点可见性：把「队列为什么不动」变成可读答案（2026-11-29 追加）

上一轮只能回答「队列 6 个任务」，回答不了「为什么不动」。现在 `/api/node` 每个节点带：

| 字段 | 含义 | 数据来源 |
| --- | --- | --- |
| `currentTaskId` / `currentTaskName` | 此刻在跑哪条任务 | Redis `node:<name>:currentTask` + `task` 集合 |
| `currentModule` / `currentTarget` / `currentSince` | 跑到哪个模块、哪个目标、什么时候开始 | `TaskInfo:progress:<id>:<target>`（只认有 `_start` 无 `_end` 的模块） |
| `idleMinutes` | 最近一次进度写入距今多久 | 同上，取全部字段最大值 |
| `stalled` | 是否超过巡检阈值（与 watchdog 同一口径） | `watchdog.StalledAfterMinutes()` |
| `queueEtaMin` | 队列折算成还要等多久 | 队列长度 × 单任务耗时中位数 |

界面：任务数量列显示「1 / 任务名 / 目标 / 已 N 分钟无产出」，卡住时顶部红色横幅一键重启；
队列列显示「预计 3.2 小时」；Action 列固定右侧且低频动作收进「更多」下拉。

### 两个必须记住的坑

1. **均值估不准**：单任务耗时均值被「暂停过夜 / 发布自动重投」拉爆（均值 430 分钟、中位数 40 分钟），
   5 条任务曾被估成 36 小时；只用中位数又被「秒级空任务」拉到 1 分钟（5 条估成 5 分钟）。
   最终口径：丢掉 <3 分钟和 >4 小时的样本再取中位数（`services/node.estTaskMinutes`，3 个单测）。
   当前线上样本被秒级空任务占满，所以 `queueEtaMin=0`（未知）——**宁可不显示，也不显示假数字**。
2. **`scan` 不是模块**：节点在任务开始时写 `scan_start`（`runner.go`），把它当「当前模块」显示等于
   告诉运维「正在跑 scan」。解析时跳过它；没有模块在跑时仍回退显示最近活跃的目标。

### UI 回归（截图实测后修的）

- 加一列「当前任务」后总宽超视口，Action 整列被挤出可视区，重启按钮点不到；
- 把 Action 固定右侧后，6 个按钮放不下，后 3 个被裁成「…」——**正是重启**；
- 最终：当前任务并进任务数量列（列数回到 11）、Action 固定右侧、常驻 Log/Config/Restart、
  Plugin/Rebalance/Clear queue 收进「更多」、10 个列宽各收几像素到刚好不横向滚动。

