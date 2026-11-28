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
- 任务层：改配置之后**新领取**的任务日志里不应再出现 `VulnerabilityScan module start scanning the target`
  （已在跑的任务用的是领取时解析的模板，仍会跑完 nuclei）。
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
