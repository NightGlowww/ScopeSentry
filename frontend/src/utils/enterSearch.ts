/**
 * 列表页搜索框回车即搜索。
 *
 * 现状：所有列表页都要点「搜索」按钮，输入完敲回车没反应。
 * 这里做一次全局委托：回车时向上找 3 层父节点，若存在文案为「搜索 / Search」的按钮，
 * 就触发它的点击。只对 input 生效，textarea / 组合输入（中文输入法）不处理。
 */
const SEARCH_LABEL = /^(搜索|Search)$/i

const findSearchButton = (input: HTMLElement): HTMLElement | null => {
  let node: HTMLElement | null = input
  for (let i = 0; i < 4; i++) {
    node = node?.parentElement ?? null
    if (!node) return null
    const buttons = Array.from(node.querySelectorAll('button.el-button')) as HTMLElement[]
    const hit = buttons.find((b) => SEARCH_LABEL.test((b.innerText || '').trim()))
    if (hit) return hit
  }
  return null
}

export const setupEnterToSearch = () => {
  const flag = '__ssEnterToSearchBound'
  if ((window as any)[flag]) return
  ;(window as any)[flag] = true

  window.addEventListener(
    'keydown',
    (e: KeyboardEvent) => {
      if (e.key !== 'Enter' || e.isComposing) return
      const el = e.target as HTMLElement | null
      if (!el || el.tagName !== 'INPUT') return
      const type = (el as HTMLInputElement).type
      if (type === 'textarea' || type === 'password') return
      const btn = findSearchButton(el)
      if (!btn) return
      e.preventDefault()
      btn.click()
    },
    true
  )
}
