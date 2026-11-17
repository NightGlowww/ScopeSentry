# 工程化约定

## 前端基线（Vue 3.5）

- Vue **3.5.13** + vue-router 4.6 + pinia 2.3 + element-plus 2.14 + vue-i18n 9.14；
  构建链升级到 `@vitejs/plugin-vue` 5.2、`vue-tsc` 2.2、TypeScript 5.6（Vite 仍为 5.0.6）。
- 升级由 `owl234/ARL-Next` 对齐任务驱动（其前端为 Vue 3.5），功能对齐路线见 `docs/ARL_NEXT_PARITY.md`。
- `vue-tsc` 在 CI 中仍是非阻塞（历史告警未清零）；升级后用 `pnpm ts:check` 查看当前基线。

## 本地命令（Makefile）

```bash
make help            # 列出目标
make build           # go build ./...
make binary          # 产出 dist/ScopeSentry
make test-hermetic   # 不需要 Mongo/Redis 的单元测试
make vet             # 全仓 go vet（CI 同款阻塞闸）
make fmt             # gofmt -w .
make fmt-check       # CI 同款：有未格式化文件就失败
make mod-check       # go mod tidy 后检查 go.mod/go.sum 是否漂移
make frontend-test   # 前端单元测试（vitest）
make frontend-lint   # 前端 ESLint（与 CI 同一条命令，--max-warnings 0）
make frontend-check  # ESLint + 生产构建 + 语言包/主题自检 + 单测
make ci              # 与 CI 三道阻塞闸一一对应：fmt/mod-check + go build + go vet + 单测 + 前端全套
```

前端另有：

```bash
cd frontend
pnpm install --no-frozen-lockfile
pnpm build:pro       # dist-pro
node scripts/check-locales.mjs
npx esno ./scripts/check-theme.ts
```

## CI

- 运行在自建 runner `dev-tx-sh-app-01-ss`（标签 `self-hosted,linux,x64,scopesentry-ci`，systemd 服务）。
- 触发：PR / push main / 手动 `workflow_dispatch`；同一 ref 的旧运行会被取消（concurrency）。
- 门禁：
  - **gofmt**（阻塞）
  - **go mod tidy 漂移**（阻塞）
  - `go build ./...`（阻塞）
  - hermetic 单测（阻塞）
  - 全仓 `go vet ./...`（阻塞；历史 144 条告警已清零）
  - 前端 `eslint --ext .js,.ts,.vue ./src --max-warnings 0`（阻塞）
  - 前端 `build:pro` + 语言包 + 主题自检（阻塞）
  - 前端 vitest（阻塞）
  - `vue-tsc`（非阻塞，历史类型错误较多）
  - **Integration job**（阻塞）：临时起 Mongo + Redis（随机端口、run_id 命名、跑完清理），
    先跑一个包把空库初始化一次，再 `go test ./...`；CI 上等价于 `make test-integration`
- Go 单测产出 `coverage.out` 并上传 artifact（`go-coverage`）。
- Go 模块走 `goproxy.cn`（这台机器到 proxy.golang.org 的 IPv6 无路由）；`setup-go` 的 Actions 缓存关闭，模块缓存在 runner 的 `~/go/pkg/mod` 常驻。

## Redis 日志上限

- 节点日志（`log:<node>`）与插件日志（`logs:plugins:<module>:<hash>`）都是 Redis list，
  写入统一走 `internal/utils/redislist.PushCapped`：`RPush` 后跟一次 `LTRIM key -N -1`，
  只保留最新 N 条。
- N 取 `logs.total_logs`（默认 1000）；配置缺失/为 0/负数时回落到 `redislist.DefaultMaxLen`，
  不会退化成「不裁剪」。改动前是「进程内计数到阈值就 DEL 整个 key」——重启归零、多实例各数一份，
  触发时历史日志全丢。
- **任务队列不能这样裁**：`NodeTask:<node>`、`TaskInfo:<id>` 是待消费数据，`LTRIM` 等于直接丢任务。

## 标签配色（issue #217）

- 标签颜色由标签名稳定推导：`utils/tagColor.ts` 的 `tagColorType(tag)`，同一个标签在任何页面、任何一次渲染里都同色。
- 只从 Element Plus 的语义色（`primary/success/info/warning/danger`）里挑，颜色由主题变量驱动，日间/夜间自动适配——
  不要在这里写死 hex，否则深色模式下对比度会崩。
- 用户覆盖：Mongo `TagColor`（tag 唯一）+ `POST /api/assets/tag_color/list` / `set`（`set` 传空 color 即清除覆盖）。
  资产页操作菜单「标签颜色」打开 `TagColorSetting.vue` 管理；保存/清除后触发列表重新拉覆盖表。
  合法值是语义色名或 `#rgb/#rrggbb`；前端 `tagColorProps(tag, overrides)` 合并——hex 走 `ElTag.color`，语义色走 `ElTag.type`。
  列表加载失败时静默退回默认配色，不影响渲染。

## 导出时间范围（issue #261）

- 导出请求可带 `startTime` / `endTime`，格式 `YYYY-MM-DD HH:mm:ss`，可以只给一边。
- `time` 字段就是这个格式的字符串，同格式下字典序即时间序，所以范围过滤直接比较字符串，不需要 `$dateFromString`。
- 合并逻辑在 `services/export/exportfilter.ApplyTimeRange`：base 里已有 `time` 条件时用 `$and` 合并而不是覆盖；
  格式非法或 start > end 直接 400。

## 删除项目时的资产归属

- 删除项目支持两种语义：`delA=true` 连资产一起删（`DeleteProjectAsset`），`delA=false` 只删项目。
- 「只删项目」时资产上的 `project` 字段必须清空（`UnsetProjectAssets`）：否则列表里会显示一个已删除的项目 ID，
  按项目筛选也永远命中不到，形成幽灵归属。
- `ProjectTargetData` 在两种语义下都会删除；项目级定时任务当前功能未启用（`CreateOrUpdateProjectSchedule` 调用被注释），
  启用时需要在这里补 `RemoveProjectSchedule`。

## 任务完成后自动同步到绑定项目

- `Task.BindProject`（MCP 建任务时可填）之前没有任何代码消费，等于“填了也没用”。
- 现在任务到 100% 时：`tasksync.AutoBindRequest(task)` 生成 `{ids, option=existing, project}`（项目为空/非法 ObjectID 直接跳过），
  后台 goroutine 取出任务目标 → `project.SyncTaskTargets(ctx, projectID, targets)` 并入项目、按出现顺序去重、重算资产关联。
- 自动同步失败只记日志，不影响任务完成状态；同步是幂等的，重复跑只会新增缺失的目标。
- `SyncTaskTargets` 是 `SyncTargets` 的 context 版本：后台没有 gin 上下文，这条路径只用 `context.Context`。

## 手工录入资产（issue #203）

- `POST /api/assets/asset/add`，body `{host, ip, port, url, service, title, project, tags}`：
  资产平时由扫描节点直接写库，这里给「外部 recon 结果 + 人工确认过的主机」一个入口。
- `host` 必填；`url` 必须是绝对地址，并据此推导 `type`（http/https）；`time` 由服务端填。
- **host+port 是唯一索引**：重复录入返回 **409** `ErrAssetExists`（不是 500）；非法参数 400 `ErrAssetInvalid`。
- 前端入口在资产页操作菜单「添加资产」（`AssetAddDialog.vue`）；错误提示交给 axios 拦截器统一弹（含 409），组件只负责成功后刷新列表。
- 批量：`POST /api/assets/asset/add_batch`，body `{items: [...], project, tags}`，一次最多 **500** 条；
  逐条复用单条校验与唯一索引处理，返回 `{inserted, duplicated, invalid, errors}`（错误最多回传 20 条），
  前端弹窗里的「批量导入」模式把每行一个 host 转成 items，共享的 project/tags 由服务端补到每条。

## 资产备注（issue #335）

- `Asset` 新增 `remark` 字段（扫描不会写，只有用户改）。
- `POST /api/assets/asset/remark`，body `{id, remark}`：**定向 `$set` 只改 remark**——
  扫描结果随时可能被节点写入，整文档覆盖会把用户没改过的字段一起冲掉。
- 服务端 `TrimSpace` 后按**字符数**限长 500（空串表示清空），非法 ObjectID 返回 400 而不是 500。
- 资产列表的「备注」列是行内编辑：失焦或回车保存，失败回滚到上一次保存成功的内容；
  `GetAssets` 的投影里要带 `remark`，否则列表拿不到这个字段。

## 按筛选条件批量删除（issue #258 / #191）

- `POST /api/assets/common/delete_by_search`，body `{index, search, filter, confirmAll}`：
  `search` / `filter` 与列表页同源（asset 走 `BuildSearchQuery`，其它集合走 `helper.GetSearchQuery`）。
- 分批删：每次先查 1000 个 `_id` 再按 `_id` 批量删，避免单次 `deleteMany` 长时间持锁；
  单次请求上限 `DeleteBySearchMaxPerRun = 50000` 条，剩下的再点一次，防止一个 HTTP 请求把网关拖超时。
- **空筛选等于清空整个集合**：必须显式 `confirmAll: true`，否则返回 `ErrNeedConfirmAll`（400）。
- 副作用与按 ID 删除保持一致：asset 清理不再被引用的 `HttpBody`，vulnerability 清理 `vulnerabilityDetail`（按 hash）。

## 目标级暂停（issue #277）

- `POST /api/task/target/pause` / `resume`，body `{taskId, target}`。
- 暂停语义是「这个目标先不扫」：还没被节点领取的目标从 `TaskInfo:<taskId>` 里 `LREM` 摘掉，并 `taskNum-1`——
  这样进度仍然能走到 100%；**已经领取/扫完的目标不会被中断**，只记录在 `TaskTargetControl` 里，不扣 `taskNum`。
- 恢复：删暂停记录 → `LPUSH` 回队列头（优先重扫）→ `taskNum+1`；已完成的任务只清记录、不再塞回队列。
- 幂等：同目标重复暂停不会重复扣减（先查 `TaskTargetControl`）。
- 进度详情接口会带上 `paused` 标记；任务进度表的「操作」列按状态显示「暂停/恢复」，暂停前弹确认（说明已领取的目标会继续跑完）。

## 停止/删除任务时清队列

- 只把任务状态置为暂停（2）并广播 `stop_task` 不够：任务消息还躺在 `NodeTask:<node>` 里，
  节点如果在停止之后才上线（或重启），会把这条消息当新任务领走——用户看到的“已停止”任务又跑起来。
- `StopTasks` 与 `ClearTaskCache` 会调用 `taskRepo.RemoveQueuedNodeTasks`：扫描 `NodeTask:*`，
  按 payload 里的任务 ID（模板是小写 `id`、页面监控是大写 `ID`，两种都认）用 `LREM` 精确删除，
  不影响同一队列里的其它任务。
- 停止只清“还没被领取”的消息；节点正在跑的目标仍然只能靠它自己响应 `stop_task`。

## 任务进度快照（issue #278）

- 节点把逐目标进度写在 Redis 的 `TaskInfo:progress:<taskID>:<target>` 哈希里；任务到 100% 时服务端会删
  `TaskInfo:tmp/time`，Redis 数据也可能被清理或重启丢掉，前端「进度详情」随之变空。
- 现在完成瞬间先把这些哈希快照进 Mongo `TaskProgressSnapshot`（每个目标一行，`data` 原样保存），
  接口在 Redis 读不到某目标的数据时回放快照；只有真的 miss 才查一次库，进度轮询不会每轮打 Mongo。
- 删除任务时（`DeleteMany`）与 `ClearTaskCache` 会连带删快照，不留孤儿文档；`taskID` 有索引。
- 快照失败只记日志，不影响任务状态更新——它是补充数据，不是主链路。

## 节点相关队列的清理

- `refresh_config:<node>` 是「状态型」消息（指纹/字典/系统配置变更后通知节点重载），节点只关心近期几条。
  以前是裸 `RPush`：节点离线时无限堆积，删节点后 key 也没人清。现在走 `redislist.PushQueuedCapped`——
  每次写入后 `LTRIM` 只留最新 100 条，并带 7 天滑动 TTL。
- `DeleteNodes` 现在一并删除 `node:<name>` / `refresh_config:<name>` / `NodeTask:<name>`，
  不再留下永远不会被消费的孤儿 key。

## Redis 队列 TTL

- 任务队列以前只在「删除任务」时清（issue #200）：节点长期离线/被移除后，消息会永久留在 Redis 里。
- 现在 `NodeTask:<node>` 与 `TaskInfo:<taskID>` 走 `redislist.PushQueued / PushQueuedHead`：
  每次投递后 `EXPIRE` 续期，存活窗口 `TaskQueueTTL = 30 天`。一直在投递的队列不会中途过期，
  没人消费的最后一条消息 30 天后回收。取 30 天是留给运维窗口——节点维护一周以上很常见，TTL 太短会清掉还在排队的任务。
- 节点重新上线时 `checkNodeTask` 会重新投递待开始/运行中的任务，所以过期丢掉的只是早就没人领的旧消息。
- 节点侧直接写的那几个 key（`TaskInfo:tmp/progress/time` 的进度明细）本仓库只读，
  TTL 需要 scan 节点写入时自己设置；服务端能做的已做完。

## 节点存活判定

- 节点心跳写在 Redis `node:<name>` 哈希里（`state` / `updateTime`），服务端只读。
- 判定用 `ClassifyLiveness`：同一时间字符串按「服务器时区」和「UTC」各解释一次，取与服务器时间最接近者；
  因此节点与服务器时区不同（例如节点跑 UTC）不会再被误判离线，节点时钟略快也只标记 ClockSkew。
- 超时窗口默认 300s，可用 `NODE_TIMEOUT_SECONDS` 覆盖（节点多、网络慢时可放宽）。
- 时间字符串解析失败时保留节点并打 Warn，而不是把它从列表里抹掉。

## 探活

- `GET /healthz`：ping Mongo + Redis，正常 200 `{status:"ok",version,time,checks:{mongo,redis}}`；任一依赖不可用返回 **503**。
- K8s 的 readiness / liveness 已指向该端点（`k8s/pods/scope-sentry-deployment.yml`）。

## 测试分层

| 层 | 范围 | 运行方式 |
| --- | --- | --- |
| hermetic | 纯逻辑/中间件，不连库 | `make test-hermetic` |
| integration | 需要 Mongo/Redis（包 init 就会连库） | 本地起容器后 `make test-integration` |
| api smoke | 真实路由 + 中间件 + Mongo/Redis：/healthz、未认证 401、登录拿 token、带 token 访问受保护接口；新增接口的鉴权与参数校验（remark / delete_by_search / export 时间范围）也在真实路由上验一遍 | 同上（`internal/api/routes/smoke_integration_test.go`） |
| frontend | ESLint + 语言包/主题自检 + 单元测试（vitest + jsdom） | `make frontend-check` |

新增测试优先放叶子包（不 import `internal/bootstrap`），否则包级 init 会直接连库、CI 无法运行。

**新增 CI 阻塞步骤必须同步进 Makefile**（`make ci` 要能本地复现）。ESLint 就是因为只在 CI 里跑，
本地全绿、push 之后才被打回——这类往返本可以避免。

## 本地 Git hook（可选）

```bash
git config core.hooksPath .githooks   # 启用 pre-commit
```

pre-commit 会做两件事：

1. 暂存的 `.go` 文件必须已 `gofmt`（`make fmt` 修）。
2. 暂存的 `frontend/src/**` 文件必须通过 ESLint——**只在 `frontend/node_modules/.bin/eslint` 存在时才跑**，
   没装依赖的贡献者只会看到一行「跳过」提示，不会被卡住；CI 里 ESLint 始终是阻塞闸。

## 静态图片尺寸

- 图片按「展示尺寸 ×2（Retina）」给，别直接放设计稿原图：logo 显示 40–100px，1024×1024 的 PNG 属于纯浪费。
- 本轮实测：`logo.png` **664KB → 76KB**（1024²→256²，登录页 + `index.html` 首屏 loading 都用它）；
  `avatar.jpg` **664KB → 9.5KB**（其实是张 1024² 的 PNG，转成 128² JPEG）；
  `wx.jpg` 117KB→38KB、`zfb.png` 137KB→98KB（二维码 512px 足够扫）。
- 二维码不要顺手转 JPEG 追求更小：压缩噪点会影响识别率，512px 的 PNG 已经够用。

## 首屏体积与 manualChunks 的坑

- monaco 只在指纹编辑 / 资产 diff 里用，必须保持**按需加载**：源码里只 `import type`，运行时 `await import()`。
- 光这样还不够——`manualChunks` 把 monaco 和其它 node_modules 一起归到 `vendor` 时，入口 chunk 会通过
  共享 helper 静态依赖到它，Vite 就会把 `vendor-monaco` 放进 `modulepreload`，等于首屏白拉 ~962KB(gz)。
  **规则**：按需加载的重库要在 `manualChunks` 里显式 `return undefined`，让它单独成异步 chunk，别落进 vendor。
- 实测（`vite build --mode pro`，gzip）：首屏 modulepreload 的 JS 从 **1777KB → 815KB**（−54%）；
  monaco 变成 `editor.main-*.js`（832KB gz）异步 chunk + 不再预加载 130KB 的 monaco CSS。

## 大结果集合的 time 索引

- 所有资产类列表都按 `time` 倒序分页；`UrlScan` / `crawler` / `SensitiveResult` / `DirScanResult` /
  `vulnerability` / `PageMonitoring` 之前没有 `time` 索引，查询会退化成全集合 `SORT`。
- 实测（Mongo 7，20 万条文档，`sort({time:-1}).limit(20)`）：无索引 **29ms / stage=SORT / docsExamined=200000**；
  加 `{time:-1}` 后 **3ms / stage=LIMIT / docsExamined=20**。除延迟外，大集合的 blocking sort 还有 100MB 内存上限风险。
- 索引在 `initdb.createIndexes` 创建；`initdb_integration_test.go` 会断言这六个集合都有 `time` 索引，防止以后漏掉。

## 仓库规范

- `.editorconfig`：Go 用 tab，其余 2 空格；LF。
- `.gitattributes`：统一 LF，图片/字体标记为 binary。
- PR 模板在 `.github/pull_request_template.md`，要求写清验证项。
- Dependabot 每周检查 gomod / npm / GitHub Actions。

## 分支与发布

- 功能分支 → PR → main；CI 绿后合并。
- 版本由 `.goreleaser.yaml` 在 tag 时注入 `internal/constants.Version`。
