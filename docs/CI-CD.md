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

**端到端 27 秒**（实测 `TOTAL=27s result=PASS`）：拉源码+构建 2s、压缩 1s、传输 20s、替换 1s、冒烟 3s。

部署末尾带**冒烟 + 自动回滚**：冒烟四项（healthz / 版本号 / Mongo / 节点数）任一不过，
自动把 `/home/ubuntu/ScopeSentry.prev` 换回去、重启、再冒烟一次。

> 传输是唯一的大头，所以**二进制在开发机 zstd -9 压缩后再传**：100 MB → 27.5 MB（3x），
> 传输从 73 秒降到 19 秒。档位实测见下。

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

### zstd 档位实测（100 MB 二进制，开发机）

| 档位 | 体积 | 压缩耗时 | 传输(1.4 MB/s) | 结论 |
| --- | --- | --- | --- | --- |
| 3 | 30.5 MB | 0s | 22s | 可用 |
| **9** | **27.5 MB** | **1s** | **19s** | **选它** |
| 15 | 26.9 MB | 5s | 19s | 多花 4s 换 0.6 MB，不划算 |
| 19 | 24.3 MB | 17s | 17s | 省下的传输还不够补压缩时间 |

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
| 冒烟必须**轮询** healthz（最多 120s） | 刚 restart 时端口还没监听，单发一次 curl 拿到 `000` —— 我因此误触发了第一次回滚 |
| 冒烟结论用 `SMOKE_RESULT=PASS\|FAIL` 传递，不看退出码 | 实测 `sudo -n bash -c "exit 1"` 经 ssh 回来仍是 `0`，退出码不可靠 |

## 备用路径：生产机本机构建

`/opt/scopesentry/deploy-server-from-src.sh [branch] [--skip-download]`

生产机拉 tar.gz（20 分钟）+ `golang:1.24` 容器构建。开发机不可用时才用，
`--skip-download` 复用已解包源码树（改一行代码重打时只需 5 秒构建）。
