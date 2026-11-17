import request from '@/axios'
import type { CertDetailQuery, CertListQuery, CertListResult, CertRow } from './types'

// 证书列表：同一张证书（指纹，或 serial+issuer）合并成一行，默认按 notAfter 升序。
// pageSize 上限 200 由后端 binding 校验，前端选择器也只给到 200。
export const getCertListApi = (payload: CertListQuery): Promise<IResponse<CertListResult>> => {
  return request.post({ url: '/api/assets/cert/list', data: payload })
}

// 证书详情：按列表返回的聚合键取单张证书，附带关联 host+ip+port（最多 50 条）。
export const getCertDetailApi = (payload: CertDetailQuery): Promise<IResponse<CertRow>> => {
  return request.post({ url: '/api/assets/cert/detail', data: payload })
}
