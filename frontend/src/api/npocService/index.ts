import request from '@/axios'
import type { NpocServiceDeleteResult, NpocServiceListQuery, NpocServiceListResult } from './types'

// NPoC 服务列表：按 host/target 子串(keyword)、协议(scheme) 与主机(host) 过滤，
// 按发现时间倒序分页；pageSize 上限 200（后端 binding 校验）。
export const getNpocServiceListApi = (
  payload: NpocServiceListQuery
): Promise<IResponse<NpocServiceListResult>> => {
  return request.post({ url: '/api/assets/npoc_service/list', data: payload })
}

// 批量删除：ids 由页面从表格勾选行取；空数组由页面先拦住，不发请求
export const deleteNpocServiceApi = (
  ids: string[]
): Promise<IResponse<NpocServiceDeleteResult>> => {
  return request.post({ url: '/api/assets/npoc_service/delete', data: { ids } })
}
