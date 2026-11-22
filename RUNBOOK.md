# ScopeSentry 开发/验证 RUNBOOK

> 改完代码该跑什么、失败了先查什么。目标：三分钟内能自己接上。

## 0. 环境前提

生产服务端在 `prod-tx-sh-app-01`（1.15.46.212，8G）。本地通过 SSH 隧道访问：

```bash
ssh -f -N -L 18082:127.0.0.1:8082 prod-tx-sh-app-01
curl -s -m 3 -o /dev/null -w "%{http_code}\n" http://127.0.0.1:18082/healthz   # 期望 200
```

**隧道会断**（容器重启/网络抖动）。断了之后所有页面都打不开，看起来像"全站挂了"。

## 1. 改动后按顺序验证

```bash
# ① 前端构建（含组件导入检查 + UI 审计）
cd frontend && pnpm build:pro && cd ..
python3 scripts/lint-vue-imports.py          # 组件用了但没 import → 静默失效
python3 scripts/check-ui-audit.py docs/ui-audit-baseline.json

# ② 发版（构建 + sha256 + 部署 + 版本/compose/节点断言 + 镜像清理）
bash scripts/release.sh 2.0.245

# ③ API 链路冒烟（约 5 分钟）：建任务→结果→处置→导出
bash scripts/e2e-smoke.sh

# ④ UI 矩阵（约 6 分钟）：25 用例，读/写/重置/弹窗/搜索
#    见 scripts/ui-smoke/README.md，用 Agent Bridge 逐用例执行
```

节点侧改动：

```bash
cd ../ScopeSentry-Scan
bash scripts/deploy-node.sh            # 构建 + 哈希 + 备份 + 部署 + 自动回滚 + 版本一致性
bash scripts/deploy-node.sh --dry-run  # 只打印计划
bash scripts/deploy-node.sh --rollback # 回滚到上一版
```

## 2. 失败了先查什么（按顺序）

| 现象 | 先查 | 处理 |
| --- | --- | --- |
| 页面全打不开 / 断言全挂 | 隧道 | `curl healthz`；非 200 就重建隧道 |
| 建标签报 `FAILED_TAB_RETAINED` | 有超时的旧 tab | `close_tab({tabId})` 后再继续 |
| 建标签报 `TASK_URL_CONFLICT` | taskKey 复用但 URL 不同 | 每次用唯一 taskKey |
| 页面是登录页 | 会话失效 | 用测试账号重新登录后继续 |
| 发版"成功"但版本没变 | compose tag / 容器是否重建 | 脚本已断言，看 `[7/7]` 输出 |
| 节点版本没变 | 上传被截断 | 脚本已校验 sha256；看远端字节数 |

## 3. 断言/调试常用入口

```bash
# 服务端健康（含 mongo/redis/磁盘）
ssh prod-tx-sh-app-01 "curl -s localhost:8082/healthz"

# 节点在线与版本
ssh prod-tx-sh-app-01 "python3 /opt/scopesentry/nodecheck.py --json"

# 日志（节点）
sg docker -c "docker logs --tail 100 scopesentry-scan"
```

凭据说明：服务器本机 `/opt/scopesentry/.api_cred`（600，`user:pass`），仓库里不存密码。

## 4. 已知缺口（别以为有自动化）

- **i18n key 缺失**没有自动检查：locale 里没有的 key 会原样显示在界面上。
  改弹窗/确认框文案后，必须在浏览器里真的点开看一眼。
- **UI 矩阵要人工驱动**（Agent Bridge eval），没进 CI。
- **深分页**（跳过 4.8 万条）约 50ms，靠索引遍历，属正常；没有做游标分页。

## 5. 发版前自查清单

1. `version.json` 的 server/scan 与将要发布的版本一致
2. `git status` 干净（`cmd/main/static` 由 release 脚本还原）
3. 三节点版本一致（release 的 `[7/7]` 会打印）
4. 发布后 `docs/ui-smoke-report-*.md` 追加一行结果（可选，但便于回溯）
## 6. 前端导入检查的覆盖范围（2026-11-20 扩展）

`scripts/lint-vue-imports.py` 现在查两类问题：

1. **Element Plus 组件**用了但没 import（渲染成 `<elalert>` 之类未解析标签，界面静默失效）
2. **Vue API**（`computed` / `watch` / `onMounted` / `nextTick` …）用了但没 import
   —— 这类会让 setup 直接抛错、组件整个白屏（2.0.246/247 的进度弹窗就是这么空白的）

已接进 `release.sh` 的 `[1/7]` 审计门禁，跑不过就不发版。

自测方式：临时放一个用 `computed()` 但没 import 的 .vue 到 `frontend/src/views/`，
运行脚本应报 `Vue API: computed` 并返回 1。
## 7. 外部集成（MCP / huntdeck）排障（2026-11-20）

### 403 无原因 → 看响应头

```
X-ScopeSentry-Auth: API key is disabled
X-ScopeSentry-Auth: API key has expired
X-ScopeSentry-Auth: Invalid API key
X-ScopeSentry-Auth: API key is required. Use X-API-Key header …
```

调用方（huntdeck）通常只打 HTTP 状态码，所以服务端把原因放进响应头，
body 里也有 JSON-RPC error（`{"error":{"code":-32003,"message":"API key is disabled"}}`）。

**关键**：key 被停用后，所有 `tools/call` 都是 403，看起来像「功能坏了」。
检查 `api_key` 集合的 `enabled` 字段（`db.api_key.find({},{name:1,enabled:1})`）。

### 队列满 → 用「迁移排队」而不是删任务

单节点背压上限 5（`maxQueuedPerNode`）。积压后新派发被拒，报错形如：

```
节点 node-sh-01 队列已积压 5 个任务（上限 5），请等待当前任务完成后再提交
```

处理顺序：

1. 节点页看各节点「队列」列，找空闲的
2. 有积压节点 → 行操作「迁移排队」→ 选目标节点 → 只迁「待开始」的任务
3. 三台都满 → 要么等，要么调低派发频率/每任务目标数

对应接口：`POST /api/node/queue/rebalance {from,to,limit}`。

### 让集成自己选空闲节点

`list_nodes` 的返回里带两个字段：

```json
{ "recommended": "node-dev-02", "recommendedQueueLen": 0, "list": [...] }
```

客户端按 `recommended` 选节点，就不会一直往最忙的那台推。

### 相关历史事故

| 现象 | 根因 | 已修 |
| --- | --- | --- |
| 集成每分钟建一个任务，队列涨到 28 | 无创建频率限制 | 每分钟 10 个上限（v2.0.252） |
| 同一任务在队列里出现 12 次 | 节点重复注册触发重复投递 | 投递前去重 + 加锁（v2.0.256） |
| 403 但看不出原因 | 客户端只打状态码 | `X-ScopeSentry-Auth` 头（v2.0.264） |
| 集成总往最忙节点推 | `list_nodes` 没有队列长度 | `recommended` 字段（v2.0.265） |
| 队列堵死后只能手工改 Redis | 无迁移入口 | 节点页「迁移排队」（v2.0.267） |
## 8. 任务调度：四道闸、两个环境变量、三个计数（2026-11-20）

### 四道闸（按生效顺序）

| 闸门 | 触发条件 | 拒绝文案里的关键词 | 计数 key |
| --- | --- | --- | --- |
| 队列闸 | 单节点排队 ≥ 5 | 「队列已积压」 | `queue_full` |
| 内存闸 | 节点容器内存 ≥ 90% 且正在跑任务 | 「容器内存已用 …%」 | `container_mem` |
| 前缀闸 | 单节点同名前缀排队 ≥ 3 | 「队列里已有 N 个『x-*』任务」 | `prefix_quota` |
| 限流闸 | 1 分钟内创建 > 10 个任务 | 「每分钟最多创建 10 个任务」 | `rate_limited`（未接计数） |

> 前缀 = 任务名里第一个 `-` 之前的部分。`huntdeck-20261120-xxx` 的前缀是 `huntdeck-`。

### 两个环境变量（服务端 compose 里设）

```
TASK_QUEUE_MAX_PER_NODE=5        # 单节点排队上限，1..50
TASK_QUEUE_MAX_PER_PREFIX=3      # 同前缀在单节点的排队上限，1..50
```

改完重启服务端生效。**注意闸门顺序**：队列闸先于前缀闸，队列没满时前缀闸才可能触发
（2026-11-20 实测：队列长期 4-5 条，前缀闸几乎见不到；把队列腾空后第 4 个同前缀任务立刻被拒）。

### 三个计数（节点页顶部可见）

```
派发被拒（自启动起）：前缀占满 3 · 队列满 2 · 内存高 1
```

接口：`POST /api/task/dispatch-stats` → `{"prefix_quota":3,"queue_full":2}`

- **进程内计数**，服务端重启归零；不是审计（审计在 `audit_logs`）
- 只用来回答「最近有没有人在撞闸」，不是长期趋势

### 节点侧：容器内存与自动降压

节点心跳上报 `containerMem`（cgroup v2 `memory.current/max`，兼容 v1）。
**宿主机 `memNum` 看不出 cgroup 压力**——实测宿主 24% 时容器已 82%。

自动降压（`internal/node/throttle.go`）：

```
连续 3 次（≈60s）容器内存 ≥90% → 任务并发降到 1
连续 3 次 ≤70%                → 恢复到配置值
```

**池调整必须在独立协程里做**：`ants.Tune` 会等在跑的任务结束，
放心跳 goroutine 里会把心跳卡死 → 三台节点全部被判离线（2026-11-20 事故）。

### 积压清理工具

```bash
python3 scripts/remote/queue-drain.py --prefix huntdeck-           # 预演
python3 scripts/remote/queue-drain.py --prefix huntdeck- --apply   # 执行
```

把匹配前缀的**待开始**任务从节点队列摘掉并标为暂停（`status=2`），**不删除数据**，
想恢复在界面上点「开始」。
### 事故复盘 6：配置被空值覆盖 → 三台节点连环崩溃（2026-11-20）

**现象**：三台扫描节点同时 `Restarting (2)`，服务端 0/3 在线；
日志里 `panic` 栈顶指向 `internal/pool/pool.go:50` 的 `ants.NewPool(...)`。

**根因链**：

```
心跳无条件写 "modulesConfig": cfgStr（多数时候 cfgStr == ""）
  → Redis HMSet 用空串覆盖了已有的节点配置
  → 节点重启时 GetGoroutineCount 返回 0
  → ants.NewPool(0) 直接 panic（不是返回 error！）
  → 容器崩溃循环
```

**为什么原有的兜底没救到**：`pool.go` 里写的是 `if err != nil { 用默认值 }`，
但 `ants.NewPool` 对非法 size 是 **panic**，那个分支永远进不去。

**修法**（三层）：

1. `internal/node/node.go`：心跳**只在配置变化时**才带 `modulesConfig` 字段
2. `internal/pool/pool.go`：调用前判断 `count <= 0` → 兜底 2 并告警
3. `internal/configupdater/update.go`：序列化失败或结果为空时**不写**该字段

**验证**：把某节点配置改成 `maxGoroutineCount: 0` 再重启 → 容器 `Up`（不再崩溃循环）。

**通用教训**：

- Redis `HMSet`/`HSet` 对空字符串是**覆盖**，不是跳过 → 可选字段必须条件写入
- `if err != nil` 兜底的前提是**被调方会返回 error**；会 panic 的调用要前置校验
- 回滚二进制救不了数据面故障（配置已被清空），必须同时修数据
### 节点配置下发是「热生效」，不需要重启（2026-11-20 实测）

```
服务端 POST /api/node/config/update  →  给节点投一个 refresh_config:<node> 列表项
节点每 3 秒轮询该键，消费后立即应用（写本地配置 + 调各模块协程池）
```

实测（node-dev-02，maxGoroutineCount 3 → 2，vulnerabilityScan 2 → 1）：

```
投递后 refresh 键 len=1
25 秒后：节点上报的 modulesConfig 已是 maxGoroutineCount: 2
        refresh 键已消费（exists=0）
        容器未重启
```

注意：界面上 `maxTaskNum` 显示的是**当前生效值**——如果容器内存 ≥90%，
自动降压会把它压到 1，看起来像"配置没生效"。实际底层基准值已改成 2，
内存回落后会自动恢复到 2。这一点在排查时容易被误读。
## 9. 生产服务器带宽实测（2026-11-20）

在 `prod-tx-sh-app-01`（1.15.46.212）上实测：

| 方向 | 目标 | 传输量 | 耗时 | 速率 |
| --- | --- | --- | --- | --- |
| 下载（入站） | Cloudflare `speed.cloudflare.com/__down` | 50 MiB | 3.78s | 13.9 MB/s ≈ **111 Mbps** |
| 下载（入站） | `mirrors.tuna.tsinghua.edu.cn` | 50 MiB | 4.03s | 13.0 MB/s ≈ **104 Mbps** |
| 上传（出站） | Cloudflare `speed.cloudflare.com/__up` | 25 MiB | 22.6s | 1.16 MB/s ≈ **9.3 Mbps** |

**结论：入站 ~100-111 Mbps，出站 ~9.3 Mbps，不对称比约 12:1。**
出站是典型腾讯云 10 Mbps 带宽包规格。

内网互通（app-01 → app-02，25 MiB）：0.48s ≈ 54 MB/s，走内网不受公网出站影响。

### 运维含义

- 大文件**从服务器往外传**（节点二进制、备份下载）受 10 Mbps 限制：
  50 MiB 压缩包约需 45-130 秒（取决于同时是否有扫描任务争用带宽）
- 大文件**往服务器传**（拉日志、拉导出）几乎无感（百兆入站）
- 服务器之间倒数据走内网，很快，不需要走公网

### 测速时踩的坑

`https://test.ustc.edu.cn/` 的后端接口做了来源校验，curl 直接打 `/backend/garbage.php` 返回
`500 not ustc`（带 `Referer`/`Origin` 也一样）。要测服务器带宽请用上面两个独立来源交叉验证，
不要依赖单一测速站的接口。
### 容量实测：一台节点跑一个任务就能吃满 2GB（2026-11-21）

节点反复"心跳停 → 判离线 → 重启"的根因不是任务太多，而是**单任务内存**：

```
node-tx-sh-02   容器上限 2.0GB   maxGoroutineCount 3 → 2 → 仍 100%
                最后只剩 1 个任务在跑（running=1）时，内存依然 99.9%
node-dev-02     容器上限 1.17GB  maxGoroutineCount 1，长期 88-100%
node-sh-01      容器上限 2.44GB  maxGoroutineCount 3，74-82%
```

**结论：降任务并发几乎无效**——瓶颈是单个任务内部的内存（nuclei 模板库 + 端口扫描缓冲）。
真正有效的手段只有两个：

```
① 提高容器内存上限（2GB → 4GB），或
② 降单任务内部并发：vulnerabilityScan.goroutineCount（已对 tx-02 设 1）
```

已做的两次调整（都可热回滚）：

```
node-tx-sh-02  maxGoroutineCount: 3 → 2
node-tx-sh-02  vulnerabilityScan.goroutineCount: 2 → 1
```

回滚方式：同样的 `POST /api/node/config/update` 把数值改回去，3 秒热生效，不需要重启节点。
## 10. 节点内存：让 Go 运行时感知 cgroup 上限（2026-11-21）

**问题**：三台节点的容器内存长期 74-99%，其中一台每 10-20 分钟心跳停一次被判离线。
之前判断是"任务太多"，实测发现**单任务就能吃满 2GB**，降并发收效有限。

**真因**：Go 运行时默认只看物理内存、**不读 cgroup 限制**。容器被限到 1-2GB 时，
它仍按自己的节奏堆内存，RSS 一路顶到 cgroup 上限 → 频繁 GC 与限流 → 心跳变慢 → 判离线。

**修法**（`internal/node/memlimit.go`）：启动时读 `/sys/fs/cgroup/memory.max`，
调 `debug.SetMemoryLimit(上限 × 0.8)`。Go 1.19+ 会在接近软目标时主动加大 GC 力度。

```
日志：memory limit: container=1200MiB soft-limit=960MiB
```

**实测效果**（同一批任务在跑，未减少负载）：

| 节点 | 容器上限 | 改前 | 改后 |
|---|---|---|---|
| node-sh-01 | 2.44GB | 1.818GiB (74%) | **929MiB (37%)** |
| node-tx-sh-02 | 2.0GB | 1.895GiB (95%) | **1.64GiB (82%)** |
| node-dev-02 | 1.17GB | 1.159GiB (99%) | **673-943MiB (56-78%)** |

**比"提容器上限"更划算**：不动机器规格、不加钱，只让运行时自己收敛。

**后续调优（2026-11-21 凌晨）**：容器里除 Go 进程还有 chrome/katana 子进程，
软目标从 0.8 降到 **0.7**（给子进程留余量）。三台再降一档：

```
node-sh-01   78.7% → 51.2%   (soft 1750MB)
node-tx-sh-02 90.5% → 56.1%   (soft 1433MB)
node-dev-02  97.3% → 88.1%   (soft 840MB，容器只有 1.17GB)
```
### 吞吐换算：一个任务要跑多久（2026-11-21 抽样）

抽样 20 个运行中模块，耗时分布：

```
< 10 分钟   13 个
10-20 分钟   0 个
20-30 分钟   2 个   (23 / 24 分钟)
30-40 分钟   1 个   (38 分钟)
40-60 分钟   1 个   (53 分钟)
> 60 分钟    1 个   (60+ 分钟，仍在推进)
```

**结论**：nuclei/URL 安全检测对**单个目标**就要 20-60 分钟。
一个任务 = 14 个模块 × 2 个目标（当前 huntdeck 派发的粒度），
所以单任务耗时以小时计；每台节点同时只跑 1-2 个任务。

这解释了"任务排队消化慢"——**不是队列闸门太严，是单任务本身就长**。
想提吞吐只有两条：加节点，或缩小单任务的目标数/模板数。

排查提示：任务"停在 81%"不代表卡住。看「当前模块 @ 目标 · N 分钟」，
20 分钟以内是常态；超过 60 分钟界面上会显示「疑似停滞」。
## 11. 任务页每一行数字的含义与局限（2026-11-21）

运行中的任务在列表里最多显示六层信息：

```
████░░░░  50%                进度条：已完成模块数 / 模块总数
资产处理 · 5/14 @ target     当前模块（第 5 步/共 14 步）+ 当前目标
· 12 分钟                    当前模块已跑多久；≥20 分钟标黄，≥60 分钟显示「疑似停滞」
≈ 约剩 46 分钟               剩余时间；带 ≈ 表示「用创建时间凑的，可能含暂停时长」
目标 1/2                     目标级进度（已完成/总数）
本次工具失败 3 次             外部工具非零退出次数，出现即说明产出可能不全
```

**这些数字的局限（必须知道）**：

- 百分比 = 已完成模块数/总数，**不代表耗时占比**（nuclei 一个模块可能占全任务一半时间）
- 「当前模块 12 分钟」是**单目标单模块**的耗时，不是任务总时长
- ETA 是线性外推：后面若赶上慢模块会偏小，赶上快模块会偏大
- 没有 `startedAt` 的老任务，ETA 前会带 `≈`

判断「是不是卡住」的正确方式是**点开「任务进度」看逐目标的模块时间戳**，
而不是盯着百分比。20 分钟内不动属正常。
## 12. 扫描出站走代理（2026-11-21 上午，按用户要求改）

**背景**：腾讯云对两台扫描机的「对外攻击行为（TCP:80）」告警，根因是扫描器直接以云 IP 出站。
用户决定：扫描必须走代理，且**不使用 Chimon（x4.cio.bz）的节点**，改用新订阅：

```
MESL          https://em.mesl.cloud/ems/get?token=…
oxycontinon   https://3732feba743a.oxycontinon.com/osubscribe.php?sid=21814&token=…
```

### 做了什么

1. 三台主机都加了这两个订阅（`clashctl sub add`），并把**活动订阅切成 MESL**（`clashctl sub use 3`）
2. 扫描容器的环境变量加代理：

```
HTTP_PROXY / HTTPS_PROXY / ALL_PROXY = http://127.0.0.1:7890
NO_PROXY = localhost,127.0.0.1,10/8,172.16/12,192.168/16,1.15.46.212,111.229.44.240
```

3. 容器重建方式（保留可回滚）：`docker commit` 当前容器 → 用同参数 + 代理环境变量 `docker run` 新容器，
   旧容器改名 `scopesentry-scan-old-<ts>` 保留，不删。

### 验证（clash 连接链）

```
dev-02   squaretrade.eu        -> 🇭🇰 香港 02 / Fallback / MESL
prod-01  webdisk.us.auth0.com -> 🇭🇰 香港 03 / Fallback / MESL
prod-02  update.googleapis.com -> 🇭🇰 香港 02 / Fallback / MESL
```

扫描流量全部经 **MESL** 组出站；huntdeck 的平台请求仍走 `HUNTDECK-AUTO`（未受影响）。
出站 IP 从 `172.81.111.248`（x4 机房）变为住宅段（`125.103.212.107` / `216.236.32.2`）。

### 回滚

```bash
sg docker -c "docker rm -f scopesentry-scan && docker rename scopesentry-scan-old-<ts> scopesentry-scan && docker start scopesentry-scan"
```
### 容器内存规格统一到 ≥2GB（2026-11-22 00:30）

```
prod-tx-sh-app-01  node-sh-01   2.62GB（未动，本来就 > 2G）
prod-tx-sh-app-02  node-tx-sh-02 2.00GB
dev-tx-sh-app-02   node-dev-02  1.17GB → 2.00GB   ← 本次调整
```

调整方式（不改容器，运行时生效）：

```bash
sg docker -c "docker update --memory 2g --memory-swap 4g scopesentry-scan"
sg docker -c "docker restart scopesentry-scan"   # 必须重启，Go 软上限在启动时按 cgroup 计算
```

重启后日志确认：`memory limit: container=2048MiB soft-limit=1433MiB`。

**背景**：dev-02 原先 1.17GB，Go 进程 ~1.0GB + chrome 爬虫 ~0.48GB 就已超限，
看门狗因此每 1-1.5 小时触发一次重启（RestartCount 25），正在跑的任务被打断。
提到 2GB 后软上限 1433MB、看门狗阈值 1.95GB，留出足够余量。

注意：`docker update` 只改容器运行参数，**容器被删除重建时要重新带上 `--memory`**
（`scripts/proxyfy3.sh` 这类重建脚本已经从旧容器读取该值）。
## 15. 扫描出站全覆盖：裸 TCP 也走代理（2026-11-22 腾讯封 80 事故后）

**事故**：111.229.44.240 的 TCP:80 出站被腾讯封禁 24 小时。
上一轮只把 **HTTP** 走了代理（`HTTP_PROXY` 环境变量），两条流量完全绕过：

```
端口扫描（rustscan/naabu）   裸 TCP socket，不读 HTTP_PROXY
chrome 无头浏览器            rod 拉起子进程，只认 --proxy-server
```

### 修法一：chrome 走代理（代码）

`libs/httpx/runner/headless.go` 与 `libs/nuclei/pkg/protocols/headless/engine/engine.go`：
没显式给 proxy 时回落到 `HTTP_PROXY` 环境变量，再传给 `launcher.Proxy()`。

### 修法二：裸 TCP 透明代理（iptables + clash redir）

clash 开 `redir-port: 7892`，再按**容器 cgroup** 把该容器的 TCP 全部 REDIRECT 过去：

```bash
CG="system.slice/docker-<容器ID>.scope"    # cgroup v2 路径，不能带前导斜杠
# 放行：环回/内网/三台服务器
iptables -t nat -A OUTPUT -m cgroup --path "$CG" -d 1.15.46.212/32 -j RETURN
# 其余 TCP 全重定向到 clash redir
iptables -t nat -A OUTPUT -m cgroup --path "$CG" -p tcp -j REDIRECT --to-ports 7892
```

脚本 `/usr/local/bin/ss-tproxy.sh` + systemd `ss-tproxy.service`（enabled）落地；
**容器重建后 cgroup 变，必须重挂**——已在 `deploy-node.sh` 的 restart 之后自动重启该 unit。

### 验证

```
封禁中的 prod-02：容器内裸 TCP 连 93.184.216.34:80  → OPEN（直连本应超时）
redirect 计数器：prod-01 4252 包 / dev-02 8114 包  → 确有流量经此转发
节点 3/3 在线，控制面（Redis/Mongo）未受影响
```
## 16. 访问方式改为 Tailscale（2026-11-22）

**不再需要 SSH 隧道。** 直接访问：

```
http://100.115.186.92:8082          # prod-tx-sh-app-01 的 tailnet IP
```

实测：本机（经 Surge 的 tailscale 出站）200；prod-02 经 tailnet 200；公网 IP 000（不可达）。

### 做了什么

1. **`~/.ssh/config` 四台主机改指 tailnet IP**（原公网 IP 保留为 `<host>-pub` 回退入口，备份 `~/.ssh/config.config.bak-202611221414`）
   ```
   prod-tx-sh-app-01   1.15.46.212    → 100.115.186.92
   prod-tx-sh-app-02   111.229.44.240 → 100.72.154.12
   dev-tx-sh-app-02    43.142.188.143 → 100.118.145.73
   dev-tx-sh-app-01    43.143.38.5    → 100.124.31.96
   ```
2. **8082 只允许 tailnet**（`/usr/local/bin/ss-web-tailnet.sh` + `ss-web-tailnet.service`，enabled）：
   `-s 100.64.0.0/10 ACCEPT` / `-s 127.0.0.1 ACCEPT` / 其余 `DROP`

### 别的端口

Redis 6379、Mongo 27017 等未改动；如需同样收敛，照这个 unit 加一条 `--dport`。
