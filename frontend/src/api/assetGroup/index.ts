import request from '@/axios'
import { resolveGroupIdForWrite } from './groupId'
import {
  AssetGroupDeleteForm,
  AssetGroupEditForm,
  AssetGroupForm,
  AssetGroupListResult,
  AssetScopeForm,
  AssetScopeListParams,
  AssetScopeListResult,
  AssetScopeUpdateForm
} from './types'

export { UNASSIGNED_GROUP_ID, resolveGroupIdForWrite } from './groupId'

export const getAssetGroupListApi = (): Promise<IResponse<AssetGroupListResult>> =>
  request.post({ url: '/api/assets/asset_group/list' })

export const addAssetGroupApi = (form: AssetGroupForm): Promise<IResponse<{ id: string }>> =>
  request.post({ url: '/api/assets/asset_group/add', data: form })

export const editAssetGroupApi = (form: AssetGroupEditForm): Promise<IResponse<null>> =>
  request.post({ url: '/api/assets/asset_group/edit', data: form })

export const deleteAssetGroupApi = (form: AssetGroupDeleteForm): Promise<IResponse<null>> =>
  request.post({ url: '/api/assets/asset_group/delete', data: form })

export const reorderAssetGroupApi = (orderIds: string[]): Promise<IResponse<null>> =>
  request.post({ url: '/api/assets/asset_group/reorder', data: { orderIds } })

export const getAssetScopeListApi = (
  params: AssetScopeListParams
): Promise<IResponse<AssetScopeListResult>> =>
  request.post({ url: '/api/assets/asset_scope/list', data: params })

export const addAssetScopeApi = (form: AssetScopeForm): Promise<IResponse<{ id: string }>> =>
  request.post({
    url: '/api/assets/asset_scope/add',
    data: { ...form, groupId: resolveGroupIdForWrite(form.groupId) }
  })

export const updateAssetScopeApi = (form: AssetScopeUpdateForm): Promise<IResponse<null>> =>
  request.post({
    url: '/api/assets/asset_scope/update',
    data: { ...form, groupId: resolveGroupIdForWrite(form.groupId) }
  })

export const deleteAssetScopeApi = (ids: string[]): Promise<IResponse<null>> =>
  request.post({ url: '/api/assets/asset_scope/delete', data: { ids } })

export const moveAssetScopeGroupApi = (ids: string[], groupId?: string): Promise<IResponse<null>> =>
  request.post({
    url: '/api/assets/asset_scope/batch_move_group',
    data: { ids, groupId: resolveGroupIdForWrite(groupId) }
  })

export const bindAssetScopeEnterpriseApi = (
  id: string,
  enterprise: string
): Promise<IResponse<null>> =>
  request.post({ url: '/api/assets/asset_scope/bind_enterprise', data: { id, enterprise } })

export const clearAssetScopeUpdateErrorApi = (ids: string[]): Promise<IResponse<null>> =>
  request.post({ url: '/api/assets/asset_scope/clear_update_error', data: { ids } })
