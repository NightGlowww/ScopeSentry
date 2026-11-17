import { describe, expect, it } from 'vitest'
import { TAG_COLOR_TYPES, tagColorProps, tagColorType } from './tagColor'

describe('tagColorType', () => {
  it('is deterministic for the same tag', () => {
    const first = tagColorType('prod')
    for (let i = 0; i < 5; i++) {
      expect(tagColorType('prod')).toBe(first)
    }
  })

  it('ignores surrounding whitespace so the same tag keeps one color', () => {
    expect(tagColorType('  prod  ')).toBe(tagColorType('prod'))
  })

  it('falls back to info for empty or missing tags', () => {
    expect(tagColorType('')).toBe('info')
    expect(tagColorType('   ')).toBe('info')
    expect(tagColorType(undefined)).toBe('info')
    expect(tagColorType(null)).toBe('info')
  })

  it('always returns a palette entry', () => {
    for (const tag of ['a', 'b', '测试', 'web', 'cdn', '10.0.0.1', '🙂']) {
      expect(TAG_COLOR_TYPES).toContain(tagColorType(tag))
    }
  })

  it('spreads a realistic tag set across the palette', () => {
    const tags = ['prod', 'test', 'cdn', 'internal', 'api', 'admin', 'staging', 'legacy']
    const used = new Set(tags.map((tag) => tagColorType(tag)))
    // 不要求均匀，但八个常用标签至少落到三种颜色上，否则配色没意义
    expect(used.size).toBeGreaterThanOrEqual(3)
  })
})
describe('tagColorProps', () => {
  it('falls back to the deterministic type without overrides', () => {
    expect(tagColorProps('prod')).toEqual({ type: tagColorType('prod') })
  })

  it('applies a semantic override', () => {
    expect(tagColorProps('prod', { prod: 'danger' })).toEqual({ type: 'danger' })
  })

  it('passes hex overrides through the color attribute', () => {
    expect(tagColorProps('prod', { prod: '#ff8800' })).toEqual({ color: '#ff8800' })
  })

  it('ignores unknown or empty overrides', () => {
    expect(tagColorProps('prod', { prod: 'rainbow' })).toEqual({ type: tagColorType('prod') })
    expect(tagColorProps('prod', { prod: '' })).toEqual({ type: tagColorType('prod') })
  })

  it('matches overrides after trimming the tag name', () => {
    expect(tagColorProps('  prod  ', { prod: 'success' })).toEqual({ type: 'success' })
  })
})
