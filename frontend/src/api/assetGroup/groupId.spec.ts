import { describe, expect, it } from 'vitest'
import { UNASSIGNED_GROUP_ID, resolveGroupIdForWrite } from './groupId'

describe('asset group id normalization', () => {
  it('keeps the unassigned sentinel stable for list requests', () => {
    expect(UNASSIGNED_GROUP_ID).toBe('unassigned')
  })

  it('turns the sentinel into an empty group id for write requests', () => {
    // 写接口传 'unassigned' 会被后端当成非法 ObjectID 拒绝
    expect(resolveGroupIdForWrite(UNASSIGNED_GROUP_ID)).toBe('')
    expect(resolveGroupIdForWrite('')).toBe('')
    expect(resolveGroupIdForWrite(undefined)).toBe('')
  })

  it('passes real group ids through untouched', () => {
    const id = '6650f2a1b3c4d5e6f7a8b9c0'
    expect(resolveGroupIdForWrite(id)).toBe(id)
  })
})
