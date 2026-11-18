import request from '@/axios'

export interface AuditLogItem {
  id: string
  user: string
  method: string
  path: string
  status: number
  durationMs: number
  ip: string
  ua?: string
  at: string
}

export const listAuditLogsApi = (data: {
  user?: string
  path?: string
  method?: string
  statusMin?: number
  pageIndex?: number
  pageSize?: number
}): Promise<IResponse<{ list: AuditLogItem[]; total: number }>> =>
  request.post({ url: '/api/audit/list', data })
