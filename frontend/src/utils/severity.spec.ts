import { describe, expect, it } from 'vitest'
import { normalizeSeverity, SEVERITY_LEVELS, severityTagType } from './severity'

describe('normalizeSeverity', () => {
  it('accepts the casing variants nuclei writes', () => {
    expect(normalizeSeverity('Critical')).toBe('critical')
    expect(normalizeSeverity(' HIGH ')).toBe('high')
    expect(normalizeSeverity('info')).toBe('info')
  })

  it('falls back to unknown for empty or unexpected values', () => {
    expect(normalizeSeverity('')).toBe('unknown')
    expect(normalizeSeverity(undefined)).toBe('unknown')
    expect(normalizeSeverity(null)).toBe('unknown')
    expect(normalizeSeverity('severe')).toBe('unknown')
  })

  it('always returns a declared level', () => {
    for (const raw of SEVERITY_LEVELS) {
      expect(SEVERITY_LEVELS).toContain(normalizeSeverity(raw))
    }
  })
})

describe('severityTagType', () => {
  it('maps each level to a stable tag type', () => {
    expect(severityTagType('critical')).toBe('danger')
    expect(severityTagType('high')).toBe('danger')
    expect(severityTagType('medium')).toBe('warning')
    expect(severityTagType('low')).toBe('success')
    expect(severityTagType('info')).toBe('info')
    expect(severityTagType('unknown')).toBe('primary')
  })

  it('handles mixed case and missing values', () => {
    expect(severityTagType('MEDIUM')).toBe('warning')
    expect(severityTagType('')).toBe('primary')
    expect(severityTagType(undefined)).toBe('primary')
  })
})
