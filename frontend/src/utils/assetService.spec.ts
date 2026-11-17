import { describe, expect, it } from 'vitest'
import {
  SERVICE_PAGE_SIZE_MAX,
  SERVICE_SAMPLE_LIMIT,
  clampServicePageSize,
  foldedHostCount,
  normalizeServiceKeyword,
  splitSamples
} from './assetService'

describe('splitSamples', () => {
  it('shows the first five hosts and folds the rest', () => {
    const samples = ['h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'h7']
    const { shown, hidden, hiddenCount } = splitSamples(samples)
    expect(shown).toEqual(['h1', 'h2', 'h3', 'h4', 'h5'])
    expect(hidden).toEqual(['h6', 'h7'])
    expect(hiddenCount).toBe(2)
    expect(SERVICE_SAMPLE_LIMIT).toBe(5)
  })

  it('leaves short lists untouched', () => {
    const { shown, hidden, hiddenCount } = splitSamples(['h1', 'h2'])
    expect(shown).toEqual(['h1', 'h2'])
    expect(hidden).toEqual([])
    expect(hiddenCount).toBe(0)
  })

  it('drops blank and duplicated hosts before folding', () => {
    const { shown, hidden } = splitSamples(['h1', ' h1 ', '', '  ', 'h2', 'h2', 'h3'])
    expect(shown).toEqual(['h1', 'h2', 'h3'])
    expect(hidden).toEqual([])
  })

  it('handles missing samples without throwing', () => {
    expect(splitSamples(undefined)).toEqual({ shown: [], hidden: [], hiddenCount: 0 })
    expect(splitSamples(null)).toEqual({ shown: [], hidden: [], hiddenCount: 0 })
  })

  it('accepts an explicit limit, including zero', () => {
    expect(splitSamples(['h1', 'h2'], 1)).toEqual({ shown: ['h1'], hidden: ['h2'], hiddenCount: 1 })
    expect(splitSamples(['h1'], 0)).toEqual({ shown: [], hidden: ['h1'], hiddenCount: 1 })
  })
})

describe('foldedHostCount', () => {
  it('counts the hosts the backend did not return as samples', () => {
    // 后端 samples 只回 5 个，hostCount 才是真实主机数
    expect(foldedHostCount(120, 5, 5)).toBe(115)
  })

  it('folds surplus samples when more than the limit come back', () => {
    expect(foldedHostCount(8, 8, 5)).toBe(3)
  })

  it('never returns a negative fold', () => {
    expect(foldedHostCount(3, 5, 5)).toBe(0)
    expect(foldedHostCount(0, 0, 0)).toBe(0)
  })

  it('falls back to the listed samples when hostCount is missing or invalid', () => {
    expect(foldedHostCount(undefined, 7, 5)).toBe(2)
    expect(foldedHostCount(null, 7, 5)).toBe(2)
    expect(foldedHostCount(Number.NaN, 7, 5)).toBe(2)
  })
})

describe('normalizeServiceKeyword', () => {
  it('trims a real keyword', () => {
    expect(normalizeServiceKeyword('  redis ')).toBe('redis')
  })

  it('omits empty and blank keywords', () => {
    expect(normalizeServiceKeyword('')).toBeUndefined()
    expect(normalizeServiceKeyword('   ')).toBeUndefined()
    expect(normalizeServiceKeyword(undefined)).toBeUndefined()
    expect(normalizeServiceKeyword(null)).toBeUndefined()
  })
})

describe('clampServicePageSize', () => {
  it('keeps a normal page size', () => {
    expect(clampServicePageSize(20)).toBe(20)
    expect(clampServicePageSize(200)).toBe(SERVICE_PAGE_SIZE_MAX)
  })

  it('caps oversized page sizes at the backend limit', () => {
    expect(clampServicePageSize(500)).toBe(200)
    expect(clampServicePageSize(SERVICE_PAGE_SIZE_MAX + 1)).toBe(SERVICE_PAGE_SIZE_MAX)
  })

  it('floors non-positive and non-numeric values', () => {
    expect(clampServicePageSize(0)).toBe(1)
    expect(clampServicePageSize(-10)).toBe(1)
    expect(clampServicePageSize(3.9)).toBe(3)
    expect(clampServicePageSize(Number.NaN)).toBe(20)
    expect(clampServicePageSize(undefined)).toBe(20)
    expect(clampServicePageSize(null, 10)).toBe(10)
  })
})
