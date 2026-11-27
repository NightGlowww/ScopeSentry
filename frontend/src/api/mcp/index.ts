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

export interface McpConfig {
  enabled: boolean
  allowedTools: string[] | null
  rateLimitPerMin: number
  logRetention: number
  updatedAt?: string
  updatedBy?: string
}

export const getMcpConfigApi = (): Promise<IResponse<McpConfig>> => {
  return request.get({ url: '/api/mcp/config' })
}

export const updateMcpConfigApi = (data: Partial<McpConfig>): Promise<IResponse<McpConfig>> => {
  return request.post({ url: '/api/mcp/config', data })
}

export interface McpToolEntry {
  name: string
  scope: string
}

export interface McpScopeEntry {
  scope: string
  tools: number
  names: string[]
}

// 工具 ↔ scope 对照表：key 的 scope 选择器用它显示「这个 scope 解锁几个工具」，
// 也让用户能反查「某个工具需要什么 scope」。
export const getMcpToolsApi = (): Promise<
  IResponse<{ list: McpToolEntry[]; scopes: McpScopeEntry[]; total: number }>
> => {
  return request.get({ url: '/api/mcp/tools' })
}
