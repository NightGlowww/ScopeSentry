# 契约核对（verify_contract.sh）

冒烟脚本 `scripts/ci/smoke.sh` 只回答「服务活着吗」（healthz / 版本 / Mongo / 节点数）。
它管不到「行为有没有悄悄退化」——例如分页上限被绕过、删除接口又开始假成功、
静态资源的 404 又被缓存一年。这些都是在 2026-11-27~28 的真实事故里逐条踩出来的，
所以固化成一份可重复执行的核对表。

## 用法

```bash
SS_BASE=http://<host>:8082 SS_TOKEN=<JWT> bash scripts/ci/verify_contract.sh
```

`SS_TOKEN` 用管理员登录后的 `access_token`（`POST /api/user/login` 的 `data.access_token`）。
脚本只读 + 只用不存在的 id 触发写接口的拒绝分支，不会改动任何真实数据。

## 核对项

| # | 不变量 | 为什么 |
|---|--------|--------|
| 1 | `/assets/<入口 chunk>` 200 时 `Cache-Control` 含 `immutable` | 带 hash 的资源该长缓存 |
| 2 | `/assets/<不存在>` 404 时 `Cache-Control: no-store` | 曾经 404 也被 immutable，浏览器缓存一年 → 页面永久白屏 |
| 3 | `POST /api/task/delete` 传不存在 id → 404 | 曾经回 200 且 `deleted=1` |
| 4 | `POST /api/node/restart` 传不存在节点 → 404 | 同上，且错误里应列出当前节点 |
| 5 | `POST /api/assets/asset` 传 `pageSize=100000` → 行数 ≤ 5000 且带 `X-Pagesize-Clamped` | 曾经单请求 45363 行 / 22.1MB / 15.9s |
| 6 | `GET /api/mcp/tools` 工具数 ≥ 60 且每条都有 scope | scope 漏登记会让受限 key 被默认拒绝 |
| 7 | `GET /api/export/records` 的 `list` 是数组 | 空集曾经返回 `null`，表格拿不到数组 |

## 何时跑

- 动过分页、删除类接口、静态缓存、MCP 治理之后；
- 发版后（脚本对线上实例只读，可直接打生产）；
- 结论看最后一行 `CONTRACT_RESULT=PASS|FAIL`。

新增不变量时：先想清楚「它失败时用户会看到什么」，再按同样的 `ok/bad` 风格追加一节。
