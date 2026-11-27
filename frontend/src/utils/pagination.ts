// 越界分页回退
//
// 场景：删掉末页最后一条、把筛选条件收紧、或换了个更小的数据集，
// 但 currentPage 还停在原来的页码上。ElPagination 于是显示一个「第 999 页 / 共 3 页」，
// 表格是空的——用户看到的是「没有数据」，而不是「页码越界了」。
// ARL-Next 0b8276b5 修的就是这一类。

export function maxPageOf(total: number, pageSize: number): number {
  const size = Number(pageSize) > 0 ? Number(pageSize) : 20
  const n = Number(total)
  if (!Number.isFinite(n) || n <= 0) return 1
  return Math.max(1, Math.ceil(n / size))
}

/**
 * 把页码夹进 [1, maxPage]。返回 null 表示不需要改动（避免无谓的重新请求）。
 */
export function clampPage(current: number, total: number, pageSize: number): number | null {
  const max = maxPageOf(total, pageSize)
  const cur = Number(current)
  if (!Number.isFinite(cur) || cur < 1) return 1
  if (cur > max) return max
  return null
}
