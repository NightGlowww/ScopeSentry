// ICP 任务在 UI 层的枚举映射与输入归一，和后端 internal/models/icp.go 保持一致。
// 放在 utils 里是为了让「状态文案/tag 颜色/多行查询解析」有单测，不必挂载组件。

export type IcpTagType = 'info' | 'primary' | 'warning' | 'success' | 'danger'

export interface IcpStateMeta {
  value: number
  labelKey: string
  tagType: IcpTagType
}

// 0 待开始 / 1 运行中 / 2 已暂停 / 3 已完成
export const ICP_TASK_STATES: IcpStateMeta[] = [
  { value: 0, labelKey: 'icpTask.stateWaiting', tagType: 'info' },
  { value: 1, labelKey: 'icpTask.stateRunning', tagType: 'primary' },
  { value: 2, labelKey: 'icpTask.statePaused', tagType: 'warning' },
  { value: 3, labelKey: 'icpTask.stateDone', tagType: 'success' }
]

export const ICP_TASK_TYPES = ['company', 'domain'] as const

// 状态筛选下拉：value 用数字，空值表示全部。
export const ICP_STATE_OPTIONS = ICP_TASK_STATES.map((item) => ({
  value: item.value,
  labelKey: item.labelKey
}))

// 未知状态（后端将来新增）不猜语义，统一落到 unknown 文案上。
export const icpStateMeta = (state?: number | null): IcpStateMeta => {
  return (
    ICP_TASK_STATES.find((item) => item.value === state) ?? {
      value: -1,
      labelKey: 'icpTask.stateUnknown',
      tagType: 'info'
    }
  )
}

// 新建任务的多行输入：按行切分、去空白、去重保序，
// 与后端 NormalizeIcpQueries 的口径一致（企业名保持原样，域名由后端小写）。
export const parseIcpQueries = (text?: string | null): string[] => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of (text ?? '').split(/\r?\n/)) {
    const value = raw.trim()
    if (!value || seen.has(value)) continue
    seen.add(value)
    out.push(value)
  }
  return out
}

// 查询进度：cursor 指向「下一个待处理查询」，queries 是查询总数。
// 不能用 stats.done / stats.total —— 那两个数的是 provider 返回的「备案记录条数」，
// 一个命中 0 条的任务跑完后仍是 0/0，列表上看起来像没跑（2026-11-28 实测：state=3 已完成，进度仍显示 0/0 (0%)）。
export const icpQueryProgress = (
  queries?: readonly string[] | null,
  cursor?: number | null
): { done: number; total: number; percent: number } => {
  const total = queries?.length ?? 0
  if (total <= 0) return { done: 0, total: 0, percent: 0 }
  const done = Math.min(total, Math.max(0, Math.trunc(cursor ?? 0)))
  return { done, total, percent: icpProgressPercent(done, total) }
}

// 任务本轮进度：total 由 provider 返回的记录条数决定，未开始时是 0，不能除零。
export const icpProgressPercent = (done?: number, total?: number): number => {
  if (!total || total <= 0) return 0
  const percent = Math.round(((done ?? 0) / total) * 100)
  return Math.min(100, Math.max(0, percent))
}
