import { describe, expect, it } from 'vitest'
import { clampPage, maxPageOf } from './pagination'

describe('maxPageOf', () => {
  it('按 total/pageSize 向上取整', () => {
    expect(maxPageOf(100, 20)).toBe(5)
    expect(maxPageOf(101, 20)).toBe(6)
  })
  it('total 为 0 时仍给 1 页，避免出现第 0 页', () => {
    expect(maxPageOf(0, 20)).toBe(1)
    expect(maxPageOf(-5, 20)).toBe(1)
  })
  it('pageSize 非法时回落 20', () => {
    expect(maxPageOf(100, 0)).toBe(5)
    expect(maxPageOf(100, Number.NaN)).toBe(5)
  })
})

describe('clampPage', () => {
  it('越界回退到最后一页', () => {
    expect(clampPage(999, 45, 20)).toBe(3)
  })
  it('删除末页唯一一条：3 页变 2 页，回退到 2', () => {
    // 41 条时第 3 页还剩 1 条，不该动页码
    expect(clampPage(3, 41, 20)).toBeNull()
    // 40 条时只剩 2 页，第 3 页已经不存在
    expect(clampPage(3, 40, 20)).toBe(2)
  })
  it('非法页码归 1', () => {
    expect(clampPage(0, 100, 20)).toBe(1)
    expect(clampPage(Number.NaN, 100, 20)).toBe(1)
  })
  it('在范围内时返回 null，不触发多余重取', () => {
    expect(clampPage(2, 100, 20)).toBeNull()
    expect(clampPage(1, 0, 20)).toBeNull()
  })
})
