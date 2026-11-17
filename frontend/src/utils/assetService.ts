// 服务维度列表的展示规则：一行只展示前若干个样例 host，其余折叠成 "+N"。
// 折叠是纯展示逻辑，不影响后端分页与 counts——所以放在 utils 里单独测。
export const SERVICE_SAMPLE_LIMIT = 5

// 后端 pageSize 上限 200：超出或非正数会被后端静默回落成 20（不是报错），
// 所以前端先夹住，避免用户选了 200 却只拿到 20 条。
export const SERVICE_PAGE_SIZE_MAX = 200

// 表格默认每页条数，与 useTable 的默认值保持一致。
const DEFAULT_PAGE_SIZE = 20

export interface ServiceSamples {
  shown: string[]
  hidden: string[]
  hiddenCount: number
}

// splitSamples 把后端的 samples 裁成「可见 + 折叠」两段。
// 空白项与重复 host 都会被丢掉：同一 host 重复出现会让「样例」看起来比实际多，
// 用户会误判成多条资产。
export const splitSamples = (
  samples?: string[] | null,
  limit: number = SERVICE_SAMPLE_LIMIT
): ServiceSamples => {
  const bounded = Number.isFinite(limit) ? Math.max(0, Math.trunc(limit)) : SERVICE_SAMPLE_LIMIT
  const cleaned: string[] = []
  for (const item of samples ?? []) {
    const host = (item ?? '').trim()
    if (!host || cleaned.includes(host)) continue
    cleaned.push(host)
  }
  const shown = cleaned.slice(0, bounded)
  const hidden = cleaned.slice(bounded)
  return { shown, hidden, hiddenCount: hidden.length }
}

// foldedHostCount：折叠起来的 host 数。
// 后端 samples 只回前 5 个（$slice:5），剩下的 host 只能用 hostCount 推算——
// 不看 hostCount 的话折叠数永远是 0，"前 5 个 + 折叠"就只剩前 5 个。
// listed 是 samples 实际给出的条数（shown + hidden）；total 取 hostCount 与 listed 的较大值，
// 防止 hostCount 缺失或小于样例条数时把折叠数算成负数。
export const foldedHostCount = (hostCount?: number | null, listed = 0, shown = 0): number => {
  const safeHostCount = typeof hostCount === 'number' && Number.isFinite(hostCount) ? hostCount : 0
  const total = Math.max(safeHostCount, listed)
  return Math.max(0, total - shown)
}

// normalizeServiceKeyword：空的/纯空白的 keyword 直接省略（和历史读模型页一致），
// 别让请求里带一个空串——后端会当成一次无意义的子串匹配，日志里也看不出筛选有没有设过。
export const normalizeServiceKeyword = (value?: string | null): string | undefined => {
  const text = (value ?? '').trim()
  return text ? text : undefined
}

// clampServicePageSize：把页大小夹到 [1, SERVICE_PAGE_SIZE_MAX]。
// 非法值（NaN/负数/未定义）回落到表格默认页大小，而不是把脏值发给后端。
export const clampServicePageSize = (
  size?: number | null,
  fallback: number = DEFAULT_PAGE_SIZE
): number => {
  if (typeof size !== 'number' || !Number.isFinite(size)) return fallback
  const value = Math.trunc(size)
  if (value < 1) return 1
  return value > SERVICE_PAGE_SIZE_MAX ? SERVICE_PAGE_SIZE_MAX : value
}
