import request from '@/axios'

export interface ConsoleDependency {
  connected: boolean
  latencyMs: number
  error?: string
}

export interface ConsoleInfo {
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
