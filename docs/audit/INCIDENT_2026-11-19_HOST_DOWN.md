# 事故记录：生产主机失去响应（2026-11-19 06:35 起）

## 现象

```
ping 111.229.44.240     → 通（24ms，0% 丢包）
TCP 22 / 8082 connect   → 均 open
但：SSH banner exchange timeout；GET /healthz 8s 无响应；nc 连上后 0 字节返回
本机 127.0.0.1:18082 隧道 → 000（隧道进程已断）
```

内核仍在接受连接、用户态不再响应，属主机级资源耗尽或 I/O 阻塞，不是应用代码问题。

## 时间线

| 时间 | 事件 |
| --- | --- |
| 06:32 | v2.0.128 部署成功，healthz 正常 |
| 06:35 | `release.sh 2.0.129` 在 [5/7] 上传阶段 scp 超时退出（未执行 docker cp/commit/compose up，生产仍是 2.0.128） |
| 06:36–06:45 | 多次复探：ping 通、端口 open、无 banner/HTTP |

## 可疑诱因

1. 当日为验证漏洞链路，在 `scopesentry-scan` 里跑了多次 `nuclei -t *`（每次 ~5 分钟全量模板）并反复 `docker restart`；扫描容器与宿主共享 network_mode: host 且未设资源上限。
2. 失败部署可能残留 ~100MB 二进制在 `$DEPLOY_DIR`，若磁盘告警会放大问题。

## 已落地的防护（本仓库）

1. **扫描容器限载**：`single-host-deployment*.yml` 的 `scopesentry-scan` 增加 `cpus: 2.0` / `mem_limit: 2g` / `pids_limit: 4096`，避免单个扫描把整机拖死。
2. **发布前置探活**：`scripts/release.sh` 新增 `[0/7] 生产探活`，`ssh` 内 `curl /healthz` 不通就直接失败，不再白构建 + 上传半截。
3. **半截上传清理**：sha256 不一致时自动 `rm -f` 远端文件，避免 DEPLOY_DIR 堆垃圾。

## 恢复步骤（需在云控制台操作）

1. 控制台查看 CPU/内存/系统盘曲线，确认是否打满；
2. 重启实例；
3. 确认容器起来：`scope-sentry`、`scopesentry-mongodb`、`scopesentry-redis`、`scopesentry-scan`；
4. `curl http://127.0.0.1:8082/healthz` 应返回 `{"version":"2.0.128"}`；
5. 重新执行 `scripts/release.sh 2.0.129`（或更高版本）完成积压发布。

## 待办

- 恢复后把扫描容器限载应用到生产 compose（`docker compose up -d scopesentry-scan` 需重建容器）；
- 给 `nuclei` 的全量模板用法在节点侧加硬上限（当前 UI 只提示，不阻断）；
- 磁盘水位监控：`/healthz` 增加磁盘可用空间字段，低于阈值直接告警。
