# 工程化约定

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
| api smoke | 真实路由 + 中间件 + Mongo/Redis：/healthz、未认证 401、登录拿 token、带 token 访问受保护接口 | 同上（`internal/api/routes/smoke_integration_test.go`） |
| frontend | ESLint + 语言包/主题自检 + 单元测试（vitest + jsdom） | `make frontend-check` |

新增测试优先放叶子包（不 import `internal/bootstrap`），否则包级 init 会直接连库、CI 无法运行。

**新增 CI 阻塞步骤必须同步进 Makefile**（`make ci` 要能本地复现）。ESLint 就是因为只在 CI 里跑，
本地全绿、push 之后才被打回——这类往返本可以避免。

## 本地 Git hook（可选）

```bash
git config core.hooksPath .githooks   # 启用 pre-commit：暂存的 .go 文件必须已 gofmt
```

## 仓库规范

- `.editorconfig`：Go 用 tab，其余 2 空格；LF。
- `.gitattributes`：统一 LF，图片/字体标记为 binary。
- PR 模板在 `.github/pull_request_template.md`，要求写清验证项。
- Dependabot 每周检查 gomod / npm / GitHub Actions。

## 分支与发布

- 功能分支 → PR → main；CI 绿后合并。
- 版本由 `.goreleaser.yaml` 在 tag 时注入 `internal/constants.Version`。
