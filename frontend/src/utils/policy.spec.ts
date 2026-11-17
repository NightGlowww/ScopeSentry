import { describe, expect, it } from 'vitest'
import {
  FOFA_MAX_PER_HOST_TASKS,
  FOFA_MAX_PREVIEW_SIZE,
  POLICY_MAX_PAGE_SIZE,
  applyPolicyToTaskForm,
  formatOptionsText,
  hostsToText,
  parseOptionsText,
  parsePluginsText,
  policyConfigHasContent,
  policyFieldLabelKey,
  suggestFofaTaskName,
  toNullableNumber
} from './policy'

describe('policy / fofa presentation helpers', () => {
  it('keeps the UI limits in sync with the backend contract', () => {
    expect(POLICY_MAX_PAGE_SIZE).toBe(200)
    expect(FOFA_MAX_PREVIEW_SIZE).toBe(100)
    expect(FOFA_MAX_PER_HOST_TASKS).toBe(50)
  })

  it('parses plugin lists from commas and newlines without duplicates', () => {
    expect(parsePluginsText('a\nb, a\n\nc')).toEqual(['a', 'b', 'c'])
    expect(parsePluginsText('')).toEqual([])
    expect(parsePluginsText(null)).toEqual([])
  })

  it('parses options key=value lines and reports duplicates', () => {
    expect(parseOptionsText('a=1\nb=2')).toEqual({
      options: { a: '1', b: '2' },
      duplicates: [],
      invalid: []
    })
    expect(parseOptionsText('a=1\na=2')).toEqual({
      options: { a: '2' },
      duplicates: ['a'],
      invalid: []
    })
    // 没有 '=' 的行按「无值」处理，并记进 invalid 让 UI 提示
    expect(parseOptionsText('flag')).toEqual({
      options: { flag: '' },
      duplicates: [],
      invalid: ['flag']
    })
  })

  it('formats options back into sorted text', () => {
    expect(formatOptionsText({ b: '2', a: '1', flag: '' })).toBe('a=1\nb=2\nflag')
    expect(formatOptionsText(null)).toBe('')
  })

  it('detects whether a config carries anything worth saving', () => {
    expect(policyConfigHasContent(null)).toBe(false)
    expect(policyConfigHasContent({})).toBe(false)
    expect(policyConfigHasContent({ templateId: ' ' })).toBe(false)
    // rateLimit=0 是合法值（不限速），不能被当成「未设置」
    expect(policyConfigHasContent({ rateLimit: 0 })).toBe(true)
    expect(policyConfigHasContent({ dicts: { dir: 'dict.txt' } })).toBe(true)
    expect(policyConfigHasContent({ options: { a: '1' } })).toBe(true)
  })

  it('normalizes empty numeric inputs to null instead of 0', () => {
    expect(toNullableNumber('')).toBeNull()
    expect(toNullableNumber(undefined)).toBeNull()
    expect(toNullableNumber('abc')).toBeNull()
    expect(toNullableNumber('0')).toBe(0)
    expect(toNullableNumber(12)).toBe(12)
  })

  it('maps known policy fields to i18n keys and leaves unknown ones to the caller', () => {
    expect(policyFieldLabelKey('templateId')).toBe('policy.fieldTemplateId')
    expect(policyFieldLabelKey('dicts.dir')).toBe('policy.fieldDictDir')
    expect(policyFieldLabelKey('whatever')).toBe('')
    expect(policyFieldLabelKey(undefined)).toBe('')
  })

  it('fills the task form template only when the user left it empty', () => {
    const merged = { templateId: 'tmpl-a', threadNum: 20 }

    // 用户没填 → 直接填
    expect(applyPolicyToTaskForm({ template: '' }, merged)).toEqual({
      values: { template: 'tmpl-a' },
      unchanged: [],
      conflicts: []
    })

    // 用户填的就是同一个 → unchanged，不算冲突
    expect(applyPolicyToTaskForm({ template: 'tmpl-a' }, merged)).toEqual({
      values: {},
      unchanged: ['template'],
      conflicts: []
    })

    // 用户填了别的 → 不覆盖，交给 warning 提示
    expect(applyPolicyToTaskForm({ template: 'tmpl-b' }, merged)).toEqual({
      values: {},
      unchanged: [],
      conflicts: [{ field: 'template', current: 'tmpl-b', next: 'tmpl-a' }]
    })

    // 策略没给模板 → 什么都不做
    expect(applyPolicyToTaskForm({ template: '' }, { threadNum: 5 })).toEqual({
      values: {},
      unchanged: [],
      conflicts: []
    })
  })

  it('suggests a task name from the fofa query', () => {
    expect(suggestFofaTaskName('  app="nginx"  ')).toBe('app="nginx"')
    expect(suggestFofaTaskName('a\n\nb')).toBe('a-b')
    expect(suggestFofaTaskName('')).toBe('')
    expect(Array.from(suggestFofaTaskName('x'.repeat(300)))).toHaveLength(120)
  })

  it('joins preview hosts one per line', () => {
    expect(hostsToText(['a.com', 'b.com:8080'])).toBe('a.com\nb.com:8080')
    expect(hostsToText(null)).toBe('')
  })
})
