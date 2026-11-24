# 东京扫描节点（node-jp-01 / prod-cb-tyo-app-01）

主机：Contabo 东京，8 vCPU / 24 GB / 290 GB，Ubuntu 24.04，Tailscale `100.65.237.20`。

## 组成

| 文件 | 作用 |
| --- | --- |
| `compose.yaml` | 节点运行定义：`init: true`、4 CPU、12 GiB、`pids_limit 2048`、`restart: unless-stopped`、日志轮转、健康检查 |
| `Dockerfile` | 以官方 `autumn27/scopesentry-scan` 固定 digest 为基础，覆盖已核验的扫描二进制 |
| `healthcheck.py` | 读 Redis 心跳 `node:node-jp-01.updateTime`，超过 100 秒判不健康 |
| `fixture.py` | 自有 HTTP 夹具，仅监听 `127.0.0.1:18765` 与 `100.65.237.20:18765`，用于端到端烟测 |
| `provision-control.py` | 在控制面创建仅 `readWrite@ScopeSentry` 的专属 Mongo 账号，写 `node-jp-01` 初始配置 |
| `modules.yaml` | 低强度并发：`maxGoroutineCount: 1`，各模块 1–5 |

## 部署要点

1. 基础镜像从 Docker Hub 自行拉取并固定 digest；扫描二进制（sha256 `ec7eaa34…`）与三台国内节点一致，经 Tailscale 私网传输，不经公网。
2. 数据库连接走 Tailscale `100.115.186.92`，Mongo 账号只对 `ScopeSentry` 库有读写，Redis 使用控制面既有口令。
3. 节点以 `node-jp-01` 注册，`state: 1`。首次注册时程序会硬写 `state=1`，因此**用 `state: 2` 当暂停闸不可靠**：控制面的 `checkNodeTask` 只看任务归属节点，不看节点状态。真正停用节点必须停容器。
4. `docker stop` 后 `restart: always` 仍会在 Docker 守护进程重启时拉起，所以停用必须同时 `docker update --restart=no`。

## 启用 / 停用

```bash
# 启用（东京）
cd /opt/scopesentry/scan && docker compose up -d scan

# 停用（任一台国内节点）
docker update --restart=no scopesentry-scan && docker stop --time 90 scopesentry-scan

# 重新启用国内节点
docker update --restart=always scopesentry-scan && docker start scopesentry-scan
```

## 烟测

```bash
systemd-run --unit=ss-jp-fixture --property=RuntimeMaxSec=3600 /usr/bin/python3 /opt/scopesentry/scan/state/fixture.py
# 控制面建任务：target 100.65.237.20，模板 jp-node-owned-smoke-20261124（只开端口扫描 + URLScan）
# 夹具日志：/opt/scopesentry/scan/state/fixture-requests.jsonl
```

