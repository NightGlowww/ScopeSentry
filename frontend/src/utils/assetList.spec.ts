import { describe, expect, it } from 'vitest'
import {
  ASSET_PAGE_SIZE_MAX,
  clampPageSize,
  humanBytes,
  normalizeKeyword,
  normalizeScheme,
  portText,
  statusCodeParam
} from './assetList'

describe('clampPageSize', () => {
  it('keeps sizes inside [1, 200]', () => {
    expect(ASSET_PAGE_SIZE_MAX).toBe(200)
    expect(clampPageSize(20)).toBe(20)
    expect(clampPageSize(100)).toBe(100)
    expect(clampPageSize(200)).toBe(200)
    expect(clampPageSize(201)).toBe(200)
    expect(clampPageSize(5000)).toBe(200)
  })

  it('never sends a non-positive size', () => {
    expect(clampPageSize(0)).toBe(1)
    expect(clampPageSize(-20)).toBe(1)
  })

  it('truncates fractions', () => {
    expect(clampPageSize(2.9)).toBe(2)
    expect(clampPageSize(199.9)).toBe(199)
  })

  it('falls back to the table default for unusable values', () => {
    expect(clampPageSize(undefined)).toBe(20)
    expect(clampPageSize(null)).toBe(20)
    expect(clampPageSize(Number.NaN)).toBe(20)
    expect(clampPageSize(Number.POSITIVE_INFINITY)).toBe(20)
    expect(clampPageSize(undefined, 50)).toBe(50)
  })
})

describe('normalizeKeyword', () => {
  it('trims and keeps real keywords', () => {
    expect(normalizeKeyword('  admin  ')).toBe('admin')
    expect(normalizeKeyword('a b')).toBe('a b')
  })

  it('drops empty input so the request carries no useless filter', () => {
    expect(normalizeKeyword('')).toBeUndefined()
    expect(normalizeKeyword('   ')).toBeUndefined()
    expect(normalizeKeyword(undefined)).toBeUndefined()
    expect(normalizeKeyword(null)).toBeUndefined()
  })
})

describe('humanBytes', () => {
  it('keeps small sizes in bytes', () => {
    expect(humanBytes(0)).toBe('0 B')
    expect(humanBytes(1)).toBe('1 B')
    expect(humanBytes(1023)).toBe('1023 B')
  })

  it('scales by 1024 without a trailing .0', () => {
    expect(humanBytes(1024)).toBe('1 KB')
    expect(humanBytes(1536)).toBe('1.5 KB')
    expect(humanBytes(1024 ** 2)).toBe('1 MB')
    expect(humanBytes(1024 ** 2 * 1.5)).toBe('1.5 MB')
    expect(humanBytes(1024 ** 3)).toBe('1 GB')
    expect(humanBytes(1024 ** 4)).toBe('1 TB')
  })

  it('promotes instead of printing 1024 of a smaller unit', () => {
    // 1048575 B = 1023.999 KB，四舍五入会进位成 1024 KB，应显示成 1 MB
    expect(humanBytes(1024 ** 2 - 1)).toBe('1 MB')
  })

  it('accepts numeric strings from legacy records', () => {
    expect(humanBytes('2048')).toBe('2 KB')
  })

  it('separates unknown length from an empty body', () => {
    expect(humanBytes(undefined)).toBe('-')
    expect(humanBytes(null)).toBe('-')
    expect(humanBytes(-1)).toBe('-')
    expect(humanBytes('abc')).toBe('-')
  })
})

describe('normalizeScheme', () => {
  it('lowercases and trims so exact-match filters find data', () => {
    expect(normalizeScheme('HTTP')).toBe('http')
    expect(normalizeScheme('  Redis ')).toBe('redis')
  })

  it('returns undefined when nothing is typed', () => {
    expect(normalizeScheme('')).toBeUndefined()
    expect(normalizeScheme('   ')).toBeUndefined()
    expect(normalizeScheme(null)).toBeUndefined()
  })
})

describe('statusCodeParam', () => {
  it('passes valid exact codes through as numbers', () => {
    expect(statusCodeParam('404')).toBe(404)
    expect(statusCodeParam(' 301 ')).toBe(301)
    expect(statusCodeParam(200)).toBe(200)
    expect(statusCodeParam(599)).toBe(599)
  })

  it('drops anything that is not a usable http code', () => {
    expect(statusCodeParam('')).toBeUndefined()
    expect(statusCodeParam('99')).toBeUndefined()
    expect(statusCodeParam('600')).toBeUndefined()
    expect(statusCodeParam('4o4')).toBeUndefined()
    expect(statusCodeParam('4xx')).toBeUndefined()
    expect(statusCodeParam(null)).toBeUndefined()
    expect(statusCodeParam(Number.NaN)).toBeUndefined()
  })
})

describe('portText', () => {
  it('renders numeric and string ports the same way', () => {
    expect(portText(80)).toBe('80')
    expect(portText('443')).toBe('443')
    expect(portText(' 8443 ')).toBe('8443')
  })

  it('renders missing or invalid ports as a dash', () => {
    expect(portText(0)).toBe('-')
    expect(portText('0')).toBe('-')
    expect(portText('')).toBe('-')
    expect(portText(undefined)).toBe('-')
    expect(portText(null)).toBe('-')
  })
})
