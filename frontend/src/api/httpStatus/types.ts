// HTTP 状态读模型（对标 ARL-Next http_status）：一行是一个 host:port 的最近一次
// HTTP 探测结果。status 既可以是分档（2xx/3xx/4xx/5xx），也可以是精确码（如 404）。
export interface HttpStatusRow {
  host: string
  port: string | number
  // 0 表示扫描端没拿到响应码（超时/连接失败），列表里显示成 "-"
  statusCode: number
  title: string
  url: string
  service: string
  project: string
  time: string | null
}

export interface HttpStatusListQuery {
  // 2xx/3xx/4xx/5xx 或三位精确码
  status?: string
  keyword?: string
  project?: string
  pageIndex?: number
  // 上限 200，后端 binding 会把更大的值拒成 400
  pageSize?: number
}

export interface HttpStatusListResult {
  list: HttpStatusRow[]
  total: number
}
