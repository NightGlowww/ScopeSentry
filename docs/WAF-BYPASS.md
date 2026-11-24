# WAF 绕过：工程化设计与闭环

> 状态：v1 已落地（检测 + 画像入库 + 与漏洞扫描同链）　2026-11-24

## 1. 为什么要有这个功能

漏洞扫描（nuclei）的载荷绝大多数走 query 或 body。目标挂了 WAF 时，结果就是**一片零**——
不是没有洞，是根本没送到源站。

waf-harness 在 9 个厂商、48 台生产主机上的实测给出了根因：**WAF 只检查它看得懂的编码形态**。
压缩体（gzip）、非常规字符集（UTF-16BE）、多数头部载体、multipart 文件部件，普遍不被检查。

所以这件事不该停在「人肉试」，要变成扫描链路里的一等公民：
**先测出哪个载体不被检查，再决定怎么打，最后验证真的到了源站。**

## 2. 闭环五段

```
① 资产发现 ──► ② WAF 指纹 ──► ③ 载体画像 ──► ④ 按画像扫描 ──► ⑤ 证据回填
  AssetHttp      厂商识别       三态矩阵         载体感知          vuln + viaCarrier
                                   │                                    │
                                   └──────────► waf_profile ◄───────────┘
```

| 段 | 输入 | 输出 | 实现位置 | 状态 |
| --- | --- | --- | --- | --- |
| ① 资产 | 任务目标 | AssetHttp / UrlResult | 既有链（subdomain→port→urlscan） | 已有 |
| ② 指纹 | URL | `cloudflare`/`imperva`/`akamai`/`cloudfront`/`azure-frontdoor`/`f5`/`sucuri`/`safeline`/`google-frontend` | `modules/wafscan/probe.go::fingerprint` | **本轮新增** |
| ③ 画像 | URL | 11 载体 × 6 攻击族，逐格三态 | `modules/wafscan/probe.go::Probe` | **本轮新增** |
| ④ 扫描 | 画像 | 按可用载体构造请求 | 待接（见 §5） | 设计完成 |
| ⑤ 回填 | 命中 | `vulnerability.viaCarrier` + `waf_profile` | 待接（见 §5） | 设计完成 |

## 3. 三态判定（这条是整个功能的正确性基础）

每个「载体 × 攻击族」必须先发 **benign** 再发 **payload**，只发 payload 得出的结论全是错的。

| 判定 | 条件 | 含义 |
| --- | --- | --- |
| `inspected` | benign 放行、payload 被拦 | 该载体被检查 |
| `not_inspected` | 两者都放行 | **该载体不被检查 → 可绕过** |
| `inconclusive` | benign 就被拦 | 不可测（不是防御好） |
| `app_rejected` | 两者同状态且非 2xx | 应用拒绝，无法归因 |

把 `inconclusive` 和 `not_inspected` 分开是关键：waf-harness 在 48 台生产主机上实测，
**13 台（27%）第一次对照就被放行**——没有这条闸，它们会被报成「7 类攻击全部可绕过」，全是假战果。

## 4. 载体清单（按实测命中率排序）

| 排名 | 载体 | 跨主机命中 | 说明 |
| --- | --- | --- | --- |
| 1 | `json-gzip-u16` | 36 | gzip + UTF-16BE 双层 |
| 2-4 | `json-gzip` / `form-gzip` / `plain-gzip` | 35 / 34 / 33 | WAF 不解 `Content-Encoding` |
| 5-7 | `json-utf16be` / `xml-gzip` / `mp-gzip` | 30 | 字符集 / multipart 压缩 |
| 8-13 | `header-ua` / `header-xfh` / `header-referer` / `header-cookie` / `header-xff` / `header-origurl` | 25→12 | 头部载体，多数引擎不检查 |
| 14-19 | `query` / `json` / `plain` / `xml` / `form` / `mp-field` | 10→6 | 明文 body，检查最严 |
| 20 | `path` | 1 | 路径段 |

一句话：**WAF 只检查它看得懂的编码**。优先级是 压缩 → 字符集 → 头部 → 明文 body → multipart。

## 5. ④⑤ 两段的设计（未实现部分）

### ④ 按画像扫描

画像给出「这台主机 `json-gzip` 可绕」之后，有三种接法，按侵入性从低到高：

**A. 模板变体（推荐先做）**
对同一模板生成一个 `-bypass` 变体：matcher 完全不变，只把请求体换成 gzip + `Content-Encoding: gzip`。
nuclei 支持多模板并行，命中即记 `viaCarrier=json-gzip`。
优点：不改 nuclei 本体、可审计、可回滚。

**B. 本地 bypass 代理**
节点上跑一个 Go 代理，nuclei `-proxy` 指过去，代理对每个请求体做 gzip/UTF-16BE 重编码。
缺点：HTTPS 需要 MITM（自签 CA 注入容器信任链），运维成本高。

**C. 自建重放引擎**
解析模板 → 按载体重发 → 用服务端已有的 `vulnverify.Check(templateContent, rawResponse)` 复验。
最彻底，但要重写一遍 nuclei 的请求模型，最后做。

### ⑤ 证据回填

命中的漏洞写入 `vulnerability.viaCarrier`（值为载体名），并回写 `waf_profile.usedCarriers`。
这样报告里能写清「这个洞是在 WAF 存在的前提下、通过 X 载体打到的」，比只写「绕过了」有说服力。

## 6. 与去重的关系

同一洞经不同载体命中时，`(vulnid, url)` 相同 → 唯一索引**只保留一条**，`viaCarrier` 记最先命中的载体。
这是刻意的：载体是实现细节，不是不同的洞。

## 7. 运行参数与开关

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| 每宿主速率 | 1 rps（串行 + 1s 间隔） | 与 waf-harness 的纪律一致 |
| 每宿主请求数 | 11 载体 × 6 族 × 2 + 2×6×2 ≈ 156 | 单台约 2.6 分钟 |
| 同时探测主机数 | 2 | `probeSem` |
| 同任务内同 host | 只测一次 | `measured sync.Map` |
| 关闭 | `WAFSCAN=off` | |

## 8. 已验证的行为

- 官方 GeoServer 3.0.0 容器上，`wafscan` 对 `/geoserver/wfs` 的判定与人工一致。
- 数据目录泄露模板（只读）命中并抽出 `/opt/geoserver_data`；
  XInclude 模板命中并抽出 remember-me key `geoserver` 与 admin 摘要 `digest1:...`；
  错误数据目录的模板**零命中**（阴性对照）。

## 9. 已知边界

1. 画像只测「WAF 是否检查该载体」，**不证明源站能解析**。gzip 的源站解压能力要单独确认
   （waf-harness 用「响应体字节差」判定：benign 与 payload 长度一致 ⇒ 没解压）。
2. DataDome / PerimeterX 这类 bot 管理不在本模块射程内——它们拦的是 TLS 指纹和 IP 信誉，
   要 `impersonate` 或换出口，属于另一层。
3. 一次画像 156 个请求，对敏感目标应先确认 scope 允许主动测试。
