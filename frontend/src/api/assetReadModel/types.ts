// 三个只读资产读模型（stat_finger / nuclei_result / wih）的返回结构，
// 字段与后端 models/stat_finger.go、models/nuclei_result.go、models/wih.go 一一对应。
export interface StatFingerRow {
  name: string
  count: number
  assetCount: number
  lastSeen: string
  samples: string[]
}

export interface StatFingerListResult {
  list: StatFingerRow[]
  total: number
}

export interface StatFingerAssetRow {
  id: string
  host: string
  port: string
  ip: string
  url: string
  title: string
  statusCode: number
  service: string
  project: string
  rootDomain: string
  time: string
}

export interface StatFingerAssetsResult {
  list: StatFingerAssetRow[]
  total: number
}

// 列表查询：pageSize 由后端限制 ≤ 200。
export interface StatFingerListQuery {
  keyword?: string
  project?: string
  pageIndex?: number
  pageSize?: number
}

export interface StatFingerAssetsQuery {
  technology: string
  project?: string
  pageIndex?: number
  pageSize?: number
}

export interface NucleiResultItem {
  id: string
  templateId: string
  name: string
  severity: string
  url: string
  host: string
  matched: string
  project: string
  rootDomain: string
  taskName: string
  tags: string[]
  status: number
  matchedAt: string
  hash: string
}

export interface NucleiResultDetail extends NucleiResultItem {
  request: string
  response: string
  // 命中的 POC 模板正文（后端按 templateId 从 PocList 取，bson:"-"）
  templateContent?: string
}

export interface NucleiResultListResult {
  list: NucleiResultItem[]
  total: number
}

export interface NucleiResultListQuery {
  project?: string
  severity?: string
  templateId?: string
  keyword?: string
  // 与 internal/models/nuclei_result.go NucleiResultListRequest 对齐
  // status: 0=全部 1未处理 2处理中 3忽略 4疑似 5确认 6已处理
  status?: number
  taskName?: string
  host?: string
  pageIndex?: number
  pageSize?: number
}

export interface WihRow {
  id: string
  type: string
  url: string
  host: string
  match: string[]
  color: string
  tags: string[]
  status: number
  project: string
  rootDomain: string
  taskName: string
  time: string
  bodyId: string
}

// 详情与列表行同构：正文仍按 bodyId 走 /api/assets/sensitive/body 取，不进列表。
export type WihDetail = WihRow

export interface WihListResult {
  list: WihRow[]
  total: number
}

export interface WihListQuery {
  type?: string
  project?: string
  keyword?: string
  pageIndex?: number
  pageSize?: number
}
