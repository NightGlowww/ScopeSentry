# UI 中危问题修复报告（操作列裁切 / 禁用按钮零提示 / 数字输入框加减钮塌陷）

- 修复范围：仅前端 `frontend/src`（未改 `internal/**`、未改 `Project/detail`、未改 `Asset/components`、未部署、未重启生产、未动数据库）
- 构建：`cd frontend && pnpm build:pro` → **exit 0**（`Build successful`），末次产物 `assets/index-D9cnCO8o.js`
  （同一份源码连续构建两次，入口 chunk hash 分别为 `C64eZuXe` / `D9cnCO8o` —— 该工程构建产物 hash 不完全可复现；两轮均按同一脚本复测，DOM 数字完全一致，下列表格采用末轮 `D9cnCO8o` 的读数）
- 测量环境（重要）：`http://127.0.0.1:18082` 是到 **prod-tx-sh-app-02** 的 SSH 本地转发（`-L 127.0.0.1:18082:127.0.0.1:8082`），**该实例仍服务旧包 `assets/index-Bkk5rmXG.js`**，本任务不允许部署。
  因此「修复前」度量在 18082 旧包上取，「修复后」度量在**同一份生产构建产物**（`frontend/dist-pro`，hash `D9cnCO8o`）上取：
  本地静态服务 `127.0.0.1:18099` 直接托管 `dist-pro`，非静态请求反向代理到 18082（同一套生产 API、同一登录态 JWT 从 18082 的 localStorage 复制到新 origin）。
  Cookie 与宿主同名，`fetch('/api/...')` 实测返回生产数据（任务 6 条、插件 13 行），因此下列数字是真实生产数据下的布局度量，不是 mock。

---

## 1【中】plugin-management 操作列拥挤，最后一个「删除」被裁 6px

### 根因

操作列 `width: 300`，单元格 `.cell` 内容区 `clientWidth 298`（左右各 12px padding），而 5 个控件实测：
`操作 56 + 日志 44 + 清除日志 68 + 编辑 44 + 删除 44` + 4×6px gap = **280px** 内容 + 24px padding = **304px**，超出 6px，被 `.cell{overflow:hidden}` 裁掉。

### 改动

`frontend/src/views/Plugins/components/ClientPlugin.vue`（扫描端插件）：行内控件收敛为 **操作（下拉：重装/重检/卸载）/ 日志 / 编辑 / 更多（下拉：清除日志、删除）**，列宽 300 → 260；
`frontend/src/views/Plugins/components/ServerPlugin.vue`（服务端插件）：同一套结构（更多 = 清除日志、删除），列宽 300 → 260。
「删除」对系统插件仍禁用（`ElDropdownItem :disabled="row.isSystem"`，命令回调里再兜一层 `if (!row.isSystem)`）。

### 度量（修复前，18082 / Bkk5rmXG）

```js
td:            { l: 1259, r: 1559, w: 300 }
.cell:          clientWidth 298, scrollWidth 304   // 溢出 6px
buttons:        操作 1271–1327 / 日志 1333–1377 / 清除日志 1387–1455 / 编辑 1465–1509 / 删除 1519–1563
overflowPx:     6        // 1519+44 - 1557(内容右缘)
elementFromPoint(1562, 362) → DIV       // 命中表格容器，不是「删除」按钮
```

### 度量（修复后，dist-pro / D9cnCO8o）

```js
td:            { l: 1299, r: 1559, w: 260 }         // 收紧 40px
.cell:          clientWidth 258, scrollWidth 258    // 无溢出
buttons:        操作 1311–1367 / 日志 1373–1417 / 编辑 1427–1471 / 更多 1477–1533
contentRight:  1533   ≤  cellRight 1557             // 余量 24px
allInsideTd:   true                                 // 4 个控件矩形全部落在 td 内
hit-test:      每按钮取左 5% / 中 / 右 95% 三个点，共 12 点
               → probeOk: 12/12  命中元素都在该 td 内且属于该控件
```

### 功能回归

```js
// hover「更多」读下拉项
items: [{ text: '清除日志', disabled: false }, { text: '删除', disabled: true }]  // 系统插件行
```

> 服务端插件页修复后列宽公式同上（200px 控件 + 18px gap + 24px padding = 242 ≤ 260），但**生产该表为空**（`/api/plugin/ type=server` → `total: 0`，页面显示「暂无数据」），无法产出真实行内度量；改动与扫描端完全同构，构建通过。

---

## 2【中】ScanTask / 资产组地契：禁用按钮零提示

### 根因

4+4 个按钮只是 `:disabled`，没有 `title` / `aria-label` / `ElTooltip`。原生 `title` 在 disabled 的 button 上**不触发**（浏览器不向 disabled 控件派发鼠标事件），所以只加 title 不够。

### 改动

新增 `frontend/src/components/DisableHint/`（`index.ts` + `src/DisableHint.vue`）：
外层 `span` 承接指针事件 → `ElTooltip` 由 span 触发；span 带 `aria-disabled`；disabled 时给按钮 `pointer-events: none`，保证整个按钮面积都能唤起提示。

- `frontend/src/views/Task/Task.vue`：删除任务/暂停/开始/同步到项目 包 `DisableHint`，`:title="noTaskSelected ? t('common.disabledNeedTask') : undefined"`；新增 `computed noTaskSelected`；工具条改 `flex + gap:10px`（按钮多了一层包裹，`.el-button + .el-button` 相邻选择器失效）。
- `frontend/src/views/AssetGroup/components/ScopeTable.vue`：批量移动/绑定企业/清空错误/删除 同上（`noScopeSelected`，文案 `common.disabledNeedScope`）；`.scope-toolbar` 的按钮相邻 margin 归零，间距统一由既有 flex gap 提供。
- i18n 三个 locale 的 `common` 新增 **三个 key**：`more` / `disabledNeedTask` / `disabledNeedScope`
  `zh-CN`「更多 / 请先勾选任务后再操作 / 请先勾选记录后再操作」、`en`「More / Select at least one task first / Select at least one record first」、`ko-KR`「더보기 / 먼저 작업을 하나 이상 선택하세요 / 먼저 레코드를 하나 이상 선택하세요」。

### 度量（修复后，ScanTask，D9cnCO8o）

```js
[
 { t:'新建任务',   dis:false, title:null },
 { t:'删除任务',   dis:true,  title:'请先勾选任务后再操作', aria:'true' },
 { t:'暂停',       dis:true,  title:'请先勾选任务后再操作', aria:'true' },
 { t:'开始',       dis:true,  title:'请先勾选任务后再操作', aria:'true' },
 { t:'同步到项目', dis:true,  title:'请先勾选任务后再操作', aria:'true' }
]
// 每按钮左5%/中/右95% 三点命中包裹层：hitAllWrap = true（3/3 × 4 按钮）
// 派发 mouseenter 后实测出现可见 popper：'请先勾选任务后再操作'
// 勾选 1 行后：dis 全部 false，title 全部 null（提示随禁用态撤下）
```

### 度量（修复后，资产组地契，D9cnCO8o）

```js
gap: '10px'
批量移动 1214–1304 / 绑定企业 1314–1404 / 清空错误 1414–1504 / 删除 1514–1576
全部 dis:true, title:'请先勾选记录后再操作', aria:'true', hitAllWrap:true(3/3)
popper 文本：'请先勾选记录后再操作'
```

---

## 3【中·顺带】configuration/system 去重周期：加减钮塌陷 + 11px 死区

### 根因（与初版判断有一处修正）

EP 2.14.8 在 `is-controls-right` 下把 `--el-input-number-controls-height` **直接声明在按钮上**：

```css
.el-input-number.is-controls-right .el-input-number__increase,...{ --el-input-number-controls-height:15px; height:var(...) }
.el-input-number.is-controls-right[class*=small] [class*=increase]{ --el-input-number-controls-height:11px }
```

按钮高度取的是「标称」输入高度的一半：small = (24-2)/2 = **11px**，而全局 `html .el-input__wrapper{min-height:34px}` 把容器撑到 36px → 上下各 11~12px 按钮 + 中间 11px 死区。
只给**容器**设变量无效（按钮自身声明覆盖继承值，实测仍是 12/11），必须以更高权重重写按钮。

### 改动

`frontend/src/styles/modern.less`（输入类样式段）：

```less
html .el-input-number{ height: 36px; }
html .el-input-number .el-input{ height: 100%; }
html .el-input-number .el-input__wrapper{ min-height: 100%; box-sizing: border-box; }

/* 带 [class] 提高权重：(0,4,1) > EP 的 (0,4,0) */
html .el-input-number.is-controls-right[class] .el-input-number__increase,
html .el-input-number.is-controls-right[class] .el-input-number__decrease{
  --el-input-number-controls-height: calc(50% - 1px);
  height: calc(50% - 1px);
  line-height: normal;
}
```

### 度量（修复前）

```js
容器 .el-input-number: 120×36
__increase: 25×12   (left 760.3, top 1665 → bottom 1677)
__decrease: 25×11   (top 1688 → bottom 1699)
间隙: 11px（点不到）
```

### 度量（修复后，D9cnCO8o，元素滚入视口后测量）

```js
容器:        672 → 708 (36)
__increase:  673 → 691  (h 18, 顶边距容器 1px)
__decrease:  690 → 707  (h 17, 底边距容器 1px)
gap:         -1（1px 重叠，无死区）
命中:        每个按钮 3×3 = 9 点网格 → elementFromPoint 全部 `closest()` 命中该按钮（9/9 + 9/9）
接缝点:      (cx, 两按钮交界) → el-input-number__decrease（不是空白）
功能:        点击 __increase，输入框值 3 → 4（加减真生效，不只是外观）
```

---

## 变更文件清单

| 文件 | 变更 |
|---|---|
| `frontend/src/components/DisableHint/index.ts` + `src/DisableHint.vue` | **新增**：禁用按钮的 tooltip 包裹层（span 承接事件 + aria-disabled + title 透传） |
| `frontend/src/views/Plugins/components/ClientPlugin.vue` | 操作列 300→260；清除日志/删除 收进「更多」下拉 |
| `frontend/src/views/Plugins/components/ServerPlugin.vue` | 同上（同构改动） |
| `frontend/src/views/Task/Task.vue` | 4 个批量按钮包 DisableHint + title；`noTaskSelected`；工具条 flex/gap |
| `frontend/src/views/AssetGroup/components/ScopeTable.vue` | 4 个批量按钮包 DisableHint + title；`noScopeSelected`；按钮相邻 margin 归零 |
| `frontend/src/locales/{zh-CN,en,ko-KR}.ts` | `common` 新增 `more` / `disabledNeedTask` / `disabledNeedScope` |
| `frontend/src/styles/modern.less` | el-input-number 显式高度 + controls-right 按钮 50% 高度（含注释说明权重） |

## 验证环境搭建（可复现）

```bash
# 1) 生产构建
cd frontend && pnpm build:pro            # 产物 frontend/dist-pro

# 2) 本地托管产物 + 反向代理到 18082（只代理非静态路径，页面只发查询请求）
#    /tmp/scs-preview-18099.mjs：静态命中 dist-pro，其余 http.request 转发 127.0.0.1:18082
node /tmp/scs-preview-18099.mjs          # http://127.0.0.1:18099

# 3) 登录态迁移：在 18082 标签页读 localStorage → 写入 18099 origin → 硬 reload
#    （Cookie 不区分端口，JWT 在 localStorage 的 user 键里）
```

## 约束遵守

- 未修改 `internal/**`、`Project/detail`、`Asset/components`；未部署、未重启生产、未写数据库。
- 验证期只在生产读了列表接口（`/api/task/`、`/api/plugin/`、资产组列表）；点过「去重周期」的 +4 与「更多」下拉，全部是**本地表单/菜单状态**，没有任何提交请求；验证标签页用完即关（未提交即丢弃，生产配置未变）。
- `frontend/dist-pro` 未纳入 git（`git ls-files frontend/dist-pro` 为空）。
