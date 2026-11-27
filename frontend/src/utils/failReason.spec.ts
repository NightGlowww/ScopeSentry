import { describe, expect, it } from 'vitest'
import { failReasonText, FAIL_CODES } from './failReason'

// 假 t：把 key + 参数摊平成字符串，便于断言「用了哪个 code、带了哪些参数」
const fakeT = (key: string, params?: Record<string, unknown>) =>
  params && Object.keys(params).length ? `${key}(${JSON.stringify(params)})` : key

describe('failReasonText', () => {
  it('有已知 failCode 时按当前语言渲染，不直接吐后端中文句子', () => {
    const text = failReasonText(
      {
        failReason: '节点 node-1 心跳停在 20:15 …',
        failCode: 'nodeHeartbeatStale',
        failParams: { node: 'node-1' }
      },
      fakeT
    )
    expect(text).toContain('task.failCode.nodeHeartbeatStale')
    expect(text).toContain('node-1')
    expect(text).not.toContain('心跳')
  })

  it('没有 code（历史数据）时回落原文，不能显示成裸 key', () => {
    expect(failReasonText({ failReason: '超过 10m0s 无进展' }, fakeT)).toBe('超过 10m0s 无进展')
  })

  it('未知 code 也回落原文，避免把没登记的 key 打到界面上', () => {
    const text = failReasonText({ failReason: '老原因', failCode: 'someFutureCode' }, fakeT)
    expect(text).toBe('老原因')
  })

  it('空输入不报错', () => {
    expect(failReasonText(null, fakeT)).toBe('')
    expect(failReasonText(undefined, fakeT)).toBe('')
    expect(failReasonText({}, fakeT)).toBe('')
  })

  it('登记的 code 与后端写入方保持一致（新增写入方必须同步这个列表）', () => {
    expect([...FAIL_CODES]).toEqual(['phantomNoProgress', 'nodeHeartbeatStale', 'staleNoProgress'])
  })
})
