// 服务端日志级别：/api/dashboard/logs 的 binding 允许
// debug/info/warn/error/dpanic/panic/fatal；页面只暴露前四档，
// 更严重的级别仍可能出现在历史日志文件里，统一按错误色显示。
export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

export const LOG_LEVELS: LogLevel[] = ['debug', 'info', 'warn', 'error']

export type LogLevelTagType = 'info' | 'primary' | 'success' | 'warning' | 'danger'

// normalizeLogLevel 统一大小写与空白：日志文件里常见 "WARN" / " warn "。
export const normalizeLogLevel = (value?: string | null): string =>
  (value ?? '').trim().toLowerCase()

// logLevelTagType：debug 用低饱和的中性色，info 用品牌色，warn 警告色，
// error 及以上（dpanic/panic/fatal）用危险色，未知级别保持中性。
export const logLevelTagType = (value?: string | null): LogLevelTagType => {
  switch (normalizeLogLevel(value)) {
    case 'debug':
      return 'info'
    case 'info':
      return 'primary'
    case 'warn':
    case 'warning':
      return 'warning'
    case 'error':
    case 'dpanic':
    case 'panic':
    case 'fatal':
      return 'danger'
    default:
      return 'info'
  }
}
