import request from '@/axios'
import type { FileLeakDeleteResult, FileLeakListQuery, FileLeakListResult } from './types'

// 文件泄露列表：按 url/站点/标题子串(keyword) 与精确响应码(statusCode) 过滤，
// 按入库时间倒序分页；pageSize 上限 200（后端 binding 校验）。
export const getFileLeakListApi = (
  payload: FileLeakListQuery
): Promise<IResponse<FileLeakListResult>> => {
  return request.post({ url: '/api/assets/fileleak/list', data: payload })
}

// 批量删除：ids 由页面从表格勾选行取；空数组由页面先拦住，不发请求
export const deleteFileLeakApi = (ids: string[]): Promise<IResponse<FileLeakDeleteResult>> => {
  return request.post({ url: '/api/assets/fileleak/delete', data: { ids } })
}
