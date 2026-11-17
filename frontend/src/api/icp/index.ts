import request from '@/axios'
import type {
  IcpAssetListQuery,
  IcpAssetListResult,
  IcpBackfillSummary,
  IcpDeleteResult,
  IcpProviderStatusResult,
  IcpStopResult,
  IcpSyncForm,
  IcpSyncSummary,
  IcpTask,
  IcpTaskCreateForm,
  IcpTaskListQuery,
  IcpTaskListResult,
  IcpTaskLogQuery,
  IcpTaskLogResult,
  IcpTaskOverviewCounts,
  IcpTaskStatistics
} from './types'

// 任务列表：state/type/keyword/provider 过滤；空 body 等价默认分页。
export const getIcpTaskListApi = (
  payload: IcpTaskListQuery
): Promise<IResponse<IcpTaskListResult>> => {
  return request.post({ url: '/api/icp/task/list', data: payload })
}

// 新建任务：queries 按行拆分后传数组，name 留空由后端取第一个 query。
export const createIcpTaskApi = (payload: IcpTaskCreateForm): Promise<IResponse<IcpTask>> => {
  return request.post({ url: '/api/icp/task/create', data: payload })
}

// 重启：清零 stats/cursor 后后台重跑，状态靠轮询任务列表/详情拿到。
export const restartIcpTaskApi = (taskId: string): Promise<IResponse<IcpTask>> => {
  return request.post({ url: '/api/icp/task/restart', data: { taskId } })
}

// 暂停：只对运行中任务生效，可批量。
export const stopIcpTasksApi = (ids: string[]): Promise<IResponse<IcpStopResult>> => {
  return request.post({ url: '/api/icp/task/stop', data: { ids } })
}

// 从暂停处继续：保留已有统计与 cursor，仅对 paused 状态任务有效。
export const resumeIcpTaskApi = (taskId: string): Promise<IResponse<IcpTask>> => {
  return request.post({ url: '/api/icp/task/resume', data: { taskId } })
}

// 删除：连带该任务的结果与日志。
export const deleteIcpTasksApi = (ids: string[]): Promise<IResponse<IcpDeleteResult>> => {
  return request.post({ url: '/api/icp/task/delete', data: { ids } })
}

// 任务日志：返回最近 limit 行（后端上限 200）。
export const getIcpTaskLogApi = (
  payload: IcpTaskLogQuery
): Promise<IResponse<IcpTaskLogResult>> => {
  return request.post({ url: '/api/icp/task/log', data: payload })
}

// 任务统计：带 taskId 为单任务本轮统计，不带为全局汇总。
export const getIcpTaskStatisticsApi = (taskId?: string): Promise<IResponse<IcpTaskStatistics>> => {
  return request.post({ url: '/api/icp/task/statistics', data: taskId ? { taskId } : {} })
}

// 各状态任务数（waiting/running/paused/done/total）。
export const getIcpTaskOverviewApi = (): Promise<IResponse<IcpTaskOverviewCounts>> => {
  return request.post({ url: '/api/icp/task/overview_count', data: {} })
}

// provider 状态：available=false 时 reason 说明缺什么（例如未配置 icp.provider_url）。
export const checkIcpProviderApi = (): Promise<IResponse<IcpProviderStatusResult>> => {
  return request.post({ url: '/api/icp/tyc_check', data: {} })
}

// 任务结果：icp_result 分页，唯一键 (domain, company, icpNo) 跨任务去重。
export const getIcpAssetListApi = (
  payload: IcpAssetListQuery
): Promise<IResponse<IcpAssetListResult>> => {
  return request.post({ url: '/api/icp/asset', data: payload })
}

// 同步到资产组地契：按「groupId + 任务名」定位地契，存在则并入，不存在则新建。
export const syncIcpTaskApi = (payload: IcpSyncForm): Promise<IResponse<IcpSyncSummary>> => {
  return request.post({ url: '/api/icp/sync', data: payload })
}

// 补全结果里的 company / icpNo（逐条失败不中断整批）。
export const backfillIcpTaskApi = (taskId: string): Promise<IResponse<IcpBackfillSummary>> => {
  return request.post({ url: '/api/icp/backfill_details', data: { taskId } })
}

// 导出 CSV：接口带 BOM 且用 Content-Disposition 返回文件流，所以走 blob 请求，
// 由调用方触发下载；默认响应拦截器对 responseType=blob 不解包。
export const exportIcpTaskCsvApi = async (taskId: string): Promise<Blob> => {
  const res = await request.get<Blob>({
    url: '/api/icp/export',
    params: { taskId },
    responseType: 'blob'
  })
  // 拦截器对 blob 返回原始 AxiosResponse，这里同时兼容两种形态
  const raw = res as unknown as Blob | { data?: Blob }
  return raw instanceof Blob ? raw : (raw.data as Blob)
}
