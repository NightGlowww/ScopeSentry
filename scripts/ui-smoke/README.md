# UI 冒烟探针

三个针对关键页面交互的断言脚本，用 Agent Bridge 在真实浏览器里执行，
返回 `{ok, checks:[{name, pass, detail}]}`。

## 为什么需要

API 层冒烟（`scripts/e2e-smoke.sh`）验证不了这些：
- 按钮存在但点了没反应（组件没绑事件）
- 弹窗渲染成未解析标签（缺 import）
- 进度条/统计条不出现（v-if 条件错）

这类问题构建通过、接口 200，只有真的点一下才知道。

## 用法（在 agent 会话里）

```js
// 1. 打开页面（先登录）
ensure_tab({ taskKey: "uismoke-result", url: "http://127.0.0.1:18082/#/asset-information/nuclei-result" })
// 2. 读探针文件内容，作为 expression 传给 eval
eval({ tabId, expression: <scripts/ui-smoke/result-page.js 的内容> })
// 3. 看返回的 ok 是否为 true；不过就看 checks 里哪条 pass=false
```

## 三个探针覆盖的内容

| 文件 | 页面 | 断言 |
| --- | --- | --- |
| `task-page.js` | 任务列表 | 工具栏按钮齐全、状态筛选存在、列表有数据、新建弹窗可打开 |
| `result-page.js` | 漏洞结果 | 处置进度条有数字、按任务筛选框、行点击打开详情且含 POC 模板、批量按钮 |
| `node-page.js` | 节点管理 | 「工具失败」「队列」列存在、节点非空、日志弹窗打开、错误摘要块、行数计数 |
| `task-vuln-loop.js` | 任务 ↔ 结果 闭环 | 产出栏出现、漏洞标签可点、跳转带 taskName、结果页显示来源、返回带 search |
| `asset-tabs.js` | 资产 13 个 Tab | ?tab=&search= 是否真的过滤、空状态是否说明原因（逐个 Tab 切换后 eval） |

## 最近一次实测（2026-11-20, 2.0.226）

```
task-page:   ok  toolbar=新建任务|按筛选删除|删除任务|暂停|开始|同步到项目  20 行
result-page: ok  共 8 条 未处理 8  /  详情含 POC 模板  /  批量确认存在|批量标记误报
node-page:   ok  3 个节点  /  错误摘要（3）  /  日志 661/661
```

## 注意

- 探针假设已登录（会话失效时会返回 checks 全 false，先重新登录）
- 探针只读 + 打开/关闭弹窗，不提交写操作（不会改动数据）
- 页面结构改版后需要同步更新探针里的选择器
## 矩阵验证（cases.json + assert.js）

`cases.json` 列出「页面 × 参数 × 断言」，`assert.js` 是按断言名分派的通用检查器。
执行方式（agent）：

1. 读 `cases.json`，逐条打开 `hash`（把 `__none__` 换成不存在的名字）
2. `eval({tabId, expression: "window.__ASSERT__ = \"<assert名>\"; \"ok\""})`
3. `eval({tabId, expression: <assert.js 全文>})`，看返回的 `ok`

**注意**：`ensure_tab` 的 `taskKey` 必须每次唯一（同 key 不同 URL 会报 `TASK_URL_CONFLICT`）。

### 支持的模式

| assert | 断言内容 |
| --- | --- |
| `tableOrEmpty` | 表格有数据，或存在明确的空状态 |
| `contextualEmpty` | 列表为空 **且** 空状态说明了原因（两套文案都认：资产页「当前筛选…」/ 结果页「该任务…未扫出漏洞」） |
| `hasToolbar` | 工具栏有「新建任务」「按筛选删除」，列表非空 |
| `hasColumns` | 节点表含「工具失败」列，节点非空 |
| `hasButtons` | 存在「保存」按钮，页面正文足够长 |

### 最近一次全量结果（2026-11-20, 2.0.243）

```
13/13 通过
漏洞结果-默认 / 任务空 / 主机空
资产-资产Tab / 子域空 / IP空 / 漏洞Tab空
任务列表 / 节点列表 / POC / 指纹 / 敏感规则 / 系统配置
```
### 重置类断言（2026-11-20 新增）

`resetWorks` 专治「后加的筛选字段漏在重置之外」：点「重置」后断言
URL query 清空、所有文本输入框为空、列表回到有数据状态。

已覆盖用例：
- `重置-漏洞主机筛选`：`?host=xxx` → 重置后 URL 干净、输入框空、12 行
- `重置-漏洞任务筛选`：`?taskName=xxx` → 同上
### 弹窗类断言（2026-11-20 新增）

`dialogOpens` 专治「按钮在但点了没反应」：按标签找到「新建」类按钮，点击后断言
弹窗真的出现（`.el-dialog` 高度 > 60），然后点「取消」关闭，不留残留状态。

标签通过 `window.__LABEL__` 指定（同一次 eval 里先设置再执行断言）：

```js
eval({ tabId, expression: 'window.__ASSERT__ = "dialogOpens"; window.__LABEL__ = "新建项目"; "ok"' })
eval({ tabId, expression: <assert.js 全文> })
```

已覆盖用例：新建任务 / POC / 指纹 / 敏感规则 / 项目（5/5 通过）。
### 保存类断言（2026-11-20 新增）

`saveFlow`：点保存/提交 → 若有确认框则确认 → 断言出现「成功」提示。
抓的是"保存按钮点了没反应 / 没有反馈"（本轮修过系统配置、去重、通知、指纹等多处）。

同页多个同名按钮时用 `window.__INDEX__` 指定第几个（0 起）：

```js
eval({ tabId, expression: 'window.__ASSERT__ = "saveFlow"; window.__LABEL__ = "提交"; window.__INDEX__ = 1; "ok"' })
```

已覆盖用例：系统配置「保存」（✓ 确认框 → 成功）、去重配置「提交」#1/2（✓ 成功）。
