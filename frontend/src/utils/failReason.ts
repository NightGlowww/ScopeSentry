// 后端巡检写入的失败原因是 Go 侧拼好的中文句子（failReason）。
// 有了机器可读的 failCode / failParams 之后，界面按当前语言渲染；
// 不认识的老 code（或历史数据没有 code）继续回落到原文，绝不显示成裸 key。

export interface FailReasonTask {
  failReason?: string | null
  failCode?: string | null
  failParams?: Record<string, string> | null
}

type Translate = (key: string, params?: Record<string, unknown>) => string

// 与后端写入方一一对应：watchdog/pipeline.go（两条）与 task/task.go（一条）
export const FAIL_CODES = [
  'phantomNoProgress',
  'nodeHeartbeatStale',
  'stalledNoProgress',
  'staleNoProgress',
  'reconciledStale',
  'queueExpired'
] as const

export const failReasonText = (task: FailReasonTask | null | undefined, t: Translate): string => {
  const raw = (task?.failReason as string) || ''
  const code = (task?.failCode as string) || ''
  if (!code || !(FAIL_CODES as readonly string[]).includes(code)) return raw
  return t(`task.failCode.${code}`, task?.failParams || {})
}
