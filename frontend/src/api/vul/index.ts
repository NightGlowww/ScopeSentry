import request from '@/axios'
import type { vulResultData } from './types'

interface vulResultDataResponse {
  list: vulResultData[]
  total: number
}

interface vulDetailResponse {
  res: string
  req: string
}
export const getVulResultDataApi = (
  search: string,
  pageIndex: number,
  pageSize: number,
  filter: Record<string, any>,
  timeFrom?: string,
  timeSort?: string
): Promise<IResponse<vulResultDataResponse>> => {
  return request.post({
    url: '/api/assets/vulnerability',
    data: {
      search,
      pageIndex,
      pageSize,
      filter,
      timeFrom: timeFrom || undefined,
      sort: timeSort ? { time: timeSort } : undefined
    }
  })
}

// eslint-disable-next-line prettier/prettier
export const getVulDetailApi = (
  hash: string
): Promise<IResponse<vulDetailResponse>> => {
  return request.post({
    url: '/api/assets/vulnerability/detail',
    data: { hash }
  })
}

// LLM 按需判定：把选中的漏洞交给判定后端（agent/LLM）立即打分，
// 返回 isVuln / evidence / nq / decision，界面据此显示「模型怎么看这条洞」。
export const llmVerifyApi = (ids: string[], apply = false): Promise<IResponse<any>> => {
  return request.post({
    url: '/api/assets/vulnerability/llm-verify',
    data: { ids, apply }
  })
}
