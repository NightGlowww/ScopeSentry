# 资产信息 / 子域名表 — 死链与多值拼接修复报告

- 对应缺陷：`docs/audit/UI_FINDINGS.md` #1（高，域名列链接批量失效）、#2（高，多值无分隔拼接）
- 代码位置：`frontend/src/views/Asset/components/AssetInfo2.vue`、`frontend/src/views/Asset/components/Subdomain.vue`
- 改动时间：2026-11-19 01:43 — 02:00 (+08)
- 约束遵守：未部署、未重启生产、未改任何库数据；生产镜像仍是旧前端（v2.0.109/110），DOM 复测用**同仓库 dist-pro 构建 + 同源代理回源 18082**，数据与生产同一套后端

---

## 0. 结论

| # | 缺陷 | 修复前（生产 DOM） | 修复后（dist-pro DOM） |
| --- | --- | --- | --- |
| 1 | `href="unknown://<host>"` | 资产页 p1 17 个 / p2 20 个 | **0** |
| 1 | `href=""` 可点链接 | 资产页 p1 17 个 / p2 20 个 | **0** |
| 2 | 多值塞进单个 `<a>`（整串 664px） | p2 有 7 行、0 个徽标；单元格 `cw 198 / sw 664`（maxRatio 8.92） | p2 **7 个 `+N` 徽标**、每行单值、无溢出（maxRatio 0→1.00，仅剩既有产品列） |
| 2 | 子域名表长主机裁切 | 子域名 p1/p2/p3 溢出单元格 2 / 2 / 1（maxRatio 1.13） | **0 / 0 / 0**（maxRatio 1.00） |

> 说明：#1 的 `a.el-link` 实际落在**「资产」Tab（AssetInfo2）** 的域名列与 IP 列（子域名 Tab 的 host 列在旧版是纯文本，0 个链接）。报告仍按任务要求同时修了两处，并在两处都做了度量。

---

## 1. 根因（代码级）

### 1.1 `unknown://` —— 协议来自不可信的 `service`

```tsx
// AssetInfo2.vue（修复前 223 行）
href={row.type === 'http' ? row.url : `${row.service}://${domainValue}`}
```

端口指纹没识别出服务时 `row.service === 'unknown'`，于是拼出 `unknown://s3.etoro.com`（`unknown` 不是注册 scheme，点了浏览器无动作）。生产 125 条资产里 `type=other & service=unknown` 有 115 条。

### 1.2 `href=""` —— 把空 `row.url` 当 href

```tsx
// AssetInfo2.vue（修复前 255 行，IP 列）
<ElLink href={row.url} underline={false}>{ipValue}</ElLink>
```

`type=other` 的行 `url` 恒为空 → `href=""`，且 `a` 带 `cursor:pointer`、下划线与主色，视觉上像可点链接，点击等于刷新当前页。

详情看板同一类问题（`site.host` 直接当 href → 相对路径）：

```html
<!-- 修复前 1317 行 -->
: href="site.type == 'http' ? site.url : site.host"
```

### 1.3 多值拼接 —— Vue 把数组 children 直接串起来

```jsx
<ElLink href={...}>{domainValue}</ElLink>
// 编译产物：Re(t) ? t : { default: () => [t] }
// t 是数组时 → 多个文本 children，无分隔符直接连成一串
```

生产库确实存在这种脏值（**21/125 条**），后端 `asset.host` 被粘成一串：

```text
comwww.ig.comnew.nasdaq.cometherscan.iowww.rakuten-sec.co.jpjfinfo.comwww.etoro.com
comwww.alipay.cominvestingolymptrade.compit.blockchain.comwww.advfn.com...
```

```bash
# 只读取证（未改库）
curl -s -X POST http://127.0.0.1:18082/api/assets/asset -H 'Content-Type: application/json' \
  -H "Authorization: Bearer <REDACTED>" -d '{"search":"","pageIndex":1,"pageSize":500,"filter":{}}'
# => total 125，domain 里出现 ≥2 个 TLD 的行 21 条，全部 type=other / service=unknown / url=""
```

---

## 2. 改动清单

| 文件 | 类型 | 内容 |
| --- | --- | --- |
| `frontend/src/utils/hostLink.ts` | 新增 | 链接/多值口径：`buildHref` `normalizeHttpUrl` `isHostname` `isIpLiteral` `resolveAssetHref` `resolveSubdomainHref` `splitHostValues` |
| `frontend/src/utils/hostLink.spec.ts` | 新增 | 11 个单测：unknown 不进 href、空 url、https 优先、拼接串拆分、通配符/IP 边界 |
| `frontend/src/components/HostValueCell/index.vue` | 新增 | 表格单元格组件：能确定 http(s) 才渲染 `<a>`，否则 `<span>`；多值「首值 + `+N`」+ 全量 title/tooltip；单值 ellipsis；复制按钮（多值复制全量） |
| `frontend/src/components/HostValueCell/index.ts` | 新增 | 导出 |
| `frontend/src/views/Asset/components/AssetInfo2.vue` | 修改 | 域名列/ IP 列改用 `HostValueCell`；详情看板站点卡片改 `siteHref()`；删除不再使用的 `copyToClipboard` / `ElLink` |
| `frontend/src/views/Asset/components/Subdomain.vue` | 修改 | host 列改用 `HostValueCell` + `resolveSubdomainHref`（只有 A/AAAA/CNAME 按 https 优先补链接） |

### 2.1 链接判定（`buildHref`）

- `from-data`（资产表）：只有 `row.url` 是合法 `http(s)` 绝对地址，或 `service ∈ {http, https}` 时才给链接；`service=unknown` / 空 url 一律 **纯文本**（不猜协议）。
- `assume-https`（子域名表）：值本身是合法 host（≥2 段、DNS label 合规、非通配符、非 IP）时补 `https://`，**https 优先**；`TXT/NS/MX/PTR` 记录不给链接。
- 非法值（`unknown://x`、带空格、`http://` 残缺、`*.x`）返回 `null`，组件渲染 `<span class="host-cell__value">`：`cursor:auto`、`text-decoration:none`、无 `<a>` 祖先。

### 2.2 多值渲染（`splitHostValues` + `HostValueCell`）

1. 数组 → 逐项展开；
2. 字符串按 `\s , ; |` 切分；
3. 仍粘连的单段按 **TLD 边界**再切（`co.jp` 等双段后缀优先），剥掉上一段残留的 TLD 残尾（`comwww.ig.com` → `www.ig.com`），丢弃不成形碎片；
4. 渲染：`首个值` + `+N` 徽标（`aria-label="共 N 个值"`），`title` 与 tooltip 列全量（换行分隔）；复制按钮在多值时复制全量。

### 2.3 溢出

`.host-cell__value { display:block; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap }` —— 单元格内唯一可伸缩元素，长值截断并由 `title` 兜底；容器 `min-width:0` 让 flex 允许收缩（旧实现是 `<a>` 直接参与 flex，收缩失败 → 224px 内容塞 198px 列）。

---

## 3. 度量对比（同一后端、同一页、同一 token）

浏览器：Agent Bridge 后台标签页（`document.visibilityState === "hidden"`），全部结论来自布局盒度量，不用截图。

### 3.1 修复前（生产 `http://127.0.0.1:18082/?cb=uifix1#/asset-information/index`）

```js
// 资产 Tab，第 1 页
{ links: 40, unknown: 17, empty: 17, overCount: 23, maxRatio: 1.27 }  // 1.27 来自「产品」标签列，与域名列无关
// 资产 Tab，第 2 页（多值脏值都在这页）
{ links: 40, unknown: 20, empty: 20, overCount: 29, maxRatio: 8.92, badges: 0,
  over: [ { cw: 198, sw: 664, t: "comwww.ig.comnew.nasdaq.cometherscan.iowww" },
          { cw: 128, sw: 158, t: "128.251.126.175" } ] }
// 子域名 Tab p1/p2/p3
[ { links: 0, over: 2, maxRatio: 1.13 }, { links: 0, over: 2 }, { links: 0, over: 1 } ]
```

### 3.2 修复后（`dist-pro` + 本地同源代理 `127.0.0.1:18083`，`/api` 与 `/images` 回源 18082）

```js
{
 "assetPage1": { "links": 6, "unknown": 0, "empty": 0, "https": 4, "http": 2,
                 "overCount": 2, "maxRatio": 1.27 },   // 2 个残留 + 1.27 均来自既有「产品」标签列（cw108/sw137）
 "assetPage2": { "links": 0, "unknown": 0, "empty": 0, "overCount": 0, "maxRatio": 1,
                 "badges": 7, "plain": 40 },
 "multiValue": { "text": "www.ig.com", "tag": "SPAN", "badge": "+5",
                 "titleLines": 6,
                 "title": ["www.ig.com","new.nasdaq.com","etherscan.io",
                           "www.rakuten-sec.co.jp","jfinfo.com","www.etoro.com"] },
 "subdomains": [ { "pg": "1", "links": 20, "unknown": 0, "empty": 0, "https": 20, "over": 0, "maxRatio": 1 },
                 { "pg": "2", "links": 20, "unknown": 0, "empty": 0, "https": 20, "over": 0, "maxRatio": 1 },
                 { "pg": "3", "links": 20, "unknown": 0, "empty": 0, "https": 20, "over": 0, "maxRatio": 1 } ]
}
```

### 3.3 纯文本单元格的样式证据（资产 Tab 第 2 页，40 个 span）

```js
{ spanCount: 40, tag: "SPAN", cursor: "auto", decoration: "none",
  ellipsis: "ellipsis", ancestorA: false,
  title: "www.ig.com\nnew.nasdaq.com\netherscan.io\nwww.rakuten-sec.co.jp\njfinfo.com\nwww.etoro.com" }
```

### 3.4 子域名 Tab 链接形态（p1 抽样）

```js
[ { h: "https://kaos.api.getdelta.io",      t: "kaos.api.getdelta.io" },
  { h: "https://tvinform.api.getdelta.io",  t: "tvinform.api.getdelta.io" },
  { h: "https://seasat.api.getdelta.io",    t: "seasat.api.getdelta.io" } ]
// 20/20 链接均为 https://，0 个 unknown://，0 个空 href
```

---

## 4. 验证命令与输出

```bash
# 1) 单测（链接判定 / 多值拆分）
cd frontend && npx vitest run src/utils/hostLink.spec.ts
# ✓ src/utils/hostLink.spec.ts (11 tests) 4ms
# Test Files 1 passed (1) | Tests 11 passed (11)

# 2) 前端构建
cd frontend && pnpm build:pro
# Build successful. Please see dist-pro directory

# 3) 后端构建（本次未改 Go 代码，按约束回归）
GOPROXY=https://goproxy.cn,direct go build ./...
# EXIT=0

# 4) 类型检查（只看本次涉及的文件；仓库原有大量 TS6133 存量告警）
cd frontend && pnpm ts:check 2>&1 | grep -E 'hostLink|HostValueCell|AssetInfo2|Asset/components/Subdomain'
# 仅剩存量告警：watch/url/props/getScreenshot/action 未使用（改动前即存在）
```

DOM 复测（Agent Bridge，后台）：

```js
// 修复前：生产 18082
// 修复后：本地 18083（node 静态服务 dist-pro + /api 代理到 18082，token 由生产 localStorage 复制）
const p0 = document.querySelectorAll('.el-tabs__content > .el-tab-pane')[0]
const links = [...p0.querySelectorAll('a')]
({
  links: links.length,
  unknown: links.filter(a => (a.getAttribute('href') || '').startsWith('unknown://')).length,
  empty: links.filter(a => !a.getAttribute('href')).length,
  over: [...p0.querySelectorAll('td .cell')].filter(c => c.scrollWidth > c.clientWidth + 1).length
})
// 修复前 p1: { links: 40, unknown: 17, empty: 17, over: 23 }
// 修复后 p1: { links: 6,  unknown: 0,  empty: 0,  over: 2 }  // 2 = 既有产品标签列
```

---

## 5. 未做 / 遗留（明确边界）

1. **后端脏数据未改**（约束：不动库）：21 条 `asset.host` 被粘成一串。前端只能做 TLD 边界还原（`www.ig.com` + `+5`，title 列全量），根因在资产写回侧（`AssetHandle`/`PortFingerprint` 多 host 聚合时未用分隔符或数组）。建议另开任务：`asset.host` 改为 `hosts: []string` 或在写入侧做 `join('\n')`。
2. **「产品」列 2 个溢出单元格**（`cw 108 / sw 137`，`CloudflareHTTP/3Cloudflare-product`）：既有问题，属标签列不是域名列，本次未动。
3. **ElTooltip 弹层内容未在后台标签页实测**：隐藏标签页里 EP tooltip 的 transition 不推进（`UI_FINDINGS` 已记为伪象），所以用 `title` 属性（6 行全量值）作为可验证证据；tooltip 绑定同一份 `values.join('\n')`。
4. **生产未部署**：本次只改仓库 + 本地构建验证；上线需按发布流程重建镜像。
