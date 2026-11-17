// 字典管理（对标 ARL-Next 的 dictionary.py / brute_dict.py / cdn_dict.py）。
// kind 决定分类；行以「行」为粒度落库，列表只带统计，内容走 preview/search。
export type DictKind = 'dir' | 'subdomain' | 'brute' | 'cdn' | 'other'

export type DictUploadStatus = 'pending' | 'running' | 'done' | 'failed'

export type DictUploadMode = 'create' | 'append'

export interface DictionaryKindCount {
  kind: string
  count: number
}

export interface DictionaryItem {
  id: string
  name: string
  kind: string
  category: string
  size: string
  lineCount: number
  updatedAt: string
}

export interface DictionaryListQuery {
  kind?: string
  search?: string
  pageIndex?: number
  pageSize?: number
}

export interface DictionaryListResult {
  list: DictionaryItem[]
  total: number
  pageIndex: number
  pageSize: number
  kinds: DictionaryKindCount[]
}

export interface DictionaryPreviewResult {
  kind: string
  name: string
  total: number
  lines: string[]
  limit: number
  truncated: boolean
}

export interface DictionaryMatch {
  value: string
  seq: number
}

export interface DictionarySearchResult {
  kind: string
  name: string
  keyword: string
  matches: DictionaryMatch[]
  returned: number
  limit: number
  truncated: boolean
}

export interface DictionaryAppendResult {
  kind: string
  name: string
  added: number
  duplicated: number
  total: number
}

export interface DictionaryDeleteEntriesResult {
  kind: string
  name: string
  deleted: number
  total: number
}

export interface DictionaryDeleteFileResult {
  kind: string
  name: string
  deleted: boolean
}

export interface DictionaryCreateResult {
  id: string
  kind: string
  name: string
  lineCount: number
  size: string
  replaced: boolean
}

export interface DictionaryUploadTask {
  taskId: string
  kind: string
  name: string
  mode: DictUploadMode
  status: DictUploadStatus
  totalLines: number
  processedLines: number
  added: number
  duplicated: number
  progress: number
  error: string
  createdAt: string
  updatedAt: string
  finishedAt?: string
}

// DictionaryRow 右侧表格的展示行：预览时 seq 是行号，搜索时是后端返回的真实 seq。
export interface DictionaryRow {
  value: string
  seq: number
}
