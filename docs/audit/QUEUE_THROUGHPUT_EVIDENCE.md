# 队列吞吐决策的量化依据

> 用途：给「DirScan 做成可选档位 / `maxTaskNum` 1→2」这个决定提供数据。
> 采集方式：只读调用生产 `/api/task/progress/info`（逐目标逐模块的开始/结束时间）与 `/api/node`。
> 采集时间：2026-11-29。**本文件不含任何建议性改动，决定权在 YG。**

## 1. 时间都花在哪

最近 12 个已完成任务、合计 656.7 分钟「目标级跨度」的模块分布：

| 模块 | 累计分钟 | 占目标级跨度 | 覆盖目标数 |
| --- | ---: | ---: | ---: |
| VulnerabilityScan（nuclei） | 639 | **97.2%** | 19 |
| URLSecurity | 144 | 21.9% | 15 |
| URLScan | 142 | 21.6% | 19 |
| AssetMapping | 45 | 6.8% | 16 |
| AssetHandle | 43 | 6.5% | 17 |
| PortFingerprint | 35 | 5.3% | 18 |
| PortScan | 31 | 4.7% | 18 |
| SubdomainScan | 27 | 4.1% | 16 |
| SubdomainSecurity | 26 | 4.0% | 16 |
| PortScanPreparation | 1 | 0.1% | 2 |

百分比之和 >100% 是因为同一目标的多个模块在时间上重叠（例如 URLScan 与 URLSecurity）。
单任务看更直白：几个典型任务的 top-4 全是 `VulnerabilityScan` 打头，且占该任务跨度的 80~97%：

```
span=215.2  VulnerabilityScan 208.7 / URLSecurity 89.1 / URLScan 88.5
span=138.6  VulnerabilityScan 135.3 / URLSecurity 4.5  / URLScan 4.2
span= 98.0  VulnerabilityScan  97.4 / URLSecurity 10.6 / URLScan 10.3
```

## 2. 两个候选旋钮各自能省什么

最近 **30 个已完成任务**里各模块的出现率（出现 = 至少一个目标真的跑了该模块）：

| 模块 | 出现率 |
| --- | --- |
| URLScan | 30/30 |
| VulnerabilityScan | **28/30** |
| DirScan | **0/30** |
| WebCrawler | 0/30 |

结论很直接：**当前 workload 下把 DirScan 做成可选档位，省不到任何时间**——它根本没被跑到。
（这不代表这个档位没用：将来针对目录爆破类目标跑 DirScan 时，它会是主要耗时项。
它只是解决不了「队列排 5 个」这件事。）

真正决定队列长度的是 `VulnerabilityScan` 的时长 × 串行度。

## 3. 并发余量

生产节点当前状态：

| 指标 | 值 |
| --- | --- |
| `maxTaskNum` | 1（同时只跑一个任务） |
| 队列长度 | 5 |
| CPU 核数 | 24 |
| 容器内存占用 | 16.4% |
| 单任务中位耗时 | ≈10 小时（最近 20 个已完成任务的 creatTime→endTime） |

按现在的串行配置，5 个排队任务 ≈ **2.1 天**（节点页 ETA 实测 3013 分钟）。
改 `maxTaskNum=2` 时，理想情况下队列等待减半（≈1 天出头），代价是同一节点上两个 nuclei 共享 CPU 与出口带宽，
单任务耗时会被拉长——拉长多少取决于目标是 IO 受限还是 CPU 受限，这一点没有历史样本可算（现在从来只有 1 个并发）。

## 4. 采集脚本

数据来自两块，都可重复采集：

- 逐模块时间：`POST /api/task/progress/info {id, pageIndex, pageSize}` → `list[].<Module> = [开始, 结束]`；
- 节点容量与队列：`GET /api/node` → `maxTaskNum / running / queueLen / containerMem / cpuNum / queueEtaMin`。

复采方式：对最近 N 个 `status=3` 的任务逐个取 progress/info，按模块累加 `(end - start)`。
注意完成任务的进度明细来自 `TaskProgressSnapshot`（完成瞬间从 Redis 快照落库），
所以历史任务是可回溯的，不依赖节点在线。
