import request from '@/axios'
import type {
  ScanPolicyApplyResult,
  ScanPolicyCreateForm,
  ScanPolicyDeleteResult,
  ScanPolicyDetail,
  ScanPolicyListQuery,
  ScanPolicyListResult,
  ScanPolicyTaskTemplate,
  ScanPolicyUpdateForm,
  ScanPolicyValidation,
  ScanPolicyConfig
} from './types'

// 扫描策略：列表（pageSize ≤ 200）/ 增删改查 / 校验 / 合并预览。
// 后端 handler 对 pageSize>200 直接 400，这里的分页控件不要再给更大的档位。

// 列表：enabled 不传即不过滤；空 body 等价默认分页。
export const getPolicyListApi = (
  payload: ScanPolicyListQuery
): Promise<IResponse<ScanPolicyListResult>> => {
  return request.post({ url: '/api/policy/list', data: payload })
}

// 新建：name 唯一，config 里 rateLimit=0 表示不限速（不是「未设置」）。
export const createPolicyApi = (
  payload: ScanPolicyCreateForm
): Promise<IResponse<ScanPolicyDetail>> => {
  return request.post({ url: '/api/policy/create', data: payload })
}

// 更新：config 传了就整体替换，其余字段不传即不改。
export const updatePolicyApi = (
  payload: ScanPolicyUpdateForm
): Promise<IResponse<ScanPolicyDetail>> => {
  return request.post({ url: '/api/policy/update', data: payload })
}

// 删除：后端接收 id 或 ids，统一按 ids 传，便于批量扩展。
export const deletePolicyApi = (ids: string[]): Promise<IResponse<ScanPolicyDeleteResult>> => {
  return request.post({ url: '/api/policy/delete', data: { ids } })
}

// 详情：附带 usage 说明（任务表没有 policyId，引用计数不可用）。
export const getPolicyDetailApi = (id: string): Promise<IResponse<ScanPolicyDetail>> => {
  return request.post({ url: '/api/policy/detail', data: { id } })
}

// 合并预览：策略覆盖任务模板默认值，返回 merged config + warnings，不写库。
// taskTemplate 传模板 ID 字符串时由服务端读模板分阶段插件清单生成 baseline；
// 传对象时直接把对象当默认值（前端把当前表单原样带上来）。
export const applyPolicyApi = (
  policyId: string,
  taskTemplate: ScanPolicyTaskTemplate
): Promise<IResponse<ScanPolicyApplyResult>> => {
  return request.post({ url: '/api/policy/apply', data: { policyId, taskTemplate } })
}

// 参数校验：后端对不合法参数也回 200，前端按 valid/errors 渲染，不当作请求失败。
export const testPolicyApi = (
  config: ScanPolicyConfig
): Promise<IResponse<ScanPolicyValidation>> => {
  return request.post({ url: '/api/policy/test', data: { config } })
}
