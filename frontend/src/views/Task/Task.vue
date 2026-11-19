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
import { useRouter } from 'vue-router'
import { getProjectAllApi } from '@/api/project'
import { getNodeDataApi } from '@/api/node'
import { exportTaskApi } from '@/api/export'
const { push } = useRouter()
const searchicon = useIcon({ icon: 'iconoir:search' })
const { t } = useI18n()
// 搜索词记忆：切走再回来不用重输（sessionStorage，同一标签页内有效）
const SEARCH_KEY = 'task:search'
const search = ref(sessionStorage.getItem(SEARCH_KEY) || '')
// 状态筛选：-1=全部。任务堆积时（几十个暂停任务）只靠名字搜很难找
const STATUS_KEY = 'task:status'
const statusFilter = ref<number>(Number(sessionStorage.getItem(STATUS_KEY) ?? -1))
// 下拉一改就重新查询：之前只有点「搜索」才生效，用户会以为筛选没反应
watch(statusFilter, (v) => {
  sessionStorage.setItem(STATUS_KEY, String(v ?? -1))
  currentPage.value = 1
  getList()
})
const handleSearch = () => {
  sessionStorage.setItem(SEARCH_KEY, search.value || '')
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
    showOverflowTooltip: true
  },
  {
    field: 'taskNum',
    label: t('task.taskCount'),
    width: 90,
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
        return h('div', { class: 'progress-cell' }, [
          bar,
          h('div', { class: 'progress-sub queued' }, t('task.queuedHint'))
        ])
      }
      // 本次任务期间外部工具失败：直接标在进度下方，产出可信度一眼可见
      const cmdFail = (row.cmdFail as number) || 0
      if (cmdFail > 0) {
        return h('div', { class: 'progress-cell' }, [
          bar,
          h('div', { class: 'progress-sub fail' }, t('task.cmdFailHint', { n: cmdFail }))
        ])
      }
      if (row.status !== 1 || !mod) {
        return bar
      }
      let elapsed = ''
      if (row.currentSince) {
        const t0 = new Date(String(row.currentSince).replace(/-/g, '/')).getTime()
        const mins = Math.floor((Date.now() - t0) / 60000)
        if (mins >= 1) {
          // 超过 1 小时还没结束的模块基本是停滞，直接标注而不是显示 480 分钟
          elapsed = mins > 60 ? ' · ' + t('task.moduleStalled') : ' · ' + mins + t('task.minutesShort')
        }
      }
      return h('div', { class: 'progress-cell' }, [
        bar,
        h('div', { class: 'progress-sub' }, mod + ' @ ' + (row.currentTarget || '-') + elapsed)
      ])
    }
  },
  {
    field: 'status',
    label: t('common.state'),
    width: 110,
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      // 0 待开始，1 运行中，2 暂停，3 运行完成，4 失败
      let tagType, tagText
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
    label: t('task.createTime')
  },
  {
    field: 'endTime',
    label: t('task.endTime'),
    width: 152,
    formatter: (_: Recordable, __: TableColumn, cellValue: string) => {
      if (cellValue == '') {
        return '-'
      }
      return cellValue
    }
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    /* 操作列固定在右侧：横向空间不足时用户仍能直接点到按钮，
       不必拖滚动条。其余列宽收紧，总宽控制在 1280 内尽量不出现横向滚动。 */
    width: 268,
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
        }
      }
      const retestAndDeleteDropdown = h(
        ElDropdown,
        {
          onCommand: handleCommand
        },
        {
          default: () =>
            h(
              ElButton,
              {
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
                  h(ElDropdownItem, { command: 'retest' }, () => t('task.retest')),
                  h(ElDropdownItem, { command: 'delete' }, () => t('common.delete'))
                ]
              } else {
                return [
                  h(ElDropdownItem, { command: 'start' }, () => t('task.start')),
                  h(ElDropdownItem, { command: 'stop' }, () => t('task.stop')), // 如果是运行中，显示“停止”按钮
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
          <ElButton

            type="info"

            link

            loading={exportingId.value === row.id}

            onClick={() => exportTask(row)}

          >

            {t('task.exportTask')}

          </ElButton>
          <ElButton
            type="warning"
            loading={rowActionId.value === row.id}
            onClick={() => getProgressInfo(row.id)}
          >
            {t('task.taskProgress')}
          </ElButton>
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
const exportTask = async (row: any) => {
  if (!row?.id) return
  exportingId.value = row.id
  try {
    const blob = await exportTaskApi(row.id)
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'task-' + (row.name || row.id) + '.zip'
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

const getProgressInfo = async (id) => {
  // 弹窗打开后立刻解除按钮 loading：真正的加载状态由弹窗内部的表格承担，
  // 这里只负责防止连点触发多次 open。
  rowActionId.value = id || ''
  getProgressInfoID = id
  const row = (dataList.value || []).find((r: any) => r.id === id) as any
  progressCmdFail.value = (row?.cmdFail as number) || 0
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
  // 成功/失败都给明确反馈：此前点「开始」只有列表静默刷新，用户不知道有没有生效
  try {
    await starTaskApi(ids)
    ElMessage.success(t('task.startSubmitted'))
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
    const res = await getTaskDataApi(
      search.value,
      currentPage.value,
      pageSize.value,
      statusFilter.value >= 0 ? statusFilter.value : undefined
    )
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
const loadNodeHealth = async () => {
  try {
    // 直接用节点列表接口（与节点管理页同源、已验证可用），按 state 统计在线数
    const res: any = await getNodeDataApi()
    const raw = res?.data?.list ?? res?.data ?? res?.list ?? []
    const list: any[] = Array.isArray(raw) ? raw : []
    nodeTotal.value = list.length
    nodeOnline.value = list.filter((n) => String(n?.state) === '1').length
  } catch {
    // 静默：不打扰主流程
  }
}

// 列表自动刷新：有任务在跑时每 10 秒静默刷新一次，
// 用户不用反复点刷新按钮也能看到进度变化；全部结束后自动停止。
const autoListTimer = ref<ReturnType<typeof setInterval> | null>(null)
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
      try {
        await rawGetList()
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
onUnmounted(() => {
  if (autoListTimer.value) clearInterval(autoListTimer.value)
})
// 每次刷新列表后重置选择状态，避免"按钮可点但选中为空"
const getList = async () => {
  const r = await rawGetList()
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
              showCheckbox: true,
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
    const res = await deleteTaskApi([data.id], delA)
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
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
    const res = await deleteTaskApi(ids.value, delA)
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
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
    <ElRow class="locale-search-row">
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
        v-if="nodeTotal > 0 && nodeOnline < nodeTotal"
        type="warning"
        :closable="false"
        show-icon
        style="margin-bottom: 10px"
        :title="t('task.nodeOfflineWarn') + ': ' + nodeOnline + '/' + nodeTotal"
      />
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
            <div class="empty-title">{{ t('task.emptyTitle') }}</div>
            <div class="empty-desc">{{ t('task.emptyDesc') }}</div>
            <ElButton type="primary" @click="addTask">{{ t('task.addTask') }}</ElButton>
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
      :closeDialog="progresscloseDialog"
      :getProgressInfoID="getProgressInfoID"
      getProgressInfotype="scan"
      getProgressInforunnerid=""
      :cmdFail="progressCmdFail"
    /></Dialog>
</template>

<style scoped>
.progress-cell {
  line-height: 1.2;
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
