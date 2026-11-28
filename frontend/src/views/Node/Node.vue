<script setup lang="tsx">
import { confirmDialog } from '@/utils/confirm'
import { agoText } from '@/utils/ago'
import {
  ElScrollbar,
  ElTag,
  ElTooltip,
  ElCheckbox,
  ElInput,
  ElMessage,
  ElMessageBox,
  ElDialog,
  ElSelect,
  ElOption,
  ElInputNumber,
  ElAlert,
} from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { ref, reactive, h, computed } from 'vue'

import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
// import { useIcon } from '@/hooks/web/useIcon'
import { onMounted } from 'vue'
import { Dialog } from '@/components/Dialog'
import { BaseButton } from '@/components/Button'
import Configuration from './components/Configuration.vue'
import plugin from './components/plugin.vue'
import {
  getNodeDataApi,
  deleteNodeApi,
  getNodeLogApi,
  restartNodeApi,
  clearNodeQueueApi,
  rebalanceNodeQueueApi,
  getMemPeaksApi
} from '@/api/node'
import { getDispatchRejectStatsApi, getDispatchRecentApi } from '@/api/task'
import { useVisibleInterval } from '@/hooks/web/useVisibleInterval'
import { useSingleTabLeader } from '@/hooks/web/useSingleTabLeader'
const { t } = useI18n()
// 离线原因推断：把「最后心跳多久前」翻译成可行动的判断
const offlineReason = (row: any, state: string): string => {
  if (state === '2') return ''
  const t0 = new Date(String(row?.updateTime || '').replace(/-/g, '/')).getTime()
  if (!isFinite(t0) || t0 <= 0) return ''
  const sec = Math.round((Date.now() - t0) / 1000)
  if (sec < 0) return ''
  const ago =
    sec < 60 ? sec + 's' : sec < 3600 ? Math.round(sec / 60) + 'm' : Math.round(sec / 3600) + 'h'
  const running = Number(row?.running) || 0
  if (state === '1') return t('node.heartbeatAgo', { ago })
  if (running > 0) return t('node.offlineBusy', { ago })
  if (sec < 180) return t('node.offlineJustNow', { ago })
  return t('node.offlineLong', { ago })
}

const nodeColums = reactive<TableColumn[]>([
  {
    field: 'selection',
    type: 'selection',
    width: 55
  },
  {
    field: 'name',
    label: t('node.nodeName'),
    minWidth: 180
  },
  {
    field: 'maxTaskNum',
    label: t('configuration.maxTaskNum'),
    width: 110,
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      return h(
        ElTag,
        {
          type: 'info'
        },
        () => cellValue
      )
    }
  },
  {
    field: 'running',
    label: t('node.taskCount'),
    width: 110,
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      return h(
        ElTag,
        {
          round: true,
          effect: 'plain',
          hit: true
        },
        () => cellValue
      )
    }
  },
  {
    field: 'finished',
    label: t('node.finished'),
    width: 110,
    formatter: (_: Recordable, __: TableColumn, cellValue: string) => {
      return h(
        ElTag,
        {
          round: true,
          effect: 'plain',
          hit: true
        },
        () => cellValue
      )
    }
  },
  {
    field: 'cpuNum',
    label: t('node.nodeUsageCpu'),
    width: 100,
    formatter: (_: Recordable, __: TableColumn, cellValue: string) => {
      let numericValue = parseFloat(cellValue)
      numericValue = parseFloat(numericValue.toFixed(2))
      return h(
        ElTag,
        {
          round: true,
          effect: 'plain',
          type: numericValue < 50 ? undefined : numericValue < 80 ? 'warning' : 'danger'
        },
        () => numericValue + '%'
      )
    }
  },
  {
    field: 'memNum',
    label: t('node.nodeUsageMemory'),
    width: 130,
    formatter: (row: Recordable, __: TableColumn, cellValue: string) => {
      const host = parseFloat(parseFloat(cellValue).toFixed(1))
      const ctr = Number(row.containerMem) || 0
      const tag = h(
        ElTag,
        {
          round: true,
          effect: 'plain',
          type: host < 50 ? undefined : host < 80 ? 'warning' : 'danger'
        },
        () => host + '%'
      )
      // 容器内存单独一行：宿主 24% 时容器可能已经 42%，被 cgroup 限住时只有它能看出压力
      if (ctr <= 0) return tag
      const peak = memPeaks.value[String(row.name)]?.max || 0
      // 连续高压时长：区分「刚冲高」和「已经饱和很久」
      const highMin = Number((memPeaks.value as any)[String(row.name)]?.highMinutes || 0)
      const highTag =
        highMin >= 15
          ? h(ElTag, { size: 'small', effect: 'dark', type: 'danger' }, () =>
              t('node.memHighFor', { n: highMin })
            )
          : null
      const peakTag =
        peak > 0
          ? h(ElTag, { size: 'small', effect: 'plain', type: peak >= 90 ? 'danger' : 'info' }, () =>
              t('node.memPeak1h', { n: peak.toFixed(1) })
            )
          : null
      const ctrTag = h(
        ElTag,
        {
          round: true,
          effect: 'plain',
          size: 'small',
          type: ctr < 70 ? 'info' : ctr < 90 ? 'warning' : 'danger'
        },
        () => t('node.containerMem', { n: ctr.toFixed(1) })
      )
      const soft = Number((row as any).memSoftLimitMB || 0)
      const title = soft > 0 ? t('node.memSoftLimit', { n: soft }) : ''
      return h('div', { class: 'node-mem-cell', title }, [
        tag,
        h('div', { class: 'node-mem-sub' }, [ctrTag]),
        peakTag,
        highTag
      ])
    }
  },
  {
    field: 'state',
    label: t('node.nodeStatus'),
    width: 150,
    formatter: (row: Recordable, __: TableColumn, cellValue: string) => {
      const tag = h(
        ElTag,
        {
          type: cellValue === '1' ? 'success' : cellValue === '2' ? 'warning' : 'danger',
          effect: 'light',
          hit: true
        },
        () =>
          cellValue === '1'
            ? t('node.statusRun')
            : cellValue === '2'
              ? t('node.statusStop')
              : t('node.statusError')
      )
      // 「离线」有两种完全不同的处理：容器挂了（要重启）vs 被压得没空上报（要减负载）。
      // 只给一个红标签看不出区别，所以补一行「最后心跳」与推断。
      const hint = row?.stale ? t('node.heartbeatDelayed') : offlineReason(row, cellValue)
      if (!hint) return tag
      return h('div', { class: 'node-state-cell' }, [
        tag,
        h('div', { class: 'node-state-hint' }, hint)
      ])
    }
  },
  {
    field: 'currentTaskName',
    label: t('node.currentTask'),
    minWidth: 230,
    formatter: (row: Recordable, __: TableColumn, cellValue: string) => {
      // 「在线 + 队列 6」看不出队列为什么不动：把此刻在跑的模块和「多久没产出」摆出来。
      // 没有这一列，只能登服务器翻 Redis 才知道节点卡在哪个模块上（2026-11-29）。
      if (!cellValue) {
        return h('div', { class: 'node-task-none', title: t('node.currentTaskHint') }, [
          t('node.currentTaskNone')
        ])
      }
      const idle = Number(row.idleMinutes) || 0
      const stalled = Boolean(row.stalled)
      const mod = String(row.currentModule || '')
      const tgt = String(row.currentTarget || '')
      const sub = mod ? t('node.currentTaskModule', { module: mod, target: tgt }) : ''
      return h('div', { class: 'node-task-cell', title: t('node.currentTaskHint') }, [
        h('div', { class: 'node-task-name' }, cellValue),
        sub ? h('div', { class: 'node-task-sub' }, sub) : null,
        idle >= 1
          ? h(
              ElTag,
              {
                size: 'small',
                effect: stalled ? 'dark' : 'plain',
                type: stalled ? 'danger' : idle >= 10 ? 'warning' : 'info'
              },
              () => t('node.currentTaskIdle', { n: idle })
            )
          : null
      ])
    }
  },
  {
    field: 'queueLen',
    label: t('node.queueLen'),
    width: 100,
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      // 队列积压说明有任务在排队（节点一次只跑一个），>5 用警告色提示
      const v = cellValue || 0
      return h(ElTag, { type: v > 5 ? 'danger' : v > 0 ? 'warning' : 'info' }, () => v)
    }
  },
  {
    field: 'cmdFail',
    label: t('node.cmdFail'),
    width: 120,
    formatter: (row: Recordable, __: TableColumn, cellValue: number) => {
      // 累计次数 + 最近 10 分钟增量：前者看历史，后者判断「刚跑这批可不可信」
      const v = cellValue || 0
      const recent = (row.cmdFail10m as number) || 0
      const tip = t('node.cmdFailTip', { total: v, recent })
      if (v === 0 && recent === 0) {
        return h(ElTooltip, { content: tip, placement: 'top' }, () =>
          h(ElTag, { type: 'info' }, () => '0')
        )
      }
      return h(ElTooltip, { content: tip, placement: 'top' }, () =>
        h('div', { style: 'display:flex;gap:4px;align-items:center' }, [
          h(ElTag, { type: v > 0 ? 'warning' : 'info' }, () => String(v)),
          recent > 0
            ? h(ElTag, { type: 'danger', size: 'small' }, () => '+' + String(recent))
            : null
        ])
      )
    }
  },
  {
    field: 'updateTime',
    label: t('node.updateTime'),
    // 时间戳需要约 175px，小于这个值右侧会被截断
    width: 180,
    showOverflowTooltip: true
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    /* 按钮实际 ~176px；不再固定，避免脱离文档流压住上一列 */
    width: 220,
    formatter: (row, __: TableColumn, _: number) => {
      console.log(row)
      return (
        <>
          <BaseButton type="warning" size="small" onClick={() => openPlugin(row.name)}>
            {t('node.plugin')}
          </BaseButton>
          <BaseButton type="success" size="small" onClick={() => openLogDialogVisible(row)}>
            {t('node.log')}
          </BaseButton>
          <BaseButton type="primary" size="small" onClick={() => openConfig(row)}>
            {t('common.config')}
          </BaseButton>
          <ElTooltip content={t('node.rebalanceMsg')}>
            <BaseButton type="warning" size="small" onClick={() => openRebalance(row)}>
              {t('node.rebalance')}
            </BaseButton>
          </ElTooltip>
          <ElTooltip content={t('node.clearQueueMsg')}>
            <BaseButton type="info" size="small" onClick={() => clearQueue(row.name)}>
              {t('node.clearQueue')}
            </BaseButton>
          </ElTooltip>
          <ElTooltip content={t('node.restartMsg')}>
            <BaseButton type="danger" size="small" onClick={() => restartNode(row.name)}>
              {t('node.restart')}
            </BaseButton>
          </ElTooltip>
        </>
      )
    }
  }
])
// 自愈建议：节点离线但容器还活着（有心跳记录/仍有任务在跑）时，
// 直接把「该做什么」摆出来——上一轮是人工判断才发现要「先迁任务再重启」。
const offlineNodes = computed(() =>
  (dataList.value || []).filter((n: any) => String(n.state) === '3')
)
const healedTargets = computed(() => {
  const online = (dataList.value || []).filter((n: any) => String(n.state) === '1')
  if (!online.length) return ''
  return [...online].sort(
    (a: any, b: any) => (Number(a.queueLen) || 0) - (Number(b.queueLen) || 0)
  )[0].name
})
// 卡住节点：心跳正常、currentTask 挂着，但已经超过巡检阈值没有产出。
// 这是「队列不动」最常见的原因，且没有任何按钮能自己好——必须重启节点。
const stalledNodes = computed(() => (dataList.value || []).filter((n: any) => Boolean(n.stalled)))
const restartStalled = async () => {
  const bad = stalledNodes.value[0]
  if (!bad) return
  try {
    await ElMessageBox.confirm(
      t('node.stalledConfirm', { name: bad.name }),
      t('common.delWarning'),
      { confirmButtonText: 'OK', cancelButtonText: t('common.cancel'), type: 'warning' }
    )
  } catch {
    return
  }
  await restartNode(bad.name)
  ElMessage.success(t('node.stalledSent', { name: bad.name }))
  getList()
}

// 节点健康一眼可见：只看「在线/离线」会漏掉「在线但工具一直在失败」。
// 用 10 分钟窗口——节点本来就按这个粒度上报 cmdFail10m。
// 降压中：节点因容器内存高把并发降下来了，但已经在跑的任务不会被打断，
// 所以会出现「并发上限 1、当前在跑 2」。这不是异常，界面上要说清楚（2026-11-20）。
// 最近一小时内存峰值（趋势）：瞬时值看不出「一直高」还是「偶尔冲高」
const memPeaks = ref<Record<string, { max: number; avg: number }>>({})
const loadMemPeaks = async () => {
  try {
    const res: any = await getMemPeaksApi()
    memPeaks.value = res?.data || {}
  } catch {
    memPeaks.value = {}
  }
}

// 派发拒绝统计：外部集成反复撞闸时一眼看出来（进程内累计，重启归零）
const rejectStats = ref<Record<string, number>>({})
const loadRejectStats = async () => {
  try {
    const res: any = await getDispatchRejectStatsApi()
    rejectStats.value = res?.data || {}
  } catch {
    rejectStats.value = {}
  }
}
// 最近拒绝记录（持久化）：点开看具体是哪个任务、哪个闸门、什么时间
const recentRejections = ref<any[]>([])
const showRecentRejections = ref(false)
const loadRecentRejections = async () => {
  try {
    const res: any = await getDispatchRecentApi(5)
    recentRejections.value = res?.data || []
  } catch {
    recentRejections.value = []
  }
}
const toggleRecentRejections = async () => {
  showRecentRejections.value = !showRecentRejections.value
  if (showRecentRejections.value) await loadRecentRejections()
}
// 服务端重启后进程内计数归零，但库里还有最近的拒绝记录——
// 只要任一非空就要显示入口，否则「重启后看不见有人撞闸」（2026-11-20）。
const hasRejectInfo = computed(
  () => rejectSummary.value.length > 0 || recentRejections.value.length > 0
)
// 进程内计数与库里的历史是两回事：重启后计数归零，但库里还躺着两天前的记录。
// 只说「最近有拒绝记录」会让人以为正在发生，所以带上离现在多久。
const latestRejectText = computed(() => {
  const latest = recentRejections.value?.[0]
  if (!latest?.at) return t('node.rejectRecentOnly')
  return t('node.rejectRecentAgo', { ago: agoText(latest.at) })
})
const gateLabel = (gate: string) => {
  const map: Record<string, string> = {
    prefix_quota: t('node.rejectPrefix'),
    queue_full: t('node.rejectQueue'),
    container_mem: t('node.rejectMem'),
    rate_limited: t('node.rejectRate'),
    name_exists: t('node.rejectName')
  }
  return map[gate] || gate
}

const rejectSummary = computed(() => {
  const s = rejectStats.value || {}
  const label: Record<string, string> = {
    prefix_quota: t('node.rejectPrefix'),
    queue_full: t('node.rejectQueue'),
    container_mem: t('node.rejectMem'),
    rate_limited: t('node.rejectRate'),
    name_exists: t('node.rejectName')
  }
  return Object.keys(label)
    .filter((k) => (s[k] || 0) > 0)
    .map((k) => label[k] + ' ' + s[k])
})

const throttledNodes = computed(() =>
  (dataList.value || []).filter((n: any) => {
    const max = Number(n.maxTaskNum) || 0
    const run = Number(n.running) || 0
    return max > 0 && run > max
  })
)

const healthSummary = computed(() => {
  const list = dataList.value || []
  if (!list.length) return null
  const offline = list.filter((n: any) => String(n.state) !== '1').length
  const fails = list.reduce((s: number, n: any) => s + (Number(n.cmdFail10m) || 0), 0)
  return { total: list.length, offline, fails }
})

const healOne = async () => {
  const bad = offlineNodes.value[0]
  if (!bad) return
  const moveCount = Math.min(Number(bad.queueLen) || 0, 5)
  const target = healedTargets.value
  try {
    if (moveCount > 0 && target && target !== bad.name) {
      await rebalanceNodeQueueApi(bad.name, target, moveCount)
    }
    await restartNodeApi(bad.name)
    // 重启指令要节点自己来领：被压住的节点恰恰领不到（2026-11-20 实测 node-tx-sh-02
    // 内存 97%、load 12，重启指令排队没人取，容器 Up 6h 纹丝不动）。
    // 所以等一会儿再看，没回来就明说要在主机上手工处理，而不是假装成功。
    await new Promise((r) => setTimeout(r, 12000))
    await getList()
    const still = (dataList.value || []).find((n: any) => n.name === bad.name)
    if (still && String(still.state) === '3') {
      ElMessage.warning(t('node.healPending', { name: bad.name }))
      return
    }
    ElMessage.success(t('node.healDone', { name: bad.name, n: moveCount, to: target || '-' }))
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  }
}

// 迁移排队任务：某节点积压超上限（新任务被拒）而其他节点空转时用
const rebalanceVisible = ref(false)
const rebalanceFrom = ref('')
const rebalanceTo = ref('')
const rebalanceLimit = ref(5)
const openRebalance = (row: any) => {
  rebalanceFrom.value = row?.name || ''
  rebalanceTo.value = ''
  rebalanceLimit.value = Math.max(1, Math.min(Number(row?.queueLen) || 5, 20))
  rebalanceVisible.value = true
}
const submitRebalance = async () => {
  if (!rebalanceFrom.value || !rebalanceTo.value) {
    ElMessage.warning(t('node.rebalanceNeedTarget'))
    return
  }
  try {
    const res: any = await rebalanceNodeQueueApi(
      rebalanceFrom.value,
      rebalanceTo.value,
      rebalanceLimit.value
    )
    ElMessage.success(t('node.rebalanceDone', { n: res?.data?.moved ?? 0 }))
    rebalanceVisible.value = false
    getList()
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  }
}

// 清空节点待领取队列：任务洪泛时不用再登服务器 redis-cli
const clearQueue = async (name: string) => {
  try {
    await ElMessageBox.confirm(t('node.clearQueueConfirm', { name }), t('common.delWarning'), {
      confirmButtonText: 'OK',
      cancelButtonText: t('common.cancel'),
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    const res: any = await clearNodeQueueApi(name)
    ElMessage.success(t('node.clearQueueDone', { n: res?.data?.cleared ?? 0 }))
    getList()
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  }
}

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const res = await getNodeDataApi()
    // 顺带刷新「派发被拒」统计与最近记录：和节点列表一起看才有意义
    loadRejectStats()
    loadRecentRejections()
    loadMemPeaks()
    return {
      list: res.data.list
    }
  }
})
const { loading, dataList, currentPage, pageSize } = tableState
const { getList, getElTableExpose } = tableMethods

// 节点页也有「实时性」需求（排队、内存、在线状态一直在变），和任务页保持一致：
// 可见时每 15 秒刷一次，后台不刷，回来补一次（2026-11-20）。
const lastRefreshAt = ref('')
// 多标签页只让一个轮询（避免 N 个标签页把节点列表接口刷 N 倍）
const { isLeader: nodeLeader } = useSingleTabLeader('nodes')
useVisibleInterval(() => {
  if (!nodeLeader.value) return
  getList()
  lastRefreshAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
}, 15000)
function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}
const dialogVisible = ref(false)
const closeDialog = () => {
  dialogVisible.value = false
}
const detailData = reactive({
  name: '',
  maxTaskNum: '',
  state: '',
  ModulesConfig: ''
})
const openConfig = async (data) => {
  detailData.name = data.name
  detailData.maxTaskNum = data.maxTaskNum
  detailData.ModulesConfig = data.modulesConfig
  detailData.state = data.state
  dialogVisible.value = true
}
const restartNode = async (name) => {
  await restartNodeApi(name)
}
const delLoading = ref(false)
// 未勾选任何行时，工具条上的批量删除应禁用
const tableSelectedCount = ref(0)
const handleTableSelectionChange = (rows: unknown[]) => {
  tableSelectedCount.value = rows?.length ?? 0
}
const names = ref<string[]>([])
const delSelect = async () => {
  const elTableExpose = await getElTableExpose()
  const selectedRows = elTableExpose?.getSelectionRows() || []
  names.value = selectedRows.map((row) => row.name)
  delLoading.value = true
  try {
    const res = await deleteNodeApi(names.value)
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    delLoading.value = false
    getList()
  }
}
const confirmDelete = async () => {
  const confirmed = await confirmDialog(t('common.delMessage'))
  if (confirmed) {
    await delSelect()
  }
}
const logDialogVisible = ref(false)
const closeLogDialogVisible = () => {
  logDialogVisible.value = false
}
const logContent = ref('')
const scrollbarRef = ref<InstanceType<typeof ElScrollbar>>()
const logLoading = ref(false)
// 日志过滤：175KB 全量塞进 <pre> 找 ERROR 只能手滚（2026-11-20）。
// 按行切分 + 关键字/级别过滤，并只渲染末尾 2000 行控制 DOM 规模。
const logKeyword = ref('')
const logErrorsOnly = ref(false)
const MAX_LOG_LINES = 2000
const logLines = computed(() => (logContent.value ? logContent.value.split('\n') : []))
const filteredLogLines = computed(() => {
  const kw = logKeyword.value.trim().toLowerCase()
  return logLines.value.filter((line) => {
    if (logErrorsOnly.value && !/ERROR|FATAL|panic/i.test(line)) return false
    if (kw && !line.toLowerCase().includes(kw)) return false
    return true
  })
})
// 错误摘要：打开日志先看「哪类错误出现了多少次」，
// 不用手动勾「只看错误」再一条条读（2026-11-20）。
const errorSummary = computed(() => {
  const map = new Map<string, number>()
  for (const line of logLines.value) {
    if (!/ERROR|FATAL|panic/i.test(line)) continue
    // 归一化：去掉行首时间戳、协程号、地址等易变部分，只留稳定特征
    const key = line
      .replace(/^\s*\[?\d{4}-\d{2}-\d{2}[^\]]*\]?\s*/, '')
      .replace(/\b[0-9a-f]{8,}\b/gi, '<id>')
      .replace(/\b\d+(\.\d+)?(ms|s|µs)\b/g, '<dur>')
      .trim()
      .slice(0, 120)
    map.set(key, (map.get(key) || 0) + 1)
  }
  return [...map.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, 8)
    .map(([text, count]) => ({ text, count }))
})
const showSummary = ref(true)
const applyErrorFilter = (text: string) => {
  logKeyword.value = text.slice(0, 40)
  logErrorsOnly.value = true
}

const shownLog = computed(() => {
  const arr = filteredLogLines.value
  return (arr.length > MAX_LOG_LINES ? arr.slice(-MAX_LOG_LINES) : arr).join('\n')
})

const openLogDialogVisible = async (data) => {
  // 大日志接口要几百毫秒到几秒：先开弹窗显示 loading，再填内容，
  // 否则点完「日志」会有一段毫无反应的空白（2026-11-20）。
  logContent.value = ''
  logLoading.value = true
  logDialogVisible.value = true
  try {
    const res = await getNodeLogApi(data.name)
    logContent.value = res.data.logs || ''
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    logLoading.value = false
  }
}
onMounted(() => {
  setMaxHeight()
  window.addEventListener('resize', setMaxHeight)
})

const maxHeight = ref(0)

const setMaxHeight = () => {
  const screenHeight = window.innerHeight || document.documentElement.clientHeight
  maxHeight.value = screenHeight * 0.7
}

const nodeName = ref('')
const pluginDialogVisible = ref(false)
const openPlugin = async (data) => {
  nodeName.value = data
  pluginDialogVisible.value = true
}
const closepluginDialogVisible = () => {
  pluginDialogVisible.value = false
}
</script>

<template>
  <ContentWrap>
    <!-- <ElRow :gutter="20" style="margin-bottom: 15px">
      <ElCol :span="1.5">
        <ElText class="mx-1" style="position: relative; top: 8px">{{ t('node.nodeName') }}:</ElText>
      </ElCol>
      <ElCol :span="5">
        <ElInput v-model="search" :placeholder="t('common.inputText')" style="height: 36px" />
      </ElCol>
      <ElCol :span="5" style="position: relative; left: 16px">
        <ElButton type="primary" :icon="searchicon" @click="handleSearch"
          >{{ t('common.search') }}</ElButton
        >
      </ElCol>
    </ElRow> -->
    <div class="node-toolbar">
      <BaseButton
        type="danger"
        :loading="delLoading"
        :disabled="tableSelectedCount === 0"
        @click="confirmDelete"
      >
        {{ t('common.delete') }}
      </BaseButton>
    </div>
    <div>
      <div class="refresh-hint">
        <template v-if="!nodeLeader">{{ t('common.pollFollower') }}</template>
        <template v-else>
          {{ t('node.autoRefreshHint', { t: lastRefreshAt || t('task.refreshJustNow') }) }}
        </template>
      </div>

      <div v-if="hasRejectInfo" class="node-health">
        <ElTag
          type="info"
          size="small"
          effect="plain"
          style="cursor: pointer"
          @click="toggleRecentRejections"
        >
          {{ t('node.rejectTitle') }}{{ t('node.rejectSep')
          }}{{ rejectSummary.length ? rejectSummary.join(' · ') : latestRejectText }}
          <span style="margin-left: 4px">{{ showRecentRejections ? '▲' : '▼' }}</span>
        </ElTag>
      </div>
      <div v-if="showRecentRejections" class="recent-reject">
        <div v-if="!recentRejections.length" class="recent-reject__empty">{{
          t('node.recentRejectEmpty')
        }}</div>
        <div v-for="r in recentRejections" :key="r._id" class="recent-reject__row">
          <span class="recent-reject__time">{{ String(r.at).slice(11, 19) }}</span>
          <ElTag size="small" type="warning" effect="plain">{{ gateLabel(r.gate) }}</ElTag>
          <span class="recent-reject__node">{{ r.node }}</span>
          <span class="recent-reject__task">{{ r.taskName }}</span>
          <span class="recent-reject__detail">{{ r.detail }}</span>
        </div>
      </div>

      <div v-if="throttledNodes.length" class="node-health">
        <ElTag type="warning" size="small" effect="plain">
          {{ t('node.throttling', { names: throttledNodes.map((n: any) => n.name).join(' · ') }) }}
        </ElTag>
      </div>

      <div v-if="healthSummary" class="node-health">
        <ElTag v-if="healthSummary.offline > 0" type="danger" size="small" effect="plain">
          {{ t('node.healthOffline', { n: healthSummary.offline, total: healthSummary.total }) }}
        </ElTag>
        <ElTag v-else type="success" size="small" effect="plain">
          {{ t('node.healthAllOnline', { total: healthSummary.total }) }}
        </ElTag>
        <ElTag v-if="healthSummary.fails > 0" type="warning" size="small" effect="plain">
          {{ t('node.healthFails', { n: healthSummary.fails }) }}
        </ElTag>
        <ElTag v-else type="success" size="small" effect="plain">
          {{ t('node.healthNoFails') }}
        </ElTag>
      </div>

      <ElAlert
        v-if="stalledNodes.length"
        type="error"
        :closable="false"
        show-icon
        style="margin-bottom: 10px"
      >
        <template #title>
          {{
            t('node.stalledBanner', {
              name: stalledNodes[0].name,
              n: stalledNodes[0].idleMinutes || 0,
              module:
                stalledNodes[0].currentModule || stalledNodes[0].currentTaskName || '-',
              queue: stalledNodes[0].queueLen || 0
            })
          }}
        </template>
        <BaseButton type="danger" size="small" style="margin-top: 6px" @click="restartStalled">
          {{ t('node.stalledAction') }}
        </BaseButton>
      </ElAlert>
      <ElAlert
        v-if="offlineNodes.length"
        type="warning"
        :closable="false"
        show-icon
        style="margin-bottom: 10px"
      >
        <template #title>
          {{
            t('node.healBanner', {
              name: offlineNodes[0].name,
              n: offlineNodes[0].queueLen || 0,
              to: healedTargets || '-'
            })
          }}
        </template>
        <BaseButton type="warning" size="small" style="margin-top: 6px" @click="healOne">
          {{ t('node.healAction') }}
        </BaseButton>
      </ElAlert>
      <Table
        v-model:pageSize="pageSize"
        v-model:currentPage="currentPage"
        :columns="nodeColums"
        :data="dataList"
        stripe
        :border="true"
        :loading="loading"
        :resizable="true"
        @selection-change="handleTableSelectionChange"
        @register="tableRegister"
        :headerCellStyle="tableHeaderColor"
        :style="{
          fontFamily:
            '-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica Neue,Arial,Noto Sans,sans-serif,Apple Color Emoji,Segoe UI Emoji,Segoe UI Symbol,Noto Color Emoji'
        }"
      />
    </div>
  </ContentWrap>
  <Dialog
    v-model="dialogVisible"
    :title="$t('common.config')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="maxHeight"
  >
    <Configuration :closeDialog="closeDialog" :nodeConfForm="detailData" :getList="getList" />
  </Dialog>
  <Dialog
    v-model="logDialogVisible"
    :title="t('node.log')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="maxHeight"
  >
    <ElScrollbar ref="scrollbarRef">
      <div v-if="logLoading" class="log-loading">
        <span class="spinner" />
        <span>{{ t('node.logLoading') }}</span>
      </div>
      <template v-else-if="logContent">
        <div v-if="errorSummary.length" class="log-summary">
          <div class="log-summary__head" @click="showSummary = !showSummary">
            {{ t('node.logErrorSummary') }}（{{ errorSummary.length }}）
            <span class="log-summary__toggle">{{ showSummary ? '−' : '+' }}</span>
          </div>
          <div v-if="showSummary" class="log-summary__list">
            <div
              v-for="item in errorSummary"
              :key="item.text"
              class="log-summary__item"
              @click="applyErrorFilter(item.text)"
            >
              <span class="log-summary__count">×{{ item.count }}</span>
              <span class="log-summary__text">{{ item.text }}</span>
            </div>
          </div>
        </div>
        <div class="log-toolbar">
          <ElInput
            v-model="logKeyword"
            :placeholder="t('node.logFilterPlaceholder')"
            clearable
            size="small"
            style="width: 240px"
          />
          <ElCheckbox v-model="logErrorsOnly" size="small">
            {{ t('node.logOnlyError') }}
          </ElCheckbox>
          <span class="log-count">{{ filteredLogLines.length }}/{{ logLines.length }}</span>
        </div>
        <pre v-if="filteredLogLines.length">{{ shownLog }}</pre>
        <div v-else class="log-empty">{{ t('node.logNoMatch') }}</div>
      </template>
      <div v-else class="log-empty">{{ t('node.logEmpty') }}</div>
    </ElScrollbar>
    <template #footer>
      <BaseButton @click="closeLogDialogVisible">{{ t('common.off') }}</BaseButton>
    </template>
  </Dialog>

  <ElDialog v-model="rebalanceVisible" :title="t('node.rebalance')" width="420px">
    <div class="rebalance-body">
      <div class="rebalance-row">
        <span class="rebalance-label">{{ t('node.rebalanceFrom') }}</span>
        <ElTag type="info">{{ rebalanceFrom }}</ElTag>
      </div>
      <div class="rebalance-row">
        <span class="rebalance-label">{{ t('node.rebalanceTo') }}</span>
        <ElSelect v-model="rebalanceTo" :placeholder="t('node.rebalancePick')" style="width: 220px">
          <ElOption
            v-for="n in dataList.filter((x) => x.name !== rebalanceFrom)"
            :key="n.name"
            :label="n.name + '（' + t('node.queueShort', { n: n.queueLen || 0 }) + '）'"
            :value="n.name"
          />
        </ElSelect>
      </div>
      <div class="rebalance-row">
        <span class="rebalance-label">{{ t('node.rebalanceCount') }}</span>
        <ElInputNumber v-model="rebalanceLimit" :min="1" :max="20" />
      </div>
      <div class="rebalance-hint">{{ t('node.rebalanceHint') }}</div>
    </div>
    <template #footer>
      <BaseButton @click="rebalanceVisible = false">{{ t('common.cancel') }}</BaseButton>
      <BaseButton type="primary" @click="submitRebalance">OK</BaseButton>
    </template>
  </ElDialog>

  <Dialog
    v-model="pluginDialogVisible"
    :title="t('node.plugin')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="maxHeight"
  >
    <plugin :closeDialog="closepluginDialogVisible" :name="nodeName" />
  </Dialog>
</template>

<style scoped>
/* 日志弹窗的加载与空态：此前点击后是一片空白，看不出在加载还是没日志 */
.log-summary {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  margin-bottom: 10px;
  overflow: hidden;
}
.log-summary__head {
  display: flex;
  justify-content: space-between;
  padding: 6px 10px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  background: var(--el-fill-color-light);
}
.log-summary__list {
  max-height: 160px;
  overflow: auto;
}
.log-summary__item {
  display: flex;
  gap: 8px;
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
}
.log-summary__item:hover {
  background: var(--el-fill-color-lighter);
}
.log-summary__count {
  color: var(--el-color-danger);
  flex: 0 0 auto;
}
.log-summary__text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 24px 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.spinner {
  width: 14px;
  height: 14px;
  border: 2px solid var(--el-border-color);
  border-top-color: var(--el-color-primary);
  border-radius: 50%;
  animation: node-log-spin 0.8s linear infinite;
}
@keyframes node-log-spin {
  to {
    transform: rotate(360deg);
  }
}
.recent-reject {
  margin-bottom: 10px;
  padding: 8px 10px;
  border-radius: 6px;
  background: var(--el-fill-color-lighter);
  font-size: 12px;
}
.recent-reject__row {
  display: flex;
  gap: 10px;
  align-items: center;
  padding: 2px 0;
}
.recent-reject__time {
  color: var(--el-text-color-secondary);
}
.recent-reject__node {
  color: var(--el-text-color-secondary);
}
.recent-reject__task {
  font-family: monospace;
}
.recent-reject__detail {
  color: var(--el-text-color-secondary);
}
.recent-reject__empty {
  color: var(--el-text-color-secondary);
}

.node-mem-cell {
  line-height: 1.2;
}
.node-mem-sub {
  margin-top: 2px;
}

.refresh-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}

.node-health {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.node-task-cell {
  line-height: 1.25;
}
.node-task-name {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-task-sub {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin: 2px 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-task-none {
  color: var(--el-text-color-placeholder);
}

.node-state-cell {
  line-height: 1.25;
}
.node-state-hint {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
}

.log-empty {
  padding: 24px 0;
  text-align: center;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.node-toolbar {
  margin-bottom: 12px;
}
</style>
