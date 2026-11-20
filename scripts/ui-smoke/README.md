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
