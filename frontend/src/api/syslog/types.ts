// 系统日志（对标 ARL-Next syslog）：读进程内环形缓冲或配置的日志文件。
// level 与后端 zap 级别对齐；source 说明这一页数据是从哪里读出来的。
export type SysLogLevel = 'debug' | 'info' | 'warn' | 'error' | 'dpanic' | 'panic' | 'fatal'

export type SysLogSource = 'file' | 'memory' | 'none'

export interface SysLogEntry {
  time: string
  level: SysLogLevel | string
  // 调用位置（file:line），部分日志没有该字段
  caller?: string
  message: string
}

export interface SysLogListQuery {
  level?: string
  keyword?: string
  pageIndex?: number
  // 上限 200，后端 binding 会把更大的值拒成 400
  pageSize?: number
}

export interface SysLogListResult {
  list: SysLogEntry[]
  total: number
  page: number
  pageSize: number
  // file=读配置的日志文件，memory=进程内环形缓冲，none=两处都没读到
  source: SysLogSource
  // 文件尾部/缓冲区只保留了一部分时的提示位
  truncated: boolean
}
