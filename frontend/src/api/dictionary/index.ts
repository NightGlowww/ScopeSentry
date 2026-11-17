import request from '@/axios'
import type {
  DictionaryAppendResult,
  DictionaryCreateResult,
  DictionaryDeleteEntriesResult,
  DictionaryDeleteFileResult,
  DictionaryListQuery,
  DictionaryListResult,
  DictionaryPreviewResult,
  DictionarySearchResult,
  DictionaryUploadTask,
  DictUploadMode
} from './types'

// 预览与行搜索的条数上限与后端一致（MaxPreviewLines / MaxSearchLimit）。
export const DICT_PREVIEW_LIMIT = 200
export const DICT_SEARCH_LIMIT = 200
// 单次 append 行数上限（后端 MaxAppendLines）。
export const DICT_MAX_APPEND_LINES = 100000

const FIXED_KIND_ENDPOINTS: Record<string, { list: string; save: string }> = {
  brute: { list: '/api/dict_brute/list', save: '/api/dict_brute/save' },
  cdn: { list: '/api/dict_cdn/list', save: '/api/dict_cdn/save' }
}

// listDictionariesApi 通用列表：kind 作为过滤条件放在请求体里。
export const listDictionariesApi = (
  payload: DictionaryListQuery
): Promise<IResponse<DictionaryListResult>> => {
  return request.post({ url: '/api/dictionary/list', data: payload })
}

// listDictionariesByKindApi brute / cdn 走各自固定 kind 的 list，其余 kind 走通用列表。
export const listDictionariesByKindApi = (
  kind: string,
  payload: Omit<DictionaryListQuery, 'kind'>
): Promise<IResponse<DictionaryListResult>> => {
  const fixed = FIXED_KIND_ENDPOINTS[kind]
  if (fixed) {
    return request.post({ url: fixed.list, data: payload })
  }
  return listDictionariesApi({ ...payload, kind })
}

// previewDictionaryApi 预览前 limit 行（后端默认/上限 200）。
export const previewDictionaryApi = (
  kind: string,
  name: string,
  limit = DICT_PREVIEW_LIMIT
): Promise<IResponse<DictionaryPreviewResult>> => {
  return request.post({ url: '/api/dictionary/preview', data: { kind, name, limit } })
}

// searchDictionaryApi 在单个字典内按子串搜行。
export const searchDictionaryApi = (
  kind: string,
  name: string,
  keyword: string,
  limit = DICT_SEARCH_LIMIT
): Promise<IResponse<DictionarySearchResult>> => {
  return request.post({ url: '/api/dictionary/search', data: { kind, name, keyword, limit } })
}

// appendDictionaryApi 追加行；重复行由后端唯一索引剔除，返回新增/去重计数。
export const appendDictionaryApi = (
  kind: string,
  name: string,
  lines: string[]
): Promise<IResponse<DictionaryAppendResult>> => {
  return request.post({ url: '/api/dictionary/append', data: { kind, name, lines } })
}

// deleteDictionaryEntriesApi 删除指定行。
export const deleteDictionaryEntriesApi = (
  kind: string,
  name: string,
  entries: string[]
): Promise<IResponse<DictionaryDeleteEntriesResult>> => {
  return request.post({
    url: '/api/dictionary/delete_entries',
    data: { kind, name, entries }
  })
}

// deleteDictionaryFileApi 删除整个字典（连同所有行）。
export const deleteDictionaryFileApi = (
  kind: string,
  name: string
): Promise<IResponse<DictionaryDeleteFileResult>> => {
  return request.post({ url: '/api/dictionary/delete_file', data: { kind, name } })
}

// createDictionaryApi 新建字典；同名时后端返回 400（brute / cdn 用 save 覆盖语义）。
export const createDictionaryApi = (
  kind: string,
  name: string,
  content = ''
): Promise<IResponse<DictionaryCreateResult>> => {
  return request.post({ url: '/api/dictionary/create', data: { kind, name, content } })
}

// saveDictionaryByKindApi 新建/保存的统一入口：
// brute / cdn 走各自的 save（存在即整份覆盖），其余 kind 走 create（同名由后端拒绝）。
export const saveDictionaryByKindApi = (
  kind: string,
  name: string,
  content = ''
): Promise<IResponse<DictionaryCreateResult>> => {
  const fixed = FIXED_KIND_ENDPOINTS[kind]
  if (fixed) {
    return request.post({ url: fixed.save, data: { name, content } })
  }
  return createDictionaryApi(kind, name, content)
}

// downloadDictionaryApi 按 kind + name 下载纯文本字典（blob）。
export const downloadDictionaryApi = (kind: string, name: string): Promise<IResponse<Blob>> => {
  return request.get({
    url: '/api/dictionary/download',
    params: { kind, name },
    responseType: 'blob'
  })
}

// uploadLargeDictionaryApi 大文件上传：后端流式落临时文件后返回 taskId，进度查 upload_status。
export const uploadLargeDictionaryApi = (payload: {
  kind: string
  name: string
  mode: DictUploadMode
  file: File
}): Promise<IResponse<DictionaryUploadTask>> => {
  const form = new FormData()
  form.append('kind', payload.kind)
  form.append('name', payload.name)
  form.append('mode', payload.mode)
  form.append('file', payload.file)
  return request.post({
    url: '/api/dictionary/upload_large',
    data: form,
    // 浏览器端由 axios 清掉 Content-Type 自己补 boundary，这里只是标注意图
    headers: { 'Content-Type': 'multipart/form-data' } as any
  })
}

// uploadDictionaryStatusApi 查询上传任务进度。
export const uploadDictionaryStatusApi = (
  taskId: string
): Promise<IResponse<DictionaryUploadTask>> => {
  return request.get({ url: '/api/dictionary/upload_status', params: { taskId } })
}
