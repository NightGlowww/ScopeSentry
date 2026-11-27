# 契约核对（verify_contract.sh）

冒烟脚本 `scripts/ci/smoke.sh` 只回答「服务活着吗」（healthz / 版本 / Mongo / 节点数）。
它管不到「行为有没有悄悄退化」——例如分页上限被绕过、删除接口又开始假成功、
静态资源的 404 又被缓存一年。这些都是在 2026-11-27~28 的真实事故里逐条踩出来的，
所以固化成一份可重复执行的核对表。

## 用法

```bash
SS_BASE=http://<host>:8082 SS_TOKEN=<JWT> bash scripts/ci/verify_contract.sh
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

## MCP 工具矩阵（mcp_sweep.py）

契约脚本管的是 REST 侧的横切口；MCP 有 71 个工具，逐个手工验不现实，所以单独一个脚本：

```bash
SS_BASE=http://<host>:8082 SS_MCP_KEY=<全 scope 的 ssk_...> SS_ASSET_ID=<任一资产 id> \
  python3 scripts/ci/mcp_sweep.py
```

它做四件事（当前 40 条读 + 37 条守卫，71 个工具全覆盖）：

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
