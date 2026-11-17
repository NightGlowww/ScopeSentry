// 文件泄露读模型（对标 ARL-Next assetFileleak）：一行是扫描端「文件泄露检测」
// 命中的一条记录（url + 所属站点 + 标题 + 响应码 + 响应长度）。
// 数据由节点侧模块落库，前端只读；删除是按 id 批量删。
export interface FileLeakRow {
  // 批量删除按 id 走，后端返回的 ObjectId 会字符串化
  id: string
  // 命中的完整地址；历史数据可能没有 scheme，展示层只把 http(s) 渲染成外链
  url: string
  // 所属站点（根域名/站点标识），用于把同一站点的命中归在一起看
  site: string
  // 响应标题，扫描端没解析出来时为空串
  title: string
  // 响应码：0 表示没拿到响应（超时/连接失败），列表里显示成 "-"
  statusCode: number
  // 响应体字节数，展示时转成人类可读（B/KB/MB）
  contentLength: number
  // 首次入库 / 最近一次命中的时间；老数据可能为 null
  insertTime: string | null
  updateTime: string | null
}

export interface FileLeakListQuery {
  // url / 站点 / 标题子串
  keyword?: string
  // 精确响应码（如 404）。分档（4xx）只用于页面着色，不发给后端
  statusCode?: number
  pageIndex?: number
  // 上限 200，后端 binding 会把更大的值拒成 400
  pageSize?: number
}

export interface FileLeakListResult {
  list: FileLeakRow[]
  total: number
}

// 批量删除的返回：后端给 deleted 时页面可以据此提示，不给也不影响刷新
export interface FileLeakDeleteResult {
  deleted?: number
}
