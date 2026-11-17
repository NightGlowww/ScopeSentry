// 证书读模型（对标 ARL-Next assetCert）。数据从 asset 的 TLS 字段现算，
// 不依赖回填集合；字段与后端 internal/models/cert.go 一一对应。

export interface CertHost {
  host: string
  ip: string
  port: string
}

export interface CertRow {
  // id 是证书聚合键：fp:<fingerprint> / sn:<issuer>|<serial> / sub:<subject>|...
  id: string
  subject: string
  subjectCN: string
  issuer: string
  issuerCN: string
  serial: string
  fingerprint: string
  fingerprintSha1: string
  fingerprintMd5: string
  notBefore: string | null
  notAfter: string | null
  expired: boolean
  san: string[]
  // hosts 只保留前 50 个；hostCount 是完整关联数
  hosts: CertHost[]
  hostCount: number
}

export interface CertListQuery {
  keyword?: string
  project?: string
  // true=只看已过期，false=只看未过期，不传=全部
  expired?: boolean
  pageIndex?: number
  pageSize?: number
}

export interface CertListResult {
  list: CertRow[]
  total: number
}

export interface CertDetailQuery {
  id: string
  project?: string
}
