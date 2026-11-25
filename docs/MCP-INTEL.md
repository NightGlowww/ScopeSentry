# MCP 工具：`intel_for_assets`

按项目/主机/技术名产出 **CVE 情报**，供 ARTEX 的 planner 消费。

> **它是情报，不是漏洞。** 拿不到版本时 `applicability` 一律 `unknown`，
> 既不会降级成 `likely`，也不会进「疑似漏洞」。

## 入参

```json
{
  "project": "etoro-2026-11",          // 可选：项目 ObjectID 或名称
  "hosts": ["a.example.com"],          // 可选：与 project 二选一，也可都不给（全库）
  "techs": ["Nginx", "Tengine"],       // 可选：限定技术名
  "since_days": 90,                    // 只看这段时间内发布的 CVE，默认 90
  "limit": 20                          // 条数上限，默认 20，最多 100
}
```

## 出参

```json
{
  "items": [{
    "cve": "CVE-2026-110694", "title": "…", "url": "https://nvd.nist.gov/vuln/detail/…",
    "severity": "MEDIUM", "published": "2026-10-14", "source": "nvd", "summary": "…",
    "affected": [{
      "host": "h5.open.ys7.com", "tech": "Tengine",
      "version": null, "version_source": null,
      "applicability": "unknown",
      "reason": "该资产暴露了 Tengine 指纹，但响应头/正文/banner 均未给出版本号，无法与受影响范围比对"
    }],
    "unmatched_hosts": ["h5.open.ys7.com"]
  }],
  "stats": { "assets_scanned": 3, "with_version": 0, "without_version": 3 }
}
```

`applicability` 三种取值的判据：

| 值 | 条件 |
| --- | --- |
| `confirmed` | 有版本 **且** 落在 CVE 声明的 CPE 区间内 |
| `likely` | 有版本 **且** 产品匹配、但 CVE 没给可比对的区间 |
| `unknown` | 拿不到版本（→ 同时进 `unmatched_hosts`） |

**版本明确落在区间之外**（阴性对照）**不收录** —— 它不是「无法判定」，
把它放进 `affected` 会让调用方误以为一堆机器可疑。同理，一条 CVE 若所有资产都是阴性，整条不进 `items`。

## 版本从哪来

按顺序取第一个拿到的，并写进 `version_source`：

| source | 来源 |
| --- | --- |
| `fingerprint` | 指纹名自带版本，如 `IIS:10.0` |
| `header` | 响应头 `Server: nginx/1.24.0` |
| `banner` | `webServer` 字段 |
| `body` | 响应正文前 4KB |
| `null` | 都没有 → `applicability=unknown` |

## 限速与缓存

| 项 | 值 |
| --- | --- |
| NVD 限速 | 无 key 时 5 次/30s；本工具串行，请求间隔 6s |
| 单次请求窗口 | **≤120 天**（NVD 超限直接 404，看起来像「没数据」） |
| 回溯上限 | 4 个窗口（480 天）；`since_days` 更大也只翻到 480 天 |
| 缓存 | 集合 `cve_cache`，键 `(tech, since_days)`，TTL **6 小时** |
| 失败语义 | NVD 404 = 该窗口无命中（空结果，非错误）；其它非 200 记日志并跳过该技术名 |

## 安全与治理

- 需要的 scope：`asset:read`（已在 `mcpguard` 登记；未登记工具会被默认拒绝）
- 只读：不写资产库、不产生漏洞记录、不改任何扫描状态

## 已知边界（必须知道）

1. **拿不到版本就是这个工具的瓶颈**。实测 eToro 项目 12 台 Nginx 资产，
   `with_version=0` —— 响应头里根本没有版本号。这种情况下工具会如实返回
   `77/89 无版本` 这类数字，**不会美化**。真正提升需要主动探测（发请求读 banner /
   指纹规则带版本 / POC 探测），那是下一步的事。
2. CPE 区间匹配是产品级的（`cpeProduct` 含包含关系即算匹配），
   不做 vendor 精确匹配；对同名不同产品会有噪声，`reason` 里写清了依据。
3. `technologies` 为空或没有指纹的资产不参与统计。
