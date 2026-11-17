// ICP 企业主体 ↔ 域名查询（对标 ARL-Next icp.py 的 task/asset/sync 系列）。
// 字段与后端 internal/models/icp.go、internal/services/icp/service.go 一一对应。
//
// 状态机：0 待开始 / 1 运行中 / 2 已暂停 / 3 已完成；pause 后可 resume 从 cursor 继续，
// restart 会清零 stats 与 cursor 重跑整轮。

export type IcpTaskType = 'company' | 'domain'

export type IcpTaskState = 0 | 1 | 2 | 3

export interface IcpTaskStats {
  total: number
  done: number
  found: number
  failed: number
}

export interface IcpTask {
  id: string
  name: string
  type: IcpTaskType | string
  queries: string[]
  state: number
  stats: IcpTaskStats
  // 创建任务时解析出的数据来源：manual / none / http
  provider: string
  cursor: number
  lastError?: string
  createdAt: string
  updatedAt: string
  nextRunAt?: string | null
}

export interface IcpTaskListQuery {
  state?: number
  type?: string
  keyword?: string
  provider?: string
  pageIndex?: number
  pageSize?: number
}

export interface IcpTaskListResult {
  list: IcpTask[]
  total: number
  pageIndex: number
  pageSize: number
}

// 新建任务：queries 与 target 至少填一个；name 为空时后端用第一个 query 当任务名，
// provider 为空时按当前环境自动解析（manual / http）。
export interface IcpTaskCreateForm {
  name?: string
  type: IcpTaskType
  queries?: string[]
  target?: string
  provider?: string
}

export interface IcpTaskLogLine {
  id: string
  taskId: string
  level: string
  line: string
  createdAt: string
}

export interface IcpTaskLogResult {
  taskId: string
  total: number
  lines: IcpTaskLogLine[]
}

export interface IcpTaskLogQuery {
  taskId: string
  // 后端上限 200 行
  limit?: number
}

// taskId 可选：不传则返回全局汇总（tasks/results 与各状态累计）
export interface IcpTaskStatistics {
  taskId?: string
  total: number
  done: number
  found: number
  failed: number
  results: number
  tasks: number
  state?: number
}

// 各状态任务数，键为 waiting/running/paused/done/total
export type IcpTaskOverviewCounts = Record<string, number>

export interface IcpProviderStatus {
  provider: string
  available: boolean
  reason: string
}

export interface IcpProviderStatusResult extends IcpProviderStatus {
  configured: boolean
  candidates: IcpProviderStatus[]
}

export interface IcpResultRow {
  id: string
  taskId: string
  domain: string
  company: string
  icpNo: string
  siteName: string
  type: string
  source: string
  createdAt: string
}

export interface IcpAssetListQuery {
  taskId: string
  pageIndex?: number
  pageSize?: number
}

export interface IcpAssetListResult {
  list: IcpResultRow[]
  total: number
  pageIndex: number
  pageSize: number
  taskId: string
}

export interface IcpSyncForm {
  taskId: string
  // 空串 = 未分组（后端 ensureGroup 对空 groupId 放行）
  groupId?: string
}

export interface IcpSyncSummary {
  taskId: string
  scopeId: string
  scopeName: string
  groupId: string
  domains: number
  added: number
  total: number
}

export interface IcpBackfillSummary {
  taskId: string
  scanned: number
  updated: number
  failed: number
}

export interface IcpStopResult {
  updated: number
  ids: string[]
}

export interface IcpDeleteResult {
  deletedTasks: number
  deletedResults: number
  deletedLogs: number
  ids: string[]
}
