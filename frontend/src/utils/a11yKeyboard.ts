/**
 * 键盘可达性补丁：
 * 复制图标这类 role="button" 的元素（Icon 组件渲染成 <i>）不是原生按钮，
 * 聚焦后按 Enter 不会触发 click。这里做一次全局委托，让键盘用户也能复制。
 */

export const setupKeyboardActivation = () => {
  const flag = '__ssKeyboardActivationBound'
  if ((window as any)[flag]) return
  ;(window as any)[flag] = true

  window.addEventListener(
    'keydown',
    (e: KeyboardEvent) => {
      if (e.key !== 'Enter' || e.isComposing) return
      const el = document.activeElement as HTMLElement | null
      if (!el) return
      if (el.getAttribute('role') !== 'button') return
      if (!el.getAttribute('aria-label')) return
      e.preventDefault()
      el.click()
    },
    true
  )
}
