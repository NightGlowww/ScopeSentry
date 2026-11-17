// 标签配色（issue #217）：按标签名稳定取色，同一个标签在列表、卡片、详情里永远同色。
//
// 默认只从 Element Plus 的语义色里挑（primary/success/info/warning/danger）：
// 这些颜色由主题变量驱动，日间/夜间切换会自动适配，不用自己维护一套 hex。
// 用户给某个标签指定固定颜色时，用后端下发的覆盖表；覆盖值可以是语义色名，
// 也可以是 #rgb / #rrggbb——hex 走 ElTag 的 color 属性，语义色走 type。
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

const isHexColor = (value: string) => /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(value)

// tagColorType 返回标签默认应使用的 ElTag type；空标签统一用 info。
export const tagColorType = (tag?: string | null): TagColorType => {
  const text = (tag ?? '').trim()
  if (!text) return 'info'
  return TAG_COLOR_TYPES[hashTag(text) % TAG_COLOR_TYPES.length]
}

export interface TagColorProps {
  type?: TagColorType
  color?: string
}

// tagColorProps 合并「用户覆盖」与「默认配色」，返回可直接展开给 ElTag 的属性。
// overrides 来自 GET /api/assets/tag_color/list；非法/空覆盖会被忽略。
export const tagColorProps = (
  tag?: string | null,
  overrides?: Record<string, string> | null
): TagColorProps => {
  const text = (tag ?? '').trim()
  const override = text && overrides ? overrides[text] : undefined
  if (override) {
    const value = override.trim()
    if (isHexColor(value)) {
      return { color: value }
    }
    const semantic = TAG_COLOR_TYPES.find((item) => item === value.toLowerCase())
    if (semantic) {
      return { type: semantic }
    }
  }
  return { type: tagColorType(text) }
}
