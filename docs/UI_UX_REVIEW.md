# UI/UX Review Log

> 目标：对前端做长期、可追踪的精细化 review 与修复。每轮记录「查了什么、修了什么、还欠什么」。
> 版本基线：v2.0.12

## 审查方法

1. **确定性采集**：在真实登录态浏览器里按路由逐页跑脚本，采集可量化的 UI 指标，避免"看着像没问题"。
   - 页面横向溢出：`documentElement.scrollWidth - innerWidth`
   - 按钮越界：`getBoundingClientRect().left < 0 || right > innerWidth`
   - 工具条按钮间距：同一 `.el-row` 内相邻按钮的水平间隔 < 2px 计数
   - 输入框被压扁：可见 `input` 宽度 < 50px
   - 表格横向溢出：`.el-table__body-wrapper` 的 `scrollWidth - clientWidth`
   - 文字不可见：`color === backgroundColor`
   - 仅统计可见元素（`offsetParent`/rect 过滤），避免被隐藏 tab 面板污染
2. **静态扫描**：对 `frontend/src` 扫已知反模式（字符串列宽、`fixed: 'right'`、`:has()` 用法、内联 margin、硬编码色值、固定高度）。
3. 修复后重跑同一脚本做回归。

## 已修（本轮）

| 版本 | 问题 | 根因 | 修法 |
| --- | --- | --- | --- |
| v2.0.10 | 多处表格列被压扁/挤没 | 列定义写成 `width: '55'`、`minWidth: '200'` 字符串，Element Plus 要 number，字符串被当 CSS 值忽略 → 列退化为 auto/0 | 27 个页面统一转数字 |
| v2.0.11 | 敏感信息规则页「新建」按钮左半被卡片切掉 | 工具条用「一个 col 包一个按钮」+ `:gutter`，ElRow 给行负 margin，col padding 又被归零 → 整行左移 | 行负 margin 归零；注意 **`:has()` 不能嵌套 `:has()`**，嵌套写法整条选择器被丢弃 |
| v2.0.12 | 表格列总宽超出容器时，固定列盖住中间列且看不到滚动条 | 固定列浮动在滚动层之上，容器无可见滚动条提示 | 共享 Table 组件统一 `scrollbar-always-on` |
| v2.0.1–v2.0.9 | 标签栏视觉、插件列表列宽/固定列覆盖、导入按钮白底白字、节点插件弹窗列宽、工具条间距被 `:last-child` 清空 | 见对应 PR | — |

## 路由覆盖

已逐页跑过：dashboard、asset-information(14)、task-management(3)、plugin/node/project/poc/fingerprint/sensitive、dictionary-management(4)、configuration(5)、about、project-detail。

当前结果：页面横向溢出 0、按钮越界 0、可见输入框挤压 0。

## 待办（按优先级）

1. **`fixed: 'right'` 复核**：8 处（Task / Node / IcpTask / ScanPolicy / ScopeTable / ServerPlugin / AssetInfo2 / ApiKey）。需逐个确认列宽总和与常见视口宽度，避免再次出现覆盖。
2. **搜索行响应式**：`.locale-search-row`（19 处）+ 各页 `ElRow :gutter` 固定 span 组合，窄屏（<1100px）会挤压输入框，计划统一改成 flex + wrap。
3. **暗色模式对比度**：抽查文字/边框/标签在 dark 下的对比，尤其是 `--app-surface-2` 与表格斑马纹。
4. **对话框宽度**：多处 `width="50%"`/`30%` 在 1280 以下过窄或过宽，改成 `min(px, 92vw)` 策略。
5. **表格密度与空态**：统一行高、操作列按钮尺寸、空数据文案与插画。
6. **可访问性**：给纯图标按钮补 `aria-label`；表格操作按钮补 `title`。
