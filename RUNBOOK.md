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
