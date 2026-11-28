/** * App 根组件：初始化主题（日间/夜间/跟随系统） */
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useAppStore } from '@/store/modules/app'
import { ConfigGlobal } from '@/components/ConfigGlobal'
import { useDesign } from '@/hooks/web/useDesign'
import { useI18n } from '@/hooks/web/useI18n'
import { setCssVar } from './utils'

const { getPrefixCls } = useDesign()

const { t } = useI18n()

const prefixCls = getPrefixCls('app')

const appStore = useAppStore()

const currentSize = computed(() => appStore.getCurrentSize)

const greyMode = computed(() => appStore.getGreyMode)

// 初始化主题：读取持久化模式（旧版本 isDark 自动迁移），默认跟随系统，
// 并在“跟随系统”模式下监听 prefers-color-scheme 的实时变化
appStore.initTheme()

// 初始化标签页高度
const tagsViewEnabled = appStore.getTagsView
setCssVar('--tags-view-height', tagsViewEnabled ? '42px' : '0px')

// ---- 版本哨兵 ----
// 部署后浏览器常常还在跑旧的 index-*.js（SPA 不会重新请求文档），
// 界面看起来像「改了个寂寞」：标签空白、按钮没反应、页面缺块。
// 这里定时拉一次 index.html，比对当前加载的入口 JS 文件名，不一致就提示刷新。
const currentEntry = (() => {
  const s = document.querySelector('script[src*="/assets/index-"]') as HTMLScriptElement | null
  return s ? s.src.split('/').pop() || '' : ''
})()
const staleBuild = ref('')
let timer: number | undefined

const checkBuild = async () => {
  try {
    const res = await fetch(`${location.origin}/?__v=${Date.now()}`, { cache: 'no-store' })
    if (!res.ok) return
    const html = await res.text()
    const m = html.match(/assets\/(index-[A-Za-z0-9_-]+\.js)/)
    if (m && currentEntry && m[1] !== currentEntry) {
      staleBuild.value = m[1]
    }
  } catch {
    // 网络抖动不打扰用户
  }
}

// 模板里直接写 location.reload() 解析不到组件作用域（Vue 会告警并返回 undefined），
// 点击立刻 TypeError；走 window.location 并暴露成方法
const reloadPage = () => window.location.reload()

onMounted(() => {
  checkBuild()
  timer = window.setInterval(checkBuild, 5 * 60 * 1000)
})
onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<template>
  <ConfigGlobal :size="currentSize">
    <div v-if="staleBuild" class="stale-build-bar">
      {{ t('app.staleBuild', { build: staleBuild }) }}
      <a @click="reloadPage">{{ t('app.staleBuildReload') }}</a>
    </div>
    <RouterView :class="greyMode ? `${prefixCls}-grey-mode` : ''" />
  </ConfigGlobal>
</template>

<style lang="less">
.stale-build-bar {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 3000;
  padding: 6px 16px;
  font-size: 13px;
  text-align: center;
  color: #7a4b00;
  background: #fff7e6;
  border-bottom: 1px solid #ffd591;

  a {
    margin-left: 6px;
    color: #1677ff;
    cursor: pointer;
    text-decoration: underline;
  }
}

@prefix-cls: ~'@{namespace}-app';

.size {
  width: 100%;
  height: 100%;
}

html,
body {
  padding: 0 !important;
  margin: 0;
  overflow: hidden;
  .size;

  #app {
    .size;
  }
}

.@{prefix-cls}-grey-mode {
  filter: grayscale(100%);
}
</style>
