// HTTP 状态码分档：页面把「档位筛选」翻译成接口的 status 参数，并给表格里的状态码上色。
// 接口接受分档（2xx/3xx/4xx/5xx）与精确码（如 404）两种写法。
export type HttpStatusBand = '2xx' | '3xx' | '4xx' | '5xx'

export const HTTP_STATUS_BANDS: HttpStatusBand[] = ['2xx', '3xx', '4xx', '5xx']

export type HttpStatusTagType = 'success' | 'info' | 'warning' | 'danger' | 'primary'

// 精确码：三位数字，1xx-5xx 都收（扫描端偶尔会留下 1xx 的中间响应）
const EXACT_STATUS_CODE = /^[1-5]\d{2}$/

// httpStatusBand 把状态码落到档位；缺失、0、越界返回 null。
// 数值型与字符串型都接受：老数据里 statuscode 存成字符串的情况不少。
export const httpStatusBand = (code?: number | string | null): HttpStatusBand | null => {
  if (code === undefined || code === null || code === '') return null
  const value = typeof code === 'string' ? Number(code.trim()) : code
  if (typeof value !== 'number' || !Number.isFinite(value)) return null
  const status = Math.trunc(value)
  if (status < 200 || status > 599) return null
  return `${Math.floor(status / 100)}xx` as HttpStatusBand
}

// 颜色对齐 ARL-Next：2xx success、3xx info、4xx warning、5xx danger；未知状态码用中性色。
export const httpStatusTagType = (code?: number | string | null): HttpStatusTagType => {
  switch (httpStatusBand(code)) {
    case '2xx':
      return 'success'
    case '3xx':
      return 'info'
    case '4xx':
      return 'warning'
    case '5xx':
      return 'danger'
    default:
      return 'primary'
  }
}

// normalizeStatusFilter：精确码优先于档位；非法输入返回 undefined，不把脏值发给后端
// （后端对未知 status 返回空列表，用户会以为是没有数据）。
export const normalizeStatusFilter = (
  band?: string | null,
  exactCode?: string | null
): string | undefined => {
  const code = (exactCode ?? '').trim()
  if (EXACT_STATUS_CODE.test(code)) return code
  const value = (band ?? '').trim().toLowerCase()
  return (HTTP_STATUS_BANDS as string[]).includes(value) ? value : undefined
}

// safeHttpUrl：历史资产的 url 可能没有 scheme（只存了 host:port/path），
// 只有明确的 http(s) 才交给 <a>，否则浏览器会把它拼成当前站点的相对路径。
export const safeHttpUrl = (value?: string | null): string | undefined => {
  const text = (value ?? '').trim()
  return /^https?:\/\//i.test(text) ? text : undefined
}
