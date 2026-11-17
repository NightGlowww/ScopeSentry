# 部署

本文覆盖三种场景：单机部署、**国内网络拉镜像超时**（issue #283）、分布式多节点部署（issue #321）。

## 1. 单机部署

前置：Docker 20.10+ 与 Compose v2（`docker compose version` 能看到版本）。

```bash
git clone https://github.com/Autumn-27/ScopeSentry.git
cd ScopeSentry
cp .env.example .env   # 没有 .env.example 时直接编辑仓库里的 .env
# 修改 .env 里的 MONGO_INITDB_ROOT_USERNAME / MONGO_INITDB_ROOT_PASSWORD / REDIS_PASSWORD
docker compose -f single-host-deployment.yml up -d
```

起来后一共四个容器：`scope-sentry`（服务端 + Web）、`scopesentry-scan`（默认扫描节点）、`scopesentry-mongodb`、`scopesentry-redis`。

```bash
docker compose -f single-host-deployment.yml ps      # 等 healthcheck 变 healthy
curl -f http://127.0.0.1:8082/healthz               # 200 才代表 Mongo/Redis 都通
docker logs scope-sentry                            # 初始密码与插件二次验证密码
```

> `scope-sentry` 的 healthcheck 打的就是 `/healthz`：它会同时 ping Mongo 与 Redis，任一不可用返回 503，
> 所以 `healthy` 不是「进程活着」，而是「依赖都通」。

## 2. 国内网络拉镜像超时（issue #283）

Docker Hub 在国内经常超时。按代价从低到高有四种做法：

### 方案 A：给 Docker 守护进程配镜像加速

编辑 `/etc/docker/daemon.json`（Windows/macOS 在 Docker Desktop 的 Docker Engine 设置里）：

```json
{
  "registry-mirrors": ["https://docker.m.daocloud.io"]
}
```

然后 `sudo systemctl restart docker`。注意：镜像加速只对 Docker Hub 生效，私有仓库仍需直连。

### 方案 B：用仓库自带的镜像覆盖文件（推荐）

仓库里有 `single-host-deployment.cn.yml`，只把四个镜像换成带镜像源前缀的地址，其余配置全部沿用主文件：

```bash
docker compose -f single-host-deployment.yml -f single-host-deployment.cn.yml up -d
```

换源时只改这个文件里的前缀即可（例如换成你所在环境可用的公共镜像），不用动主 compose。

### 方案 C：本地构建镜像

服务端镜像可以用仓库里的 `dockerfile` 自己构建：

```bash
cd frontend && pnpm install --no-frozen-lockfile && pnpm build:pro && cd ..
docker build -t autumn27/scopesentry:latest -f dockerfile .
```

扫描节点镜像在独立仓库 [ScopeSentry-Scan](https://github.com/Autumn-27/ScopeSentry-Scan) 里构建。
本地构建完成后，compose 会直接使用本地同名镜像，不再拉取。

### 方案 D：离线导入

在能联网的机器上：

```bash
docker pull mongo:7.0.28 && docker pull redis:7.0.11
docker pull autumn27/scopesentry:latest && docker pull autumn27/scopesentry-scan:latest
docker save -o scopesentry-images.tar mongo:7.0.28 redis:7.0.11 autumn27/scopesentry:latest autumn27/scopesentry-scan:latest
```

拷到目标机器后 `docker load -i scopesentry-images.tar`。

### 改完先校验

```bash
make compose-check   # 等价于 docker compose -f ... config -q，两个文件都校验
```

## 3. 分布式多节点部署（issue #321）

架构：**一份服务端**（含 Mongo/Redis）+ **N 个扫描节点**。扫描节点无状态，扩容就是加节点；
节点名（`NodeName`）必须唯一，重复会让任务分发和日志串台。

### 节点部署

节点镜像与 compose 在独立仓库 [ScopeSentry-Scan](https://github.com/Autumn-27/ScopeSentry-Scan)：

```bash
git clone https://github.com/Autumn-27/ScopeSentry-Scan.git
cd ScopeSentry-Scan/build
# 编辑 .env：MONGODB_IP/PORT/USER/PASSWORD、REDIS_IP/PORT/PASSWORD、NodeName
docker compose -f scan-docker-compose.yml up -d
```

关键点：

- 节点要能**直连服务端的 Mongo 与 Redis**。同机部署时节点用 `network_mode: host` 走 `127.0.0.1`；
  跨机部署时把 27017 / 6379 暴露到内网，并在防火墙/安全组里只放行节点 IP。
- 节点注册成功会写 `Register Success` 日志，之后服务端会把待开始/运行中的任务重新投递给它，
  所以节点临时离线不会丢任务。
- 节点侧不存业务状态，重装/加机器不需要迁移数据。

### Kubernetes

仓库 `k8s/` 下是可直接套用的清单（Deployment / Service / PVC / Secret），已用 `kubeconform -strict` 校验。
适合已有集群的环境；单机小规模用 compose 更省事。

## 4. 常见问题

- **端口被占用**：8082（Web）、27017、6379。改 compose 里左侧端口即可，节点侧连的仍是服务端实际端口。
- **时区**：compose 通过 `TIMEZONE`（服务端）与 `TimeZoneName`（节点）设置，默认 `Asia/Shanghai`；
  节点与服务端时区不同也不会再被误判离线（见 `docs/engineering.md` 的节点存活判定）。
- **容器 healthy 但页面打不开**：先 `curl /healthz` 看是 200 还是 503，503 说明依赖没通。
