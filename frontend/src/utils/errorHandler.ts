// 全局错误处理：组件渲染/生命周期抛错时，Vue 默认只 console.error，
// 产物里表现为「整块区域变成空白」，用户完全不知道发生了什么（资产页工具栏消失事故就是这么来的）。
// 这里统一收口：控制台保留完整堆栈，同时给一次可见提示，并记到 window.__SS_RENDER_ERRORS 便于排查。
import type { App } from 'vue'
import { ElMessage } from 'element-plus'

interface RenderErrorRecord {
  at: number
  info: string
  component: string
  message: string
  stack?: string
}

// 未处理的 Promise 拒绝：事件处理器里 await 失败但没 try/catch 时，
// 用户界面完全无反应（点了没动静），控制台也只有一行。这里记到内存里便于排查，
// 但**不弹提示** —— axios 拦截器已经把网络/业务错误提示过了，再弹一次是双重噪音。
interface UnhandledRecord {
  at: number
  message: string
  stack?: string
  route: string
}

declare global {
  interface Window {
    __SS_RENDER_ERRORS?: RenderErrorRecord[]
    __SS_UNHANDLED?: UnhandledRecord[]
  }
}

const seen = new Set<string>()

const componentName = (instance: any): string => {
  const t = instance?.type
  if (!t) return '未知组件'
  if (typeof t === 'string') return t
  return t.name || t.__name || t.file || '匿名组件'
}

export const setupErrorHandler = (app: App) => {
  window.__SS_RENDER_ERRORS = window.__SS_RENDER_ERRORS || []
  window.__SS_UNHANDLED = window.__SS_UNHANDLED || []
  const seenUnhandled = new Set<string>()
  window.addEventListener('unhandledrejection', (event) => {
    const reason = event?.reason
    const message = reason?.message || String(reason ?? 'unknown rejection')
    // axios 的错误对象已经由拦截器提示过，这里只记录不重复提示
    const key = message
    if (!seenUnhandled.has(key)) {
      seenUnhandled.add(key)
      window.__SS_UNHANDLED!.push({
        at: Date.now(),
        message,
        stack: reason?.stack,
        route: location.hash
      })
    }
    console.error('[unhandled-rejection] ' + message, reason)
  })
  app.config.errorHandler = (err: unknown, instance: any, info: string) => {
    const e = err as Error
    const component = componentName(instance)
    const message = e?.message || String(err)
    window.__SS_RENDER_ERRORS!.push({
      at: Date.now(),
      info,
      component,
      message,
      stack: e?.stack
    })
    // 堆栈一定要留在控制台，页面提示只负责让用户知道「这里坏了」
    console.error('[render-error] ' + component + ' (' + info + '): ' + message, err)
    const key = component + '|' + message
    if (seen.has(key)) return
    seen.add(key)
    ElMessage({
      type: 'error',
      duration: 8000,
      showClose: true,
      message: component + ' 渲染失败：' + message
    })
  }
}
