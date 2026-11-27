import request from '@/axios'
import type {
  TaskData,
  taskRespData,
  TaskDetail,
  TaskProgessInfo,
  ScheduledTaskData,

  TemplateDetail
} from './types'
import type { commonRespData } from '../scommon/types'

interface TaskDataResponse {
  list: TaskData[]
  total: number
}
export const getTaskDataApi = (
  search: string,
  pageIndex: number,
  pageSize: number,
  status?: number,
  node?: string,
  bindProject?: string,
  sort?: string
): Promise<IResponse<TaskDataResponse>> => {
  // bindProject：项目详情页用它列出「为这个项目跑过的任务」。
  // 注意与 task.project（资产归属数组）不同，bindProject 才是任务→项目的归属。
  // sort："<字段>:<asc|desc>"，服务端白名单校验。
  return request.post({
    url: '/api/task/',
    data: { search, pageIndex, pageSize, status, node, bindProject, sort }
  })
}

// 项目优先接入：往项目里加一批目标并建任务（项目不存在则由服务端按名字新建）。
// 这是 HuntDeck 之外的人也能用的入口——界面上不再需要「先建任务再同步到项目」。
export const ingestToProjectApi = (payload: {
  projectId?: string
  projectName?: string
  tag?: string
  targets: string
  template?: string
  node?: string[]
  autoStart?: boolean
}): Promise<IResponse<any>> => {
  return request.post({ url: '/api/task/ingest', data: payload })
}

// 任务归属补录：把已有任务挂到项目下（projectId 传空 = 解除归属）
export const assignTaskProjectApi = (ids: string[], projectId: string) => {
  return request.post({ url: '/api/task/assignProject', data: { ids, projectId } })
}

export const stopTaskApi = (ids: string[]): Promise<IResponse<TaskDataResponse>> => {
  return request.post({ url: '/api/task/stop', data: { ids } })
}

export const starTaskApi = (ids: string[]): Promise<IResponse<TaskDataResponse>> => {
  return request.post({ url: '/api/task/start', data: { ids } })
}

export const addTaskApi = (
  name: string,
  target: string,
  ignore: string,
  node: string[],
  allNode: boolean,
  duplicates: string,
  scheduledTasks: boolean,
  hour: number,
  template: string,
  targetTp: string,
  search: string,
  filter: Record<string, any>,
  targetNumber: number,
  targetIds: string[],
  project: string[],
  targetSource: string,
  day: number,
  minute: number,
  week: number,
  bindProject: string | null,
  cycleType: string,
  // 扫描策略：建任务时选中的策略 id（可空 = 不套用策略）。
  // 后端把它存进 task.policyId，建扫描载荷时按策略的插件清单收窄模板（docs/audit/SCAN_POLICY.md）。
  policyId?: string
): Promise<IResponse<taskRespData>> => {
  return request.post({
    url: '/api/task/add',
    data: {
      name,
      target,
      ignore,
      node,
      allNode,
      duplicates,
      scheduledTasks,
      hour,
      template,
      targetTp,
      search,
      filter,
      targetNumber,
      targetIds,
      project,
      targetSource,
      day,
      minute,
      week,
      bindProject,
      cycleType,
      policyId: policyId || undefined
    }
  })
}

export const addScheduledTaskApi = (
  name: string,
  target: string,
  ignore: string,
  node: string[],
  allNode: boolean,
  duplicates: string,
  scheduledTasks: boolean,
  hour: number,
  template: string,
  targetTp: string,
  search: string,
  filter: Record<string, any>,
  targetNumber: number,
  targetIds: string[],
  project: string[],
  targetSource: string,
  day: number,
  minute: number,
  week: number,
  bindProject: string | null,
  cycleType: string,
  // 计划任务同样存 policyId（ScheduledTasks 里存的就是 models.Task，定时轮次会原样回读）
  policyId?: string
): Promise<IResponse<taskRespData>> => {
  return request.post({
    url: '/api/task/scheduled/add',
    data: {
      name,
      target,
      ignore,
      node,
      allNode,
      duplicates,
      scheduledTasks,
      hour,
      template,
      targetTp,
      search,
      filter,
      targetNumber,
      targetIds,
      project,
      targetSource,
      day,
      minute,
      week,
      bindProject,
      cycleType,
      policyId: policyId || undefined
    }
  })
}

export const updateScheduleApi = (
  id: string,
  name: string,
  target: string,
  ignore: string,
  node: string[],
  allNode: boolean,
  duplicates: string,
  scheduledTasks: boolean,
  hour: number,
  template: string,
  targetTp: string,
  search: string,
  filter: Record<string, any>,
  targetNumber: number,
  targetIds: string[],
  project: string[],
  targetSource: string,
  day: number,
  minute: number,
  week: number,
  bindProject: string | null,
  cycleType: string
): Promise<IResponse<taskRespData>> => {
  return request.post({
    url: '/api/task/scheduled/update',
    data: {
      id,
      name,
      target,
      ignore,
      node,
      allNode,
      duplicates,
      scheduledTasks,
      hour,
      template,
      targetTp,
      search,
      filter,
      targetNumber,
      targetIds,
      project,
      targetSource,
      day,
      minute,
      week,
      bindProject,
      cycleType
    }
  })
}

export const getTaskDetailApi = (id: string): Promise<IResponse<TaskDetail>> => {
  return request.post({ url: '/api/task/detail', data: { id } })
}

export const getScheduleDetailApi = (id: string): Promise<IResponse<TaskDetail>> => {
  return request.post({ url: '/api/task/scheduled/detail', data: { id } })
}

export const deleteTaskApi = (ids: string[], delA: boolean): Promise<IResponse<commonRespData>> => {
  return request.post({ url: '/api/task/delete', data: { ids, delA } })
}

export const retestTaskApi = (id: string): Promise<IResponse<commonRespData>> => {
  return request.post({ url: '/api/task/retest', data: { id } })
}

interface TaskProgessInforesp {
  list: TaskProgessInfo[]
  total: number
}
// 目标级暂停/恢复（issue #277）
export const pauseTaskTargetApi = (taskId: string, target: string): Promise<IResponse<null>> => {
  return request.post({ url: '/api/task/target/pause', data: { taskId, target } })
}

export const resumeTaskTargetApi = (taskId: string, target: string): Promise<IResponse<null>> => {
  return request.post({ url: '/api/task/target/resume', data: { taskId, target } })
}

export const getTaskProgressApi = (
  id: string,
  pageIndex: number,
  pageSize: number
): Promise<IResponse<TaskProgessInforesp>> => {
  return request.post({ url: '/api/task/progress/info', data: { id, pageIndex, pageSize } })
}

interface ScheduledTaskDataResponse {
  list: ScheduledTaskData[]
  total: number
}

export const getScheduledTaskDataApi = (
  search: string,
  pageIndex: number,
  pageSize: number
): Promise<IResponse<ScheduledTaskDataResponse>> => {
  return request.post({ url: '/api/task/scheduled', data: { search, pageIndex, pageSize } })
}

export const taskRunApi = (id: string): Promise<IResponse<commonRespData>> => {
  return request.post({ url: '/api/task/scheduled/run', data: { id } })
}

export const scheduledDeleteTaskApi = (ids: string[]): Promise<IResponse<commonRespData>> => {
  return request.post({ url: '/api/task/scheduled/delete', data: { ids } })
}

export const getScheduledTaskPageMonitDataApi = (
  search: string,
  pageIndex: number,
  pageSize: number
): Promise<IResponse<ScheduledTaskDataResponse>> => {
  return request.post({
    url: '/api/task/scheduled/pagemonit/data',
    data: { search, pageIndex, pageSize }
  })
}

export const deleteScheduledTaskPageMonitApi = (
  ids: string[]
): Promise<IResponse<commonRespData>> => {
  return request.post({ url: '/api/task/scheduled/pagemonit/delete', data: { ids } })
}

export const updateScheduledTaskPageMonitApi = (
  hour: number,
  node: string[],
  allNode: boolean,
  scheduledTasks: boolean
): Promise<IResponse<taskRespData>> => {
  return request.post({
    url: '/api/task/scheduled/pagemonit/update',
    data: {
      hour,
      node,
      allNode,
      scheduledTasks
    }
  })
}

export const addScheduledTaskPageMonitApi = (url: string): Promise<IResponse<taskRespData>> => {
  return request.post({
    url: '/api/task/scheduled/pagemonit/add',
    data: {
      url
    }
  })
}

interface TaskDataResponse {
  list: TaskData[]
  total: number
}
export const getTemplateDataApi = (
  search: string,
  pageIndex: number,
  pageSize: number
): Promise<IResponse<TaskDataResponse>> => {
  return request.post({ url: '/api/task/template', data: { search, pageIndex, pageSize } })
}

export const getTemplateDetailApi = (id: string): Promise<IResponse<TemplateDetail>> => {
  return request.post({ url: '/api/task/template/detail', data: { id } })
}

export const saveTemplateDetailApi = (
  result: Record<string, any>,
  id: string
): Promise<IResponse<TemplateDetail>> => {
  return request.post({ url: '/api/task/template/save', data: { result, id } })
}

export const deleteTemplateDetailApi = (ids: string[]): Promise<IResponse<commonRespData>> => {
  return request.post({ url: '/api/task/template/delete', data: { ids } })
}

export const syancProjectApi = (
  ids: string[],
  option: string,
  project: string,
  tag: string,
  name: string
): Promise<IResponse<commonRespData>> => {
  return request.post({ url: '/api/task/sync', data: { ids, option, project, tag, name } })
}

interface TaskNameData {
  id: string
  name: string
}

export const getTaskNamesApi = (): Promise<IResponse<TaskNameData[]>> => {
  return request.get({ url: '/api/task/names' })
}

// 按当前筛选批量删除任务（必须带 search 或 status）
export const deleteTaskByFilterApi = (
  search: string,
  status?: number
): Promise<IResponse<any>> =>
  request.post({ url: '/api/task/deleteByFilter', data: { search, pageIndex: 1, pageSize: 10, status } })

// 各道派发闸门的拒绝次数（进程内累计，重启归零）
export const getDispatchRejectStatsApi = (): Promise<IResponse<Record<string, number>>> =>
  request.post({ url: '/api/task/dispatch-stats', data: {} })

// 最近 N 条派发拒绝记录（持久化，跨重启可查）
export const getDispatchRecentApi = (limit = 5): Promise<IResponse<any[]>> =>
  request.post({ url: '/api/task/dispatch-recent?limit=' + limit, data: {} })
