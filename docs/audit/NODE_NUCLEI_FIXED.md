# 节点侧 nuclei 修复（2026-11-19）

## 三个串联缺陷（都已修复）

| # | 缺陷 | 后果 | 修复 |
| --- | --- | --- | --- |
| 1 | 容器 config.yaml 是 `NodeName: node-test`，与 compose env 的 `node-tx-sh-02` 不一致，**config 优先** | 任务投到 `NodeTask:node-tx-sh-02`，节点从 `NodeTask:node-test` 消费 → **任务静默不执行**（队列堆积、进度 0） | 修 `/apps/config/config.yaml`，日志确认 `Register Success:node-tx-sh-02` |
| 2 | `-t *` 直接把 `/apps/poc` 目录塞给 `nuclei.TemplateSources{Templates:[dir]}` | 内嵌引擎不加载目录内的扁平 yaml → **0 命中**（跑满 5-6 分钟无结果） | 新增 `expandPocDir()`，目录展开成 14319 个文件路径 |
| 3 | 14319 个模板塞进**同一个**引擎实例 | 长时间无产出（实测 5-6 分钟 0 命中），且中途取消会卡住任务循环 | 改**分批执行**：每批 300 模板、独立引擎、逐批日志 |

## 验证证据

- 单模板小目录 + 完整上游模块 → **60 秒内产出 2 条 vulnerability 记录**（`{"url":"https://example.com","level":"high"}`）
- 批处理日志：`batch 22/48 done (300 templates)`，每批约 30-35s
- 节点注册正确：`Register Success:node-tx-sh-02 - version 1.9.1`

## 已部署

- 镜像 `autumn27/scopesentry-scan:nuclei-batch`（`docker commit` 保留改动）
- 生产 compose 已指向该镜像
- 节点配置 NodeName 已修正

## 仍待解决

1. **全量 14319 模板一轮约 28 分钟**（48 批 × 35s）。当前节点单 worker 串行，期间其它任务排队。需要：调大批次（如 1000）或并行多 worker。
2. **取消任务会卡住节点任务循环**（实测队列堆积不消费，需 `docker restart scopesentry-scan`）。批循环里需要在每批开始检查 ctx 并清理引擎。
3. **vulnerabilityDetail 孤儿**：任务删除（delA）会删 vulnerability 主表，但 detail 留在库里。
4. 节点仓库（`Autumn-27/ScopeSentry-Scan`）未提交 PR —— 改动仅存在于生产镜像与本地 clone，**下次重建容器会丢失**，需尽快把 patch 提交上游或自建 fork。

