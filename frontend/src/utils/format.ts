/**
 * 字节数人性化显示：3145728 → 3.0 MB。
 * 列表里长度/大小列直接显示原始字节数很难读。
 */
export const formatBytes = (input: number | string | null | undefined): string => {
  const n = Number(input)
  if (!Number.isFinite(n) || n <= 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  const text = v >= 100 ? Math.round(v).toString() : v.toFixed(1)
  return `${text} ${units[i]}`
}
