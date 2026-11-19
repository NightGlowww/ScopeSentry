import request from '@/axios'
import type { pocData, pocRespData, pocContent } from './types'

interface pocDataResponse {
  list: pocData[]
  total: number
}
export const getPocDataApi = (
  search: string,
  pageIndex: number,
  pageSize: number,
  filter: Record<string, any>
): Promise<IResponse<pocDataResponse>> => {
  return request.post({ url: '/api/poc', data: { search, pageIndex, pageSize, filter } })
}

// 「立即同步」：从上游仓库拉取 POC 并导入，返回导入统计
export const syncPocApi = (url?: string): Promise<IResponse<any>> => {
  return request.post({ url: '/api/poc/data/sync', data: url ? { url } : {} })
}

export const getPocDataAllApi = (): Promise<IResponse<pocDataResponse>> => {
  return request.get({ url: '/api/poc/data/all' })
}
export const getPocNameListApi = (): Promise<IResponse<pocDataResponse>> => {
  return request.get({ url: '/api/poc/name/list' })
}

export const getPocContentApi = (id: string): Promise<IResponse<pocContent>> => {
  return request.post({ url: '/api/poc/content', data: { id } })
}

export const getPocDetailApi = (id: string): Promise<IResponse<pocData>> => {
  return request.post({ url: '/api/poc/detail', data: { id } })
}

export const addPocDataApi = (
  name: string,
  content: string,
  level: string,
  tags: string[]
): Promise<IResponse<pocRespData>> => {
  return request.post({ url: '/api/poc/add', data: { name, content, level, tags } })
}

export const updatePocDataApi = (
  id: string,
  name: string,
  content: string,
  level: string,
  tags: string[]
): Promise<IResponse<pocRespData>> => {
  return request.post({ url: '/api/poc/update', data: { id, name, content, level, tags } })
}

export const deletePocDataApi = (ids: string[]): Promise<IResponse<pocRespData>> => {
  return request.post({ url: '/api/poc/delete', data: { ids } })
}


// POC 导入进度（前端轮询）
export const getImportStatusApi = (): Promise<IResponse<any>> =>
  request.get({ url: '/api/poc/data/import/status' })
