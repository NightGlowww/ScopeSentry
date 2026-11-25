// 把构建期抽取出来的 Iconify 图标注册到运行时，避免依赖 api.iconify.design。
import { addCollection } from '@iconify/vue'
// @ts-ignore 构建产物，没有类型声明
import icons from '@/generated/iconify-offline.json'

const collections = icons as Record<string, { prefix: string; icons: Record<string, unknown> }>
let n = 0
for (const key of Object.keys(collections)) {
  addCollection(collections[key] as never)
  n++
}
if (n === 0) {
  // 生成文件缺失时不要整页崩：图标退化成空，其余功能照常。
  console.warn('[iconify] 没有离线图标集合，请先跑 node scripts/collect-icons.mjs')
}
