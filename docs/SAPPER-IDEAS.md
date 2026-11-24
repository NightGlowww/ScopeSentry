# 从 SapperV2 借到 ScopeSentry 的五个东西

> 读了 `_tools/sapper`（SapperV2）的实现和它的部署形态之后，挑出对 ScopeSentry 真正有用的部分。
> 不照搬：SapperV2 是「一个 agent 在一个项目里挖一个靶」的作战记忆；
> ScopeSentry 是「一台调度器替很多目标持续做资产测绘和扫洞」，两者的约束完全不同。

## 1. 事实图（typed fact graph）——最该借的一条

SapperV2 里所有东西都是**带类型的事实**：`host / service / cred / vuln / loot / flag`，
每条带 `data` 和时间戳，新事实可以推翻旧事实（`--supersedes`）。

ScopeSentry 现在是**扁平的集合**：`asset`、`subdomain`、`UrlScan`、`vulnerability` 各自一张表，
彼此只靠 `taskName`/`host` 字符串弱关联。后果：

- 看到一个 jenkins 服务，不知道它属于哪个根域、走过哪些扫描、上次结论是什么；
- 同一个 host 在不同任务里被重复扫，因为「已经知道的东西」没有沉淀成可查询的状态。

**可落地的最小版本**：新增 `fact` 集合（`{kind, key, data, task, firstSeen, lastSeen, supersedes}`），
把「这台 host 是 GeoServer 2.27.6」这类结论写进去。下一步的扫描决策读事实图，而不是只读任务配置。

## 2. intent + frontier —— 扫描应该「按图索骥」而不是「按流水线」

SapperV2 的核心循环：`frontier` 列出当前可做的工作单元（intent），`pick` 按价值挑一个，做完 `done`，
新的 intent 随事实变化自动出现。

ScopeSentry 的扫描链是**固定流水线**：subdomain → port → fingerprint → urlscan → crawler → dirscan → vuln。
所有目标走同一条路，不管它是什么。结果是：一个 GeoServer 和一个静态站花一样的预算。

**可落地的最小版本**：给每个模块加一个 `when` 条件（事实驱动），例如：
`technologies contains "geoserver" → enqueue geoserver-chain 模板`；
`waf_profile.wafActive && bypasses 非空 → 用可用载体重放被拦模板`。
这正好接上这次做的 waf 画像——画像就是事实，重放就是 intent。

## 3. policy gate —— 每个动作前过一道闸

SapperV2 的 `policy` 在动作前统一裁决（允许/需确认/禁止），而不是散落在各处。

ScopeSentry 现在把这类判断分散在模块里（catch-all 拦截、网关拒绝过滤、速率 sleep）。
**可落地**：抽一个 `policy` 包，输入（目标、模块、请求数、写操作？），输出 allow/deny + 理由，
所有出网请求过它。好处是 scope、速率、写操作三条纪律只写一遍。

## 4. supersede（事实推翻）——漏洞状态该有历史

SapperV2 允许新事实 supersede 旧事实，所以「上次判是洞、这次判是误报」有完整轨迹。

ScopeSentry 现在是就地覆盖（`$set status=5`）。**可落地**：状态变更写 `vulnHistory` 数组
（from/to/by/at/evidence），报告和被质疑时能自证。这条成本最低，收益很直接。

## 5. arena（同靶多策略对比）——评估规则集质量

SapperV2 的 arena 让多个策略在同一起点上跑、按结果排名。

ScopeSentry 可以做**规则集 A/B**：同一批目标，A 组用当前模板集，B 组用候选模板集，
比较命中数 / 隔离率 / 人工确认率。没有这个，「精准度提升」只能靠感觉。

## 不借的部分

| SapperV2 的做法 | 为什么不搬 |
| --- | --- |
| 一个 cwd 一张图 | ScopeSentry 是中心调度，图应该在 Mongo 里按 host 建，不按目录 |
| tmux + watchdog 保活 | 那是给无人值守 CLI agent 的；ScopeSentry 的节点已经由容器 restart 策略兜底 |
| arena 的竞技场协议 | 那是 TSecBench 评测用的，我们没有对应平台 |

## 建议顺序

1. **supersede / vulnHistory**（一天，纯数据）
2. **policy 包**（两天，收敛三条纪律）
3. **fact 集合 + 模块 when 条件**（一周，最大收益，也最需要设计）
4. arena（等模板集稳定后再做）
