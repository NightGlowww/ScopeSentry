import { describe, expect, it } from 'vitest'
import { dateText, timeText } from './timeText'

describe('read model time text', () => {
  it('renders an empty placeholder for missing certificate dates', () => {
    // 证书 notAfter 可能是 null；不能让 dayjs 取当前时间冒充数据
    expect(timeText(null)).toBe('-')
    expect(timeText(undefined)).toBe('-')
    expect(timeText('')).toBe('-')
  })

  it('formats RFC3339 timestamps from the read models', () => {
    expect(timeText('2026-11-18T04:30:00+08:00')).toBe('2026-11-18 04:30:00')
    expect(dateText('2026-11-18T04:30:00+08:00')).toBe('2026-11-18')
  })

  it('falls back to the placeholder for unparsable input', () => {
    expect(timeText('not-a-time')).toBe('-')
  })
})
