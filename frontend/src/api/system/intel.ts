import request from '@/axios'

export interface IntelFeed {
  _id: string
  name: string
  url: string
  kind: string
  enabled: boolean
  intervalHours: number
  lastRunAt?: string
  lastError?: string
  itemCount?: number
}

export interface IntelItem {
  source: string
  title: string
  url: string
  summary?: string
  published?: string
  addedAt: string
}

export interface IntelFeedsResp {
  feeds: IntelFeed[]
  items: IntelItem[]
  total: number
}

export const getIntelFeedsApi = (): Promise<IResponse<IntelFeedsResp>> => {
  return request.get({ url: '/api/system/intel-feeds' })
}

export const runIntelFeedApi = (id?: string): Promise<IResponse<any>> => {
  return request.post({ url: '/api/system/intel-feeds/run', data: id ? { id } : {} })
}

export const updateIntelFeedApi = (
  id: string,
  payload: { enabled?: boolean; intervalHours?: number }
): Promise<IResponse<any>> => {
  return request.post({ url: '/api/system/intel-feeds/update', data: { id, ...payload } })
}
