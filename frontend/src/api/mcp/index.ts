import request from '@/axios'

export interface McpCallLog {
  id: string
  keyId: string
  keyPrefix: string
  tool: string
  status: string
  durationMs: number
  clientIp: string
  error?: string
  createdAt: string
}

export interface McpLogQuery {
  keyId?: string
  tool?: string
  status?: string
  from?: string
  to?: string
  pageIndex?: number
  pageSize?: number
}

export const getMcpLogsApi = (
  params: McpLogQuery
): Promise<IResponse<{ list: McpCallLog[]; total: number }>> => {
  return request.get({ url: '/api/mcp/logs', params })
}

export const exportMcpLogsApi = (params: McpLogQuery): string => {
  const qs = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '') qs.set(k, String(v))
  })
  return '/api/mcp/logs/export?' + qs.toString()
}
