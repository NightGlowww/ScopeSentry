import { describe, expect, it } from 'vitest'
import { LOG_LEVELS, logLevelTagType, normalizeLogLevel } from './logLevel'

describe('LOG_LEVELS', () => {
  it('offers exactly the four selectable levels', () => {
    expect(LOG_LEVELS).toEqual(['debug', 'info', 'warn', 'error'])
  })
})

describe('normalizeLogLevel', () => {
  it('trims and lowercases file-style levels', () => {
    expect(normalizeLogLevel(' WARN ')).toBe('warn')
    expect(normalizeLogLevel('Error')).toBe('error')
  })

  it('returns an empty string for missing values', () => {
    expect(normalizeLogLevel(undefined)).toBe('')
    expect(normalizeLogLevel(null)).toBe('')
  })
})

describe('logLevelTagType', () => {
  it('maps the four selectable levels', () => {
    expect(logLevelTagType('debug')).toBe('info')
    expect(logLevelTagType('info')).toBe('primary')
    expect(logLevelTagType('warn')).toBe('warning')
    expect(logLevelTagType('error')).toBe('danger')
  })

  it('treats more severe zap levels as errors', () => {
    for (const level of ['dpanic', 'panic', 'fatal']) {
      expect(logLevelTagType(level)).toBe('danger')
    }
  })

  it('accepts uppercase and padded values', () => {
    expect(logLevelTagType('ERROR')).toBe('danger')
    expect(logLevelTagType(' warn ')).toBe('warning')
    expect(logLevelTagType('warning')).toBe('warning')
  })

  it('keeps unknown levels neutral', () => {
    expect(logLevelTagType('trace')).toBe('info')
    expect(logLevelTagType('')).toBe('info')
    expect(logLevelTagType(undefined)).toBe('info')
  })
})
