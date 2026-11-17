// FOFA 查询 → 批量建任务（对标 ARL-Next taskFofa.py）。
// 字段与后端 internal/models/fofa.go、internal/services/fofa/service.go 一一对应。
//
// 两种落库方式：per-host（一个 host 一个任务，单次最多 50 个）与
// single（一个任务 N 个目标，N ≤ 1000）。同一 query 的 host 已存在于最近一次导入
// 记录时跳过，所以 created/skipped/failed 三个计数要一起看。

export type FofaCreateMode = 'per-host' | 'single'

// 余额字段对齐 FOFA /api/v1/info/my，未配置 key 时整个 balance 缺省
export interface FofaBalance {
  username?: string
  fofaPoint?: number
  remainFreePoint?: number
  remainApiQuery?: number
  vipLevel?: number
  isVIP?: boolean
}

// candidates 里每一项是单个内置 provider（http / none）的自述状态
export interface FofaProviderCandidate {
  provider: string
  available: boolean
  reason: string
  configured: boolean
  keySource?: string
  baseUrl?: string
  balance?: FofaBalance
}

export interface FofaProviderStatus extends FofaProviderCandidate {
  // 所有内置 provider 的状态，用来解释「为什么不可用」
  candidates?: FofaProviderCandidate[]
}

export interface FofaSearchQuery {
  query: string
  page?: number
  // 单页上限 100，超过后端 400
  size?: number
}

export interface FofaSearchPage {
  provider: string
  query: string
  page: number
  size: number
  total: number
  hosts: string[]
  // 本次查询消耗的 F 点（provider 未返回时不出现）
  consumedFPoint?: number
}

export interface FofaTaskForm {
  query: string
  // 拉取 host 数上限 1000
  size?: number
  // 必填：per-host 会自动拼 host 后缀，single 直接用原名
  taskName?: string
  // 项目 ObjectID（可选）
  project?: string
  // 模板 ObjectID（可选；给了会自动加入全部在线节点并派发）
  templateId?: string
  createMode: FofaCreateMode
}

export interface FofaImportError {
  host: string
  error: string
}

export interface FofaImportResult {
  importId: string
  query: string
  createMode: FofaCreateMode | string
  size: number
  created: number
  skipped: number
  failed: number
  taskIds: string[]
  taskNames: string[]
  hosts: string[]
  errors: FofaImportError[]
}

// 一次导入记录：hosts 是「这个 query 累计已建过任务的 host」，去重基准就是它
export interface FofaImport {
  id: string
  query: string
  hosts: string[]
  taskIds: string[]
  taskNames: string[]
  createMode: FofaCreateMode | string
  size: number
  project?: string
  templateId?: string
  created: number
  skipped: number
  failed: number
  errors?: FofaImportError[]
  createdAt: string
}

export interface FofaImportListQuery {
  // 按 query 原文模糊过滤
  query?: string
  pageIndex?: number
  // 上限 200，超过后端 400
  pageSize?: number
}

export interface FofaImportListResult {
  list: FofaImport[]
  total: number
  pageIndex: number
  pageSize: number
}
