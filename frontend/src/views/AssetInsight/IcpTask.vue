<script setup lang="tsx">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  ElAlert,
  ElButton,
  ElCol,
  ElDescriptions,
  ElDescriptionsItem,
  ElDialog,
  ElDrawer,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElPagination,
  ElRadio,
  ElRadioGroup,
  ElRow,
  ElSelect,
  ElTag
} from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import {
  checkIcpProviderApi,
  createIcpTaskApi,
  deleteIcpTasksApi,
  exportIcpTaskCsvApi,
  getIcpAssetListApi,
  getIcpTaskListApi,
  getIcpTaskLogApi,
  getIcpTaskOverviewApi,
  getIcpTaskStatisticsApi,
  restartIcpTaskApi,
  resumeIcpTaskApi,
  stopIcpTasksApi,
  syncIcpTaskApi
} from '@/api/icp'
import type {
  IcpProviderStatusResult,
  IcpResultRow,
  IcpTask,
  IcpTaskLogLine,
  IcpTaskStatistics,
  IcpTaskType
} from '@/api/icp/types'
import { getAssetGroupListApi } from '@/api/assetGroup'
import type { AssetGroup } from '@/api/assetGroup/types'
import { ICP_TASK_STATES, icpProgressPercent, icpStateMeta, parseIcpQueries } from '@/utils/icpTask'
import { timeText } from '@/utils/timeText'

// ICP 企业主体 ↔ 域名查询任务链（对标 ARL-Next icp.py）。
// 任务跑在后台，状态靠 5s 轮询；暂停后可以 resume 从 cursor 继续，restart 则重跑整轮。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })
const refreshIcon = useIcon({ icon: 'iconoir:refresh' })
const plusIcon = useIcon({ icon: 'iconoir:plus' })

const keyword = ref('')
const typeFilter = ref('')
const stateFilter = ref<number | undefined>(undefined)

// ---- provider 状态（tyc_check）----
const providerStatus = ref<IcpProviderStatusResult | null>(null)
const providerLoading = ref(false)

const loadProvider = async () => {
  providerLoading.value = true
  try {
    const res = await checkIcpProviderApi()
    providerStatus.value = res?.data ?? null
  } catch {
    providerStatus.value = null
  } finally {
    providerLoading.value = false
  }
}

// ---- 各状态任务数 ----
const overview = ref<Record<string, number>>({})

const loadOverview = async () => {
  try {
    const res = await getIcpTaskOverviewApi()
    overview.value = res?.data ?? {}
  } catch {
    overview.value = {}
  }
}

const columns = reactive<TableColumn[]>([
  {
    field: 'name',
    label: t('icpTask.name'),
    minWidth: 220,
    formatter: (row: IcpTask) => (
      <ElButton link type="primary" onClick={() => openDetail(row)}>
        {row.name || '-'}
      </ElButton>
    )
  },
  {
    field: 'type',
    label: t('icpTask.type'),
    width: 110,
    formatter: (row: IcpTask) =>
      row.type === 'company' ? t('icpTask.typeCompany') : t('icpTask.typeDomain')
  },
  {
    field: 'state',
    label: t('icpTask.state'),
    width: 110,
    formatter: (row: IcpTask) => {
      const meta = icpStateMeta(row.state)
      return (
        <ElTag type={meta.tagType} size="small">
          {t(meta.labelKey)}
        </ElTag>
      )
    }
  },
  {
    field: 'progress',
    label: t('icpTask.progress'),
    minWidth: 150,
    formatter: (row: IcpTask) => {
      const done = row.stats?.done ?? 0
      const total = row.stats?.total ?? 0
      return `${done}/${total} (${icpProgressPercent(done, total)}%)`
    }
  },
  {
    field: 'found',
    label: t('icpTask.found'),
    width: 100,
    formatter: (row: IcpTask) => row.stats?.found ?? 0
  },
  {
    field: 'failed',
    label: t('icpTask.failed'),
    width: 100,
    formatter: (row: IcpTask) => row.stats?.failed ?? 0
  },
  { field: 'provider', label: t('icpTask.provider'), width: 110 },
  {
    field: 'updatedAt',
    label: t('icpTask.updatedAt'),
    minWidth: 170,
    formatter: (row: IcpTask) => timeText(row.updatedAt)
  },
  {
    field: 'actions',
    label: t('icpTask.actions'),
    width: 260,
    fixed: 'right',
    formatter: (row: IcpTask) => (
      <span class="row-actions">
        <ElButton link type="primary" size="small" onClick={() => restartTask(row)}>
          {t('icpTask.restart')}
        </ElButton>
        {row.state === 2 ? (
          <ElButton link type="warning" size="small" onClick={() => resumeTask(row)}>
            {t('icpTask.resume')}
          </ElButton>
        ) : null}
        {row.state === 1 ? (
          <ElButton link type="warning" size="small" onClick={() => stopTask(row)}>
            {t('icpTask.stop')}
          </ElButton>
        ) : null}
        <ElButton link type="primary" size="small" onClick={() => exportTask(row)}>
          {t('icpTask.export')}
        </ElButton>
        <ElButton link type="danger" size="small" onClick={() => deleteTask(row)}>
          {t('icpTask.delete')}
        </ElButton>
      </span>
    )
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getIcpTaskListApi({
      keyword: keyword.value.trim(),
      type: typeFilter.value || undefined,
      state: stateFilter.value,
      pageIndex: currentPage.value,
      pageSize: pageSize.value
    })
    return { list: res?.data?.list ?? [], total: res?.data?.total ?? 0 }
  }
})

const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList } = tableMethods

const handleSearch = () => {
  // currentPage 变化时 useTable 会自己拉一次，只有已经在第一页时才手动拉，避免重复请求
  if (currentPage.value !== 1) {
    currentPage.value = 1
    return
  }
  getList()
}

const handleReset = () => {
  keyword.value = ''
  typeFilter.value = ''
  stateFilter.value = undefined
  handleSearch()
}

const refreshAll = () => {
  getList()
  loadOverview()
}

// ---- 新建任务 ----
const createVisible = ref(false)
const createLoading = ref(false)
const createForm = reactive({
  name: '',
  type: 'domain' as IcpTaskType,
  queriesText: '',
  provider: ''
})

const openCreate = () => {
  createForm.name = ''
  createForm.type = 'domain'
  createForm.queriesText = ''
  createForm.provider = ''
  createVisible.value = true
}

const submitCreate = async () => {
  const queries = parseIcpQueries(createForm.queriesText)
  if (!queries.length) {
    ElMessage.warning(t('icpTask.queriesRequired'))
    return
  }
  createLoading.value = true
  try {
    await createIcpTaskApi({
      name: createForm.name.trim() || undefined,
      type: createForm.type,
      queries,
      provider: createForm.provider || undefined
    })
    ElMessage.success(t('icpTask.createSuccess'))
    createVisible.value = false
    refreshAll()
  } catch {
    // 拦截器已提示
  } finally {
    createLoading.value = false
  }
}

// ---- 任务操作 ----
const confirmAction = async (message: string) => {
  return ElMessageBox.confirm(message, t('icpTask.confirmTitle'), {
    type: 'warning',
    confirmButtonText: t('common.ok'),
    cancelButtonText: t('common.cancel')
  })
    .then(() => true)
    .catch(() => false)
}

const restartTask = async (task: IcpTask) => {
  if (!(await confirmAction(t('icpTask.confirmRestart', { name: task.name })))) return
  try {
    await restartIcpTaskApi(task.id)
    ElMessage.success(t('icpTask.restartSuccess'))
    refreshAll()
    if (drawerVisible.value && currentTask.value?.id === task.id) {
      await loadStats()
      await loadLogs()
      startPoll()
    }
  } catch {
    // 拦截器已提示
  }
}

const stopTask = async (task: IcpTask) => {
  if (!(await confirmAction(t('icpTask.confirmStop', { name: task.name })))) return
  try {
    await stopIcpTasksApi([task.id])
    ElMessage.success(t('icpTask.stopSuccess'))
    refreshAll()
    if (drawerVisible.value && currentTask.value?.id === task.id) {
      await loadStats()
      await loadLogs()
    }
  } catch {
    // 拦截器已提示
  }
}

const resumeTask = async (task: IcpTask) => {
  if (!(await confirmAction(t('icpTask.confirmResume', { name: task.name })))) return
  try {
    await resumeIcpTaskApi(task.id)
    ElMessage.success(t('icpTask.resumeSuccess'))
    refreshAll()
    if (drawerVisible.value && currentTask.value?.id === task.id) {
      startPoll()
      await loadStats()
      await loadLogs()
    }
  } catch {
    // 拦截器已提示
  }
}

const deleteTask = async (task: IcpTask) => {
  if (!(await confirmAction(t('icpTask.confirmDelete', { name: task.name })))) return
  try {
    await deleteIcpTasksApi([task.id])
    ElMessage.success(t('icpTask.deleteSuccess'))
    if (currentTask.value?.id === task.id) {
      drawerVisible.value = false
      currentTask.value = null
    }
    refreshAll()
  } catch {
    // 拦截器已提示
  }
}

const exporting = ref('')

const exportTask = async (task: IcpTask) => {
  exporting.value = task.id
  try {
    const blob = await exportIcpTaskCsvApi(task.id)
    // 后端会先写 UTF-8 BOM，所以「只有表头」的 CSV 大小是 3 字节
    if (!blob || blob.size <= 3) {
      ElMessage.warning(t('icpTask.exportEmpty'))
      return
    }
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `icp_results_${task.id}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url)
  } catch {
    // 拦截器已提示
  } finally {
    exporting.value = ''
  }
}

// ---- 详情抽屉 ----
const drawerVisible = ref(false)
const currentTask = ref<IcpTask | null>(null)
const stats = ref<IcpTaskStatistics | null>(null)
const statsLoading = ref(false)
const logs = ref<IcpTaskLogLine[]>([])
const logLoading = ref(false)
const results = ref<IcpResultRow[]>([])
const resultTotal = ref(0)
const resultPage = ref(1)
const resultPageSize = ref(20)
const resultLoading = ref(false)

const logText = computed(() =>
  logs.value
    .map((item) => `[${timeText(item.createdAt)}] ${item.level.toUpperCase()} ${item.line}`)
    .join('\n')
)

const resultColumns = reactive<TableColumn[]>([
  { field: 'domain', label: t('icpTask.resultDomain'), minWidth: 220 },
  { field: 'company', label: t('icpTask.resultCompany'), minWidth: 220 },
  { field: 'icpNo', label: t('icpTask.resultIcpNo'), minWidth: 180 },
  { field: 'siteName', label: t('icpTask.resultSiteName'), minWidth: 160 },
  { field: 'type', label: t('icpTask.resultType'), width: 100 },
  { field: 'source', label: t('icpTask.resultSource'), width: 110 },
  {
    field: 'createdAt',
    label: t('icpTask.resultCreatedAt'),
    minWidth: 170,
    formatter: (row: IcpResultRow) => timeText(row.createdAt)
  }
])

const loadStats = async () => {
  if (!currentTask.value) return
  statsLoading.value = true
  try {
    const res = await getIcpTaskStatisticsApi(currentTask.value.id)
    stats.value = res?.data ?? null
    if (stats.value && typeof stats.value.state === 'number') {
      currentTask.value = { ...currentTask.value, state: stats.value.state }
    }
  } catch {
    // 错误提示由 axios 拦截器统一给出，这里只保持上一次的统计展示
  } finally {
    statsLoading.value = false
  }
}

const loadLogs = async () => {
  if (!currentTask.value) return
  logLoading.value = true
  try {
    const res = await getIcpTaskLogApi({ taskId: currentTask.value.id, limit: 200 })
    logs.value = res?.data?.lines ?? []
  } catch {
    logs.value = []
  } finally {
    logLoading.value = false
  }
}

const loadResults = async () => {
  if (!currentTask.value) return
  resultLoading.value = true
  try {
    const res = await getIcpAssetListApi({
      taskId: currentTask.value.id,
      pageIndex: resultPage.value,
      pageSize: resultPageSize.value
    })
    results.value = res?.data?.list ?? []
    resultTotal.value = res?.data?.total ?? 0
  } catch {
    results.value = []
    resultTotal.value = 0
  } finally {
    resultLoading.value = false
  }
}

const openDetail = async (task: IcpTask) => {
  currentTask.value = { ...task }
  drawerVisible.value = true
  resultPage.value = 1
  await Promise.all([loadStats(), loadLogs(), loadResults()])
  startPoll()
}

const onResultPageChange = (page: number) => {
  resultPage.value = page
  loadResults()
}

const onResultSizeChange = (size: number) => {
  resultPageSize.value = size
  resultPage.value = 1
  loadResults()
}

// 运行中/等待中的任务每 5s 刷一次统计与日志；到终态自动停。
let pollTimer: number | undefined

const stopPoll = () => {
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer)
    pollTimer = undefined
  }
}

const startPoll = () => {
  stopPoll()
  pollTimer = window.setInterval(async () => {
    if (!drawerVisible.value || !currentTask.value) {
      stopPoll()
      return
    }
    if (currentTask.value.state !== 0 && currentTask.value.state !== 1) {
      stopPoll()
      return
    }
    await Promise.all([loadStats(), loadLogs()])
  }, 5000)
}

const onDrawerClosed = () => {
  stopPoll()
  refreshAll()
}

onBeforeUnmount(stopPoll)

// ---- 同步到资产组 ----
const syncVisible = ref(false)
const syncLoading = ref(false)
const syncGroupId = ref('')
const groups = ref<AssetGroup[]>([])
const groupsLoading = ref(false)

const openSync = async () => {
  syncVisible.value = true
  syncGroupId.value = ''
  if (groups.value.length) return
  groupsLoading.value = true
  try {
    const res = await getAssetGroupListApi()
    groups.value = res?.data?.items ?? []
  } catch {
    groups.value = []
  } finally {
    groupsLoading.value = false
  }
}

const submitSync = async () => {
  if (!currentTask.value) return
  syncLoading.value = true
  try {
    const res = await syncIcpTaskApi({
      taskId: currentTask.value.id,
      groupId: syncGroupId.value
    })
    const summary = res?.data
    ElMessage.success(
      t('icpTask.syncSuccess', {
        scope: summary?.scopeName ?? '',
        added: summary?.added ?? 0,
        total: summary?.total ?? 0
      })
    )
    syncVisible.value = false
    await loadLogs()
    await loadStats()
  } catch {
    // 拦截器已提示
  } finally {
    syncLoading.value = false
  }
}

onMounted(() => {
  loadProvider()
  loadOverview()
})
</script>

<template>
  <ContentWrap :title="t('icpTask.title')" :message="t('icpTask.desc')">
    <template #header>
      <ElButton type="primary" :icon="plusIcon" @click="openCreate">
        {{ t('icpTask.create') }}
      </ElButton>
      <ElButton :icon="refreshIcon" :loading="providerLoading" @click="loadProvider">
        {{ t('icpTask.checkProvider') }}
      </ElButton>
    </template>

    <div class="status-bar">
      <span class="provider-label">{{ t('icpTask.provider') }}:</span>
      <ElTag
        v-if="providerStatus"
        :type="providerStatus.available ? 'success' : 'warning'"
        size="small"
      >
        {{ providerStatus.provider }}
        {{
          providerStatus.available
            ? t('icpTask.providerAvailable')
            : t('icpTask.providerUnavailable')
        }}
      </ElTag>
      <span v-if="providerStatus && !providerStatus.configured" class="provider-hint">
        {{ t('icpTask.providerNotConfigured') }}
      </span>
      <span class="overview">
        <ElTag size="small">{{ t('icpTask.overviewTotal') }} {{ overview.total ?? 0 }}</ElTag>
        <ElTag size="small" type="info">
          {{ t('icpTask.stateWaiting') }} {{ overview.waiting ?? 0 }}
        </ElTag>
        <ElTag size="small" type="primary">
          {{ t('icpTask.stateRunning') }} {{ overview.running ?? 0 }}
        </ElTag>
        <ElTag size="small" type="warning">
          {{ t('icpTask.statePaused') }} {{ overview.paused ?? 0 }}
        </ElTag>
        <ElTag size="small" type="success">
          {{ t('icpTask.stateDone') }} {{ overview.done ?? 0 }}
        </ElTag>
      </span>
    </div>

    <ElAlert
      v-if="providerStatus && !providerStatus.available"
      type="warning"
      :closable="false"
      show-icon
      class="provider-alert"
      :title="t('icpTask.providerUnavailableHint', { reason: providerStatus.reason || '-' })"
    />

    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="5">
        <ElInput
          v-model="keyword"
          :placeholder="t('icpTask.keywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="3">
        <ElSelect
          v-model="typeFilter"
          clearable
          :placeholder="t('icpTask.typeAll')"
          class="full-width"
        >
          <ElOption :label="t('icpTask.typeCompany')" value="company" />
          <ElOption :label="t('icpTask.typeDomain')" value="domain" />
        </ElSelect>
      </ElCol>
      <ElCol :span="3">
        <ElSelect
          v-model="stateFilter"
          clearable
          :placeholder="t('icpTask.stateAll')"
          class="full-width"
        >
          <ElOption
            v-for="item in ICP_TASK_STATES"
            :key="item.value"
            :label="t(item.labelKey)"
            :value="item.value"
          />
        </ElSelect>
      </ElCol>
      <ElCol :span="8">
        <ElButton type="primary" :icon="searchIcon" @click="handleSearch">
          {{ t('common.search') }}
        </ElButton>
        <ElButton @click="handleReset">{{ t('common.reset') }}</ElButton>
      </ElCol>
    </ElRow>

    <Table
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      :columns="columns"
      :data="dataList"
      stripe
      :border="true"
      :loading="loading"
      :resizable="true"
      :pagination="{ total, pageSizes: [10, 20, 50, 100, 200] }"
      @register="tableRegister"
    >
      <template #empty>
        <div class="empty-state">{{ t('icpTask.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>

  <ElDialog v-model="createVisible" :title="t('icpTask.createTitle')" width="620px">
    <ElForm label-width="110px">
      <ElFormItem :label="t('icpTask.type')">
        <ElRadioGroup v-model="createForm.type">
          <ElRadio value="domain">{{ t('icpTask.typeDomain') }}</ElRadio>
          <ElRadio value="company">{{ t('icpTask.typeCompany') }}</ElRadio>
        </ElRadioGroup>
      </ElFormItem>
      <ElFormItem :label="t('icpTask.queries')">
        <ElInput
          v-model="createForm.queriesText"
          type="textarea"
          :rows="6"
          :placeholder="
            createForm.type === 'company'
              ? t('icpTask.queriesPlaceholderCompany')
              : t('icpTask.queriesPlaceholderDomain')
          "
        />
        <div class="form-hint">{{ t('icpTask.queriesHint') }}</div>
      </ElFormItem>
      <ElFormItem :label="t('icpTask.name')">
        <ElInput v-model="createForm.name" :placeholder="t('icpTask.namePlaceholder')" />
      </ElFormItem>
      <ElFormItem :label="t('icpTask.provider')">
        <ElSelect
          v-model="createForm.provider"
          :placeholder="t('icpTask.providerAuto')"
          class="full-width"
        >
          <ElOption :label="t('icpTask.providerAuto')" value="" />
          <ElOption label="manual" value="manual" />
          <ElOption label="http" value="http" />
          <ElOption label="none" value="none" />
        </ElSelect>
      </ElFormItem>
    </ElForm>
    <template #footer>
      <ElButton @click="createVisible = false">{{ t('common.cancel') }}</ElButton>
      <ElButton type="primary" :loading="createLoading" @click="submitCreate">
        {{ t('icpTask.submit') }}
      </ElButton>
    </template>
  </ElDialog>

  <ElDrawer
    v-model="drawerVisible"
    :title="currentTask?.name || t('icpTask.detailTitle')"
    size="75%"
    direction="rtl"
    @closed="onDrawerClosed"
  >
    <template v-if="currentTask">
      <div class="detail-actions">
        <ElButton size="small" @click="restartTask(currentTask)">
          {{ t('icpTask.restart') }}
        </ElButton>
        <ElButton v-if="currentTask.state === 1" size="small" @click="stopTask(currentTask)">
          {{ t('icpTask.stop') }}
        </ElButton>
        <ElButton v-if="currentTask.state === 2" size="small" @click="resumeTask(currentTask)">
          {{ t('icpTask.resume') }}
        </ElButton>
        <ElButton size="small" type="primary" @click="openSync">
          {{ t('icpTask.sync') }}
        </ElButton>
        <ElButton
          size="small"
          :loading="exporting === currentTask.id"
          @click="exportTask(currentTask)"
        >
          {{ t('icpTask.export') }}
        </ElButton>
        <ElButton size="small" :loading="logLoading" @click="loadLogs">
          {{ t('icpTask.refreshLog') }}
        </ElButton>
      </div>

      <ElDescriptions v-loading="statsLoading" :column="2" border>
        <ElDescriptionsItem :label="t('icpTask.state')">
          <ElTag :type="icpStateMeta(currentTask.state).tagType" size="small">
            {{ t(icpStateMeta(currentTask.state).labelKey) }}
          </ElTag>
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('icpTask.type')">
          {{ currentTask.type === 'company' ? t('icpTask.typeCompany') : t('icpTask.typeDomain') }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('icpTask.provider')">
          {{ currentTask.provider || '-' }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('icpTask.progress')">
          {{ currentTask.stats?.done ?? 0 }}/{{ currentTask.stats?.total ?? 0 }} ({{
            icpProgressPercent(currentTask.stats?.done, currentTask.stats?.total)
          }}%)
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('icpTask.statsTotal')">
          {{ stats?.total ?? currentTask.stats?.total ?? 0 }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('icpTask.found')">
          {{ stats?.found ?? currentTask.stats?.found ?? 0 }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('icpTask.failed')">
          {{ stats?.failed ?? currentTask.stats?.failed ?? 0 }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('icpTask.resultCount')">
          {{ stats?.results ?? resultTotal }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('icpTask.createdAt')">
          {{ timeText(currentTask.createdAt) }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('icpTask.updatedAt')">
          {{ timeText(currentTask.updatedAt) }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('icpTask.queries')" :span="2">
          {{ (currentTask.queries ?? []).join(' , ') || '-' }}
        </ElDescriptionsItem>
        <ElDescriptionsItem v-if="currentTask.lastError" :label="t('icpTask.lastError')" :span="2">
          <span class="error-text">{{ currentTask.lastError }}</span>
        </ElDescriptionsItem>
      </ElDescriptions>

      <div class="section-title">
        {{ t('icpTask.log') }}
        <span class="section-hint">{{ t('icpTask.logLimit', { limit: 200 }) }}</span>
      </div>
      <div v-loading="logLoading">
        <pre v-if="logText" class="log-panel">{{ logText }}</pre>
        <div v-else class="empty-state log-empty">{{ t('icpTask.logEmpty') }}</div>
      </div>

      <div class="section-title">{{ t('icpTask.results') }}</div>
      <Table
        :columns="resultColumns"
        :data="results"
        stripe
        :border="true"
        :loading="resultLoading"
        :resizable="true"
      >
        <template #empty>
          <div class="empty-state">{{ t('icpTask.resultsEmpty') }}</div>
        </template>
      </Table>
      <ElPagination
        class="mt-3"
        layout="sizes, prev, pager, next, total"
        :page-sizes="[10, 20, 50, 100, 200]"
        :total="resultTotal"
        :page-size="resultPageSize"
        :current-page="resultPage"
        @current-change="onResultPageChange"
        @size-change="onResultSizeChange"
      />
    </template>
  </ElDrawer>

  <ElDialog v-model="syncVisible" :title="t('icpTask.syncTitle')" width="480px">
    <ElForm label-width="100px">
      <ElFormItem :label="t('icpTask.syncGroup')">
        <ElSelect v-model="syncGroupId" clearable :loading="groupsLoading" class="full-width">
          <ElOption :label="t('icpTask.syncGroupUngrouped')" value="" />
          <ElOption v-for="item in groups" :key="item.id" :label="item.name" :value="item.id" />
        </ElSelect>
        <div class="form-hint">{{ t('icpTask.syncGroupHint') }}</div>
      </ElFormItem>
    </ElForm>
    <template #footer>
      <ElButton @click="syncVisible = false">{{ t('common.cancel') }}</ElButton>
      <ElButton type="primary" :loading="syncLoading" @click="submitSync">
        {{ t('icpTask.sync') }}
      </ElButton>
    </template>
  </ElDialog>
</template>

<style scoped>
.status-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-bottom: 12px;
}

.provider-label {
  color: var(--el-text-color-secondary);
}

.provider-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.overview {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-left: auto;
}

.provider-alert {
  margin-bottom: 12px;
}

.filter-row {
  margin-bottom: 15px;
}

.full-width {
  width: 100%;
}

.form-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
}

.empty-state {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}

.row-actions {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 2px;
}

.detail-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
}

.section-title {
  display: flex;
  gap: 10px;
  align-items: baseline;
  margin: 22px 0 10px;
  font-weight: 600;
}

.section-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-weight: 400;
}

.log-panel {
  max-height: 260px;
  margin: 0;
  padding: 12px;
  overflow: auto;
  font-family: var(--el-font-family-mono, monospace);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  background: var(--el-fill-color-light);
  border-radius: 4px;
}

.log-empty {
  border: 1px dashed var(--el-border-color);
  border-radius: 4px;
}

.error-text {
  color: var(--el-color-danger);
  word-break: break-all;
}
</style>
