import request from '@/axios'
import type {
  NucleiResultDetail,
  NucleiResultListQuery,
  NucleiResultListResult,
  StatFingerAssetsQuery,
  StatFingerAssetsResult,
  StatFingerListQuery,
  StatFingerListResult,
  WihDetail,
  WihListQuery,
  WihListResult
} from './types'

// 指纹统计：按技术名聚合出现次数/资产数/最近出现/样例 host。
export const getStatFingerListApi = (
  payload: StatFingerListQuery
): Promise<IResponse<StatFingerListResult>> => {
  return request.post({ url: '/api/assets/stat_finger/list', data: payload })
}

// 按技术名反查资产（抽屉里的列表）。
export const getStatFingerAssetsApi = (
  payload: StatFingerAssetsQuery
): Promise<IResponse<StatFingerAssetsResult>> => {
  return request.post({ url: '/api/assets/stat_finger/assets', data: payload })
}

// Nuclei 结果列表：project / severity / templateId / keyword 过滤。
export const getNucleiResultListApi = (
  payload: NucleiResultListQuery
): Promise<IResponse<NucleiResultListResult>> => {
  return request.post({ url: '/api/assets/nuclei_result/list', data: payload })
}

// 单条 Nuclei 结果：带请求/响应原文。
export const getNucleiResultDetailApi = (id: string): Promise<IResponse<NucleiResultDetail>> => {
  return request.post({ url: '/api/assets/nuclei_result/detail', data: { id } })
}

// WIH（敏感信息）列表：type / project / keyword 过滤。
export const getWihListApi = (payload: WihListQuery): Promise<IResponse<WihListResult>> => {
  return request.post({ url: '/api/assets/wih/list', data: payload })
}

// WIH 单条详情；正文用 bodyId 走现成的 sensitive/body 接口。
export const getWihDetailApi = (id: string): Promise<IResponse<WihDetail>> => {
  return request.post({ url: '/api/assets/wih/detail', data: { id } })
}
