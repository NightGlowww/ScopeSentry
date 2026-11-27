import request from '@/axios'
import { commonRespData } from '../scommon/types'

export const exportApi = (
  index: string,
  quantity: number,
  type: string,
  search: string,
  filter: Record<string, any>,
  startTime: string,
  endTime: string,
  field: string[],
  filetype: string
): Promise<IResponse<commonRespData>> => {
  return request.post({
    url: '/api/export',
    data: { index, quantity, type, search, filter, startTime, endTime, field, filetype }
  })
}

export const getExportRecordApi = () => {
  return request.get({ url: '/api/export/records' })
}

export const delExportApi = (ids: string[]): Promise<IResponse<commonRespData>> => {
  return request.post({ url: '/api/export/delete', data: { ids } })
}

export const downloadExportApi = (id: string): Promise<Blob> => {
  return new Promise((resolve, reject) => {
    request
      .get({
        url: '/api/export/download?filename=' + id,
        responseType: 'blob'
      })
      .then((response) => {
        resolve(response.data)
      })
      .catch((error) => {
        reject(error)
      })
  })
}

type FieldRespData = {
  field: string[]
}

export const getFieldApi = (index: string): Promise<IResponse<FieldRespData>> => {
  return request.post({ url: '/api/export/fields', data: { index } })
}

export interface BatchExportPayload {
  type: string
  project?: string
  taskName?: string
  startTime?: string
  endTime?: string
  fields?: string[]
}

export interface BatchPreviewResult {
  type: string
  collection: string
  fields: string[]
  availableFields: string[]
  rows: string[][]
  total: number
  limit: number
}

export const previewBatchExportApi = (
  data: BatchExportPayload
): Promise<IResponse<BatchPreviewResult>> => {
  return request.post({ url: '/api/export/preview', data })
}

export const batchExportCsvApi = async (data: BatchExportPayload): Promise<Blob> => {
  const res = await request.post({
    url: '/api/export/batch',
    data,
    responseType: 'blob'
  })
  return res.data as unknown as Blob
}

// 按任务导出：
//   format=zip（默认）→ 一个集合一个 CSV，打进 zip
//   format=xlsx       → 一个工作簿，每个集合一张工作表（对齐 ARL-Next 1.5.2）
export const exportTaskApi = (taskId: string, format: 'zip' | 'xlsx' = 'zip'): Promise<Blob> => {
  return new Promise((resolve, reject) => {
    request
      .get({
        url: '/api/export/task/' + taskId + (format === 'xlsx' ? '?format=xlsx' : ''),
        responseType: 'blob',
        timeout: 300000
      })
      .then((response) => resolve(response.data))
      .catch((error) => reject(error))
  })
}
