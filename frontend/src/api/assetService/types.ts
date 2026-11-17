// 服务维度读模型（对标 ARL-Next assetService.py）：一行是一个服务名，
// 聚合出出现该服务的资产/主机/端口数与样例 host。
// 服务来自资产端口识别与指纹探测，不是人工录入，所以 counts 都是扫描结果现算。
export interface AssetServiceRow {
  // 服务名（如 http/https/redis/ssh），按服务名精确聚合
  service: string
  assetCount: number
  hostCount: number
  portCount: number
  // 样例 host：后端给多少就收多少，前端只展示前若干个，其余折叠成 "+N"
  samples: string[]
  // 最近一次出现时间，可能为 null（老数据没有时间戳）
  lastSeen: string | null
}

export interface AssetServiceListQuery {
  // 服务名子串
  keyword?: string
  // 项目名（后端 filter 按项目名精确匹配）
  project?: string
  pageIndex?: number
  // 上限 200，后端 binding 会把更大的值拒成 400
  pageSize?: number
}

export interface AssetServiceListResult {
  list: AssetServiceRow[]
  total: number
}
