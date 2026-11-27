// 引入windi css
import '@/plugins/unocss'

// 导入全局的svg图标
import '@/plugins/svgIcon'

// Iconify 离线集合：只装源码里真正引用到的图标（构建前由 scripts/collect-icons.mjs 生成）。
// 不装的话 @iconify/vue 会在运行时去 api.iconify.design 拉图标，
// 受限网络下静默失败——菜单图标变空白且不报错（2026-11-25 实测）。
import '@/plugins/iconifyOffline'

// 初始化多语言
import { setupI18n } from '@/plugins/vueI18n'

// 引入状态管理
import { setupStore } from '@/store'

// 全局组件
import { setupGlobCom } from '@/components'

// 引入element-plus
import { setupElementPlus } from '@/plugins/elementPlus'

// 引入全局样式
import '@/styles/index.less'

// 引入动画
import '@/plugins/animate.css'

// 路由
import { setupRouter } from './router'

// 权限
import { setupPermission } from './directives'

// 列表页搜索框回车即搜索
import { setupEnterToSearch } from '@/utils/enterSearch'
import { setupKeyboardActivation } from '@/utils/a11yKeyboard'

// 渲染错误不再静默空白：控制台留堆栈，页面给提示
import { setupErrorHandler } from '@/utils/errorHandler'

import { createApp } from 'vue'

import App from './App.vue'

import './permission'

// 创建实例
const setupAll = async () => {
  setupEnterToSearch()
  setupKeyboardActivation()
  const app = createApp(App)

  await setupI18n(app)

  setupStore(app)

  setupGlobCom(app)

  setupElementPlus(app)

  setupRouter(app)

  setupPermission(app)

  setupErrorHandler(app)

  app.mount('#app')
}

setupAll()
