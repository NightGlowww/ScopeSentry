# 页面监控（PageMonitoring）链路审计与修复

- 审计对象：`/Users/night/Workspaces/security_workspace/_tools/ScopeSentry`（Go + Gin + Vue3），生产 `prod-tx-sh-app-02`（隧道 `http://127.0.0.1:18082` → 容器 `scope-sentry:8082`）
- 审计时间：2026-11-19 04:34 — 05:15 (+08)
- 约束执行：未部署、未重启；仅创建/删除自己的测试数据；`go build ./...` 通过；未改他人任务与模板
- 节点侧：`node-tx-sh-02`，`ScopeSentry-Scan v1.9.1`（Redis `node:node-tx-sh-02` → `version=1.9.1`，`state=1` 在线）
- 前置文档：`docs/audit/EMPTY_COLLECTIONS.md`、`docs/audit/ISLANDS.md`

---

## 0. 结论摘要

`PageMonitoring` 有**两条**合法生产者，而「永不出数据」不是一个自锁，而是**四个串联断点**，其中三个在本仓库、一个在节点侧无补丁亦无法自愈：

| 编号 | 断点 | 位置 | 定性 | 状态 |
| --- | --- | --- | --- | --- |
| **A** | 调度任务默认不注册：种子文档没有 `scheduledTasks`，`scheduled` 集合里没有 `page_monitoring` job | `internal/database/mongodb/initdb.go:155`、`internal/services/task/scheduler/scheduler.go:356` | 产品默认值 | **未改**（建议见 §6） |
| **B** | `getPageMonitoringData()` 用 `Find(ctx, nil, ...)`，mongo-driver 直接返回 `document is nil`，job 每次运行都失败 | `internal/scheduler/jobs/page_monitoring.go`（旧 79-98 行） | 确定性 bug | **已修** |
| **C** | 推给节点的 `TaskInfo:page_monitoring` 元素是裸 URL 字符串，节点 `json.Unmarshal` 成 `types.PageMonit` 失败后静默 `continue`，任务 0 秒结束、永远没有快照 | `internal/scheduler/jobs/page_monitoring.go:79-98`（旧） | 协议不匹配 | **已修** |
| **D** | 扇出时用「URL」当节点名：`RPush("NodeTask:"+url, ...)`，真正节点收不到、Redis 里留下一串无人消费的野 key | `internal/scheduler/jobs/page_monitoring.go:70-75`（旧） | 确定性 bug | **已修** |

对 `EMPTY_COLLECTIONS.md` 的一处纠正：该文档把原因归为「插件未挂模板 → 无 URL → `CreatePageMonitoringTask` 在空数据处 return → 永不建任务」。实测中 `return` 根本没走到——**B 在读取阶段就报错返回了**；而「插件未挂模板」只影响**自动**入库（生产者 #2），人工添加（生产者 #1）与插件完全无关。修掉 B/C/D 后，只要调度开启，人工加一条 URL 即可在节点落快照（§4 已实测）。

---

## 1. 谁在写 `PageMonitoring`（生产者定论）

### 生产者 #1（人工入口，本仓库写库）

UI「计划任务 → 页面监控 → 添加 URL」按钮：`frontend/src/views/Task/components/PageMonit.vue:136` → `addScheduledTaskPageMonitApi`（`frontend/src/api/task/index.ts:280`）→ `POST /api/task/scheduled/pagemonit/add`

```text
internal/api/routes/task/scheduler.go        POST /scheduled/pagemonit/add   (WithAuth)
internal/api/handlers/task/scheduler.go:271  AddPageMonitTask   → 只校验 url 非空
internal/services/task/scheduler/scheduler.go:419 AddPageMonitTask
    task := &models.PageMonitoringTask{ID: NewObjectID(), URL: url, Hash: []string{}, MD5: MD5(url), State: 1, Time: ""}
internal/repositories/task/scheduler/scheduler.go:131 InsertPageMonit → collection "PageMonitoring"
```

即：**人工添加只写 `PageMonitoring` 一张表**（`md5 = 32 位 MD5(url)`，`hash=[]`，`time=""`），不建任务、不触发任何扫描。唯一索引 `PageMonitoring.url`（`internal/database/mongodb/initdb.go:418`）保证同 URL 不重复。

### 生产者 #2（节点插件，扫描期自动入库）

插件 `URLSecurity/PageMonitoring`，hash `e52b8b16d49912ca564c22319c495403`（`internal/constants/defaults.go:246-256`），节点侧实现 `modules/urlsecurity/pagemonitoring/pagemonitoring.go`：

- `Execute(input types.UrlResult)`：**默认 `type=js`**，非 `.js` 后缀直接 `return nil, nil`（参数可改 `-type all` 之类才全量）；
- 计算 `md5 = utils.Tools.HashXX64String(URL)`（**xxhash64，16 位 hex**，与人工入口的 32 位 MD5 **不是同一个东西**）；
- `results.Handler.PageMonitoringInsert` → 结果队列 → `PageMonitoring`，同时 `PageMonitoringInsertBody` → `PageMonitoringBody`。

节点 `NodePlg:node-tx-sh-02` 实测该插件 `install=1 / check=1`（已装可用），5 个模板的 `URLSecurity` 里确实都没有 `e52b8b16`（见 §4 基线），所以自动入库当前为 0 —— 这部分归因与 `EMPTY_COLLECTIONS.md` 一致，但**它只是「自动入库」这一条路断了**，不是「表永远空」的根因。

### 链路：一条 URL 如何变成节点上的快照

```text
[人工] POST /api/task/scheduled/pagemonit/add
        └→ PageMonitoring{url, md5=MD5(url), hash:[], time:""}

[调度] ScheduledTasks{id:"page_monitoring"}  ← 种子文档（hour=24, allNode=true）
        │  UI 开关 scheduledTasks=true 时:
        │  POST /api/task/scheduled/pagemonit/update
        │    └→ services/task/scheduler/scheduler.go:356 UpdatePageMonitScheduledTask
        │         └→ addScheduler(handler="page_monitoring") → scheduler.AddJob
        │              ├→ 写 "scheduled" 集合{_id=ScheduledTasks._id, handler, schedule}
        │              └→ gocron Cron("0 0-23/{hour} * * *")
        ↓ 到点
[执行] internal/scheduler/scheduler.go:309 executeJob → handlers["page_monitoring"]
        └→ internal/scheduler/jobs/page_monitoring.go:19 CreatePageMonitoringTask
             ├→ allNode → NodeService.GetNodeData（补全节点列表）
             ├→ getPageMonitoringData() 读 PageMonitoring
             ├→ DEL  TaskInfo:page_monitoring ; LPUSH <payload...>
             └→ RPUSH NodeTask:<node>  {"ID":"page_monitoring","type":"page_monitoring"}
        ↓
[节点] ScopeSentry-Scan internal/task/task.go:110  if Type=="page_monitoring"
        └→ 循环 BatchGetAndDelete("TaskInfo:page_monitoring", 50)
             └→ runner.PageMonitoringRunner(targets)
                  └→ 每个元素 json.Unmarshal → types.PageMonit
                       └→ HTTP GET → HashXX64(body)
                            ├→ Update PageMonitoring{hash, time, statusCode, rootDomain} (by md5)
                            └→ Upsert PageMonitoringBody{content:[body]}        (by md5)
[读取] GET/POST /api/assets/page-monitoring/{result,diff,history,content}
```

---

## 2. 断点 B（根因）：`Find(ctx, nil, ...)` 必然失败

旧实现：

```go
// internal/scheduler/jobs/page_monitoring.go (旧)
cursor, err := ScanSvc.pageMonitoringCollection.Find(context.Background(), nil, options.Find().SetProjection(bson.M{"url": 1}))
if err != nil { return nil, fmt.Errorf("failed to find documents: %w", err) }
```

`mongo-go-driver` 对 `nil` filter 直接报 `document is nil`（不是「查不到」，是非法参数）。生产 05:00:00 的 cron 实证：

```text
2026-11-19 05:00:00 INFO  reflect/value.go:586  Executing job {"job_id":"6afaa875b7221f4aa7eaa473","handler":"page_monitoring"}
2026-11-19 05:00:00 INFO  jobs/page_monitoring.go:20  create_page_monitoring_task
2026-11-19 05:00:00 ERROR reflect/value.go:586  Job execution failed {"error":"failed to find documents: document is nil"}
```

即：**即使把调度打开、把 URL 加进去，这个 job 也一次都没成功过**。用例复现（同一 driver v1.14.0，走 SSH 转发直连生产 Mongo，`_pmverify` 临时程序，已验证后删除）：

```text
OLD Find(nil) error: document is nil
NEW doc: https://example.com/?pm_audit=page-monitoring-probe
NEW Find(bson.M{}) ok, docs: 1 cursorErr: <nil>
```

---

## 3. 断点 C/D：推给节点的协议与扇出 key

### C：`TaskInfo` 元素格式错

节点侧（upstream `github.com/Autumn-27/ScopeSentry-Scan`，`internal/task/task.go:110-129`）：

```go
if runnerOption.Type == "page_monitoring" {
    go func() {
        for {
            targets, err := redis.RedisClient.BatchGetAndDelete(ctx, "TaskInfo:"+runnerOption.ID, 50)
            if len(targets) == 0 { break }
            runner.PageMonitoringRunner(targets)
        }
    }()
}
```

`runner/pagemonitoring.go:45`：

```go
for _, target := range targets {
    var pageMonitResult types.PageMonit
    if err := json.Unmarshal([]byte(target), &pageMonitResult); err != nil {
        logger.SlogWarnLocal(...)   // 只写插件日志，然后 continue
        continue
    }
    ... pool.Submit(Handler)
}
```

而服务端旧代码推的是**裸 URL**（`data` 是 `[]string` 的 url）。节点拿到的不是 JSON → unmarshal 失败 → 整轮跳过，日志里只剩 0 秒的 `Task begin/end`（实测见 §4 04:46:41）。

### D：扇出遍历错对象

旧代码 `for _, d := range data { RPush("NodeTask:"+d, ...) }`，`data` 是 URL 列表 ⇒ key 变成 `NodeTask:https://…`，`node-tx-sh-02` 一条都收不到，还会在 Redis 里堆出永不过期的野 key（`NodeTask:*` 没有 TTL）。

---

## 4. 生产实测（证据）

### 4.1 基线（04:36，只读）

```text
pm=0  pmb=0  ScheduledTasks=1  scheduled=1
ScheduledTasks: {"id":"page_monitoring","name":"Page Monitoring","hour":24,"node":[],"allNode":true,"type":"Page Monitoring","state":true}
scheduled:      [{"_id":"github_intel_tick","handler":"github_intel"}]      ← 没有 page_monitoring
plugin e52b8b16: {"module":"URLSecurity","name":"PageMonitoring","status":false,...}
templates URLSecurity: default={} ; artex-recon-light=null ; artex-recon-deep=null ;
                       artex-recon-full={"2949994c…":"","1aa212b9…":"-pdf false -verify false"} ; canary-tpl-comma=null
nodes: Redis node:node-tx-sh-02 state=1 updateTime=04:38:18 version=1.9.1 ; node:node-test state=3(offline)
NodePlg:node-tx-sh-02: e52b8b16d49912ca564c22319c495403_install=1 _check=1
```

### 4.2 人工添加 1 条监控（04:41）

```text
POST /api/task/scheduled/pagemonit/add {"url":"https://example.com/?pm_audit=page-monitoring-probe"}
→ {"code":200,"message":"api.success"}

POST /api/task/scheduled/pagemonit/data {}
→ {"code":200,"data":{"list":[{"id":"6afe0d03b6b3e92e4e6d62ea",
     "url":"https://example.com/?pm_audit=page-monitoring-probe","hash":[],
     "md5":"ac573d5fbcf5eefe8ae812b0f20541bb","state":1,"project":"","time":""}],"total":1}}

mongo: PageMonitoring=1  PageMonitoringBody=0
scheduled: 仍只有 github_intel_tick   ← 断点 A：加 URL 不会建任何任务
```

### 4.3 打开调度（04:40，走 UI 等价 API）

```text
POST /api/task/scheduled/pagemonit/update {"hour":1,"allNode":true,"node":[],"scheduledTasks":true}
→ {"code":200,"message":"api.success"}

mongo scheduled: {"_id":"6afaa875b7221f4aa7eaa473","name":"Page Monitoring",
                  "schedule":"0 0-23/1 * * *","handler":"page_monitoring","params":"6afaa87…"}
app log: Successfully added new job {"job_id":"6afaa87…","cycle_type":"cron","next_run_time":"2026-11-19 05:00:00"}
```

### 4.4 断点 C 证据：节点收到裸 URL（04:46:41）

用 `CreatePageMonitoringTask` 的**等价路径**直接推 Redis（DEL + LPUSH 裸 url + RPUSH 任务消息，与旧代码字节级一致）：

```text
t+5s..t+30s   NodeTask:node-tx-sh-02 = 0   TaskInfo:page_monitoring = 0   ← 节点确实取走了
node log:
  04:46:41 [INFO] Get a new task: {"ID":"page_monitoring","type":"page_monitoring"}
  04:46:41 [INFO] Task begin: page_monitoring
  04:46:41 [INFO] Task end:   page_monitoring -        ← 同一秒，什么都没做
  04:46:41 [INFO] Task clean end: page_monitoring
mongo: PageMonitoring=1 (hash 仍 [])  PageMonitoringBody=0
```

（调试细节：第一次手工推 Redis 时本地 shell 把 JSON 引号吃掉，节点回 `Task parse error: invalid character 'I'…` 并**把毒消息原样重投**——顺带暴露节点侧 poison-message 死循环，见 §6。改用 `redis-cli -x` 精确字节重推后得到上面的干净证据。）

### 4.5 断点 B 证据：cron 真跑就是报错（05:00:00）

见 §2 日志。同次运行的 `scheduled` doc `last_run_time=2026-11-18T21:00:00.001Z`（UTC）= 05:00:00 +08，`next_run_time=22:00Z` 正常推进，说明**调度框架本身没问题，是 handler 内部报错**。

### 4.6 修复后行为验证（04:49:56 / 05:02:47）

把节点真正需要的 JSON（`{"url":…,"md5":…,"hash":[]}`，第二次由**复刻新版 `pageMonitPayload` 的 Go 程序**直接产出）推进 `TaskInfo:page_monitoring`：

```text
PAYLOAD {"url":"https://example.com/?pm_audit=page-monitoring-probe","hash":[],"md5":"ac573d5f…","statusCode":null,
         "similarity":0,"state":1,"project":"","time":"","rootDomain":"example.com","tags":null}

8s 后 mongo:
PageMonitoring: {"url":"…pm_audit=page-monitoring-probe","hash":["dec94c7d3adce52a"],
                 "time":"2026-11-19 05:02:47","rootDomain":"example.com","statusCode":[200]}
PageMonitoringBody: {"md5":"ac573d5f…","content":[<577 字节 HTML>]}   ← 快照落地
```

同一 URL 的第二轮（hash 非空、statusCode 一致）按节点逻辑判等、不重复写，符合预期。

### 4.7 cron 表达式语义实测（`%d 0-23/%d` 这层包装）

```text
0 0-23/24 * * *   next=2026-11-20 00:00  (+18h50m)   ← UI 写的 "24 hour"，实际=每天 00:00
0 0-23/1  * * *   next=2026-11-19 06:00  (+50m)      ← "1 hour" 才是每小时
0 0-23/48 * * *   next=2026-11-20 00:00             ← hour≥24 全部塌缩成每天 00:00
```

---

## 5. 修复（本仓库，已实现，未部署）

只改一个文件：`internal/scheduler/jobs/page_monitoring.go`（+85/−25）。

| 修复 | 内容 |
| --- | --- |
| B | `Find(ctx, nil, …)` → `Find(ctx, bson.M{}, …)`（根因，job 从「必失败」变「能跑」） |
| C | 新增 `pageMonitPayload`（`url/hash/md5/statusCode/similarity/state/project/time/rootDomain/tags`）+ `pageMonitProjection`；`getPageMonitoringData` 现在返回**每条的 JSON**，字段名与节点 `types.PageMonit` 对齐 |
| C′ | 脏数据兜底：`hash` 非空但 `statusCode` 为空时，把 `hash` 降级为 `[]`（走节点首轮基线分支），否则节点 `StatusCode[len-1]` 会 panic |
| D | 扇出改为遍历 `task.Node`（节点名），不再用 URL 当 key；空节点名跳过 |
| D′ | `allNode` 取节点改为 `GetNodeData(ctx, true)`（**只在线节点**）：`NodeTask:<node>` 没有 TTL，推给离线节点会留下永不被消费的消息 |
| — | 空数据 / 无节点 从「静默 return」改为 `logger.Warn`，排查时能区分「没调度」与「有调度但没数据」 |

核心片段（修复后）：

```go
func getPageMonitoringData() ([]string, error) {
    cursor, err := ScanSvc.pageMonitoringCollection.Find(context.Background(), bson.M{}, options.Find().SetProjection(pageMonitProjection))
    ...
    for cursor.Next(context.Background()) {
        var payload pageMonitPayload
        if err := cursor.Decode(&payload); err != nil { ... }
        if payload.URL == "" { continue }
        if len(payload.Hash) > 0 && len(payload.StatusCode) == 0 {   // 节点会 panic 的形状
            payload.Hash = []string{}
            payload.Similarity = 0
        }
        raw, err := json.Marshal(payload)
        ...
        payloads = append(payloads, string(raw))
    }
    return payloads, nil
}
```

验证：

```text
$ go build ./...            # EXIT=0
$ gofmt -l internal/scheduler/jobs/   # 无输出
```

（未改前端，故未跑 `pnpm build:pro`；如需可另跑。）

**未部署**：生产仍在跑旧镜像 `autumn27/scopesentry:v2.0.119`，所以线上行为仍是 §4.5 的报错；修复要生效需要重新构建镜像并滚动更新（本次未做）。

---

## 6. 剩余问题（未改，附最小建议）

| # | 问题 | 位置 | 最小建议 |
| --- | --- | --- | --- |
| 1 | **默认不调度**：种子文档没有 `scheduledTasks`，新装/老库都不会自动跑；用户必须在 UI 里手动打开开关 | `internal/database/mongodb/initdb.go:155`（新建）、`:336`（升级补种） | 种子加 `"scheduledTasks": true`（升级路径同样 backfill），或 `AddPageMonitTask` 里检测 job 不存在时 `addScheduler(...)` 自愈 |
| 2 | **「N hour」名不副实**：`0-23/N` 在 N≥24 时塌缩成每天 00:00；且 `UpdatePageMonitScheduledTask` 从不设置 `minute`，永远是整点 | `internal/services/task/scheduler/scheduler.go:366-380` | 用标准写法 `0 */N * * *`（N≤23）或直接给「daily/weekly」两档，UI 文案与 cron 对齐 |
| 3 | **md5 语义分裂**：人工入口 `md5=MD5(url)`（32 hex），插件 `md5=xxhash64(url)`（16 hex）。同一 URL 两条来源会产生两套 md5，节点按 md5 回写时只认自己收到的那套 | `internal/services/task/scheduler/scheduler.go:425` vs 节点 `modules/urlsecurity/pagemonitoring/pagemonitoring.go` | 统一成 xxhash64（或都用 MD5），并给老数据一次性回填 |
| 4 | **`PageMonitoringBody.md5` 唯一索引**：两个不同 URL 若正文完全相同会撞键，第二个 Upsert 失败 | `internal/database/mongodb/initdb.go:434` | 索引改成 `(md5)` 保留 + 内容按 `_id=urlmd5` 分桶，或把唯一性移到 `(_id)` |
| 5 | **读取页过滤 `hash.$size==2`**：首轮快照（只有 1 个 hash）在「页面监控结果」里看不到，用户第一次永远看不到数据 | `internal/services/assets/page_monitoring/page_monitoring.go:66` | 改成 `$size: {$gte:1}` 或按 `similarity` 存在与否过滤 |
| 6 | **`TaskInfo:page_monitoring` 无 TTL**，`NodeTask:<node>` 也无 TTL；任务废弃后 key 常驻 | `internal/scheduler/jobs/page_monitoring.go:52-75` | 复用 `internal/utils/redislist.PushQueued*`（带 TTL + 限长），与普通任务队列一致 |
| 7 | **节点侧静默跳过 + 毒消息死循环**（节点侧，本仓库无法修）：`json.Unmarshal` 失败只写 `SlogWarnLocal` 就 `continue`；而解析失败的 `NodeTask` 消息会被节点**原样重投**，形成每 1–2 秒一次的循环（实测 04:46:01–04:46:38 连续 18 次 `Task parse error`） | 节点 `internal/runner/pagemonitoring.go:45`、`internal/task/task.go:96-102` | ① 元素解析失败计数并在 >N 时 `SlogError` + 上报；② 任务消息解析失败要 **删除** 而不是重投，或加退避（指数 backoff） |
| 8 | **插件只吃 `.js`**：默认 `type=js`，非 js URL 直接跳过；把它挂进模板也几乎不产数据 | 节点 `modules/urlsecurity/pagemonitoring/pagemonitoring.go`（`tp := "js"`） | 若要「全量 URL 入库」应改默认值或在模板参数里显式给 `type=all` |

---

## 7. 本次写入与清理记录

**写入（全部为自有测试数据）**：

- `PageMonitoring` +1 条：`url=https://example.com/?pm_audit=page-monitoring-probe`，`_id=6afe0d03b6b3e92e4e6d62ea`
- `PageMonitoringBody` +1 条：`md5=ac573d5fbcf5eefe8ae812b0f20541bb`（577 字节正文）
- `scheduled` 临时 job：`_id=6afaa875b7221f4aa7eaa473`（handler=page_monitoring，已删）
- `ScheduledTasks` 的 `page_monitoring` 种子文档被 `update` 接口写过字段，已按原样还原

**清理后实测**：

```text
PageMonitoring=0   PageMonitoringBody=0
ScheduledTasks: [{"_id":"6afaa875b7221f4aa7eaa473","id":"page_monitoring","name":"Page Monitoring",
                  "hour":24,"node":[],"allNode":true,"type":"Page Monitoring","state":true}]   ← 与基线逐字段一致
scheduled:      [{"_id":"github_intel_tick","handler":"github_intel"}]                     ← page_monitoring job 已摘除
Redis: KEYS NodeTask:* = (空) ; TaskInfo:page_monitoring = (删)
git status:    仅 internal/scheduler/jobs/page_monitoring.go 属本次改动（其余 M/?? 为他人既有改动，未触碰）
```

其它：`_pmverify` / `_crontest` 临时程序已删除，SSH 转发隧道（27099）已关闭。

---

## 8. 复现/验证命令（可复用）

```bash
# 1) 计数与 job 状态
ssh prod-tx-sh-app-02 "docker exec -i scopesentry-mongodb mongosh -u admin -p *** --authenticationDatabase admin ScopeSentry --quiet --file /dev/stdin" <<'EOF'
print(db.PageMonitoring.countDocuments({}) + "|" + db.PageMonitoringBody.countDocuments({}))
printjson(db.scheduled.find({},{_id:1,handler:1,schedule:1,next_run_time:1}).toArray())
EOF

# 2) 触发一次调度（生产 UI 等价）
curl -s -X POST http://127.0.0.1:18082/api/task/scheduled/pagemonit/update \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"hour":1,"allNode":true,"node":[],"scheduledTasks":true}'

# 3) 看节点是否真的做了事（旧代码：0 秒 begin/end；新代码：body 出现）
ssh prod-tx-sh-app-02 "docker exec scopesentry-redis redis-cli -a $REDIS_PW --no-auth-warning LRANGE log:node-tx-sh-02 -6 -1"
```
