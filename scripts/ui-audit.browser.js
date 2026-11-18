/**
 * UI 清单审计（页面内执行）
 *
 * 用法（Agent Bridge）：
 *   1) 读取本文件内容，拼成 `uiAudit(['#/plugin-management/index', ...])`
 *   2) 通过 eval 在已登录的 ScopeSentry 页面里执行
 *   3) 返回 JSON 数组，保存为 docs/ui-audit-baseline.json
 *
 * 检查项对应 docs/UI_UX_REVIEW.md 的交互规范。
 */
async function uiAudit(routes, opts) {
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
  const wait = (opts && opts.wait) || 900
  const vis = (el) => el && el.offsetParent !== null
  const out = []

  for (const route of routes) {
    location.hash = route
    await sleep(wait)

    const vw = window.innerWidth
    const tables = [...document.querySelectorAll('.el-table')].filter(vis)
    const rows = tables.reduce(
      (n, t) => n + t.querySelectorAll('.el-table__body-wrapper tbody tr').length,
      0
    )
    const paginations = [...document.querySelectorAll('.el-pagination')].filter(vis).length
    const empties = [...document.querySelectorAll('.el-empty, .el-table__empty-text')].filter(vis)
      .length

    const destructive = [...document.querySelectorAll('button')]
      // 只统计「删除」类（依赖选中项）；清空/清除属于全局操作，不需要选中
      .filter((b) => vis(b) && !b.closest('.el-table') && /^删除/.test(b.innerText.trim()))
      .filter((b) => !b.disabled).length

    const anyChecked = !!document.querySelector(
      '.el-table__body-wrapper tbody tr .el-checkbox.is-checked'
    )

    // 图标本身或它的可点击父级有 aria-label 都算有名字
    const unnamedIcons = [...document.querySelectorAll('i.el-icon.cursor-pointer')].filter((i) => {
      if (!vis(i)) return false
      if (i.getAttribute('aria-label')) return false
      const labelledParent = i.closest('[aria-label]')
      return !labelledParent
    }).length

    const skeletonVisible = [...document.querySelectorAll('.el-skeleton')].filter(vis).length

    out.push({
      route,
      pageOverflow: Math.max(0, document.documentElement.scrollWidth - vw),
      tables: tables.length,
      rows,
      pagination: paginations,
      emptyState: empties,
      // 只有「有表格但没数据却还显示分页」才算问题（卡片列表页本来就没有 el-table）
      paginationWithoutRows: tables.length > 0 && rows === 0 && paginations > 0 ? 1 : 0,
      destructiveEnabledWithoutSelection: !anyChecked ? destructive : 0,
      unnamedIconControls: unnamedIcons,
      skeletonVisible
    })
  }

  return JSON.stringify(out)
}
