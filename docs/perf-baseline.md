# 接口性能基线（2026-11-20, server 2.0.281）

测法：服务端本机 `python3 scripts/remote/perf-probe.py --password ***`，取一次样本。
改动后重跑，和这张表比 —— 比在浏览器里"感觉快慢"可靠。

| 接口 | 耗时 | 响应字节 |
| --- | --- | --- |
| task list（第 1 页） | 5.7 ms | 10,888 |
| task list（第 3 页） | 4.1 ms | 10,359 |
| subdomain（第 1 页） | 1.6 ms | 4,891 |
| subdomain（第 1200 页 = 跳过 2.4 万条） | 16.7 ms | 3,848 |
| url（第 1 页） | 1.5 ms | 5,668 |
| asset（第 1 页） | 1.2 ms | 15,170 |
| nuclei list | 1.8 ms | 6,775 |
| nuclei stats | 1.1 ms | 52 |
| node list | 1.3 ms | 2,210 |
| healthz | 0.9 ms | 296 |

## 前端资源

`performance.getEntriesByType("navigation")` 实测（命中缓存）：

```
domContentLoaded 94 ms   load 95 ms   12 个资源
```

## 结论

- **没有慢接口**：全部 < 20 ms，深分页也只有 16.7 ms（走索引，不取文档）。
- 页面首屏慢的话，先怀疑渲染和数据量，不是接口。
- 之前我用"等 4.5 秒再读表格"的方式测首屏，量到 4.6 秒——那是**自己的固定等待**，
  不是真实加载时间。真实接口 1.6 ms、bundle 95 ms。
