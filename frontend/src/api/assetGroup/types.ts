export type AssetGroup = {
  id: string
  name: string
  description: string
  sortOrder: number
  // scopeCount 由列表接口填充：该集团下地契数量
  scopeCount: number
  createdAt?: string
  updatedAt?: string
}

export type AssetGroupListResult = {
  items: AssetGroup[]
  total: number
}

// scopeType 由后端按资产范围推断，也可显式指定为 domain / ip / mixed；
// 前端留一个空串表示「自动识别」，把推断交给后端，避免两套解析逻辑。
export type AssetScopeType = 'domain' | 'ip' | 'mixed'

export type AssetScope = {
  id: string
  name: string
  scope: string
  scopeType: AssetScopeType | ''
  groupId: string
  scopeArray: string[]
  domainArray: string[]
  ipArray: string[]
  enterprise: string
  updateError: string
  icpUpdatedAt?: string
  createdAt: string
  updatedAt?: string
}

export type AssetScopeListParams = {
  // 「未分组」传 unassigned，普通集团传集团 id
  groupId?: string
  search?: string
  pageIndex?: number
  pageSize?: number
}

export type AssetScopeListResult = {
  list: AssetScope[]
  total: number
}

export type AssetGroupForm = {
  name: string
  description?: string
  sortOrder?: number
}

export type AssetGroupEditForm = AssetGroupForm & {
  id: string
}

export type AssetGroupDeleteForm = {
  id: string
  action: 'dissolve' | 'transfer'
  targetGroupId?: string
}

export type AssetScopeForm = {
  name: string
  scope: string
  scopeType?: AssetScopeType | ''
  groupId?: string
}

export type AssetScopeUpdateForm = AssetScopeForm & {
  id: string
}
