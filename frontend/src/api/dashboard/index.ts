import request from '@/axios'

export interface ConsoleDependency {
  connected: boolean
  latencyMs: number
  error?: string
}

export interface HostStats {
  cpus: number
  load1: number
  diskFreeBytes: number
  diskTotalBytes: number
  diskFreePercent: number
  memAvailablePercent: number
}

export interface ConsoleInfo {
  hostStats?: HostStats
  hostWarnings?: string[]
  version: string
  startTime: string
  uptimeSeconds: number
  uptimeHuman: string
  timezone: string
  nodeCount: number
  nodeOnline: number
  taskCount: number
  taskRunning: number
  mongo: ConsoleDependency
  redis: ConsoleDependency
  collectedAt: string
}

export const getConsoleInfoApi = (): Promise<IResponse<ConsoleInfo>> =>
  request.get({ url: '/api/console/info' })

// 仪表盘统计（含漏洞处置分布）
export const getDashboardStatsApi = (): Promise<IResponse<any>> =>
  request.post({ url: '/api/dashboard/stats', data: {} })
