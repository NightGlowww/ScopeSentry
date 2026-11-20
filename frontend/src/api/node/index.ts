import request from '@/axios'
import { NodeData, nodeRespData, nodeLogRespData, pluginInfoData } from './types'
import { commonRespData } from '../scommon/types'

interface NodeDataResponse {
  list: NodeData[]
}

interface NodeDataOnlineResponse {
  list: string[]
}

export const getNodeDataApi = (): Promise<IResponse<NodeDataResponse>> => {
  return request.get({ url: '/api/node' })
}

export const restartNodeApi = (name: string): Promise<IResponse<commonRespData>> => {
  return request.post({
    url: '/api/node/restart',
    data: {
      name
    }
  })
}

export const getNodeDataOnlineApi = (): Promise<IResponse<NodeDataOnlineResponse>> => {
  return request.get({ url: '/api/node/online' })
}

export const updateNodeConfigDataApi = (
  oldName: string,
  name: string,
  ModulesConfig: string,
  state: boolean
): Promise<IResponse<NodeDataResponse>> => {
  return request.post({
    url: '/api/node/config/update',
    data: {
      oldName,
      name,
      ModulesConfig,
      state
    }
  })
}

export const deleteNodeApi = (names: string[]): Promise<IResponse<nodeRespData>> => {
  return request.post({ url: '/api/node/delete', data: { names } })
}

export const getNodeLogApi = (name: string): Promise<IResponse<nodeLogRespData>> => {
  return request.post({ url: '/api/node/log', data: { name } })
}

interface pluginInfoDataresp {
  list: pluginInfoData[]
}
export const getPluginInfoApi = (name: string): Promise<IResponse<pluginInfoDataresp>> => {
  return request.post({ url: '/api/node/plugin', data: { name } })
}

// 清空节点待领取队列
export const clearNodeQueueApi = (name: string): Promise<IResponse<any>> =>
  request.post({ url: '/api/node/queue/clear', data: { name } })

// 把待开始任务从积压节点迁到空闲节点
export const rebalanceNodeQueueApi = (from: string, to: string, limit = 10): Promise<IResponse<any>> =>
  request.post({ url: '/api/node/queue/rebalance', data: { from, to, limit } })

// 每个节点最近一小时的内存峰值/均值
export const getMemPeaksApi = (): Promise<IResponse<Record<string, { max: number; avg: number }>>> =>
  request.post({ url: '/api/node/mem-peaks', data: {} })
