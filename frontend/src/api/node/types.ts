export type NodeData = {
  name: string
  running: number
  finished: number
  state: number
  cpuNum: number
  memNum: number
  updateTime: string
  maxTaskNum: string
  urlThread: string
  urlMaxNum: string
  // 容器侧真实压力（cgroup 限额）与节点待领取队列长度：
  // 宿主 memNum 看不出 cgroup 限制，队列长度决定该不该再往这个节点推任务
  containerMem?: number
  queueLen?: number
}

export type nodeRespData = {
  code: string
  message: string
}

export type nodeLogRespData = {
  code: string
  logs: string
}

export type pluginInfoData = {
  name: string
  install: number
  check: number
}
