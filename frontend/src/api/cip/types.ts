// ICP / 主体读模型（对标 ARL-Next assetCip）。数据从 RootDomain 的 company/icp 字段
// 按 company 分组现算；字段与后端 internal/models/cip.go 一一对应。

export interface CipRow {
  company: string
  icp: string
  icps: string[]
  domainCount: number
  // samples 是样例根域名（详情按钮用它反查主体下的域名列表）
  samples: string[]
  lastTime: string
}

export interface CipListQuery {
  keyword?: string
  project?: string
  company?: string
  pageIndex?: number
  pageSize?: number
}

export interface CipListResult {
  list: CipRow[]
  total: number
}

export interface CipDomain {
  domain: string
  icp: string
  project: string
  time: string
  tags: string[]
}

// 详情以「主体下的某个根域名」为入口反查 company，再分页返回该主体的域名列表。
export interface CipDetailQuery {
  domain: string
  project?: string
  pageIndex?: number
  pageSize?: number
}

export interface CipDetailResult {
  domain: string
  company: string
  icp: string
  icps: string[]
  total: number
  list: CipDomain[]
  pageIndex: number
  pageSize: number
}
