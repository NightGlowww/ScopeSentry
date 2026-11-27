<script setup lang="tsx">
import { confirmDialog } from '@/utils/confirm'
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { ref, reactive, h, onMounted, onUnmounted, computed, watch } from 'vue'
import { ArrowDown } from '@element-plus/icons-vue'
import {
  ElButton,
  ElCol,
  ElInput,
  ElRow,
  ElText,
  ElProgress,
  ElTag,
  ElMessageBox,
  ElMessage,
  ElSwitch,
  ElDropdown,
  ElDropdownMenu,
  ElDropdownItem,
  ElIcon,
  ElRadioGroup,
  ElRadioButton,


  ElTreeSelect,
  ElTooltip,
  ElAlert,
  ElSelect,
  ElOption
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useSingleTabLeader } from '@/hooks/web/useSingleTabLeader'
import { useIcon } from '@/hooks/web/useIcon'
import {
  getTaskDataApi,
  deleteTaskApi,
  retestTaskApi,
  stopTaskApi,
  starTaskApi,
  syancProjectApi
} from '@/api/task'
import { Dialog } from '@/components/Dialog'
import { BaseButton } from '@/components/Button'
import { DisableHint } from '@/components/DisableHint'
import AddTask from './components/AddTask.vue'
import ProgressInfo from './components/ProgressInfo.vue'
import { useRouter, useRoute } from 'vue-router'
import { getProjectAllApi } from '@/api/project'
import { getNodeDataApi } from '@/api/node'
import { exportTaskApi } from '@/api/export'
import { moduleStageLabel } from '@/utils/scanModules'

import { deleteTaskByFilterApi } from '@/api/task'
const { push } = useRouter()
const searchicon = useIcon({ icon: 'iconoir:search' })
const { t } = useI18n()
// 搜索词记忆：切走再回来不用重输（sessionStorage，同一标签页内有效）
const SEARCH_KEY = 'task:search'
const route = useRoute()
// 从漏洞结果页「返回该任务」跳回来时用 query.search 覆盖记忆值
const search = ref((route.query.search as string) || sessionStorage.getItem(SEARCH_KEY) || '')
// 状态筛选：-1=全部。任务堆积时（几十个暂停任务）只靠名字搜很难找
const STATUS_KEY = 'task:status'
// 用 localStorage 而不是 sessionStorage：关掉标签/重启浏览器后筛选还在
// （sessionStorage 一关就没了，每次进来都要重筛一遍）。首次读不到再回落到 session。
const readPersisted = (key: string): string => {
  const v = localStorage.getItem(key)
  if (v !== null && v !== '') return v
  return sessionStorage.getItem(key) ?? ''
}
const statusFilter = ref<number>(Number(readPersisted(STATUS_KEY) || -1))
const restoredFilter = ref<boolean>(
  !!readPersisted(STATUS_KEY) || Number(readPersisted(STATUS_KEY) || -1) >= 0
)
// 下拉一改就重新查询：之前只有点「搜索」才生效，用户会以为筛选没反应
watch(statusFilter, (v) => {
  localStorage.setItem(STATUS_KEY, String(v ?? -1))
  sessionStorage.setItem(STATUS_KEY, String(v ?? -1))
  currentPage.value = 1
  getList()
})
const handleSearch = () => {
  sessionStorage.setItem(SEARCH_KEY, search.value || '')
  // 换了搜索词就回第 1 页：原来停在第 5 页搜一个只有 2 条结果的关键词，
  // 会看到空表格，还以为是「没搜到」（2026-11-27）
  currentPage.value = 1
  getList()
}
// 任务总耗时：创建 → 结束（运行中则算到现在）
const fmtDur = (sec: number) => {
  if (!isFinite(sec) || sec < 0) return ''
  if (sec < 60) return sec + 's'
  const m = Math.floor(sec / 60)
  if (m < 60) return m + 'm' + String(sec % 60).padStart(2, '0') + 's'
  const hh = Math.floor(m / 60)
  return hh + 'h' + String(m % 60).padStart(2, '0') + 'm'
}
const taskDuration = (row: any): string => {
  const start = row?.creatTime
  if (!start) return ''
  const s = new Date(String(start).replace(/-/g, '/')).getTime()
  const endStr = row?.endTime
  const e = endStr ? new Date(String(endStr).replace(/-/g, '/')).getTime() : Date.now()
  if (!isFinite(s) || !isFinite(e) || e < s) return ''
  return fmtDur(Math.round((e - s) / 1000))
}

// 表头点击排序：ElTable 的 sortable="custom" 只发事件，真正的排序在服务端做
// （任务列表分页，前端排当前页会骗人）。字段白名单在 service.taskSort 里。
const sortState = ref('')
const onSortChange = ({ prop, order }: { prop: string; order: string | null }) => {
  sortState.value = order ? `${prop}:${order === 'ascending' ? 'asc' : 'desc'}` : ''
  currentPage.value = 1
  getList()
}

const taskColums = reactive<TableColumn[]>([
  {
    field: 'selection',
    type: 'selection',
    width: 45
  },
  {
    field: 'name',
    label: t('task.taskName'),
    minWidth: 150,
    sortable: 'custom',
    showOverflowTooltip: true
  },
  {
    field: 'taskNum',
    label: t('task.node'),
    width: 130,
    formatter: (row: any) => {
      // allNode 的任务会分发到多台，显示「全部节点」而不是某一个
      if (row?.allNode) return t('task.allNodes')
      const list = Array.isArray(row?.node) ? row.node : row?.node ? [row.node] : []
      return list.length ? list.join(', ') : '-'
    }
  },
  {
    field: 'taskNum',
    label: t('task.taskCount'),
    width: 90,
    sortable: 'custom',
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
    field: 'progress',
    label: t('task.taskProgress'),
    minWidth: 200,
    sortable: 'custom',
    formatter: (row: Recordable, __: TableColumn, cellValue: number) => {
      const bar = h(ElProgress, {
        percentage: cellValue,
        type: 'line',
        striped: true,
        status: cellValue < 100 ? '' : 'success',
        stripedFlow: cellValue < 100 ? true : false
      })
      // 运行中额外显示「正在做什么 + 已用时长」：长任务（目录扫描 7 分钟/目标）
      // 只显示百分比会像卡死，这一段让用户看得出它到底在干什么。
      const mod = row.currentModule as string
      // 排队中：任务在节点队列里等，还没真正开跑（节点一次只跑一个任务）
      if (row.status === 1 && row.queued) {
        // 排队位置：知道「前面还有几个」比单纯「排队中」有用得多
        const pos = Number(row.queuePos) || 0
        const hint = pos > 1 ? t('task.queuePosHint', { n: pos - 1 }) : t('task.queuedHint')
        return h('div', { class: 'progress-cell' }, [
          bar,
          h('div', { class: 'progress-sub queued' }, hint)
        ])
      }
      // 工具失败和「当前在做什么」要同时看得到：
      // 以前 cmdFail>0 时直接 return，模块/目标/时长全被吞掉，
      // 恰恰在出问题时最需要知道它跑到哪一步（2026-11-20）。
      // 失败任务：把巡检写入的原因显示出来，否则只有「失败」两个字没法排查
      const failReason = (row.failReason as string) || ''
      if (row.status === 4 && failReason) {
        return h('div', { class: 'progress-cell' }, [
          bar,
          h('div', { class: 'progress-sub fail' }, failReason)
        ])
      }
      const cmdFail = (row.cmdFail as number) || 0
      const failLine =
        cmdFail > 0
          ? h('div', { class: 'progress-sub fail' }, t('task.cmdFailHint', { n: cmdFail }))
          : null
      if (row.status !== 1 || !mod) {
        return failLine ? h('div', { class: 'progress-cell' }, [bar, failLine]) : bar
      }
      let elapsed = ''
      let slow = false
      if (row.currentSince) {
        const t0 = new Date(String(row.currentSince).replace(/-/g, '/')).getTime()
        const mins = Math.floor((Date.now() - t0) / 60000)
        if (mins >= 1) {
          // 阈值按实测调过：nuclei 对单目标跑 20-60 分钟是常态（2026-11-21 抽样 9 个模块，
          // 最长 60 分钟仍在推进），所以 10 分钟标黄全是噪音。
          //   20 分钟起标黄「慢」，60 分钟以上才说「疑似停滞」
          if (mins >= 60) {
            elapsed = ' · ' + t('task.moduleStalled')
            slow = true
          } else {
            elapsed = ' · ' + mins + t('task.minutesShort')
            slow = mins >= 20
          }
        }
      }
      // 粗估剩余时间：progress ≥ 5% 且有创建时间才算，否则不出（避免「约剩 0 分钟」这类噪音）
      let etaText = ''
      const prog = Number(row.progress) || 0
      // 有 startedAt 的是「本次启动时间」，估算可信；老任务没有该字段，只能拿创建时间凑，
      // 期间可能暂停过，所以前面加 ≈ 表示粗略（2026-11-21）
      const preciseEta = !!row.startedAt
      const startRef = row.startedAt || row.creatTime
      if (prog >= 5 && startRef) {
        const started = new Date(String(startRef).replace(/-/g, '/')).getTime()
        const usedMs = Date.now() - started
        if (usedMs > 5 * 60000) {
          const remainMs = (usedMs * (100 - prog)) / prog
          const remainMin = Math.round(remainMs / 60000)
          if (remainMin >= 1 && remainMin <= 24 * 60) {
            const hPart = Math.floor(remainMin / 60)
            const mPart = remainMin % 60
            const base =
              hPart > 0
                ? t('task.etaHours', { h: hPart, m: mPart })
                : t('task.etaMinutes', { m: mPart })
            etaText = preciseEta ? base : '≈ ' + base
          }
        }
      }
      return h('div', { class: 'progress-cell' }, [
        bar,
        failLine,
        h('div', { class: slow ? 'progress-sub progress-sub--slow' : 'progress-sub' }, moduleStageLabel(mod) + ' @ ' + (row.currentTarget || '-') + elapsed),
        // 预计剩余：按「已用时间 ÷ 已完成比例」线性外推。
        // 百分比本身回答不了「还要多久」，这是最常见的追问（2026-11-21）。
        etaText
          ? h('div', { class: 'progress-sub progress-sub--dim' }, etaText)
          : null,
        // 目标进度：光看百分比不知道「一共多少个目标、现在第几个」
        (row.taskNum as number) > 0
          ? h(
              'div',
              { class: 'progress-sub progress-sub--dim' },
              t('task.targetProgress', {
                done: Math.min(Math.round(((row.progress as number) || 0) * (row.taskNum as number) / 100), row.taskNum as number),
                total: row.taskNum as number
              })
            )
          : null
      ])
    }
  },
  {
    field: 'status',
    label: t('common.state'),
    width: 110,
    sortable: 'custom',
    formatter: (row: Recordable, __: TableColumn, cellValue: number) => {
      // 0 待开始，1 运行中，2 暂停，3 运行完成，4 失败
      let tagType, tagText
      // 「排队过期」是巡检把排队太久（默认 72h，可配 task.queueMaxAgeH）的任务移出队列时写的，
      // 它和「跑挂了」是两回事：任务根本没开始过，retest 就能重新排队。
      // 混在「失败」里会让人以为是扫描出错，所以单独给一个标签。
      const fr = String((row && row.failReason) || '')
      if (cellValue === 4 && fr.startsWith('排队超过')) {
        // 悬停能看到完整原因（排队时长 + 建议 retest），不用点进详情
        return h(ElTooltip, { content: fr, placement: 'top' }, () =>
          h(ElTag, { type: 'warning', effect: 'plain' }, () => t('task.queueExpired'))
        )
      }
      switch (cellValue) {
        case 0:
          tagType = 'primary'
          tagText = t('task.waitStart') // 待开始
          break
        case 1:
          tagType = 'info'
          tagText = t('task.running') // 运行中
          break
        case 2:
          tagType = 'warning'
          tagText = t('task.stop') // 暂停
          break
        case 3:
          tagType = 'success'
          tagText = t('task.finish') // 运行完成
          break
        case 4:
          tagType = 'danger'
          tagText = t('task.failed') // 失败（节点长期无进度，后台巡检判定）
          break
        default:
          tagType = 'default'
          tagText = '' // 未知状态
      }
      return h(ElTag, { type: tagType }, () => tagText)
    }
  },
  {
    field: 'policyWarnings',
    label: t('task.policyWarnings'),
    width: 90,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      const list = Array.isArray(cellValue) ? cellValue : []
      if (!list.length) return h('span', '-')
      return h(
        ElTooltip,
        { effect: 'dark', placement: 'top', content: list.join(' / ') },
        () => h(ElTag, { type: 'warning', size: 'small' }, () => list.length + ' 条')
      )
    }
  },
  {
    field: 'creatTime',
    width: 152,
    sortable: 'custom',
    label: t('task.createTime')
  },
  {
    field: 'endTime',
    label: t('task.endTime'),
    width: 170,
    sortable: 'custom',
    formatter: (row: Recordable, __: TableColumn, cellValue: string) => {
      // 完成时间下面补一行总耗时：列表里就能横向比较不同任务的快慢。
      // 暂停/未开始的任务没有结束时间，算「到现在」会一直涨且没有意义，所以不显示。
      const end = cellValue === '' ? '-' : cellValue
      const running = Number(row.status) === 1
      const dur = cellValue !== '' ? taskDuration(row) : running ? taskDuration(row) : ''
      const label = cellValue !== '' ? t('task.elapsed') : t('task.running')
      return h('div', { class: 'time-cell' }, [
        h('div', {}, end),
        dur ? h('div', { class: 'time-cell__dur' }, label + ' ' + dur) : null
      ])
    }
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    /* 操作列固定在右侧：横向空间不足时用户仍能直接点到按钮，
       不必拖滚动条。其余列宽收紧，总宽控制在 1280 内尽量不出现横向滚动。 */
    /* 190px 放不下「操作 + 结果 + 详情」三个按钮：下拉的文字被裁掉，只剩一个图标，
       看着就像三个白点（2026-11-27 用户反馈）。放到 260 让文字完整显示。 */
    width: 260,
    fixed: 'right',
    formatter: (row, __: TableColumn, _: number) => {
      const handleCommand = (command) => {
        ids.value = []
        switch (command) {
          case 'retest':
            confirmRetest(row)
            break
          case 'delete':
            confirmDelete(row)
            break
          case 'stop':
            ids.value.push(row.id)
            stopTask(ids.value)
            break
          case 'start':
            ids.value.push(row.id)
            startTask(ids.value)
            break
          // 导出与任务进度从行内按钮收进下拉：一行放 4 个按钮 + 下拉，
          // 268px 根本放不下，最后一个会被裁掉（2026-11-25 截图复现）。
          case 'export':
            exportTask(row, 'zip')
            break
          // 多工作表 Excel：任务存档时人要看的是「一个文件几张表」，
          // zip + 5 个 CSV 还得自己拼（对齐 ARL-Next 1.5.2）
          case 'export_xlsx':
            exportTask(row, 'xlsx')
            break
          case 'progress':
            getProgressInfo(row.id)
            break
        }
      }
      const retestAndDeleteDropdown = h(
        ElDropdown,
        {
          onCommand: handleCommand,
          // 表格单元格有 overflow 裁切，下拉必须传送到 body，否则菜单被切一半。
          teleported: true,
          trigger: 'click'
        },
        {
          default: () =>
            h(
              ElButton,
              {
                // 「操作」后面那个图标以前没有任何说明，看着像三个白点（2026-11-27 反馈）。
                // 补 title：悬停就知道这里是开始/停止/导出/进度/重测/删除的集合。
                title: t('task.moreActions'),
                style: { outline: 'none', boxShadow: 'none' }
              },
              () => [
                t('common.operation'), // 下拉菜单触发按钮文字
                h(
                  ElIcon,
                  {},
                  () => h(ArrowDown) // 向下箭头图标
                )
              ]
            ),
          dropdown: () =>
            h(ElDropdownMenu, null, () => {
              // 根据 row.status 渲染不同的菜单项
              if (row.status === 3) {
                return [
                  h(ElDropdownItem, { command: 'export' }, () => t('task.exportTask')),
                  h(ElDropdownItem, { command: 'export_xlsx' }, () => t('task.exportTaskXlsx')),
                  h(ElDropdownItem, { command: 'progress' }, () => t('task.taskProgress')),
                  h(ElDropdownItem, { command: 'retest' }, () => t('task.retest')),
                  h(ElDropdownItem, { command: 'delete' }, () => t('common.delete'))
                ]
              } else {
                return [
                  h(ElDropdownItem, { command: 'start' }, () => t('task.start')),
                  h(ElDropdownItem, { command: 'stop' }, () => t('task.stop')), // 如果是运行中，显示“停止”按钮
                  h(ElDropdownItem, { command: 'export' }, () => t('task.exportTask')),
                  // 多工作表 Excel：一个文件几张表，比 zip 里 5 个 CSV 更适合存档/交付
                  h(ElDropdownItem, { command: 'export_xlsx' }, () => t('task.exportTaskXlsx')),
                  h(ElDropdownItem, { command: 'progress' }, () => t('task.taskProgress')),
                  h(ElDropdownItem, { command: 'retest' }, () => t('task.retest')),
                  h(ElDropdownItem, { command: 'delete' }, () => t('common.delete'))
                ]
              }
            })
        }
      )
      return (
        <>
          {retestAndDeleteDropdown}
          <BaseButton
            type="primary"
            onClick={() => getTaskResult(row.name)}
            style={{ marginLeft: '10px' }}
          >
            {t('task.result')}
          </BaseButton>
          <BaseButton
            type="success"
            onClick={() => getTaskContent(row)}
            style={{ marginLeft: '10px' }}
          >
            {t('common.view')}
          </BaseButton>

        </>
      )
    }
  }
])

const progressDialogVisible = ref(false)
let getProgressInfoID = ''
// 按任务导出：后端流式返回 zip（每个集合一个 CSV）。
// 任务跑完想存档时，不用再去导出页手选类型和筛选。
const exportingId = ref('')
const exportTask = async (row: any, format: 'zip' | 'xlsx' = 'zip') => {
  if (!row?.id) return
  exportingId.value = row.id
  try {
    const blob = await exportTaskApi(row.id, format)
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'task-' + (row.name || row.id) + '.' + format
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    window.URL.revokeObjectURL(url)
    ElMessage.success(t('task.exportDone'))
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    exportingId.value = ''
  }
}

// 该任务期间的工具失败次数（用于弹窗顶部告警）
const progressCmdFail = ref(0)
// 当前查看进度的任务名（产出一栏点击跳转要用）
const progressTaskName = ref('')
// 队列积压：节点一次只跑一个任务，队列深说明后面要等；把总数摆出来免得用户以为卡住
const queuedTotal = ref(0)
// 节点下拉选项：从节点接口拿，避免任务里的节点名写错
const nodeOptions = ref<string[]>([])
// 按节点筛选：多节点时排查「某个节点跑了什么」只能翻列表
const NODE_KEY = 'task:node'
const nodeFilter = ref(readPersisted(NODE_KEY) || '')
watch(nodeFilter, (v) => {
  localStorage.setItem(NODE_KEY, v || '')
  sessionStorage.setItem(NODE_KEY, v || '')
  currentPage.value = 1
  getList()
})
// 空状态区分「还没有任务」和「当前筛选没结果」——否则筛完看到"还没有扫描任务"+新建按钮很误导
const hasTaskFilter = computed(() => !!search.value.trim() || statusFilter.value >= 0)
const taskFilterSummary = computed(() => {
  const parts: string[] = []
  if (search.value.trim()) parts.push(t('task.filterByName', { q: search.value.trim() }))
  if (statusFilter.value >= 0) {
    const map: Record<number, string> = {
      1: t('task.statusRunning'),
      2: t('task.statusPaused'),
      3: t('task.statusFinished'),
      4: t('task.statusFailed')
    }
    parts.push(t('task.filterByStatus', { s: map[statusFilter.value] || String(statusFilter.value) }))
  }
  return parts.join(' · ')
})
const clearTaskFilters = () => {
  search.value = ''
  statusFilter.value = -1
  localStorage.removeItem(STATUS_KEY)
  localStorage.removeItem(NODE_KEY)
  restoredFilter.value = false
  sessionStorage.removeItem(SEARCH_KEY)
  sessionStorage.removeItem(STATUS_KEY)
  currentPage.value = 1
  getList()
}

// 按当前筛选批量删除：勾选跨页太慢，直接按筛选条件清
// 安全：后端要求必须带搜索词或状态，且单次最多 200 条
const filterDelLoading = ref(false)
const confirmDeleteByFilter = async () => {
  const hasCond = !!search.value.trim() || statusFilter.value >= 0
  if (!hasCond) {
    ElMessage.warning(t('task.deleteFilterNeedCond'))
    return
  }
  const scope = [
    search.value.trim() ? t('task.deleteFilterSearch', { q: search.value.trim() }) : '',
    statusFilter.value >= 0 ? t('task.deleteFilterStatus') : ''
  ].filter(Boolean).join(t('task.deleteFilterAnd'))
  try {
    await ElMessageBox.confirm(
      t('task.deleteFilterConfirm', { scope }),
      t('common.delWarning'),
      { confirmButtonText: 'OK', cancelButtonText: t('common.cancel'), type: 'warning' }
    )
  } catch {
    return
  }
  filterDelLoading.value = true
  try {
    const res: any = await deleteTaskByFilterApi(
      search.value.trim(),
      statusFilter.value >= 0 ? statusFilter.value : undefined,
      nodeFilter.value || undefined
    )
    ElMessage.success(t('task.deleteFilterDone', { n: res?.data?.deleted ?? 0 }))
    resetSelection()
    getList()
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    filterDelLoading.value = false
  }
}

const getProgressInfo = async (id) => {
  // 弹窗打开后立刻解除按钮 loading：真正的加载状态由弹窗内部的表格承担，
  // 这里只负责防止连点触发多次 open。
  rowActionId.value = id || ''
  getProgressInfoID = id
  const row = (dataList.value || []).find((r: any) => r.id === id) as any
  progressCmdFail.value = (row?.cmdFail as number) || 0
  progressTaskName.value = (row?.name as string) || ''
  progressDialogVisible.value = true
  setTimeout(() => {
    rowActionId.value = ''
  }, 600)
}

const getTaskResult = async (id) => {
  push(`/asset-information/index?task=${id}`)
}

const stopTask = async (ids) => {
  try {
    await stopTaskApi(ids)
    ElMessage.success(t('task.stopSubmitted'))
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  }
}

const startTask = async (ids) => {
  // 批量开始：逐个结果由后端汇总回来，界面说清「哪几个没起来、为什么」。
  // 之前只弹一句成功，失败的那几个要自己翻列表才发现（2026-11-20）。
  try {
    const res: any = await starTaskApi(ids)
    const started = Number(res?.data?.started ?? ids.length)
    const failed = Number(res?.data?.failed ?? 0)
    if (failed === 0) {
      ElMessage.success(t('task.startSubmitted'))
    } else if (started > 0) {
      const reasons = Object.values(res?.data?.failures || {}) as string[]
      ElMessage.warning(t('task.startPartial', { ok: started, fail: failed, reason: reasons[0] || '' }))
    } else {
      ElMessage.error(t('task.startAllFailed', { reason: String(Object.values(res?.data?.failures || {})[0] || '') }))
    }
    getList()
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  }
}

const progresscloseDialog = () => {
  progressDialogVisible.value = false
}
const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    // 表格自己发起的首次加载也走这里：时间戳必须在这一层更新，
    // 否则只包 getList() 时首屏永远是「刚刚」（2026-11-20 实测）。
    lastRefreshAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
    const res = await getTaskDataApi(
      search.value,
      currentPage.value,
      pageSize.value,
      statusFilter.value >= 0 ? statusFilter.value : undefined,
      nodeFilter.value || undefined,
      undefined,
      sortState.value || undefined
    )
    // 首次加载完也要评估是否该起自动刷新：
    // 之前只在 getList() 包装里调 syncAutoList()，而表格首屏走的是这里，
    // 导致有运行中任务时也不会自动刷新（2026-11-20 实测）。
    setTimeout(() => syncAutoList(), 0)
    return {
      list: res.data.list,
      total: res.data.total
    }
  },
  immediate: true
})
const { loading, dataList, total, currentPage, pageSize } = tableState
pageSize.value = 20
const { getList: rawGetList, getElTableExpose } = tableMethods

// 节点离线提示：任务卡住最常见的原因是节点掉了，但以前只能去「系统状态」页才看得到。
// 这里在任务页顶部直接给出「X/Y 节点在线」，离线时高亮成警告条。
// 行内操作防连点：点了「任务进度」后按钮转圈，避免重复请求
const rowActionId = ref('')
const nodeOnline = ref(0)
const nodeTotal = ref(0)
// 暂停的节点不算离线：state=2 是「主动停用」，不参与调度是预期行为，
// 以前把暂停也计成离线，于是永远挂一条「节点离线」警告（2026-11-25 实测 1/4）。
const nodePaused = ref(0)
const nodeOffline = computed(() => Math.max(0, nodeTotal.value - nodeOnline.value - nodePaused.value))
const loadNodeHealth = async () => {
  try {
    // 直接用节点列表接口（与节点管理页同源、已验证可用），按 state 统计在线数
    const res: any = await getNodeDataApi()
    const raw = res?.data?.list ?? res?.data ?? res?.list ?? []
    const list: any[] = Array.isArray(raw) ? raw : []
    nodeTotal.value = list.length
    nodeOnline.value = list.filter((n) => String(n?.state) === '1').length
    nodePaused.value = list.filter((n) => String(n?.state) === '2').length
    queuedTotal.value = list.reduce((sum: number, n: any) => sum + (Number(n?.queueLen) || 0), 0)
    nodeOptions.value = list.map((n: any) => String(n?.name || '')).filter(Boolean)
  } catch {
    // 静默：不打扰主流程
  }
}

// 列表自动刷新：有任务在跑时每 10 秒静默刷新一次，
// 用户不用反复点刷新按钮也能看到进度变化；全部结束后自动停止。
const autoListTimer = ref<ReturnType<typeof setInterval> | null>(null)
// 多标签页选主：只有一个标签在自动刷新，其余标签靠手动/切回前台刷新
const { isLeader: taskLeader } = useSingleTabLeader('tasks')
// 上次刷新时间：静默自动刷新时用户看不出数据是新的还是卡住了
const lastRefreshAt = ref('')
const hasRunning = computed(() =>
  (dataList.value || []).some((t: any) => t.status === 0 || t.status === 1)
)
const syncAutoList = () => {
  if (autoListTimer.value) {
    clearInterval(autoListTimer.value)
    autoListTimer.value = null
  }
  if (hasRunning.value) {
    autoListTimer.value = setInterval(async () => {
      // 页面在后台时不刷新：标签页挂着不动还每 10 秒打一次接口，纯浪费
      if (typeof document !== 'undefined' && document.hidden) return
      // 多标签页只让一个轮询：任务页是最常被开多个的页面（2026-11-21）
      if (!taskLeader.value) return
      try {
        await getList()
      } catch {
        // 静默：失败下一轮再试
      }
    }, 10000)
  }
}
onMounted(() => {
  loadNodeHealth()
  setInterval(loadNodeHealth, 60000)
  syncAutoList()
})
// 回到前台立刻补一次刷新，避免看到十几分钟前的旧数据
const onVisible = () => {
  if (!document.hidden) getList()
}
onMounted(() => document.addEventListener('visibilitychange', onVisible))
onUnmounted(() => {
  document.removeEventListener('visibilitychange', onVisible)

  if (autoListTimer.value) clearInterval(autoListTimer.value)
})
// 每次刷新列表后重置选择状态，避免"按钮可点但选中为空"
const getList = async () => {
  const r = await rawGetList()
  // 任何一次刷新（手动/自动）都更新时间戳
  lastRefreshAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  resetSelection()
  syncAutoList()
  // 顺手刷新节点健康度：页面被 keep-alive 缓存时 onMounted 不会再跑，
  // 只靠挂载钩子会导致警告条永远不出现（2026-11-20 实测）。
  loadNodeHealth()
  return r
}
function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}
const dialogVisible = ref(false)
const addTask = async () => {
  taskid.value = ''
  DialogTitle = t('task.addTask')
  Create.value = true
  dialogVisible.value = true
}

let DialogTitle = t('task.addTask')
const closeDialog = () => {
  dialogVisible.value = false
}

let Create = ref(true)
const taskid = ref('')
const getTaskContent = async (data) => {
  taskid.value = data.id
  dialogVisible.value = true
  Create.value = false
  DialogTitle = t('common.view')
}
const confirmDeleteSelect = async () => {
  const deleteAssetS = ref<boolean | string | number>(false)
  ElMessageBox({
    showCancelButton: true,
    cancelButtonText: t('common.cancel'),
    title: t('common.delete'),
    draggable: true,
    // Should pass a function if VNode contains dynamic props
    message: () =>
      h('div', { style: { display: 'flex', alignItems: 'center' } }, [
        h('p', { style: { margin: '0 10px 0 0' } }, t('task.delAsset')),
        h(ElSwitch, {
          modelValue: deleteAssetS.value,
          'onUpdate:modelValue': (val: boolean | string | number) => {
            deleteAssetS.value = val
          }
        })
      ])
  }).then(async () => {
    await delSelect(deleteAssetS.value)
  })
}

const confirmStopSelect = async () => {
  ElMessageBox({
    title: t('task.stop'),
    showCancelButton: true,
    cancelButtonText: t('common.cancel'),
    message: t('task.confirmStopSelected'),
    draggable: true
  }).then(async () => {
    await stopTaskSelect()
  })
}
const stopTaskSelect = async () => {
  ids.value = selectedRows.value.map((row: any) => row.id)
  if (!ids.value.length) {
    // 选择状态为空时不要发请求：后端会返回 “ids is required”，
    // 用户只能看到一条莫名其妙的红色报错（2026-11-19 实测）。
    delLoading.value = false
    ElMessage.warning(t('common.disabledNeedTask'))
    return
  }
  delLoading.value = true
  try {
    await stopTask(ids.value)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error Stop data:', error)
    delLoading.value = false
    getList()
  }
}
const confirmStartSelect = async () => {
  ElMessageBox({
    title: t('task.start'),
    showCancelButton: true,
    cancelButtonText: t('common.cancel'),
    message: t('task.confirmStartSelected'),
    draggable: true
  }).then(async () => {
    await startTaskSelect()
  })
}
const startTaskSelect = async () => {
  ids.value = selectedRows.value.map((row: any) => row.id)
  if (!ids.value.length) {
    // 选择状态为空时不要发请求：后端会返回 “ids is required”，
    // 用户只能看到一条莫名其妙的红色报错（2026-11-19 实测）。
    delLoading.value = false
    ElMessage.warning(t('common.disabledNeedTask'))
    return
  }
  delLoading.value = true
  try {
    await startTask(ids.value)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error Stop data:', error)
    delLoading.value = false
    getList()
  }
}
interface Project {
  value: string
  label: string
  children?: Project[]
}
const projectList = reactive<Project[]>([])
const getProjectList = async () => {
  const res = await getProjectAllApi()
  res.data.list.forEach((item: Project) => {
    projectList.push({
      label: item.label,
      value: item.value || `parent-${item.label}`, // 避免空字符串
      children: item.children || []
    })
  })
}

const confirmSyncToProjectSelect = async () => {
  const option = ref<'existing' | 'new'>('existing') // 选项：已有 or 新建
  const selectedProjectId = ref<string>('')
  const newProjectName = ref('')
  const newProjectTag = ref('')
  await getProjectList()
  ElMessageBox({
    showCancelButton: true,
    cancelButtonText: t('common.cancel'),
    title: t('task.syncToProject'),
    draggable: true,
    message: () =>
      h('div', { style: { display: 'flex', flexDirection: 'column', gap: '10px' } }, [
        h(
          'p',
          {
            style: {
              margin: '0',
              fontSize: '12px',
              color: 'var(--el-text-color-secondary)'
            }
          },
          t('task.syncToProjectHint')
        ),
        // 选择类型
        h('div', [
          h('label', { style: { marginRight: '8px' } }),
          h(
            ElRadioGroup,
            {
              modelValue: option.value,
              'onUpdate:modelValue': (val: 'existing' | 'new') => {
                option.value = val
              }
            },
            {
              default: () => [
                h(ElRadioButton, { label: 'existing' }, () => t('task.syncToExisting')),
                h(ElRadioButton, { label: 'new' }, () => t('task.createNewProject'))
              ]
            }
          )
        ]),

        // 如果是同步到已有项目，显示下拉框
        option.value === 'existing'
          ? h(ElTreeSelect, {
              modelValue: selectedProjectId.value,
              'onUpdate:modelValue': (val: string) => {
                selectedProjectId.value = val
              },
              data: projectList,
              // 单选却带复选框：树里一排方框套在下拉框里，就是用户说的「框套框」（2026-11-27）。
              // 单选场景用高亮选中即可，去掉复选框。
              showCheckbox: false,
              placeholder: t('project.project'),
              filterable: true,
              style: { width: '100%' }
            })
          : null,

        // 如果是创建新项目，显示输入框
        option.value === 'new'
          ? h('div', { style: { display: 'flex', flexDirection: 'column', gap: '8px' } }, [
              h(ElInput, {
                modelValue: newProjectName.value,
                placeholder: t('project.msgProject'),
                'onUpdate:modelValue': (val: string) => (newProjectName.value = val)
              }),
              h(ElInput, {
                modelValue: newProjectTag.value,
                placeholder: t('project.msgProjectTag'),
                'onUpdate:modelValue': (val: string) => (newProjectTag.value = val)
              })
            ])
          : null
      ])
  }).then(async () => {
    if (option.value === 'existing') {
      console.log('同步到已有项目ID:', selectedProjectId.value)
    } else {
      console.log('创建新项目:', newProjectName.value, newProjectTag.value)
    }
    ids.value = selectedRows.value.map((row: any) => row.id)
  if (!ids.value.length) {
    // 选择状态为空时不要发请求：后端会返回 “ids is required”，
    // 用户只能看到一条莫名其妙的红色报错（2026-11-19 实测）。
    delLoading.value = false
    ElMessage.warning(t('common.disabledNeedTask'))
    return
  }
    await syancProjectApi(
      ids.value,
      option.value,
      selectedProjectId.value,
      newProjectTag.value,
      newProjectName.value
    )
  })
}

const confirmDelete = async (data) => {
  const deleteAsset = ref<boolean | string | number>(false)
  ElMessageBox({
    showCancelButton: true,
    cancelButtonText: t('common.cancel'),
    title: t('common.delete'),
    draggable: true,
    // Should pass a function if VNode contains dynamic props
    message: () =>
      h('div', { style: { display: 'flex', alignItems: 'center' } }, [
        h('p', { style: { margin: '0 10px 0 0' } }, t('task.delAsset')),
        h(ElSwitch, {
          modelValue: deleteAsset.value,
          'onUpdate:modelValue': (val: boolean | string | number) => {
            deleteAsset.value = val
          }
        })
      ])
  }).then(async () => {
    await del(data, deleteAsset.value)
  })
}
const delLoading = ref(false)
// 未勾选任何行时，工具条上的批量删除应禁用
const tableSelectedCount = ref(0)
const selectedRows = ref<any[]>([])
const handleTableSelectionChange = (rows: any[]) => {
  selectedRows.value = rows || []
  tableSelectedCount.value = selectedRows.value.length
}

// 列表刷新后表格会清空选中，但选择回调不一定触发，
// 导致 count 仍 >0 → 批量按钮可点 → 请求带空 ids → 后端报 "ids is required"（2026-11-19）
const resetSelection = () => {
  tableSelectedCount.value = 0
  selectedRows.value = []
  ids.value = []
}
// 工具条四个批量按钮共用一条禁用条件，文案也共用一条
const noTaskSelected = computed(() => tableSelectedCount.value === 0)
const del = async (data, delA) => {
  delLoading.value = true
  try {
    const res: any = await deleteTaskApi([data.id], delA)
    const n = Number(res?.data?.deleted ?? 1)
    ElMessage.success(t('task.deleteOneDone', { n, name: data?.name || '' }))
    delLoading.value = false
    getList()
  } catch (error) {
    // 之前这里只 console.error：界面毫无反馈，用户不知道删没删掉（2026-11-20）
    ElMessage.error((error as any)?.message || t('common.operateFailed'))
    delLoading.value = false
    getList()
  }
}
const ids = ref<string[]>([])
const delSelect = async (delA) => {
  ids.value = selectedRows.value.map((row: any) => row.id)
  if (!ids.value.length) {
    // 选择状态为空时不要发请求：后端会返回 “ids is required”，
    // 用户只能看到一条莫名其妙的红色报错（2026-11-19 实测）。
    delLoading.value = false
    ElMessage.warning(t('common.disabledNeedTask'))
    return
  }
  delLoading.value = true
  try {
    const want = ids.value.length
    const res: any = await deleteTaskApi(ids.value, delA)
    const n = Number(res?.data?.deleted ?? want)
    // 说清「删了几个」：批量删 20 个只回一句成功，用户无法确认范围
    ElMessage.success(t('task.deleteBatchDone', { n, want }))
    delLoading.value = false
    getList()
  } catch (error) {
    ElMessage.error((error as any)?.message || t('common.operateFailed'))
    delLoading.value = false
    getList()
  }
}
const confirmRetest = async (data) => {
  const confirmed = await confirmDialog(t('localization.retestConfirm'))
  if (confirmed) {
    await retestTask(data)
  }
}
const retestTask = async (data) => {
  try {
    await retestTaskApi(data.id)
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    getList()
  }
}
onMounted(() => {
  setMaxHeight()
  window.addEventListener('resize', setMaxHeight)
})
const maxHeight = ref(0)

const setMaxHeight = () => {
  const screenHeight = window.innerHeight || document.documentElement.clientHeight
  maxHeight.value = screenHeight * 0.75
}
</script>

<template>
  <ContentWrap>
    <div v-if="restoredFilter && hasTaskFilter" class="restored-hint">
      <ElTag size="small" type="info" effect="plain">{{ t('task.filterRestored') }}</ElTag>
      <ElButton link type="primary" size="small" @click="clearTaskFilters">
        {{ t('task.clearFilter') }}
      </ElButton>
    </div>
    <ElRow class="locale-search-row">
      <ElCol :span="4">
        <ElSelect v-model="nodeFilter" :placeholder="t('task.filterByNode')" clearable>
          <ElOption v-for="n in nodeOptions" :key="n" :label="n" :value="n" />
        </ElSelect>
      </ElCol>
      <ElCol :span="4">
        <ElSelect v-model="statusFilter" :placeholder="t('task.statusFilter')" clearable>
          <ElOption :label="t('task.statusAll')" :value="-1" />
          <ElOption :label="t('task.statusRunning')" :value="1" />
          <ElOption :label="t('task.statusPaused')" :value="2" />
          <ElOption :label="t('task.statusFinished')" :value="3" />
          <ElOption :label="t('task.statusFailed')" :value="4" />
        </ElSelect>
      </ElCol>
      <ElCol :span="1">
        <ElText class="mx-1">{{ t('task.taskName') }}:</ElText>
      </ElCol>
      <ElCol :span="5">
        <ElInput v-model="search" :placeholder="t('common.inputText')" style="height: 36px" />
      </ElCol>
      <ElCol :span="5">
        <ElButton type="primary" :icon="searchicon" @click="handleSearch">{{
          t('common.search')
        }}</ElButton>
      </ElCol>
    </ElRow>
    <ElRow>
      <ElCol style="position: relative; top: 16px">
        <div class="mb-10px task-toolbar">
          <BaseButton type="primary" @click="addTask">{{ t('task.addTask') }}</BaseButton>
          <BaseButton
            type="danger"
            plain
            :loading="filterDelLoading"
            @click="confirmDeleteByFilter"
          >
            {{ t('task.deleteFilter') }}
          </BaseButton>
          <DisableHint :disabled="noTaskSelected" :reason="t('common.disabledNeedTask')">
            <BaseButton
              type="danger"
              :loading="delLoading"
              :disabled="noTaskSelected"
              :title="noTaskSelected ? t('common.disabledNeedTask') : undefined"
              @click="confirmDeleteSelect"
            >
              {{ t('task.delTask') }}
            </BaseButton>
          </DisableHint>
          <DisableHint :disabled="noTaskSelected" :reason="t('common.disabledNeedTask')">
            <BaseButton
              type="warning"
              :loading="delLoading"
              :disabled="noTaskSelected"
              :title="noTaskSelected ? t('common.disabledNeedTask') : undefined"
              @click="confirmStopSelect"
            >
              {{ t('task.stop') }}
            </BaseButton>
          </DisableHint>
          <DisableHint :disabled="noTaskSelected" :reason="t('common.disabledNeedTask')">
            <BaseButton
              type="success"
              :loading="delLoading"
              :disabled="noTaskSelected"
              :title="noTaskSelected ? t('common.disabledNeedTask') : undefined"
              @click="confirmStartSelect"
            >
              {{ t('task.start') }}
            </BaseButton>
          </DisableHint>
          <DisableHint :disabled="noTaskSelected" :reason="t('common.disabledNeedTask')">
            <BaseButton
              type="info"
              :loading="delLoading"
              :disabled="noTaskSelected"
              :title="noTaskSelected ? t('common.disabledNeedTask') : undefined"
              @click="confirmSyncToProjectSelect"
            >
              {{ t('task.syncToProject') }}
            </BaseButton>
          </DisableHint>
        </div>
      </ElCol>
    </ElRow>
    <div style="position: relative; top: 12px">
      <ElAlert
        v-if="nodeTotal > 0 && nodeOffline > 0"
        type="warning"
        :closable="false"
        show-icon
        style="margin-bottom: 10px"
        :title="t('task.nodeOfflineWarn') + ': ' + nodeOffline + '/' + nodeTotal + (nodePaused ? '（' + nodePaused + ' 台已暂停）' : '')"
      />
      <ElAlert
        v-if="queuedTotal > 0"
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 10px"
        :title="t('task.queueBacklog', { n: queuedTotal })"
      />
      <div v-if="hasRunning" class="refresh-hint">
        <template v-if="!taskLeader">{{ t('common.pollFollower') }}</template>
        <template v-else>
          {{ t('task.listRefreshHint', { t: lastRefreshAt || t('task.refreshJustNow') }) }}
        </template>
      </div>
      <Table
        :tooltip-options="{
          offset: 1,
          showArrow: false,
          effect: 'dark',
          enterable: false,
          showAfter: 0,
          popperOptions: {},
          popperClass: 'test',
          placement: 'bottom',
          hideAfter: 0,
          disabled: true
        }"
        v-model:pageSize="pageSize"
        v-model:currentPage="currentPage"
        @sort-change="onSortChange"
        :columns="taskColums"
        :data="dataList"
        stripe
        :border="true"
        :loading="loading"
        :max-height="maxHeight"
        :resizable="true"
        :pagination="{
          total: total,
          pageSizes: [20, 30, 50, 100, 200, 500, 1000]
        }"
        @selection-change="handleTableSelectionChange"
        @register="tableRegister"
        :headerCellStyle="tableHeaderColor"
        :style="{
          fontFamily:
            '-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica Neue,Arial,Noto Sans,sans-serif,Apple Color Emoji,Segoe UI Emoji,Segoe UI Symbol,Noto Color Emoji'
        }"
      >
        <template #empty>
          <div class="empty-block">
            <template v-if="hasTaskFilter">
              <div class="empty-title">{{ t('task.emptyFilteredTitle') }}</div>
              <div class="empty-desc">{{ taskFilterSummary }}</div>
              <ElButton @click="clearTaskFilters">{{ t('task.clearFilters') }}</ElButton>
            </template>
            <template v-else>
              <div class="empty-title">{{ t('task.emptyTitle') }}</div>
              <div class="empty-desc">{{ t('task.emptyDesc') }}</div>
              <ElButton type="primary" @click="addTask">{{ t('task.addTask') }}</ElButton>
            </template>
          </div>
        </template>
      </Table>
    </div>
  </ContentWrap>
  <Dialog
    v-model="dialogVisible"
    :title="DialogTitle"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
  >
    <AddTask
      :closeDialog="closeDialog"
      :getList="getList"
      :create="Create"
      :taskid="taskid"
      :schedule="false"
      tp="scan"
      :targetIds="[]"
    />
  </Dialog>
  <Dialog
    v-model="progressDialogVisible"
    :title="t('task.taskProgress')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    width="70%"
    max-height="700"
  >
    <ProgressInfo
      :visible="progressDialogVisible"
      @close="progresscloseDialog"
      :closeDialog="progresscloseDialog"
      :getProgressInfoID="getProgressInfoID"
      getProgressInfotype="scan"
      getProgressInforunnerid=""
      :cmdFail="progressCmdFail"
      :taskName="progressTaskName"
    />
  </Dialog>
</template>

<style scoped>
.refresh-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}

.time-cell__dur {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

.progress-cell {
  line-height: 1.2;
}
.restored-hint {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 12px;
}

.progress-sub--slow {
  color: var(--el-color-warning);
}

.progress-sub--dim {
  opacity: 0.75;
}

.progress-sub.fail {
  color: var(--el-color-danger);
}
.progress-sub {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 空列表引导：默认的「暂无数据」太冷，给标题+说明+主操作按钮 */
.empty-block {
  padding: 40px 0 48px;
  text-align: center;
}
.empty-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 6px;
}
.empty-desc {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 16px;
}

/* 禁用按钮外面多了一层 DisableHint 包裹，原来的 .el-button + .el-button 相邻选择器失效，
   这里改用 flex gap 统一间距，避免按钮粘在一起 */
.task-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.task-toolbar :deep(.el-button + .el-button) {
  margin-left: 0;
}

.locale-search-row {
  align-items: center;
  gap: 12px;
}
.locale-search-row > .el-col {
  flex: 0 0 auto;
  width: auto;
  max-width: 100%;
}
.locale-search-row > .el-col:nth-child(2) {
  flex: 0 1 260px;
  min-width: 0;
}
.locale-search-row .el-text {
  white-space: nowrap;
}
</style>
