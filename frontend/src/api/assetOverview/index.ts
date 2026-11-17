import request from '@/axios'
import type { AssetOverviewCount, AssetOverviewQuery } from './types'

// 一行统计卡片的数据源：一次拿到 asset/subdomain/url/app/mp/vulnerability/sensitive/cert/cip。
// 空 body 等价于全库口径，所以这里只在传了 project 时才发字段。
export const getAssetOverviewCountApi = (
  payload: AssetOverviewQuery = {}
): Promise<IResponse<AssetOverviewCount>> => {
  return request.post({ url: '/api/assets/asset_overview/count', data: payload })
}
