import request from '@/axios'
import type { HttpStatusListQuery, HttpStatusListResult } from './types'

// HTTP 状态列表：按 host/url/title/service 关键字、项目与状态码过滤，
// 按最近探测时间倒序分页；pageSize 上限 200（后端 binding 校验）。
export const getHttpStatusListApi = (
  payload: HttpStatusListQuery
): Promise<IResponse<HttpStatusListResult>> => {
  return request.post({ url: '/api/assets/http_status/list', data: payload })
}
