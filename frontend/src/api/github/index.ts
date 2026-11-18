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

export const triageGithubResultsApi = (data: {
  ids: string[]
  status: string
  note?: string
}): Promise<IResponse<unknown>> => request.post({ url: '/api/github/result/triage', data })
