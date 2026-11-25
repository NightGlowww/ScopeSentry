# CI/CD：构建与部署

## 现状（2026-11-25 起）

```
Mac ──git push──▶ GitHub                                    （改代码）
                     │
                     │ 4.9 MB/s      开发机 dev-tx-sh-app-01（43.143.38.5）
                     ▼               原生 Go 1.25.5，4 核
              git fetch --depth 1
                     │
                     │ go build（冷 47s / 热 5s）
                     ▼
              scp 100 MB → prod        1.4 MB/s ≈ 73s
                     │
                     ▼
         prod-tx-sh-app-01（1.15.46.212）
         docker cp → mv → restart → healthz
```

**端到端 80 秒**（实测 `TOTAL=80s`），其中传输 73 秒占大头。

## 为什么不是 Mac 直传，也不是生产机自己拉

| 通道 | 实测 | 结论 |
| --- | --- | --- |
| Mac → 生产 scp | 1 MB/s，但**会在中途被切断**（part 3 连挂 8 次） | 不可用 |
| 生产 → GitHub `codeload` | **39 KB/s**，40 MB ≈ 20 分钟 | 太慢 |
| 生产 → GitHub `git clone` | **300 秒超时** | 不可用 |
| 生产 → `objects.githubusercontent.com` | 8 KB/s | 不可用 |
| **开发机 → GitHub** | **4.9 MB/s** | ✅ |
| **开发机 → 生产 scp** | **1.4 MB/s** | ✅ |
| 生产 → Docker Hub（`golang:1.24`） | 245 秒拉完 1.3 GB | 备用（本机构建） |

## 用法

```bash
ssh dev-tx-sh-app-01 ~/ss-build/deploy.sh          # 部署 main
ssh dev-tx-sh-app-01 ~/ss-build/deploy.sh <branch> # 部署指定分支
```

脚本做的事（五步，任一步失败即退出）：

1. `git fetch --depth 1` + `reset --hard origin/<branch>`
2. 从 `version.json` 读版本（**不要加 `v` 前缀**，前端会自己加，否则界面显示 `vv2.0.387`）
3. `go build -trimpath -ldflags "-X ...constants.Version=$VERSION"`（不注入就是 `dev`，更新检查也会因为不可比而永不提示）
4. `scp` 到生产 `/tmp/ScopeSentry.new.tmp`（`/home/ubuntu` 是 root 拥有，ubuntu 用户写不进去）
5. 远端：`docker cp` 到 `.next` → `mv`（**不能直接覆盖运行中的二进制**，会 SIGKILL）→ `restart` → healthz 轮询

## 几个必须遵守的细节（都是踩出来的）

| 细节 | 原因 |
| --- | --- |
| 构建前 `rm -f` 旧产物，且产物为空即退出 | 否则上一次的二进制会被当成「构建成功」部署出去（实测连部署三轮旧版） |
| 远端 heredoc 必须 `<<'REMOTE'` 加引号 | 不加引号 `$c`/`$(seq)` 会在本地展开，远端脚本直接崩 |
| 用 `mv` 而不是 `cp` 覆盖运行中的二进制 | `cp` 就地重写会让正在运行的进程被内核 SIGKILL |
| 版本号不加 `v` 前缀 | UI 会自己加，否则 `vv2.0.387` |

## 备用路径：生产机本机构建

`/opt/scopesentry/deploy-server-from-src.sh [branch] [--skip-download]`

生产机拉 tar.gz（20 分钟）+ `golang:1.24` 容器构建。开发机不可用时才用，
`--skip-download` 复用已解包源码树（改一行代码重打时只需 5 秒构建）。
