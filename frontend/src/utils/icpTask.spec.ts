import { describe, expect, it } from 'vitest'
import {
  ICP_STATE_OPTIONS,
  icpProgressPercent,
  icpQueryProgress,
  icpStateMeta,
  parseIcpQueries
} from './icpTask'

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

  it('measures progress in queries, not in returned records', () => {
    // 跑完但一条没命中的任务：cursor 已经走完，进度必须是 1/1 而不是 0/0
    expect(icpQueryProgress(['a.com'], 1)).toEqual({ done: 1, total: 1, percent: 100 })
    expect(icpQueryProgress(['a.com', 'b.com', 'c.com'], 1)).toEqual({
      done: 1,
      total: 3,
      percent: 33
    })
    expect(icpQueryProgress(['a.com'], 0)).toEqual({ done: 0, total: 1, percent: 0 })
    // 越界与脏数据不许把百分比顶出 0..100
    expect(icpQueryProgress(['a.com'], 9)).toEqual({ done: 1, total: 1, percent: 100 })
    expect(icpQueryProgress(['a.com'], -3).done).toBe(0)
    expect(icpQueryProgress([], 5)).toEqual({ done: 0, total: 0, percent: 0 })
    expect(icpQueryProgress(undefined, undefined)).toEqual({ done: 0, total: 0, percent: 0 })
  })

  it('keeps progress sane before the first run', () => {
    expect(icpProgressPercent(0, 0)).toBe(0)
    expect(icpProgressPercent(undefined, undefined)).toBe(0)
    expect(icpProgressPercent(3, 10)).toBe(30)
    expect(icpProgressPercent(10, 10)).toBe(100)
    expect(icpProgressPercent(20, 10)).toBe(100)
  })
})
