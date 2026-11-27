import { describe, expect, it } from 'vitest'
import fs from 'fs'
import path from 'path'
import zh from '../zh-CN'

type Dict = Record<string, any>

function flatten(obj: Dict, prefix = '', out: Set<string> = new Set()): Set<string> {
  for (const [k, v] of Object.entries(obj)) {
    const p = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v)) flatten(v as Dict, p, out)
    else out.add(p)
  }
  return out
}

const ROOT = path.resolve(__dirname, '../..')
const SKIP_DIRS = new Set(['locales', 'node_modules', '__tests__', 'views/Components'])

function walk(dir: string, out: string[] = []): string[] {
  for (const name of fs.readdirSync(dir)) {
    const full = path.join(dir, name)
    const rel = path.relative(ROOT, full).replace(/\\\\/g, '/')
    if (SKIP_DIRS.has(name) || rel.startsWith('views/Components')) continue
    const st = fs.statSync(full)
    if (st.isDirectory()) walk(full, out)
    else if (/\.(vue|ts)$/.test(name) && !/\.spec\.ts$/.test(name)) out.push(full)
  }
  return out
}

// 只认字面量键：t('a.b') / t("a.b")。模板字符串、变量一律跳过（无法静态判定）。
const KEY_RE = /\bt\(\s*['"]([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+)['"]/g

function collectUsedKeys(): Map<string, string[]> {
  const used = new Map<string, string[]>()
  for (const file of walk(path.join(ROOT))) {
    const src = fs.readFileSync(file, 'utf8')
    let m: RegExpExecArray | null
    while ((m = KEY_RE.exec(src))) {
      const key = m[1]
      const at = path.relative(ROOT, file).replace(/\\\\/g, '/')
      const list = used.get(key) || []
      if (!list.includes(at)) list.push(at)
      used.set(key, list)
    }
  }
  return used
}

const zhKeys = flatten(zh as Dict)
const used = collectUsedKeys()

describe('locale 键位使用', () => {
  it('代码里 t() 用到的字面量键，中文词条里都存在', () => {
    // 缺键时 vue-i18n 会把键名原样渲染出来——英文界面里直接看见「asset.fooBar」，
    // 这类问题只在点到那条路径时才暴露，静态扫一遍最省事。
    const missing = [...used.keys()].filter((k) => !zhKeys.has(k)).sort()
    const detail = missing.map((k) => `${k}  <- ${used.get(k)!.join(', ')}`)
    expect(detail).toEqual([])
  })

  it('没有占位符残留的 {xxx} 之外的花括号语法', () => {
    // 只查「用到的键都能解析」，命中率高的另一类问题是键名大小写/拼写漂移，
    // 已被上一条覆盖；这条兜住历史遗留的「t() || '中文'」死回落。
    const bad: string[] = []
    for (const file of walk(path.join(ROOT))) {
      const src = fs.readFileSync(file, 'utf8')
      if (/t\([^)]*\)\s*\|\|\s*['"][^'"]*[\u4e00-\u9fa5]/.test(src)) {
        bad.push(path.relative(ROOT, file).replace(/\\\\/g, '/'))
      }
    }
    expect(bad).toEqual([])
  })
})
