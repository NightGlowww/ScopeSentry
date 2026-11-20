import { onBeforeUnmount, ref } from 'vue'

/**
 * 同一浏览器里只让一个标签页轮询。
 *
 * 背景：多个页面都在 8-15 秒自动刷新，同时开 3-4 个标签页时请求会叠加
 * （2026-11-21 调试时实测：4 个标签页让 /api/mcp/logs 的 QPS 翻了 4 倍）。
 *
 * 做法：localStorage 里放一个带时间戳的「主标签」标记，每 4 秒续期；
 * 标记过期（12 秒）或属于自己，就自己当主；否则让位给主标签。
 * 主标签关掉后标记会在 12 秒内过期，另一个标签自动接管。
 */
export const useSingleTabLeader = (key: string, ttlMs = 12000) => {
  const isLeader = ref(false)
  const id = Math.random().toString(36).slice(2)
  const storageKey = `poll-leader:${key}`

  const claimOrYield = () => {
    const now = Date.now()
    let current: { id: string; at: number } | null = null
    try {
      const raw = localStorage.getItem(storageKey)
      current = raw ? JSON.parse(raw) : null
    } catch {
      current = null
    }
    if (!current || current.id === id || now - Number(current.at || 0) > ttlMs) {
      localStorage.setItem(storageKey, JSON.stringify({ id, at: now }))
      isLeader.value = true
    } else {
      isLeader.value = false
    }
  }

  claimOrYield()
  const timer = setInterval(claimOrYield, 4000)

  onBeforeUnmount(() => {
    clearInterval(timer)
    try {
      const raw = localStorage.getItem(storageKey)
      if (raw && JSON.parse(raw).id === id) localStorage.removeItem(storageKey)
    } catch {
      // 忽略：退出时清理失败不影响功能
    }
  })

  return { isLeader }
}
