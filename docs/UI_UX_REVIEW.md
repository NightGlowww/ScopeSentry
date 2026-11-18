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


| v2.0.13 | 搜索行在窄屏挤压、label 靠 `top:8px;left:30%` 硬凑 | `ElRow + span 1/5/5` 固定比例 | 统一 flex + wrap，输入 260px，label 归位（12 页共用规则） |
| v2.0.14 | 弹窗/抽屉 30%~80% 在 1280 / 2K 屏上过窄或过宽 | 模板写死百分比 | `max-width:94vw` + `min-width:min(420px,94vw)`（抽屉）/ `min(360px,94vw)`（弹窗） |

## 路由覆盖

已逐页跑过：dashboard、asset-information(14)、task-management(3)、plugin/node/project/poc/fingerprint/sensitive、dictionary-management(4)、configuration(5)、about、project-detail。

当前结果：页面横向溢出 0、按钮越界 0、可见输入框挤压 0。


## 后续轮次（v2.0.13 → v2.0.20）

| 版本 | 内容 |
| --- | --- |
| v2.0.13 | 搜索行统一 flex + wrap（12 页），去掉 label 的 `top:8px;left:30%` 对齐 hack |
| v2.0.14 | 弹窗/抽屉尺寸 `max-width:94vw` + 最小宽夹取；新增 DEPLOY_NOTES |
| v2.0.15 | 搜索按钮 `height:100%` 塌成 16px → 36px；20 处输入 38→36px；unknown 服务提示 |
| v2.0.16 | 资产空状态码/unknown 服务解释 tooltip |
| v2.0.17 | 暗色模式硬编码白底/浅边框改 token（DetailTemplate / Csearch / AssetInfo2） |
| v2.0.18 | 标签页 `flex:0 0 auto`（不再被压到 55px 截字）；暗色表格/body 兜底 |
| v2.0.19 | 17 处 `window.confirm` → Element Plus `confirmDialog`（13 文件）；IP 红图标改中性色 |
| v2.0.20 | 首页仪表盘 126px → 56px；标签栏工具按钮 aria-label + Enter 键 |

## 待办（更新）

1. 表格密度与空态继续统一（行高、操作列按钮尺寸、空态插画）。
2. 剩余无 aria-label 的图标按钮（Icon 组件的 `cursor-pointer` 用法、表格内 icon-only 按钮）。
3. 首页「系统版本信息」表 0 行，需确认版本接口返回。
4. 扫描节点侧：PortFingerprint 不走代理导致 service=unknown，AssetMapping 跳过——需要节点逻辑支持未知服务端口也做 HTTP 探测。

## 历史待办（按优先级）

1. **`fixed: 'right'` 复核**：8 处（Task / Node / IcpTask / ScanPolicy / ScopeTable / ServerPlugin / AssetInfo2 / ApiKey）。需逐个确认列宽总和与常见视口宽度，避免再次出现覆盖。
2. **搜索行响应式**：`.locale-search-row`（19 处）+ 各页 `ElRow :gutter` 固定 span 组合，窄屏（<1100px）会挤压输入框，计划统一改成 flex + wrap。
3. **暗色模式对比度**：抽查文字/边框/标签在 dark 下的对比，尤其是 `--app-surface-2` 与表格斑马纹。
4. **对话框宽度**：多处 `width="50%"`/`30%` 在 1280 以下过窄或过宽，改成 `min(px, 92vw)` 策略。
5. **表格密度与空态**：统一行高、操作列按钮尺寸、空数据文案与插画。
6. **可访问性**：给纯图标按钮补 `aria-label`；表格操作按钮补 `title`。
## 回归基线（v2.0.23，27 条路由全量）

每轮发布前跑同一套可见元素检查，当前结果：

| 指标 | 结果 |
| --- | --- |
| 页面横向溢出 `scrollWidth - innerWidth` | 27/27 为 0 |
| 按钮越出视口（排除表格/滚动容器内） | 0 |
| 工具条相邻按钮间距 < 2px | 0 |
| 面包屑空条目 | 0 |
| 表格内零宽列 | 0（列宽全部数值化） |
| 暗色模式大面积浅色面板（>4000px²） | 0（抽查 7 页） |

覆盖路由：dashboard、asset-information 14 个子页、task-management 3 个、plugin、node、project(+detail)、poc、fingerprint、sensitive、dictionary 4 个、configuration 5 个、about。

## 交互规范（评审清单）

新增功能或修 UI 时逐条对照：

1. **确认框**：必须同时有「取消 + 确定」和一句正文（说明后果），不允许只有标题或只有「确定」。
2. **破坏性操作**（删除/清空/覆盖）：按钮用 `danger`，且未满足前置条件（未勾选、未填必填项）时禁用。
3. **空结果**：不显示分页器；空态用 `ElEmpty`，必要时说明「为什么会空、去哪看」。
4. **长文本单元格**：链接负责 `text-overflow: ellipsis`，行内图标 `flex: 0 0 auto`，不允许把图标裁掉。
5. **列表操作列**：宽度按按钮实际需要设定；不需要横向滚动时不要 `fixed: 'right'`。
6. **i18n**：新增文案必须进语言包（zh-CN / en / ko-KR），禁止在组件里写死中英文；新增 key 后要确认三个语言包都有。
7. **加载态**：首屏无数据时用骨架屏；有数据刷新时用 `v-loading`，不要闪骨架。
8. **可访问性**：纯图标按钮必须有 `aria-label`；键盘可聚焦（`role=button` + `tabindex=0` + Enter）。
9. **数字展示**：字节、百分比等按人类可读格式化（B/KB/MB、保留 1~2 位小数），原始值放 `title`。
