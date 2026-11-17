// Nuclei 危害等级：后端 level 字段大小写不统一（Critical / critical / HIGH），
// 这里统一归一化，再映射到 ElTag 的语义色，列表和详情共用同一套口径。
export type SeverityLevel = 'critical' | 'high' | 'medium' | 'low' | 'info' | 'unknown'

export type SeverityTagType = 'danger' | 'warning' | 'success' | 'info' | 'primary'

export const SEVERITY_LEVELS: SeverityLevel[] = [
  'critical',
  'high',
  'medium',
  'low',
  'info',
  'unknown'
]

// normalizeSeverity 把任意写法落到六档之一；未知/空值归 unknown。
export const normalizeSeverity = (value?: string | null): SeverityLevel => {
  const level = (value ?? '').trim().toLowerCase()
  return (SEVERITY_LEVELS as string[]).includes(level) ? (level as SeverityLevel) : 'unknown'
}

// severityTagType 只挑主题语义色：critical/high 用危险色，medium 用警告色，
// low 用成功色，info 用信息色，unknown 用中性色。
export const severityTagType = (value?: string | null): SeverityTagType => {
  switch (normalizeSeverity(value)) {
    case 'critical':
    case 'high':
      return 'danger'
    case 'medium':
      return 'warning'
    case 'low':
      return 'success'
    case 'info':
      return 'info'
    default:
      return 'primary'
  }
}
