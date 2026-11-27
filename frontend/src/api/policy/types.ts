// 扫描策略（对标 ARL-Next policy.py）：模板/插件/并发/速率/超时/字典的一组可复用预设。
// 字段与后端 internal/models/scan_policy.go 一一对应，null 表示「未设置」而不是「0」。
//
// 注意 threadNum / rateLimit / timeoutSeconds 三个指针字段：rateLimit=0 是合法值（不限速），
// 所以类型上保留 null 而不是用 0 代替「未设置」。

export interface ScanPolicyDicts {
  dir?: string | null
  subdomain?: string | null
  brute?: string | null
  // 端口字典名：模板参数里用 {dict.port.<名称>} 引用（对齐 ARL-Next 1.5.2）
  port?: string | null
}

export interface ScanPolicyConfig {
  templateId?: string
  plugins?: string[]
  threadNum?: number | null
  rateLimit?: number | null
  timeoutSeconds?: number | null
  dicts?: ScanPolicyDicts | null
  options?: Record<string, string> | null
}

export interface ScanPolicy {
  id: string
  name: string
  description: string
  config: ScanPolicyConfig
  enabled: boolean
  createdAt: string
  updatedAt: string
}

// detail 端点附带的引用说明：任务表没有 policyId，referenceCount 永远只是 0，
// note 会如实说明「无法统计引用」，前端不要把 usage 当成真实引用计数展示。
export interface PolicyUsage {
  tracked: boolean
  referenceCount: number
  note: string
}

export interface ScanPolicyDetail extends ScanPolicy {
  usage: PolicyUsage
}

// test 的 errors[] / create 的 400 详情 / apply 的 warnings[] 是同一种结构
export interface PolicyIssue {
  field: string
  code: string
  message: string
}

export interface PolicyWarning extends PolicyIssue {}

export interface ScanPolicyValidation {
  valid: boolean
  config: ScanPolicyConfig
  errors: PolicyIssue[]
  warnings?: PolicyIssue[]
}

// apply 的合并结果：纯预览，不写库
export interface ScanPolicyApplyResult {
  policyId: string
  policyName: string
  // empty / template / object：任务模板这一侧默认值的来源
  taskTemplateSource: string
  config: ScanPolicyConfig
  warnings: PolicyWarning[]
  appliedFields: string[]
}

export interface ScanPolicyListQuery {
  keyword?: string
  // 只查启用/停用的策略；undefined = 不过滤
  enabled?: boolean
  pageIndex?: number
  pageSize?: number
}

export interface ScanPolicyListResult {
  list: ScanPolicy[]
  total: number
  pageIndex: number
  pageSize: number
}

export interface ScanPolicyCreateForm {
  name: string
  description?: string
  config: ScanPolicyConfig
  enabled?: boolean
}

// config 传了就整体替换；其余字段不传即不改
export interface ScanPolicyUpdateForm {
  id: string
  name?: string
  description?: string
  config?: ScanPolicyConfig
  enabled?: boolean
}

export interface ScanPolicyDeleteResult {
  deleted: number
  ids: string[]
}

// apply 入参：taskTemplate 允许「模板 ID 字符串」或「对象」两种形态
export type ScanPolicyTaskTemplate = string | ScanPolicyConfig
