import type { App } from 'vue'
import { createPinia } from 'pinia'
import piniaPluginPersistedstate from 'pinia-plugin-persistedstate'
import { useAppStore } from './modules/app'

const store = createPinia()

store.use(piniaPluginPersistedstate)

// 一次性迁移：tagsViewIcon 的默认值从 false 改成 true，
// 但这个开关是持久化到 localStorage 的——老浏览器里存着 false，改默认值对它无效。
// 这里用一次性标记强制打开一次，之后用户想关仍然能在设置里关掉。
const MIGRATION_KEY = 'ss-ui-defaults-v1'
const migrateUiDefaults = () => {
  try {
    if (localStorage.getItem(MIGRATION_KEY)) return
    useAppStore().setTagsViewIcon(true)
    localStorage.setItem(MIGRATION_KEY, '1')
  } catch {
    // localStorage 不可用（隐私模式等）时静默跳过，不影响页面
  }
}

export const setupStore = (app: App<Element>) => {
  app.use(store)
  migrateUiDefaults()
}

export { store }
