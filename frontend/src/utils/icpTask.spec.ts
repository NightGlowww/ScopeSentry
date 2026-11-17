import { describe, expect, it } from 'vitest'
import { ICP_STATE_OPTIONS, icpProgressPercent, icpStateMeta, parseIcpQueries } from './icpTask'

describe('icp task presentation helpers', () => {
  it('maps every backend state to a translated label', () => {
    expect(icpStateMeta(0).labelKey).toBe('icpTask.stateWaiting')
    expect(icpStateMeta(1).labelKey).toBe('icpTask.stateRunning')
    expect(icpStateMeta(2).labelKey).toBe('icpTask.statePaused')
    expect(icpStateMeta(3).labelKey).toBe('icpTask.stateDone')
    expect(ICP_STATE_OPTIONS).toHaveLength(4)
  })

  it('does not invent semantics for unknown or missing states', () => {
    expect(icpStateMeta(9).labelKey).toBe('icpTask.stateUnknown')
    expect(icpStateMeta(undefined).labelKey).toBe('icpTask.stateUnknown')
    expect(icpStateMeta(9).tagType).toBe('info')
  })

  it('parses the multi-line create form like the backend does', () => {
    expect(parseIcpQueries('example.com\r\nexample.com\n\n  foo.cn  ')).toEqual([
      'example.com',
      'foo.cn'
    ])
    expect(parseIcpQueries('')).toEqual([])
    expect(parseIcpQueries(null)).toEqual([])
  })

  it('keeps progress sane before the first run', () => {
    expect(icpProgressPercent(0, 0)).toBe(0)
    expect(icpProgressPercent(undefined, undefined)).toBe(0)
    expect(icpProgressPercent(3, 10)).toBe(30)
    expect(icpProgressPercent(10, 10)).toBe(100)
    expect(icpProgressPercent(20, 10)).toBe(100)
  })
})
