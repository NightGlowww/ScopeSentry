# UI 矩阵验证报告

- 时间：2026-11-20 10:2x（Asia/Shanghai）
- 版本：server 2.0.244 / scan 1.9.20
- 环境：`http://127.0.0.1:18082`（SSH 隧道 → prod-tx-sh-app-01:8082）
- 用例集：`scripts/ui-smoke/cases.json`，断言库 `scripts/ui-smoke/assert.js`（9 种模式）

## 结果：25/25 通过

| # | 用例 | 断言模式 | 结果 |
| --- | --- | --- | --- |
| 1 | 漏洞结果-默认 | tableOrEmpty | ✅ 12 行 |
| 2 | 漏洞结果-任务空 | contextualEmpty | ✅ 0 行 + 说明原因 |
| 3 | 漏洞结果-主机空 | contextualEmpty | ✅ 0 行 + 说明原因 |
| 4 | 资产-资产Tab | tableOrEmpty | ✅ 20 行 |
| 5 | 资产-子域Tab空 | contextualEmpty | ✅ |
| 6 | 资产-IP Tab空 | contextualEmpty | ✅ |
| 7 | 资产-漏洞Tab空 | contextualEmpty | ✅ |
| 8 | 任务列表 | hasToolbar | ✅ 4 个按钮 + 20 行 |
| 9 | 节点列表 | hasColumns | ✅ 含「工具失败」+ 3 节点 |
| 10 | POC管理 | tableOrEmpty | ✅ 20 行 |
| 11 | 指纹管理 | tableOrEmpty | ✅ 20 行 |
| 12 | 敏感信息规则 | tableOrEmpty | ✅ 20 行 |
| 13 | 系统配置 | hasButtons | ✅ 保存按钮 + 1149 字符 |
| 14 | 重置-漏洞主机筛选 | resetWorks | ✅ URL/输入框清空 + 12 行 |
| 15 | 重置-漏洞任务筛选 | resetWorks | ✅ |
| 16 | 弹窗-新建任务 | dialogOpens | ✅ |
| 17 | 弹窗-POC新建 | dialogOpens | ✅ |
| 18 | 弹窗-指纹新建 | dialogOpens | ✅ |
| 19 | 弹窗-敏感规则新建 | dialogOpens | ✅ |
| 20 | 弹窗-项目新建 | dialogOpens | ✅ |
| 21 | 保存-系统配置 | saveFlow | ✅ 确认框 → 「成功」 |
| 22 | 保存-去重配置 | saveFlow | ✅ 提交#1/2 → 「成功」 |
| 23 | 搜索-任务列表 | listFilter | ✅ 20 → 0 行 |
| 24 | 搜索-POC管理 | listFilter | ✅ 20 → 0 行 |
| 25 | 搜索-指纹管理 | listFilter | ✅ 20 → 0 行 |

## 一次误判：隧道断开看起来像"全部失败"

中途出现 4/25 甚至 0/8 的极端结果，逐条排查后发现是**本地 SSH 隧道 18082 断开**
（`net::ERR_CONNECTION_REFUSED`），页面根本没加载。重建隧道后 25/25 全过。

**排查顺序（以后照这个来）**：

1. `curl -m 3 -o /dev/null -w "%{http_code}" http://127.0.0.1:18082/healthz`
2. 非 200 → 重建隧道：`ssh -f -N -L 18082:127.0.0.1:8082 <prod-host>`
3. 隧道正常后再看具体断言失败项

另：浏览器标签进入 `FAILED_TAB_RETAINED` 状态会阻塞后续所有建标签操作，
需要先 `close_tab` 掉那个 tabId，再继续。
