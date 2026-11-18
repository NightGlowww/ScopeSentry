import request from '@/axios'
import type { ApiKeyData, CreateApiKeyResponse } from './types'
import { commonRespData } from '../scommon/types'

interface ApiKeyListResponse {
  list: ApiKeyData[]
}

export const getApiKeyListApi = (): Promise<IResponse<ApiKeyListResponse>> => {
  return request.get({ url: '/api/apikey/list' })
}

export const createApiKeyApi = (name: string): Promise<IResponse<CreateApiKeyResponse>> => {
  return request.post({ url: '/api/apikey/create', data: { name } })
}

export const deleteApiKeyApi = (id: string): Promise<IResponse<commonRespData>> => {
  return request.post({ url: '/api/apikey/delete', data: { id } })
}

// ---- MCP Key 治理（scope / 工具白名单 / 限速 / 过期）----
export interface McpKeyPayload {
  name: string
  scope?: string[]
  tools?: string[]
  rateLimitPerMin?: number
  expiresAt?: string
}

export const getMcpKeysApi = (params?: {
  keyword?: string
  pageIndex?: number
  pageSize?: number
}): Promise<IResponse<{ list: ApiKeyData[]; total: number }>> => {
  return request.get({ url: '/api/mcp/keys', params })
}

export const createMcpKeyApi = (data: McpKeyPayload): Promise<IResponse<CreateApiKeyResponse>> => {
  return request.post({ url: '/api/mcp/keys', data })
}

export const setMcpKeyStatusApi = (
  id: string,
  enabled: boolean
): Promise<IResponse<commonRespData>> => {
  return request.post({ url: '/api/mcp/keys/' + id + '/status', data: { enabled } })
}

export const deleteMcpKeyApi = (id: string): Promise<IResponse<commonRespData>> => {
  return request.delete({ url: '/api/mcp/keys/' + id })
}

export const updateMcpKeyApi = (
  id: string,
  data: McpKeyPayload
): Promise<IResponse<ApiKeyData>> => {
  return request.post({ url: '/api/mcp/keys/' + id, data })
}
