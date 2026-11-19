import { describe, expect, it } from 'vitest'
import zh from '../zh-CN'
import en from '../en'
import ko from '../ko-KR'

type Dict = Record<string, any>

function flatten(obj: Dict, prefix = '', out: string[] = []): string[] {
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v)) flatten(v as Dict, path, out)
    else out.push(path)
  }
  return out
}

function diff(base: string[], other: string[]): string[] {
  const set = new Set(other)
  return base.filter((k) => !set.has(k))
}

const zhKeys = flatten(zh as Dict)
const enKeys = flatten(en as Dict)
const koKeys = flatten(ko as Dict)

describe('locale 键位对齐', () => {
  it('英文与中文键集合一致', () => {
    expect(diff(zhKeys, enKeys)).toEqual([])
    expect(diff(enKeys, zhKeys)).toEqual([])
  })
  it('韩文与中文键集合一致', () => {
    expect(diff(zhKeys, koKeys)).toEqual([])
    expect(diff(koKeys, zhKeys)).toEqual([])
  })
})
