import { describe, expect, it } from 'vitest'
import {
  HTTP_STATUS_BANDS,
  httpStatusBand,
  httpStatusTagType,
  normalizeStatusFilter,
  safeHttpUrl
} from './httpStatus'

describe('httpStatusBand', () => {
  it('maps codes onto the four bands', () => {
    expect(httpStatusBand(200)).toBe('2xx')
    expect(httpStatusBand(204)).toBe('2xx')
    expect(httpStatusBand(301)).toBe('3xx')
    expect(httpStatusBand(404)).toBe('4xx')
    expect(httpStatusBand(503)).toBe('5xx')
  })

  it('accepts numeric strings from legacy records', () => {
    expect(httpStatusBand('404')).toBe('4xx')
    expect(httpStatusBand(' 301 ')).toBe('3xx')
  })

  it('returns null for missing or out-of-range values', () => {
    expect(httpStatusBand(undefined)).toBeNull()
    expect(httpStatusBand(null)).toBeNull()
    expect(httpStatusBand(0)).toBeNull()
    expect(httpStatusBand('abc')).toBeNull()
    expect(httpStatusBand(199)).toBeNull()
    expect(httpStatusBand(600)).toBeNull()
  })
})

describe('httpStatusTagType', () => {
  it('colors each band with the documented semantic color', () => {
    expect(httpStatusTagType(200)).toBe('success')
    expect(httpStatusTagType(302)).toBe('info')
    expect(httpStatusTagType(403)).toBe('warning')
    expect(httpStatusTagType(500)).toBe('danger')
  })

  it('falls back to a neutral color without a usable code', () => {
    expect(httpStatusTagType(null)).toBe('primary')
    expect(httpStatusTagType(0)).toBe('primary')
  })
})

describe('normalizeStatusFilter', () => {
  it('prefers an exact code over the band', () => {
    expect(normalizeStatusFilter('4xx', '404')).toBe('404')
  })

  it('passes through a band when no exact code is given', () => {
    for (const band of HTTP_STATUS_BANDS) {
      expect(normalizeStatusFilter(band, '')).toBe(band)
    }
    expect(normalizeStatusFilter('5XX')).toBe('5xx')
  })

  it('ignores malformed codes without dropping the band', () => {
    expect(normalizeStatusFilter('4xx', '40')).toBe('4xx')
    expect(normalizeStatusFilter('4xx', '999')).toBe('4xx')
    expect(normalizeStatusFilter('4xx', '4o4')).toBe('4xx')
  })

  it('returns undefined when nothing usable is selected', () => {
    expect(normalizeStatusFilter('', '')).toBeUndefined()
    expect(normalizeStatusFilter(undefined, undefined)).toBeUndefined()
    expect(normalizeStatusFilter('6xx', '999')).toBeUndefined()
  })
})

describe('safeHttpUrl', () => {
  it('keeps absolute http(s) urls', () => {
    expect(safeHttpUrl('https://example.com/a')).toBe('https://example.com/a')
    expect(safeHttpUrl('HTTP://example.com')).toBe('HTTP://example.com')
  })

  it('drops scheme-less and non-http values', () => {
    expect(safeHttpUrl('example.com:8080/a')).toBeUndefined()
    expect(safeHttpUrl('javascript:alert(1)')).toBeUndefined()
    expect(safeHttpUrl('')).toBeUndefined()
    expect(safeHttpUrl(null)).toBeUndefined()
  })
})
