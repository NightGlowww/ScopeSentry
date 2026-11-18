# 生产部署注意（ScopeSentry fork）

## 二进制投递

- **不要用 `/tmp` 暂存二进制**：生产机 `/tmp` 是 1.9G 的 tmpfs，几次版本累积就会写满，`scp` 会**静默截断**（曾出现 92M 变 57M），`docker cp` 进容器后服务起不来。
- 固定投递目录：`/opt/scopesentry/deploy/`。
- **每次必须校验**：`scp` 后比对 `stat -c %s` 与本地大小、`sha256sum` 与本地一致，再 `docker cp`。

## 发布步骤（当前）

1. 构建：`frontend: pnpm build:pro` → `rsync dist-pro cmd/main/static`（保留 /map 资源）→ `go build linux/amd64`，ldflags 注入 `version.json.server`。
2. 投递到 `/opt/scopesentry/deploy/ScopeSentry-vX.Y.Z` 并校验 sha256。
3. `docker cp` 到 `scope-sentry:/opt/ScopeSentry/ScopeSentry` → `chmod +x`。
4. `docker commit scope-sentry autumn27/scopesentry:vX.Y.Z`。
5. `docker-compose.fork.yml` 里 `image` 改成新 tag。
6. `docker compose -f single-host-deployment.yml -f docker-compose.fork.yml up -d --no-deps scope-sentry`。
7. `curl /healthz` 校验 `version` 字段。

## 镜像清理

`docker commit` 每次约 +130MB（保留历史层）。保留「当前版本 + 上一版本」即可：

```bash
docker rmi autumn27/scopesentry:v2.0.0 …   # 旧版本
```

## 已知坑

- `docker cp` 不会校验完整性；截断的二进制在容器里表现为启动即退出，日志只有一句 `exec format error`／无输出。
- 修改 `docker-compose.fork.yml` 前先 `cp` 备份（脚本里带时间戳）。
- 前端改动必须重建**服务端二进制**（前端是 `go:embed` 进二进制，不是容器里挂文件）。

## 一键发版脚本

`scripts/release.sh <version>` 把之前的散步骤串起来：

1. 审计门禁 `scripts/check-ui-audit.py`（失败即中止，可用 `SKIP_AUDIT=1` 跳过）；
2. 写 `version.json`；
3. `scripts/build-linux.sh`（前端 + 内嵌二进制）；
4. 本地 sha256 → scp → 远端 sha256 比对（不一致直接退出）；
5. `docker cp / commit / compose up`；
6. 打 `/healthz`。

`DRY_RUN=1` 只跑门禁并打印计划，不构建、不上传。

注意：两个脚本都踩过同一个坑——`cd` 在设置了 `CDPATH` 的环境里会**打印**解析结果，命令替换 `$(cd ... && pwd)` 会把两行路径都吞进去，导致 `ROOT` 变成「路径A\n路径A」。修法是 `cd ... >/dev/null 2>&1 && pwd -P`。
