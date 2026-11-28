# 界面 ↔ 接口闭环

> 生成：`python3 scripts/ci/ui_api_closure.py --write`。写路由 = routes.snapshot.tsv 中非 GET 的项。
> 未接前端的必须登记在 `scripts/ci/ui_api_excuses.json` 并写清原因，否则门禁失败。

| 指标 | 数量 |
| --- | --- |
| 写路由 | 272 |
| 前端有入口 | 260 |
| 无入口·已登记 | 12 |
| 无入口·未登记 | 0 |

## 已登记的无入口写路由

| 路由 | 原因 |
| --- | --- |
| `/api/assets/site/chain` | 站点链视图，前端统一走 /api/assets/asset 的聚合口径 |
| `/api/assets/site/list` | 同上；这两个 POST 实为读接口，保留给外部集成 |
| `/api/dashboard/trend` | 仪表盘改版后前端只用 /stats；待与 ARL-Next 对齐时再决定接或删 |
| `/api/dashboard/widgets` | 同上（部件布局由前端本地配置驱动） |
| `/api/dictionary/manage/save` | 字典管理走 add/update，save 是早期合并入口，保留兼容 |
| `/api/github/monitor_result/list` | 与 result/list 共用仓库的监控视图，前端未接（无业务场景） |
| `/api/system/jev-run` | 运维/调试触发，故意不上面 |
| `/api/system/watchdog-run` | 运维/调试触发，故意不上面 |
| `/api/user` | 用户管理页未做（当前单管理员部署，注册接口保持关闭语义） |
| `/api/user/:id` | 同上 |
| `/api/user/:id` | 同上 |
| `/api/user/register` | 同上 |
