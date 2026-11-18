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
  return request.post({ url: '/api/backup/create', data: { note } })
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
  return request.post({ url: '/api/backup/restore', data: { name, confirm } })
}

export const backupDownloadUrl = (name: string): string =>
  '/api/backup/download/' + encodeURIComponent(name)
