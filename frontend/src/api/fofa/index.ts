import request from '@/axios'
import type {
  FofaImportListQuery,
  FofaImportListResult,
  FofaImportResult,
  FofaProviderStatus,
  FofaSearchPage,
  FofaSearchQuery,
  FofaTaskForm
} from './types'

// FOFA 面：provider 状态 / 查询预览 / 批量建任务 / 导入历史。
// 未配置 key 时 status 返回 available=false + reason，页面照常展示（不隐藏）。

// provider 状态：未配置时 available=false 且 reason 说明缺什么（例如未写 fofa.key）。
export const getFofaStatusApi = (): Promise<IResponse<FofaProviderStatus>> => {
  return request.post({ url: '/api/fofa/status', data: {} })
}

// 查询预览：单页，size 上限 100，host 已由后端归一为 host[:port]。
export const searchFofaApi = (payload: FofaSearchQuery): Promise<IResponse<FofaSearchPage>> => {
  return request.post({ url: '/api/fofa/search', data: payload })
}

// 批量建任务：per-host（≤50 个待建）/ single（一个任务 N 个目标）。
// 单个 host 失败不中断整批，逐个写进 errors[]。
export const createFofaTasksApi = (payload: FofaTaskForm): Promise<IResponse<FofaImportResult>> => {
  return request.post({ url: '/api/fofa/task', data: payload })
}

// 导入历史：按 createdAt 倒序分页（pageSize 上限 200）。
export const listFofaImportsApi = (
  payload: FofaImportListQuery
): Promise<IResponse<FofaImportListResult>> => {
  return request.post({ url: '/api/fofa/task/list', data: payload })
}

// saveFofaConfigApi 保存/清除 FOFA Key（写入 config.fofa.key，provider 下次调用即生效）
export const saveFofaConfigApi = (data: {
  key: string
  baseUrl?: string
}): Promise<IResponse<any>> => {
  return request.post({ url: '/api/fofa/config', data })
}
