import request from '@/axios'
import type { SysLogListQuery, SysLogListResult } from './types'

// 系统日志：level 精确过滤、keyword 子串过滤，按时间倒序分页。
// 返回里的 source/truncated 直接透传给页面展示数据来源与截断提示。
export const getSysLogListApi = (
  payload: SysLogListQuery
): Promise<IResponse<SysLogListResult>> => {
  return request.post({ url: '/api/dashboard/logs', data: payload })
}
