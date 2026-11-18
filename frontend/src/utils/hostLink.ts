// 资产 / 子域名表格的「链接 + 多值」口径（docs/audit/UI_FINDINGS.md #1 / #2）
//
// 背景：
//   1) 列表里的 service 来自端口指纹，可能是 unknown；旧实现直接拼
//      `${service}://${host}`，于是出现 href="unknown://s3.etoro.com"（点不动）。
//   2) IP 列把 row.url 直接当 href，url 为空时渲染 href=""（点击等于刷新当前页）。
//   3) 多值时后端可能给数组、给换行/逗号分隔的字符串，历史脏数据里甚至把几个
//      host 无分隔地粘成一串（...etoro.comcomwww.ig.com...），Vue 把数组 children
//      直接拼在一起，整串被塞进同一个 <a>。
//
// 这里集中两条规则，调用方（AssetInfo2.vue / Subdomain.vue）只消费结果：
//   - buildHref：只有能确定 http(s) 时才返回链接。
//       from-data   —— 只认数据自带的 http(s)（row.url 或 service=http/https）；
//       assume-https —— 值本身是合法 host 时补 https（https 优先）。
//   - splitHostValues：数组 / 分隔符 / 无分隔拼接 → 单值数组，供「首值 + N」渲染。

const HTTP_URL_RE = /^https?:\/\/[^\s]+$/i
const UNSAFE_RE = /[\s/\\?#<>"']/
const IPV4_RE = /^(?:\d{1,3}\.){3}\d{1,3}$/
const IPV6_RE = /^\[?[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,}\]?$/i
const LABEL_RE = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i

// 常见 TLD：只用于把「无分隔拼接」的脏值拆成多段，不参与合法性判定。
// 双段后缀必须排在单段前面（co.jp 先于 jp），否则会从中间切断。
const TLDS = [
  'co.jp', 'com.cn', 'net.cn', 'org.cn', 'gov.cn', 'edu.cn', 'com.hk', 'com.tw',
  'co.uk', 'co.kr', 'co.in', 'co.nz', 'co.za', 'co.id', 'com.au', 'com.br',
  'com.sg', 'com.my', 'com.tr', 'com.mx', 'com.ar', 'com.ru', 'co.il',
  'com', 'net', 'org', 'io', 'jp', 'cn', 'hk', 'tw', 'uk', 'kr', 'in', 'nz',
  'za', 'id', 'au', 'br', 'sg', 'my', 'tr', 'mx', 'ar', 'ru', 'il', 'de', 'fr',
  'nl', 'se', 'no', 'fi', 'dk', 'it', 'es', 'pt', 'pl', 'cz', 'at', 'be', 'ch',
  'gr', 'ro', 'hu', 'ua', 'us', 'ca', 'eu', 'ai', 'app', 'dev', 'cloud', 'tech',
  'info', 'biz', 'me', 'tv', 'xyz', 'site', 'online', 'shop', 'top', 'cc', 'ws',
  'name', 'pro', 'mobi', 'asia', 'club', 'vip', 'ltd', 'group', 'store', 'space',
  'website', 'host', 'press', 'link', 'live', 'life', 'world', 'today', 'news',
  'blog', 'wiki', 'zone', 'network', 'systems', 'solutions', 'services', 'agency',
  'digital', 'media', 'studio', 'design', 'fun', 'icu', 'ink', 'one', 'run', 'rip'
]
const TLD_BY_LENGTH = [...TLDS].sort((a, b) => b.length - a.length)
const CONCAT_BOUNDARY_RE = new RegExp(
  `\\.(?:${TLD_BY_LENGTH.map((t) => t.replace(/\./g, '\\.')).join('|')})(?=[a-z0-9])`,
  'gi'
)

/** 是否已经是可用的 http(s) 绝对地址 */
export const isHttpUrl = (value?: string | null): boolean => {
  const v = (value ?? '').trim()
  return HTTP_URL_RE.test(v)
}

/** IPv4 / IPv6 字面量（IP 不做「补 https」之外的猜测） */
export const isIpLiteral = (value?: string | null): boolean => {
  const v = (value ?? '').trim().replace(/^\[|\]$/g, '')
  if (!v) return false
  if (IPV4_RE.test(v)) {
    return v.split('.').every((part) => Number(part) <= 255)
  }
  return v.includes(':') && IPV6_RE.test(v)
}

/** 合法主机名：≥2 段、每段符合 DNS label 规则；通配符/带路径的值一律不认 */
export const isHostname = (value?: string | null): boolean => {
  const v = (value ?? '').trim().toLowerCase()
  if (!v || v.length > 253 || UNSAFE_RE.test(v)) return false
  if (v.startsWith('*.') || v.includes('*')) return false
  if (isIpLiteral(v)) return false
  const labels = v.split('.')
  return labels.length >= 2 && labels.every((label) => LABEL_RE.test(label))
}

/** 归一化已经带 http(s) 的值；不是绝对 http(s) 地址就返回 null */
export const normalizeHttpUrl = (value?: string | null): string | null => {
  const v = (value ?? '').trim()
  if (!isHttpUrl(v)) return null
  try {
    // 用 URL 兜一层：'http://' / 'https:///x' 这类残缺值直接判死
    return new URL(v).hostname ? v : null
  } catch {
    return null
  }
}

export type HrefPolicy = 'from-data' | 'assume-https'

/**
 * 值 → 可点链接。
 * from-data：只认 http(s) 绝对地址（协议必须由数据证明，unknown 不猜）。
 * assume-https：合法 host / IP 补 https，https 优先。
 * 返回 null 时调用方必须渲染纯文本（不可点、无 pointer、无下划线）。
 */
export const buildHref = (value?: string | null, policy: HrefPolicy = 'from-data'): string | null => {
  const v = (value ?? '').trim()
  if (!v) return null
  const direct = normalizeHttpUrl(v)
  if (direct) return direct
  if (policy !== 'assume-https') return null
  if (v.includes('://') || UNSAFE_RE.test(v)) return null
  if (!isHostname(v) && !isIpLiteral(v)) return null
  return `https://${v}`
}

/** 资产列表的链接：协议只能由数据给出（row.url / service=http|https） */
export const resolveAssetHref = (
  row: { type?: string; url?: string; service?: string } | null | undefined,
  values: string[]
): string | null => {
  const direct = normalizeHttpUrl(row?.url)
  if (direct) return direct
  const service = (row?.service ?? '').trim().toLowerCase()
  const host = values[0] ?? ''
  if (!host) return null
  if (service === 'https' || service === 'http') {
    return buildHref(`${service}://${host}`, 'from-data')
  }
  return null
}

/** 子域名表：A/AAAA/CNAME 指向真实主机，按 https 优先补协议；其它记录类型不猜 */
const LINKABLE_DNS_TYPES = ['a', 'aaaa', 'cname']
export const resolveSubdomainHref = (
  row: { type?: string } | null | undefined,
  host?: string | null
): string | null => {
  const type = (row?.type ?? '').trim().toLowerCase()
  if (!LINKABLE_DNS_TYPES.includes(type)) return null
  return buildHref(host, 'assume-https')
}

/** 单段里若粘连了上一个 host 的 TLD 残尾（comwww.ig.com），剥掉再判断 */
const stripLeadingTldResidue = (segment: string): string => {
  const lower = segment.toLowerCase()
  for (const tld of TLD_BY_LENGTH) {
    if (!lower.startsWith(tld) || segment.length <= tld.length) continue
    const rest = segment.slice(tld.length)
    if (/^[a-z0-9]/i.test(rest) && rest.includes('.')) return rest
  }
  return segment
}

const isDisplayableValue = (value: string): boolean =>
  isHttpUrl(value) || isHostname(value) || isIpLiteral(value)

/** 把一段无分隔的脏值按 TLD 边界切开；切不动时原样返回 */
const splitConcatenated = (token: string): string[] => {
  const cuts: number[] = []
  CONCAT_BOUNDARY_RE.lastIndex = 0
  let match: RegExpExecArray | null
  while ((match = CONCAT_BOUNDARY_RE.exec(token))) {
    cuts.push(match.index + match[0].length)
  }
  if (cuts.length === 0) return [token]
  const segments: string[] = []
  let start = 0
  for (const cut of cuts) {
    if (cut > start) segments.push(token.slice(start, cut))
    start = cut
  }
  if (start < token.length) segments.push(token.slice(start))
  if (segments.length > 1) segments[0] = stripLeadingTldResidue(segments[0])
  return segments
}

/**
 * 任意形态的值 → 单值数组。
 * 支持 string[] / 换行、逗号、分号、竖线、空格分隔 / 无分隔拼接的脏值。
 * 会丢掉拆分后不成形的碎片（如 `comwww.ig.com` 前面的 `com`），连续重复值去重。
 */
export const splitHostValues = (value: unknown): string[] => {
  const raw: string[] = []
  const collect = (input: unknown): void => {
    if (input === null || input === undefined) return
    if (Array.isArray(input)) {
      input.forEach(collect)
      return
    }
    const text = String(input).trim()
    if (text) raw.push(text)
  }
  collect(value)

  const out: string[] = []
  const push = (item: string): void => {
    const v = item.trim().replace(/^[,;|]+|[,;|]+$/g, '')
    if (!v) return
    if (out[out.length - 1] === v) return
    out.push(v)
  }

  for (const item of raw) {
    for (const part of item.split(/[\s,;|]+/)) {
      if (!part) continue
      const segments = splitConcatenated(part)
      if (segments.length <= 1) {
        push(part)
        continue
      }
      // 拼接串只保留成形的片段；全都不成形时保留原文，别丢数据
      const valid = segments.filter(isDisplayableValue)
      for (const seg of valid.length ? valid : segments) push(seg)
    }
  }
  return out
}
