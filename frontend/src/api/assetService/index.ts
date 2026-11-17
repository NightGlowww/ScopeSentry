import request from '@/axios'
import type { AssetServiceListQuery, AssetServiceListResult } from './types'

// 服务维度列表：按服务名子串(keyword)、项目名(project) 过滤，
// 按资产数倒序（同数按服务名）分页；pageSize 上限 200（超出后端回落成 20）。
export const getAssetServiceListApi = (
  payload: AssetServiceListQuery
): Promise<IResponse<AssetServiceListResult>> => {
  return request.post({ url: '/api/assets/service/list', data: payload })
}
