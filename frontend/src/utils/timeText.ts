import { formatToDateTime } from './dateUtil'

// 后端读模型返回 RFC3339 字符串（可能为 null，例如证书没有 notAfter）。
// dayjs(undefined) 会取「现在」，所以这里必须先挡住空值，否则空证书会被显示成当前时间。
export const timeText = (value?: string | null): string => {
  if (!value) return '-'
  const text = formatToDateTime(value)
  return text === 'Invalid Date' ? '-' : text
}

// 只到天，用于「最近出现/备案时间」这类不需要秒的列。
export const dateText = (value?: string | null): string => {
  if (!value) return '-'
  const text = formatToDateTime(value, 'YYYY-MM-DD')
  return text === 'Invalid Date' ? '-' : text
}
