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
  // 此刻在跑什么、多久没产出：队列不动的根因基本都在这几个字段里
  currentTaskId?: string
  currentTaskName?: string
  currentModule?: string
  currentTarget?: string
  currentSince?: string
  idleMinutes?: number
  stalled?: boolean
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
