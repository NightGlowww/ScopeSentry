# 待部署清单（生产停留在 v2.0.128）

> 2026-11-19 06:35 生产主机失联（sshd 无 banner、HTTP 无响应），此后所有改动都只在 main 分支，未上线。
> 主机恢复后按本清单一次性部署并逐项验收，避免逐个补版本。

## 一键恢复

```bash
scripts/recover.sh 2.0.131     # 自检 → 部署 → 重建 scopesentry-scan（限载生效）→ 复检
```

## 待上线内容（v2.0.128 → main）

| 提交 | 内容 | 验收方式 |
| --- | --- | --- |
| bc9d6fe | 策略 warning 落库 + 任务列表「策略提示」列 | 用停用策略建任务，任务行显示 `1 条` 且 tooltip 有 `policy_unavailable` |
| a52428e | `/healthz` 增加 host（磁盘/负载/内存）与 warnings；`scripts/healthcheck.sh` | `curl /healthz` 应含 `host` 字段；`bash scripts/healthcheck.sh` 全绿 |
| f60f791 | `/api/console/info` hostStats；系统状态页三卡；k8s 资源限载 | 打开配置→系统状态，三卡有值；`kubectl get deploy scan -o yaml` 可见 limits |
| eca0e21 | MCP `get_system_info`（工具面 59）+ 发布产物保留 3 份 | `tools/list` 计 59；`ls /opt/scopesentry/deploy` 只剩 3 个 v* |
| 27802bc | 磁盘 <3% 拒绝启动扫描 | 单测 `go test ./internal/utils/diskguard/` 通过（生产不便造磁盘满） |
| cbb7e26 | compose 三个文件的 scan 容器 cpus/mem/pids 限载；发布前置探活；半截上传清理 | `docker inspect scopesentry-scan` 的 NanoCpus/Memory 非 0 |
| af17e0a | 删除死代码 icon worker；`scripts/recover.sh` | 启动日志不再有 IconHandle 循环；脚本可执行 |
| 9dce6e3 | 资产表产品列溢出修复 | 资产页产品列 `scrollWidth <= clientWidth` |
| d75cdae | 节点 nuclei `-t` issue 草稿（文档，无需部署） | 无 |

## 部署后仍需人工确认的三件事

1. `docker inspect scopesentry-scan` 的 `NanoCpus=2000000000` 且 `Memory=2147483648`（限载真生效，`compose up -d --no-deps scopesentry-scan` 会重建容器）；
2. `/opt/scopesentry/deploy` 只保留最近 3 个 `ScopeSentry-v*`；
3. 页面监控 `scheduled` 中 `page_monitoring` 仍为 `0 0-23/24 * * *`，next run 次日 00:00。

## 已知未解（不在本次部署范围）

- 漏洞链路：节点 nuclei 只接受单文件 `-t`，需节点仓库修复（issue 草稿见 `docs/audit/NODE_NUCLEI_ISSUE.md`）；
- `icon` 集合本身待 `db.icon.drop()`；
- `app/mp/cip/fileleak/npoc` 无生产者，已在页面标注。
