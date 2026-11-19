# 节点侧 nuclei 修复（2026-11-19）— 已完成

## 修复的三个缺陷

| # | 缺陷 | 后果 | 修复 | 状态 |
| --- | --- | --- | --- | --- |
| 1 | 容器 config.yaml 是 NodeName: node-test，覆盖 compose env | 任务投到 NodeTask:node-tx-sh-02，节点从 node-test 消费 → 任务静默不执行 | 修正 config.yaml，日志确认 Register Success:node-tx-sh-02 | 已部署 |
| 2 | -t * 把目录塞进 TemplateSources{Templates:[dir]} | 内嵌引擎不加载目录内扁平 yaml → 跑满 5-6 分钟 0 命中 | expandPocDir() 展开成文件列表 | 已部署 |
| 3 | 14319 模板塞单引擎 | 长时间无产出 | 分批 1000/批、独立引擎、逐批日志 | 已部署 |

## 生产验证（决定性）

```
task proof3-1795070247（完整上游模块 + -t /apps/poc-test）
 75s  vuln=2  {"url":"https://example.com","vulname":"ZZ Canary Expand","level":"high"}
      batch 1/1 done (1 templates)

Nuclei 结果页（浏览器实测）：
  高危 | zz-canary-expand-test | ZZ Canary Expand | https://www.example.com
  高危 | zz-canary-expand-test | ZZ Canary Expand | https://example.com
```

修复前同一配置 6 分钟 0 命中。

## 上游与固化

- 上游 PR：https://github.com/Autumn-27/ScopeSentry-Scan/pull/40
- Fork 分支：NightGlowww/ScopeSentry-Scan:fix/nuclei-template-batch
- 生产镜像：autumn27/scopesentry-scan:nuclei-fix-v3（compose 已指向）

## 踩过的两个坑（已修正）

1. ne.Close() 不能调 —— nuclei 内部 leveldb 跨引擎共享，关闭任一引擎会让后续批次全部报 leveldb: closed。
2. 任务删除用 delA:true 会连带删掉 vulnerability 主表记录（vulnerabilityDetail 留孤儿）。

## 剩余优化项

- 全量 14319 模板一轮约 15 批，节点单 worker 期间其它任务排队，后续可并行化。
- vulnerabilityDetail 孤儿清理（任务删除时未级联）。
- 取消任务后节点任务循环的健壮性仍需长期观察。
## 全量 All-Poc 实测（2026-11-19 15:07）

```
task allpoc-v3（artex-recon-light，All Poc → -t * → 展开 14318 个 POC → 分批 1000）
 750s   batch 1/15 done (1000 templates)   ← 含前置模块（子域/端口/URL 扫描）约 10 分钟
 780s   batch 2/15 done
 870s   batch 3/15 done
 990s   batch 4/15 done
1080s   batch 5/15 done
  ...   batch 8/15 done（15:06:51）
```

- 每批稳定推进，无报错、无卡死（此前单引擎 14319 模板会静默空跑）
- vuln=0 是正确的：example.com 本身没有这批 POC 覆盖的漏洞；写入路径已用 canary 单独验证（75 秒出 2 条）
- 全量一轮约 20-25 分钟（含前置模块），单 worker 期间其它任务排队

## 性能瓶颈定位

第 1 批耗时 750s 主要是前置模块（子域枚举/端口/URL 扫描）而非 nuclei 本身；
nuclei 侧每批约 30-90s，15 批约 10-15 分钟。后续优化方向：

1. 按 -s critical,high,medium 预筛模板，减少实际加载数量（14318 → 数千）
2. 分批并行（2-3 worker），受容器 2g 内存限制
3. 前置模块与 nuclei 并行流水化
