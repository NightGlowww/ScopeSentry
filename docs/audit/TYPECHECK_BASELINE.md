# 前端类型检查基线

`pnpm ts:check`（vue-tsc）在本仓库长期从未绿过，而 `build:pro` 走 vite、**不做类型检查**，
所以类型错误只能靠人眼发现。2026-11-28 这轮把它从 **143 条清零到 0**，现在可以直接当门禁用。

## 当前状态（2026-11-28 收口）

| 指标 | 数值 |
| --- | --- |
| 起始错误数 | 143 |
| 现在 | **0**（`bash scripts/ci/typecheck.sh` → `TYPECHECK=PASS`） |
| 机械清理 | 43 处未使用 import/声明 + 8 处未使用回调参数 + 19 处整块死代码 |
| 契约补齐 | 4 组后端早就返回、前端类型却没跟上的字段 |

`build:test` 脚本本来就写成 `pnpm run ts:check && vite build --mode test`。
以前断在 ts:check（恒失败），现在 ts:check 段通过；**但 vite 段仍会崩**，与类型无关：
`RangeError: Maximum call stack size exceeded`，落在 Vite 5.0.6 自己的 `generateBundle`（`String.replace`），
`--mode test` 才触发，发布路径 `build:pro` 不受影响。所以门禁用 `scripts/ci/typecheck.sh`，不要指望 `build:test`。

## 藏在类型错误里的真问题（本轮修掉）

类型噪声最大的害处不是难看，是**把真问题埋了**。这轮挖出来的四个：

| 位置 | 问题 | 后果 |
| --- | --- | --- |
| `src/axios/index.ts` | 包装层只转发 `url/method/params/data/headers/responseType`，`timeout` 被静默丢弃 | 备份创建/恢复传的 `timeout: 1800000` **从没生效**，实际一直按 `REQUEST_TIMEOUT=60s` 超时；备份本身要几分钟，用户看到的是「失败」，服务端还在跑 |
| `src/App.vue` | 模板里写 `location.reload()`，`location` 不在组件作用域 | 点「有新版本，刷新」→ TypeError，永远刷不了（只能手动 F5） |
| `views/Asset/components/AssetInfo2.vue` | 把 VNode 传给 `ElTooltip` 的 `content`（string 属性） | banner 悬浮框空白；改用 `content` 插槽 |
| `views/Asset/components/TimeRangeFilter.vue` | `change` 事件实际带值（父组件 `onTimeFilterChange(v)` 依赖它），`emit` 声明漏了参数 | 类型层禁止，运行层靠 `v-model` 兜住；声明补上后才和实现一致 |
| `views/{Asset/components/IP,RootDomain}.vue` | `typeof getTotal === 'function'` 但 `getTotal` 根本不存在 | 死代码 + 注释误导（「共 N 条」实际由 `useTable.fetchDataList` 刷新） |

## 契约补齐（后端有、前端没声明）

| 类型 | 补的字段 | 后端来源 |
| --- | --- | --- |
| `NucleiResultListQuery` | `status` / `taskName` / `host` | `models.NucleiResultListRequest` |
| `NucleiResultDetail` | `templateContent` | `models.NucleiResultDetail` |
| `NodeData` | `containerMem` / `queueLen` | `models.Node` |
| `TaskData` | `status` / `queued` / `queuePos` / `failReason` | `models.Task` |

这类缺字段最麻烦的地方：**运行期是对的，类型上却「不许你这么用」**，
于是要么加 `as any`（把真错误一起放过），要么删掉功能（回到「没实现」）。
补齐之后前端才第一次能「从类型上确认」这些字段存在。

## 组件契约上的坑（记下来别再踩）

- `ElTag` 的 `type` 联合里**没有空字符串**，默认值是 `undefined`；写 `type=""` 会报类型错，且视觉上和默认态一样。
- `ElInput` 的 `rows` 是 number 属性：`rows="10"` 传的是字符串（能用，但类型错），写 `:rows="10"`。
- `ElLink` 不接受 `rel`；`BaseButton` 没声明 `title` —— 这两个都会作为 attr 透传到真实 DOM，TSX 里需要显式放宽。
- `ElRadioGroup`/`ElSelect` 的 `@change` 参数类型是 `string | number | boolean | undefined`，别在 emit 声明里写死 `string`。
- `components/Table/src/Table.vue` 里 `Object.assign(unref(mergeProps), props)` 会因 `TableProps` 的深递归泛型触发
  `TS2589（类型实例化过深）`；把左侧收敛成 `Record<string, any>` 再合并即可，语义不变。

## 怎么保持 0

1. 提 PR 前跑 `bash scripts/ci/typecheck.sh`（或 `cd frontend && pnpm ts:check`）；
2. 新增字段先改 `api/*/types.ts` 再写页面——顺序反了就会出现「运行正确、类型报错」；
3. 真需要绕过类型时用**单点** `as any` / `@ts-expect-error` + 一行注释说明原因，
   不要整文件 `// @ts-nocheck`（那等于把这个文件从门禁里摘掉）。
