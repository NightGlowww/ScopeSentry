// 资产列表页（文件泄露 / NPoC 服务）共用的展示与请求参数规则：
// 分页上限、关键字清洗、字节可读化、协议名归一、响应码过滤值。
// 都是纯函数，单独测——不放在页面里，是为了两个页面用同一套口径。
export const ASSET_PAGE_SIZE_MAX = 200

// 表格默认每页条数，与 useTable 的默认值保持一致。
const DEFAULT_PAGE_SIZE = 20

// clampPageSize：把页大小夹到 [1, ASSET_PAGE_SIZE_MAX]。
// 后端 pageSize 上限 200，超出的值会被拒或静默回落；前端先夹住，
// 用户选了 200 就真的拿到 200 条，而不是悄悄变成 20。
// 非法值（NaN/负数/未定义）回落到表格默认页大小，不把脏值发给后端。
export const clampPageSize = (
  size?: number | null,
  fallback: number = DEFAULT_PAGE_SIZE
): number => {
  if (typeof size !== 'number' || !Number.isFinite(size)) return fallback
  const value = Math.trunc(size)
  if (value < 1) return 1
  return value > ASSET_PAGE_SIZE_MAX ? ASSET_PAGE_SIZE_MAX : value
}

// normalizeKeyword：空的/纯空白的关键字直接省略，
// 别让请求里带一个空串——后端会当成一次无意义的子串匹配，日志里也看不出筛选有没有设过。
export const normalizeKeyword = (value?: string | null): string | undefined => {
  const text = (value ?? '').trim()
  return text ? text : undefined
}

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

// 一位小数，整数不补 ".0"：1 KB 比 1.0 KB 好读，1.5 MB 保留一位足够。
const oneDecimal = (value: number): string => {
  const rounded = Math.round(value * 10) / 10
  return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1)
}

// humanBytes：contentLength 是字节数，直接显示 182344 这种数字读不出量级。
// 1024 进制（与浏览器/nginx 日志习惯一致）。
// 0 显示成 "0 B"（真的空响应），null/负数/非数字显示成 "-"（没有长度），
// 两者必须分开——否则空响应会看起来像"未知"，或者未知长度会看起来像空文件。
export const humanBytes = (value?: number | string | null): string => {
  const parsed = typeof value === 'string' ? Number(value.trim()) : value
  if (typeof parsed !== 'number' || !Number.isFinite(parsed) || parsed < 0) return '-'
  if (parsed < 1024) return `${Math.trunc(parsed)} B`
  let size = parsed / 1024
  let unit = 1
  // 四舍五入后正好进位到 1024 时继续升一档，避免出现 "1024 KB" 这种非规范写法
  while (size >= 1023.95 && unit < BYTE_UNITS.length - 1) {
    size /= 1024
    unit += 1
  }
  return `${oneDecimal(size)} ${BYTE_UNITS[unit]}`
}

// normalizeScheme：协议过滤统一成小写去空白。
// 扫描端偶尔把协议写成 "HTTP" 或带空格，后端按精确值匹配，
// 原样发过去会查不到数据，所以前端先归一。
// 不做格式校验：未知协议本来就应该返回空列表，静默丢掉筛选条件反而更误导。
export const normalizeScheme = (value?: string | null): string | undefined => {
  const text = (value ?? '').trim().toLowerCase()
  return text ? text : undefined
}

// statusCodeParam：把状态码输入框的值转成接口要的精确码。
// 只接受 100-599 的三位码；空、越界、非数字都返回 undefined（= 不按状态码过滤）。
// 分档（2xx/4xx）只在单元格着色时用，后端 statusCode 是数值过滤，不认档位字符串。
export const statusCodeParam = (value?: string | number | null): number | undefined => {
  const parsed = typeof value === 'number' ? value : Number((value ?? '').trim())
  if (!Number.isFinite(parsed)) return undefined
  const status = Math.trunc(parsed)
  return status >= 100 && status <= 599 ? status : undefined
}

// portText：端口列的老数据可能是字符串、也可能为空；0 不是合法端口，同样显示成 "-"。
export const portText = (value?: number | string | null): string => {
  const text = typeof value === 'number' ? String(value) : (value ?? '').trim()
  return text && text !== '0' ? text : '-'
}
