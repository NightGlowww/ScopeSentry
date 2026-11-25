// 把前端实际用到的 Iconify 图标抽成一个离线集合。
//
// 背景：项目用 @iconify/vue 但没有注册任何本地集合，图标默认走 api.iconify.design 运行时拉取。
// 在受限网络/内网部署里这一步会静默失败——菜单图标变成空白，而页面不报错。
// 直接用 @iconify/json 全量内联是 45 MB，不能这么干；
// 这里只抽取源码里真正引用到的图标名（icon: "mdi:xxx" / <Icon icon="mdi:xxx">）。
//
// 用法：node scripts/collect-icons.mjs   （产物写入 src/generated/iconify-offline.json）
import { readFileSync, writeFileSync, mkdirSync, readdirSync, statSync } from 'node:fs'
import { join, extname } from 'node:path'

const ROOT = new URL('..', import.meta.url).pathname
const SRC = join(ROOT, 'src')
const OUT_DIR = join(SRC, 'generated')
const OUT = join(OUT_DIR, 'iconify-offline.json')

const RE = /([a-z0-9-]+):([a-z0-9-]+)/g
const used = new Map()

function walk(dir) {
  for (const name of readdirSync(dir)) {
    if (name === 'generated' || name === 'node_modules') continue
    const p = join(dir, name)
    const st = statSync(p)
    if (st.isDirectory()) { walk(p); continue }
    if (!['.vue', '.ts', '.tsx'].includes(extname(p))) continue
    const text = readFileSync(p, "utf8")
    const lines = text.split('\n')
    for (const line of lines) {
      if (!line.includes('icon')) continue
      let m
      while ((m = RE.exec(line)) !== null) {
        const [, prefix, name] = m
        if (!used.has(prefix)) used.set(prefix, new Set())
        used.get(prefix).add(name)
      }
    }
  }
}

walk(SRC)

const out = {}
let total = 0
for (const [prefix, names] of used) {
  const file = join(ROOT, 'node_modules', '@iconify', 'json', 'json', prefix + '.json')
  let collection
  try {
    collection = JSON.parse(readFileSync(file, "utf8"))
  } catch {
    continue
  }
  const icons = {}
  for (const n of names) {
    const ic = collection.icons?.[n] || collection.aliases?.[n]
    if (ic) { icons[n] = ic; total++ }
  }
  if (Object.keys(icons).length === 0) continue
  out[prefix] = { prefix, width: collection.width, height: collection.height, icons }
}

mkdirSync(OUT_DIR, { recursive: true })
writeFileSync(OUT, JSON.stringify(out))
console.log(`icons: prefixes=${Object.keys(out).length} icons=${total} bytes=${JSON.stringify(out).length}`)
