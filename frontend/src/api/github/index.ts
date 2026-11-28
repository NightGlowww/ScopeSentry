import request from '@/axios'

export interface GithubTask {
  id: string
  name: string
  query: string
  keywords?: string[]
  type: string
  state: number
  project?: string
  createdAt: string
  lastRunAt?: string
  stats?: { scanned: number; newFound: number }
  intervalMinutes?: number
  maxPages?: number
}

export interface GithubResult {
  id: string
  taskId: string
  type: string
  repo: string
  path: string
  url: string
  title: string
  snippet: string
  severity: string
  status: string
  project?: string
  keyword?: string
  createdAt: string
}

export const listGithubTasksApi = (data: {
  state?: number
  type?: string
  keyword?: string
  pageIndex?: number
  pageSize?: number
}): Promise<IResponse<{ list: GithubTask[]; total: number }>> =>
  request.post({ url: '/api/github/task/list', data })

export const createGithubTaskApi = (data: {
  name: string
  query: string
  keywords?: string[]
  type: string
  intervalMinutes?: number
  maxPages?: number
  tokenRef?: string
  project?: string
}): Promise<IResponse<GithubTask>> => request.post({ url: '/api/github/task/create', data })

export const stopGithubTasksApi = (ids: string[]): Promise<IResponse<unknown>> =>
  request.post({ url: '/api/github/task/stop', data: { ids } })

export const deleteGithubTasksApi = (ids: string[]): Promise<IResponse<unknown>> =>
  request.post({ url: '/api/github/task/delete', data: { ids } })

export const listGithubResultsApi = (data: {
  taskId?: string
  type?: string
  status?: string
  project?: string
  keyword?: string
  pageIndex?: number
  pageSize?: number
}): Promise<IResponse<{ list: GithubResult[]; total: number }>> =>
  request.post({ url: '/api/github/result/list', data })

// 结果总览：后端已经按 type/status 聚合好（byType/byStatus），前端不用二次聚合。
// 之前这个接口没人调，结果页只能看到「当前这一页有什么」，看不到「整体还有多少没处置」。
export const getResultOverviewApi = (data: {
  taskId?: string
  type?: string
  status?: string
  project?: string
  keyword?: string
}): Promise<
  IResponse<{
    items: { type: string; status: string; count: number }[]
    total: number
    byType: Record<string, number>
    byStatus: Record<string, number>
  }>
> => request.post({ url: '/api/github/result/overview', data })

export const triageGithubResultsApi = (data: {
  ids: string[]
  status: string
  note?: string
}): Promise<IResponse<unknown>> => request.post({ url: '/api/github/result/triage', data })

// ---- 周期任务 ----
export const listSchedulersApi = (data: {
  keyword?: string
  state?: number
  pageIndex?: number
  pageSize?: number
}): Promise<IResponse<{ list: GithubTask[]; total: number }>> =>
  request.post({ url: '/api/github/scheduler/list', data })

export const runSchedulersApi = (ids: string[]): Promise<IResponse<unknown>> =>
  request.post({ url: '/api/github/scheduler/run', data: { ids } })

export const stopSchedulersApi = (ids: string[]): Promise<IResponse<unknown>> =>
  request.post({ url: '/api/github/scheduler/stop', data: { ids } })

export const recoverSchedulersApi = (ids: string[]): Promise<IResponse<unknown>> =>
  request.post({ url: '/api/github/scheduler/recover', data: { ids } })

// 改周期配置：后端每个字段都是指针，只传要改的那个（patch 语义）。
// 界面此前只能停/跑/删 —— 周期设错了只能删了重建，历史统计跟着一起没。
export const updateSchedulerApi = (data: {
  id: string
  name?: string
  query?: string
  keywords?: string[]
  intervalMinutes?: number
  maxPages?: number
  tokenRef?: string
}): Promise<IResponse<GithubTask>> => request.post({ url: '/api/github/scheduler/update', data })

// ---- 威胁情报 ----
export type ThreatKind = 'cve' | 'hackers' | 'tools'

export interface ThreatSourceConfig {
  enabled: boolean
  keywords: string[]
  intervalHours: number
  tokenRef?: string
  targets?: string[]
}

export const getThreatConfigApi = (kind: ThreatKind): Promise<IResponse<{ kind: string; config: ThreatSourceConfig }>> =>
  request.post({ url: '/api/github/threat/' + kind + '_config', data: {} })

export const updateThreatConfigApi = (
  kind: ThreatKind,
  data: Partial<ThreatSourceConfig>
): Promise<IResponse<unknown>> => request.post({ url: '/api/github/threat/' + kind + '_config', data })

export const runThreatOnceApi = (kind: ThreatKind): Promise<IResponse<unknown>> =>
  request.post({ url: '/api/github/threat/' + kind + '_run_once', data: {} })

export const getGithubTokenStatusApi = (): Promise<IResponse<{ status: string; remaining?: number; login?: string }>> =>
  request.post({ url: '/api/github/threat/token_status', data: {} })

export interface ThreatHistoryItem {
  id: string
  kind: string
  key: string
  title: string
  url: string
  repo?: string
  description?: string
  severity?: string
  keyword?: string
  discoveredAt: string
}

export const getCveHistoryApi = (data: {
  keyword?: string
  limit?: number
}): Promise<IResponse<{ list: ThreatHistoryItem[]; total: number; limit: number }>> =>
  request.post({ url: '/api/github/threat/cve_history', data })

const targetUrl = (kind: 'hackers' | 'tools') => '/api/github/threat/' + kind + '_target'

export const listThreatTargetsApi = (
  kind: 'hackers' | 'tools'
): Promise<IResponse<{ items: string[]; total: number }>> =>
  request.post({ url: targetUrl(kind), data: { action: 'list' } })

export const addThreatTargetsApi = (
  kind: 'hackers' | 'tools',
  targets: string[]
): Promise<IResponse<{ items: string[]; total: number }>> =>
  request.post({ url: targetUrl(kind), data: { action: 'add', targets } })

export const removeThreatTargetsApi = (
  kind: 'hackers' | 'tools',
  targets: string[]
): Promise<IResponse<{ items: string[]; total: number }>> =>
  request.post({ url: targetUrl(kind), data: { action: 'remove', targets } })
