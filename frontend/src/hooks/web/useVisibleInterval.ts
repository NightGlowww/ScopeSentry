// 可见性感知的定时器：页面在后台时不执行，回到前台立刻补一次。
import { onBeforeUnmount, onMounted } from 'vue'

export const useVisibleInterval = (fn: () => void | Promise<void>, ms: number) => {
  let timer: ReturnType<typeof setInterval> | null = null

  const tick = () => {
    if (typeof document !== 'undefined' && document.hidden) return
    void fn()
  }

  const onVisible = () => {
    if (typeof document !== 'undefined' && !document.hidden) void fn()
  }

  const stop = () => {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
  }
  const start = () => {
    stop()
    timer = setInterval(tick, ms)
  }

  onMounted(() => {
    start()
    document.addEventListener('visibilitychange', onVisible)
  })
  onBeforeUnmount(() => {
    stop()
    document.removeEventListener('visibilitychange', onVisible)
  })

  return { start, stop }
}
