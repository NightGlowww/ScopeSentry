// 扫描策略 / FOFA 在 UI 层的常量、输入归一与「策略 → 建任务表单」的合并规则。
// 放 utils 是为了让这些纯函数有单测，不必挂载组件。
//
// 这些上限与后端保持一致（internal/models/scan_policy.go、internal/models/fofa.go），
// 前端只是提前拦住明显越界的输入，最终仍由后端校验。

export const POLICY_MAX_PAGE_SIZE = 200
export const POLICY_MAX_NAME_LENGTH = 64
export const POLICY_MAX_DESCRIPTION_LENGTH = 512
export const POLICY_MIN_THREAD_NUM = 1
export const POLICY_MAX_THREAD_NUM = 200
export const POLICY_MIN_RATE_LIMIT = 0
export const POLICY_MAX_RATE_LIMIT = 1000
export const POLICY_MIN_TIMEOUT_SECONDS = 10
export const POLICY_MAX_TIMEOUT_SECONDS = 3600

export const FOFA_MAX_PREVIEW_SIZE = 100
export const FOFA_MAX_IMPORT_SIZE = 1000
// per-host 模式单次最多建 50 个任务（待建 host 数，不含跳过的）
export const FOFA_MAX_PER_HOST_TASKS = 50
export const FOFA_MAX_TASK_NAME_LENGTH = 120

export interface PolicyConfigLike {
  templateId?: string
  plugins?: string[]
  threadNum?: number | null
  rateLimit?: number | null
  timeoutSeconds?: number | null
  dicts?: { dir?: string | null; subdomain?: string | null; brute?: string | null } | null
  options?: Record<string, string> | null
}

// 表单初值：全部留空 / null，提交前由 buildPolicyConfig 收敛成后端要的形状
export const emptyPolicyConfig = (): PolicyConfigLike => ({
  templateId: '',
  plugins: [],
  threadNum: null,
  rateLimit: null,
  timeoutSeconds: null,
  dicts: { dir: '', subdomain: '', brute: '' },
  options: {}
})

// 多行 / 逗号分隔的插件清单：去空白、去重保序
export const parsePluginsText = (text?: string | null): string[] => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const chunk of (text ?? '').split(/[\n,]/)) {
    const value = chunk.trim()
    if (!value || seen.has(value)) continue
    seen.add(value)
    out.push(value)
  }
  return out
}

export interface ParsedOptions {
  options: Record<string, string>
  // 重复的 key（后出现的覆盖前者）——UI 用来提示而不是静默丢数据
  duplicates: string[]
  // 没有 '=' 的行（视为 key，值为空串）
  invalid: string[]
}

// options 文本框：一行一个 key=value；没有 '=' 的行按「无值」处理并记进 invalid
export const parseOptionsText = (text?: string | null): ParsedOptions => {
  const options: Record<string, string> = {}
  const duplicates: string[] = []
  const invalid: string[] = []
  for (const rawLine of (text ?? '').split(/\r?\n/)) {
    const line = rawLine.trim()
    if (!line) continue
    const idx = line.indexOf('=')
    const key = (idx < 0 ? line : line.slice(0, idx)).trim()
    const value = idx < 0 ? '' : line.slice(idx + 1).trim()
    if (!key) {
      invalid.push(line)
      continue
    }
    if (key in options) duplicates.push(key)
    options[key] = value
    if (idx < 0) invalid.push(line)
  }
  return { options, duplicates, invalid }
}

// options → 文本（弹窗回填用）：key 排序保证每次渲染稳定
export const formatOptionsText = (options?: Record<string, string> | null): string => {
  if (!options) return ''
  return Object.keys(options)
    .sort()
    .map((key) => (options[key] === '' ? key : `${key}=${options[key]}`))
    .join('\n')
}

// 「有效配置」判定：至少填了一项，否则 create/update 没有意义
export const policyConfigHasContent = (config?: PolicyConfigLike | null): boolean => {
  if (!config) return false
  if ((config.templateId ?? '').trim()) return true
  if ((config.plugins ?? []).some((item) => item.trim())) return true
  if (typeof config.threadNum === 'number') return true
  if (typeof config.rateLimit === 'number') return true
  if (typeof config.timeoutSeconds === 'number') return true
  const dicts = config.dicts ?? {}
  if ((dicts.dir ?? '').trim() || (dicts.subdomain ?? '').trim() || (dicts.brute ?? '').trim()) {
    return true
  }
  return Object.keys(config.options ?? {}).length > 0
}

// 数字输入的空值归一：'' / null / undefined → null，其余转数字（NaN 也归 null）
export const toNullableNumber = (value: unknown): number | null => {
  if (value === '' || value === null || value === undefined) return null
  const num = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(num) ? num : null
}

// appliedFields / warnings 里的字段名 → i18n key；未知字段返回空串，由调用方回退成原文
const POLICY_FIELD_KEYS: Record<string, string> = {
  templateId: 'policy.fieldTemplateId',
  plugins: 'policy.fieldPlugins',
  threadNum: 'policy.fieldThreadNum',
  rateLimit: 'policy.fieldRateLimit',
  timeoutSeconds: 'policy.fieldTimeoutSeconds',
  'dicts.dir': 'policy.fieldDictDir',
  'dicts.subdomain': 'policy.fieldDictSubdomain',
  'dicts.brute': 'policy.fieldDictBrute',
  options: 'policy.fieldOptions',
  taskTemplate: 'policy.fieldTaskTemplate'
}

export const policyFieldLabelKey = (field?: string | null): string => {
  return POLICY_FIELD_KEYS[(field ?? '').trim()] ?? ''
}

export interface PolicyConflict {
  field: string
  // 表单里已有的值
  current: string
  // 策略给出的值
  next: string
}

export interface PolicyFormFill {
  // 实际写进表单的字段（目前只有 templateId → template）
  values: Record<string, string>
  // 与表单已有值相同、无需改动的字段
  unchanged: string[]
  // 用户已填且与策略不同：不覆盖，交给 UI 提示
  conflicts: PolicyConflict[]
}

// 把 apply 的 merged config 填进「建任务表单」的默认值。
//
// 规则（任务契约）：只填用户没填的字段，用户已填的一律不覆盖；
// 值不同才记冲突，值相同算 unchanged（避免每次选策略都弹 warning）。
// 目前建任务表单里能承接策略的字段只有扫描模板（templateId → template），
// 其余参数（插件/并发/速率/超时/字典/options）没有对应表单字段，只做展示。
export const applyPolicyToTaskForm = (
  form: { template?: string | null },
  merged?: PolicyConfigLike | null
): PolicyFormFill => {
  const fill: PolicyFormFill = { values: {}, unchanged: [], conflicts: [] }
  const policyTemplate = (merged?.templateId ?? '').trim()
  if (!policyTemplate) return fill
  const current = (form.template ?? '').trim()
  if (!current) {
    fill.values.template = policyTemplate
    return fill
  }
  if (current === policyTemplate) {
    fill.unchanged.push('template')
    return fill
  }
  fill.conflicts.push({ field: 'template', current, next: policyTemplate })
  return fill
}

// FOFA 查询语句 → 默认任务名：把换行/多余空白压成 '-'，并按后端口径截断到 120 字符。
// 只做「建议值」，用户可以自己改。
export const suggestFofaTaskName = (query?: string | null): string => {
  const base = (query ?? '')
    .replace(/[\r\n]+/g, ' ')
    .replace(/\s+/g, '-')
    .replace(/^-+|-+$/g, '')
  const runes = Array.from(base)
  if (runes.length <= FOFA_MAX_TASK_NAME_LENGTH) return base
  return runes.slice(0, FOFA_MAX_TASK_NAME_LENGTH).join('')
}

// 预览页把 host 列表拼成可复制的文本（一行一个）
export const hostsToText = (hosts?: string[] | null): string => (hosts ?? []).join('\n')
