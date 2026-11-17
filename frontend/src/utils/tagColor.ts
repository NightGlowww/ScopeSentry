// 标签配色（issue #217）：按标签名稳定取色，同一个标签在列表、卡片、详情里永远同色。
//
// 只从 Element Plus 的语义色里挑（primary/success/info/warning/danger）：
// 这些颜色由主题变量驱动，日间/夜间切换会自动适配，不用自己维护一套 hex。
// 想给单个标签指定固定颜色时，后续可以在这层加一张覆盖表，调用方不用改。
export type TagColorType = 'primary' | 'success' | 'info' | 'warning' | 'danger'

export const TAG_COLOR_TYPES: TagColorType[] = ['primary', 'success', 'info', 'warning', 'danger']

// 简单字符串哈希（31 进制）。只要求同一输入稳定输出，不需要密码学强度。
const hashTag = (text: string): number => {
  let hash = 0
  for (const ch of text) {
    hash = (hash * 31 + (ch.codePointAt(0) ?? 0)) >>> 0
  }
  return hash
}

// tagColorType 返回标签应使用的 ElTag type；空标签统一用 info。
export const tagColorType = (tag?: string | null): TagColorType => {
  const text = (tag ?? '').trim()
  if (!text) return 'info'
  return TAG_COLOR_TYPES[hashTag(text) % TAG_COLOR_TYPES.length]
}
