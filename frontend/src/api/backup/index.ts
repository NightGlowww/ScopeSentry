import request from '@/axios'

export interface BackupInfo {
  name: string
  size: number
  createdAt: string
  sha256: string
  note: string
}

export interface BackupStatus {
  running: boolean
  current: string
  progress: number
  lastError: string
}

export const listBackupsApi = (): Promise<IResponse<BackupInfo[] | { list: BackupInfo[] }>> => {
  return request.get({ url: '/api/backup/list' })
}

export const createBackupApi = (note: string): Promise<IResponse<unknown>> => {
  // 备份是分钟级操作：默认 30s 超时会让前端先报错（服务端虽然还在跑，但用户看到的是失败）
  return request.post({ url: '/api/backup/create', data: { note }, timeout: 1800000 })
}

export const getBackupStatusApi = (): Promise<IResponse<BackupStatus>> => {
  return request.get({ url: '/api/backup/status' })
}

export const deleteBackupApi = (name: string): Promise<IResponse<unknown>> => {
  return request.delete({ url: '/api/backup/' + encodeURIComponent(name) })
}

export const restoreBackupApi = (
  name: string,
  confirm: string
): Promise<IResponse<unknown>> => {
  return request.post({ url: '/api/backup/restore', data: { name, confirm }, timeout: 1800000 })
}

export const backupDownloadUrl = (name: string): string =>
  '/api/backup/download/' + encodeURIComponent(name)
