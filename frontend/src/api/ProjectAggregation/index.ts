// 项目详情页（ProjectDetail.vue）4 个 Tab 的接口封装。
// 后端实现：internal/api/routes/project_aggregation/project_aggregation.go
// 字段与后端 internal/models/project_aggregation.go 一一对应。
import request from '@/axios'

// 项目基础信息（Overview 左上卡片）
export interface ProjectInfoResponse {
  id: string
  name: string
  tag: string
  scheduledTasks: boolean
  hour: number
  // 后端字段名保持历史拼写 AssetCount（大写 A）
  AssetCount: number
  root_domains: string[]
  next_time: string
}

// 资产数量汇总（Overview 底部三个数字）
export interface ProjectAssetCountResponse {
  assetCount: number
  subdomainCount: number
  vulCount: number
  urlCount: number
  sensitiveCount: number
  dirScanCount: number
}

// 漏洞等级分布（Overview 饼图，后端直接返回 _id/count）
export interface ProjectVulLevelItem {
  _id: string
  count: number
}

// 漏洞列表行
export interface ProjectVulData {
  id: string
  url: string
  vulname: string
  level: string
  time: string
  matched: string
}

// 列表接口统一响应：total 用于分页
export interface AggregationListResponse<T> {
  list: T[]
  total: number
  pageIndex?: number
  pageSize?: number
}

// 分页 + 可选时间范围（后端 pageSize 上限 200，超过返回 400）
export interface AggregationPageQuery {
  pageIndex?: number
  pageSize?: number
  startTime?: string
  endTime?: string
}

export const getProjectInfoApi = (
  id: string
): Promise<IResponse<ProjectInfoResponse>> => {
  return request.post({ url: '/api/project_aggregation/project/info', data: { id } })
}

export const getProjectAssetCountApi = (
  id: string
): Promise<IResponse<ProjectAssetCountResponse>> => {
  return request.post({ url: '/api/project_aggregation/project/asset/count', data: { id } })
}

export const getProjectVulLevelCountApi = (
  id: string,
  page: AggregationPageQuery = {}
): Promise<IResponse<ProjectVulLevelItem[]>> => {
  return request.post({
    url: '/api/project_aggregation/project/vul/statistics',
    data: { id, ...page }
  })
}

export const getProjectVulDataApi = (
  id: string,
  page: AggregationPageQuery = {}
): Promise<IResponse<AggregationListResponse<ProjectVulData>>> => {
  return request.post({
    url: '/api/project_aggregation/project/vul/data',
    data: { id, ...page }
  })
}

// 子域名行：value / ip 恒为数组（后端把 null 归一成 []），前端可直接 forEach
export interface ProjectSubdomainData {
  id: string
  host: string
  type: string
  value: string[]
  ip: string[]
  rootDomain: string
  time: string
}

// 端口聚合行：count = 该端口资产数，host/ip/service 是该端口最近一条资产的样例
export interface ProjectPortData {
  id: string
  port: string
  host: string
  ip: string
  service: string
  count: number
  time: string
}

// 服务聚合行：count = 该服务资产数，host/ip/port 是该服务最近一条资产的样例
export interface ProjectServiceData {
  id: string
  service: string
  host: string
  ip: string
  port: string
  count: number
  time: string
}

export const getProjectSubdomainDataApi = (
  search: string,
  filter: Record<string, any>,
  fq: Record<string, any>,
  page: AggregationPageQuery = {}
): Promise<IResponse<AggregationListResponse<ProjectSubdomainData>>> => {
  return request.post({
    url: '/api/project_aggregation/project/subdomain/data',
    data: { search, filter, fq, ...page }
  })
}

export const getProjectPortDataApi = (
  search: string,
  filter: Record<string, any>,
  fq: Record<string, any>,
  page: AggregationPageQuery = {}
): Promise<IResponse<AggregationListResponse<ProjectPortData>>> => {
  return request.post({
    url: '/api/project_aggregation/project/port/data',
    data: { search, filter, fq, ...page }
  })
}

export const getProjectServiceDataApi = (
  search: string,
  filter: Record<string, any>,
  fq: Record<string, any>,
  page: AggregationPageQuery = {}
): Promise<IResponse<AggregationListResponse<ProjectServiceData>>> => {
  return request.post({
    url: '/api/project_aggregation/project/service/data',
    data: { search, filter, fq, ...page }
  })
}
