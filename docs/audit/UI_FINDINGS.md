# ScopeSentry UI/UX 精细复查 — 布局缺陷与交互死角

- 目标：`http://127.0.0.1:18082`（生产 prod-tx-sh-app-02，经隧道访问），账号 ScopeSentry
- 视口：1619 × 1003 CSS px（浏览器窗口实测 `documentElement.clientWidth/Height`）
- 方式：Agent Bridge 后台标签页 + `eval` DOM 度量（不截图，全部结论以像素/元素为证据）；只读，未改产品代码、未触发任何提交类请求
- 覆盖页面（14）：`dashboard/index`、`asset-information/index`、`task-management/ScanTask`、`plugin-management/index`、`configuration/{system,status,backup,audit,mcp-audit,apikey}`、`github/index`、`asset-information/{fofa,icp-task,asset-group}`
- 结论：8 条真问题（2 高 / 4 中 / 2 低），另附 6 项「已核查无问题」与 1 项「已排除的伪象」说明

## 汇总（按严重度）

| # | 严重度 | 页面 | 缺陷 | 关键度量 |
|---|---|---|---|---|
| 1 | 高 | asset-information/index（子域名表） | 域名列链接大批失效：`unknown://` 与空 `href` | 101 个 `a.el-link` 中 19 个 `unknown://`、19 个 `href=""`，仅 63 个真实 http(s) |
| 2 | 高 | asset-information/index（子域名表） | 域名列多值无分隔拼接，整串被当成一个链接 | 单元格 `clientWidth 198`/`scrollWidth 664`；示例串长 83 字符 |
| 3 | 中 | plugin-management/index | 操作列拥挤，最后一个按钮被裁切 | `td 300px`，`.cell` `cw 298 / sw 304`，「删除」右缘 1563 > td 右缘 1559 |
| 4 | 中 | task-management/ScanTask | 4 个禁用按钮无任何禁用原因提示 | 删除任务/暂停/开始/同步到项目：`disabled=true`，无 title/aria-label/ElTooltip |
| 5 | 中 | asset-information/asset-group | 4 个禁用按钮无提示（批量移动/绑定企业/清空错误/删除） | 同上，按钮可见宽度 90/90/90/62 |
| 6 | 中 | configuration/system（去重周期） | 数字输入框加减按钮仅 11–12px 高，中间 11px 死区 | 容器 120×36；increase 25×12、decrease 25×11，间隙 11px |
| 7 | 低 | 多页空态 | 空态文案不一致：有的给下一步引导，有的只有「暂无数据」 | fofa/icp-task 空态文案宽 735px；asset-group/backup/github 仅「暂无数据」 |
| 8 | 低 | asset-information/index | 14 列默认需 688px 横向滚动，滚动条仅 6px 且位于表底 | `el-scrollbar__wrap scrollWidth 1820 / clientWidth 1132`；横向 bar 高 6px |

---

## 1【高】asset-information/index — 域名列链接批量失效

**现象**：子域名表「域名」列共 101 个 `a.el-link`，其中 38 个点不动：
- 19 个 `href="unknown://<host>"`（`unknown` 是未注册 scheme，浏览器点击无反应/直接报错）
- 19 个 `href=""`（IP 行同样渲染 `a.el-link--default`、`cursor:pointer`，点击等于重新加载当前页）
- 仅 60 个 `https://` + 3 个 `http://` 是可用链接

**度量证据**

```js
// 页面内实际统计（230 行数据）
{ links: 101, unknown: 19, empty: 19, https: 60, http: 3 }
// 样例
"unknown://s3.etoro.com"            → <a class="el-link el-link--default">
""                                  → <a class="el-link ...>23.206.24.114</a>
```

**影响**：用户在资产表里点域名打不开站点，也无法判断「点了会怎样」；IP 行伪装成链接误导点击。

**修复建议**
1. 生成 URL 时兜底 scheme：`const url = /^https?:\/\//.test(host) ? host : (httpsPort ? 'https://' : 'http://') + host`，禁止把 `unknown` 写进 href。
2. IP/未知协议的单元格不要用 `el-link`，改用纯文本 + 复制按钮（与「记录值」列的 `copyText` 交互一致）。
3. 若产品确实要允许「未知协议」，用 `title`/`ElTooltip` 明确写「该资产未探明 web 协议，无法直接打开」。

---

## 2【高】asset-information/index — 域名列多值无分隔拼接

**现象**：一个资产行含多个域名时，前端把数组直接拼成一个字符串塞进同一个 `<a>`，没有分隔符；配合列的 `white-space: nowrap` + `text-overflow: ellipsis`，显示成一条无法分辨的串：

```
comwww.ig.comnew.nasdaq.cometherscan.iowww.rakuten-sec.co.jpjfinfo.comwww.etoro.com
```

**度量证据**

```js
// .cell
{ clientWidth: 198, scrollWidth: 664, whiteSpace: "nowrap", textOverflow: "ellipsis" }
// <a> 的 href 就是整串拼接结果
href = "unknown://comwww.ig.comnew.nasdaq.cometherscan.iowww.rakuten-sec.co.jpjfinfo.comwww.etoro.com"
// 一行数据里域名数：4（同页 14 个 link 文本长度 > 60）
```

**影响**：198px 列宽只能看到前 ~25 字符，用户既分不清有几个域名，也无法逐个点击；唯一链接指向拼接串，必然失效。

**修复建议**
1. 多值时逐条渲染：`.cell` 内用 `display:flex; flex-wrap: wrap; gap: 4px` + 每个域名一个 `el-tag`/独立的 `el-link`；或按「记录值」列的既有写法 `join('\n')` + `white-space: pre-line`（该列已验证可用）。
2. 单值超长时用 `ElTooltip` 展示完整值（当前 `.cell` 带 `el-tooltip` 类，说明 `show-overflow-tooltip` 已开，但多值拼接后 tooltip 内容同样不可读）。
3. 后端返回 `host(s)` 数组而不是已拼接的字符串，避免前后端都丢结构。

---

## 3【中】plugin-management/index — 操作列拥挤，按钮被裁

**现象**：操作列固定 300px，塞了 5 个控件（`操作` 下拉 + `日志`/`清除日志`/`编辑`/`删除`），内容宽 304px；最后一个「删除」超出单元格内边距并被 `overflow:hidden` 裁掉约 6px。

**度量证据**

```js
td:          { left: 1259, right: 1559, width: 300 }
.cell:       { clientWidth: 298, scrollWidth: 304, overflowX: "hidden", padding: "0 12px" }
buttons:     操作 1271–1327 / 日志 1333–1377 / 清除日志 1387–1455 / 编辑 1465–1509 / 删除 1519–1563
hit-test:    elementFromPoint(1561, 行中线) → DIV（表格容器），不是「删除」按钮
```

**影响**：最后 6px 不可点、hover 背景被切平，视觉上像是布局崩了；窄屏更糟。

**修复建议**：操作列宽提到 320–340；或把「清除日志」并入 `操作` 下拉；或统一按钮尺寸与间距（当前 44/68/44/44 混排 + 10px 间距 = 304，压到 298 内即可）。

---

## 4【中】task-management/ScanTask — 禁用按钮零提示

**现象**：未勾选任务行时，工具条 4 个按钮被禁用，但没有任何 title / aria-label / tooltip 说明原因。

**度量证据**

```js
[ {t:"删除任务", dis:true, title:"", aria:"", tip:false, w:90},
  {t:"暂停",     dis:true, title:"", aria:"", tip:false, w:62},
  {t:"开始",     dis:true, title:"", aria:"", tip:false, w:62},
  {t:"同步到项目", dis:true, title:"", aria:"", tip:false, w:104} ]
// 同行 checkbox 全部 enabled（header 与 body 均未禁用），说明禁用只因未选中
```

**影响**：新用户面对 4 个灰按钮不知道「为什么不能点、该先做什么」，属于典型交互死角。

**修复建议**：用 `ElTooltip` 包裹禁用按钮，文案「请先勾选任务」；或按钮旁常驻一行提示（选中 0 项时显示）。

---

## 5【中】asset-information/asset-group — 禁用按钮零提示

**现象**：资产组地契页工具条 `批量移动`/`绑定企业`/`清空错误`/`删除` 四个按钮在无选中时禁用，同样无 title/aria/tooltip。

**度量证据**

```js
[ {t:"批量移动", dis:true, l:1184, top:193, w:90},
  {t:"绑定企业", dis:true, l:1294, top:193, w:90},
  {t:"清空错误", dis:true, l:1404, top:193, w:90},
  {t:"删除",     dis:true, l:1514, top:193, w:62} ]
// 同页「搜索」「新建地契」enabled，说明是选择态驱动而非权限
```

**修复建议**：与 #4 同一方案（抽一个 `<DisabledHint reason="请先选择资产组">` 组件复用）。

---

## 6【中】configuration/system（去重周期）— 数字输入框加减按钮塌陷

**现象**：`el-input-number` 容器被撑到 36px，但 `__increase`/`__decrease` 两个按钮只渲染 12px / 11px 高，中间留下 11px 无法点击的死区。

**度量证据**

```js
容器 .el-input-number: left 564, width 120, height 36
.el-input-number__increase:  left 658, top 1665, right 683, bottom 1677  → 25 × 12
.el-input-number__decrease:  left 658, top 1688, right 683, bottom 1699  → 25 × 11
中间空隙: 1677 → 1688 = 11px（点击无效）
输入框 .el-input__wrapper: height 36（min-height:34px 来自 styles/modern.less）
```

**成因**：EP 的 `__increase/__decrease` 用绝对定位 + 百分比高度，而 `html .el-input__wrapper { min-height: 34px }`（modern.less:225）把父容器撑高、`.el-input-number` 自身没有显式高度，百分比高度无法解析 → 按钮退回内容高度，上下各贴 1px，中间空 11px。

**修复建议**：`html .el-input-number { height: 36px; }` 并给 `html .el-input-number__increase, html .el-input-number__decrease { height: calc(50% - 1px); }`；或在 `.el-input-number` 内部不套用 `.el-input__wrapper` 的 min-height。

---

## 7【低】多页空态文案不一致

**现象**：同类数据表，空态信息密度差一个数量级：

| 页面 | 空态内容 | 度量 |
|---|---|---|
| asset-information/fofa | 「还没有导入记录：在上面填查询语句 → 预览 → 建任务，每次导入都会写一条记录（…）」 | 文案宽 735px |
| asset-information/icp-task | 「还没有 ICP 查询任务：点右上角「新建任务」，填企业名或域名后创建；…」 | 文案宽 735px |
| asset-information/asset-group | 仅「暂无数据」 | 文案宽 576px |
| configuration/backup、github/index、plugin-management | 仅「暂无数据」 | — |

**建议**：统一空态组件（图标 + 一句结论 + 一个主操作按钮），fofa/icp-task 的写法作为基线。

---

## 8【低】asset-information/index — 横向滚动发现性差

**现象**：表格 14 列，容器 1132px，内容 1820px，默认 `scrollLeft=0`，「状态/封面/标题/更新时间」等第 9–13 列默认完全在视口外；横向滚动条仅 6px 高，且位于 663px 高表格的底部，首屏不可见。

**度量证据**

```js
.el-table__body-wrapper .el-scrollbar__wrap: { scrollWidth: 1820, clientWidth: 1132 }   // 需 688px 滚动
.el-scrollbar__bar.is-horizontal: height 6px
首行末列（详情，fixed right）: 1461–1561，其余 4 列 1509–2149 默认不可见
```

**缓解事实**：操作列（详情）已 sticky 在右侧，核心操作可达，所以只是浏览密度问题不是死路。

**建议**：列设置/密度开关；或把「状态/更新时间」等低优先级列默认隐藏（配合列自定义），减少默认横向滚动。

---

## 已核查、未发现真问题的项（避免重复返工）

1. **页面级横向溢出**：14 个页面 `documentElement.scrollWidth == clientWidth == 1619`，delta 全为 0；无 `toast/导航` 撑破布局。
2. **表头/表体列对齐**：asset 表 14 列 header↔cell 左缘 delta 全 0；把 body 滚动 300px 并派发 scroll 后 `headerWrapper.scrollLeft == 300`，双向同步正常（未出现「表头表体错位」）。
3. **表头文字**：7 个数据表页的 `th .cell` 均无截断（`scrollWidth <= clientWidth`）、无换行（th 高 42–46）。
4. **弹窗表单标签对齐**：实测 5 个弹窗（新建任务 / 新建 API Key / 新建地契 / 新建配置 / 新建 ICP 任务）
   - 同一弹窗内标签右缘一致：`labelRightSpread = 0–1px`，`label→content gap = 0`；
   - 无标签截断、无标签压输入框（`labelOverlapInput = 0`）；
   - 最长表单（新建任务，form 高 949px）落在 713px 弹窗内，滚动体 `scrollHeight 949 > clientHeight 600` 可滚，「保存」滚到底后 `elementFromPoint` 命中按钮（`769–805` 处于视口内）——不是「按钮被裁」。
5. **加载态**：无骨架屏（`skeleton = 0`），但加载由 `el-loading-mask` 承担（切页时观测到 3 个遮罩）；未复现「暂无数据」与加载中混淆。
6. **分页挤压**：`audit`（共 527 条）、`mcp-audit`（共 4459 条）分页容器宽 1469px，`共 N 条 + sizes + pager + jumper` 未溢出（12 个 pager 单元）。

## 已排除的伪象（方法说明，避免被当成缺陷）

Agent Bridge 全程后台，`document.visibilityState === "hidden"`。隐藏标签页里 CSS transition 不推进、Vue 的 `transitionend` 不触发，导致 **transform 驱动的进场动画冻结在起始值**：

```
plugin-management 「编辑」抽屉: class="el-drawer rtl open"，computed transform = translateX(809.5px)，left = 1619
父级 overlay 仍带 el-drawer-fade-enter-from / -enter-active
```

即抽屉「停在屏幕右侧之外」是后台渲染伪象，不是产品缺陷，故未记入。同理，任何 `el-dropdown / tooltip / drawer` 的 **位置** 不可作为证据；本报告所有结论均来自布局盒度量（flex/absolute 定位）与命中测试，不依赖 transform 终态。

## 2026-11-28 浏览器实测复检（Agent Bridge 在线上页面跑 ui_layout_audit.js）

验证方式：用 Agent Bridge 打开线上 `http://100.115.186.92:8082`，真实登录（表单填用户名密码点 Sign in），
然后在 `#/dashboard/index`、`#/asset-information/index`、`#/task-management/ScanTask`、
`#/login` 上求值 `ui_layout_audit.js`，并补充做了元素级探针（`elementFromPoint`、逐级算有效 opacity/visibility、
`el-loading-mask` 全量枚举）。

### 修掉的两个真问题

**1) 卡在离场过渡里的 loading 遮罩会吃掉整页点击（严重）**

快速切页/切 tab 时 Element Plus 的 loading 离场过渡会被打断，元素停在
`.el-loading-mask.el-loading-fade-leave-active`，计算样式 `opacity: 0`。关键点：
**`opacity: 0` 的元素仍然接收指针事件**，所以 `elementFromPoint` 命中的是遮罩而不是表格 ——
页面看着完全正常，点哪都没反应，只能刷新。连切 5 个页面后实测 8 个遮罩全部卡住，
其中 6 个覆盖整个内容区（1611×905）。

修法（`src/styles/index.less`）：离场中的遮罩 `pointer-events: none`。
数据已经加载完才会进入离场，这时不该再拦鼠标。
验证：修复后构造一个「卡住的遮罩」，计算样式 `pointer-events: none`、`elementFromPoint` 不再命中它；
修复前同样的构造是 `auto` 且命中。

**2) 登录页占位符没走 i18n**

用户名/密码框显示表单的通用兜底 `Please input`，而 `locales` 里
`login.usernamePlaceholder`（请输入用户名 / Please input username）、
`passwordPlaceholder` 中英双语早就写好了，表单从来没引用过。
现在绑上，实测英文站显示 `Please input username` / `Please input password`。

### 审计脚本本身的三类假阳（一起修了）

| 症状 | 原因 | 修法 |
| --- | --- | --- |
| `node scripts/ci/ui_layout_audit.js` 抛 `ReferenceError: document is not defined` | 这是浏览器脚本，被当成 node 脚本用过 | 无 `document` 时打印用法并正常退出 |
| 刚切页就量，跑出 6 条 `wrapped`，4 秒后同页 0 条 | loading 期间是占位尺寸 | 有可见 `.el-loading-mask` 时直接返回 `loading: true` |
| 登录页表单被判「工具栏换行」 | `el-row` 里 24+24+24+24 本来就该竖排 | 各列 `span` 之和 > 24 时跳过；`position: fixed` 的遮罩 `offsetParent` 恒为 null，改用 rect 判断 |

新增检查项 `stuck-mask`：把「opacity≈0 但仍吃点击」这个状态做成可重复检测（构造用例可稳定命中）。

复检结果：`#/login` 0 条、`#/dashboard/index` 0 条、`#/asset-information/index` 0 条、
`#/task-management/ScanTask` 0 条。
