// NPoC 服务读模型（对标 ARL-Next assetNpocService）：一行是扫描端对某个目标
// 识别出的一个服务（scheme + host + port），target 是探测时实际用的目标串。
// 数据由节点侧的 NPoC 识别模块落库，前端只读；删除按 id 批量删。
export interface NpocServiceRow {
  // 批量删除按 id 走，后端返回的 ObjectId 会字符串化
  id: string
  // 协议/服务类型（http/https/redis…）；扫描端一般写小写，老数据可能大写
  scheme: string
  // 主机名或 IP
  host: string
  // 端口；老数据里可能存成字符串，展示层统一转文本
  port: number | string
  // 探测目标（常见形态 host:port 或完整 URL），保留扫描端原样
  target: string
  // 首次入库时间；老数据可能为 null
  insertTime: string | null
}

export interface NpocServiceListQuery {
  // host / target 子串
  keyword?: string
  // 精确协议名（页面已统一小写）
  scheme?: string
  // 精确主机名；列表页不单独暴露输入框（keyword 已覆盖 host 子串），
  // 但接口支持，留在这里避免调用方各自拼参数
  host?: string
  pageIndex?: number
  // 上限 200，后端 binding 会把更大的值拒成 400
  pageSize?: number
}

export interface NpocServiceListResult {
  list: NpocServiceRow[]
  total: number
}

// 批量删除的返回：后端给 deleted 时页面可以据此提示，不给也不影响刷新
export interface NpocServiceDeleteResult {
  deleted?: number
}
