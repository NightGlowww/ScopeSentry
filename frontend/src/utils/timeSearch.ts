// 发现时间筛选 → 搜索条件后缀（ARL-Next bc22a10 的「12 维发现时间」口径）。
//
// 为什么走搜索表达式而不是给每个接口加 timeFrom 参数：
// app / mp / page-monitoring / subdomain-taker 这些列表接口本来只吃 search + filter，
// 后端 DSL 已支持 time>="YYYY-MM-DD HH:MM:SS"（字符串比较，库里的时间是可排序格式），
// 所以在前端拼一个条件就能筛，不必逐个改接口。
export const timeFromDays = (days: string): string => {
  const n = Number(days)
  if (!n) return ''
  const d = new Date(Date.now() - (n - 1) * 24 * 3600 * 1000) // 「今天」= 今天 00:00
  const pad = (x: number) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} 00:00:00`
}

// mergeTimeSearch 把时间条件并进用户搜索表达式。
// 用户表达式要用括号包起来：`a || b && time>=x` 的优先级会吃掉时间条件。
export const mergeTimeSearch = (search: string, days: string): string => {
  const from = timeFromDays(days)
  if (!from) return search || ''
  const base = (search || '').trim()
  return base ? `(${base}) && time>="${from}"` : `time>="${from}"`
}
