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

export interface DashboardStats {
  /** 所有候选记录，包含已确认与已忽略。 */
  vulnerability: number
  vulnPending: number
  vulnIgnored: number
  vulnConfirmed: number
}

export const getDashboardStatsApi = (): Promise<IResponse<DashboardStats>> =>
  request.post({ url: '/api/dashboard/stats', data: {} })
