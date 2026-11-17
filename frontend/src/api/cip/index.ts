import request from '@/axios'
import type { CipDetailQuery, CipDetailResult, CipListQuery, CipListResult } from './types'

// 主体列表：按 company 分组统计域名数与样例域名；keyword 匹配 company/icp/domain。
export const getCipListApi = (payload: CipListQuery): Promise<IResponse<CipListResult>> => {
  return request.post({ url: '/api/assets/cip/list', data: payload })
}

// 主体详情：传主体下的任一根域名，返回该主体的域名列表（分页）。
export const getCipDetailApi = (payload: CipDetailQuery): Promise<IResponse<CipDetailResult>> => {
  return request.post({ url: '/api/assets/cip/detail', data: payload })
}
