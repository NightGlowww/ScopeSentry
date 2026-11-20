<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { onMounted, reactive, ref } from 'vue'
import {
  ElButton,


  ElInput,
  ElSelect,
  ElOption,
  ElDatePicker,
  ElTag,
  ElCard,
  ElSwitch,
  ElInputNumber,
  ElAlert,
  ElMessage
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { getMcpLogsApi, exportMcpLogsApi, getMcpConfigApi, updateMcpConfigApi } from '@/api/mcp'
import { useUserStore } from '@/store/modules/user'
import { formatTime } from '@/utils'

const { t } = useI18n()
const userStore = useUserStore()

const statusOptions = [
  { value: 'success', label: 'success' },
  { value: 'denied', label: 'denied' },
  { value: 'error', label: 'error' }
]

const filters = reactive({
  keyId: '',
  tool: '',
  status: '',
  range: [] as string[]
})

// ---- 全局配置 ----
const cfgLoading = ref(false)
const cfgSaving = ref(false)
const cfg = reactive({
  enabled: true,
  rateLimitPerMin: 0,
  logRetention: 100000,
  allowedTools: [] as string[]
})

const loadConfig = async () => {
  cfgLoading.value = true
  try {
    const res = await getMcpConfigApi()
    const data = res.data
    cfg.enabled = data.enabled
    cfg.rateLimitPerMin = data.rateLimitPerMin || 0
    cfg.logRetention = data.logRetention || 100000
    cfg.allowedTools = data.allowedTools || []
  } catch (e: any) {
    // 配置读取失败此前静默：页面显示默认值，用户会以为配置就是空的
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    cfgLoading.value = false
  }
}

const saveConfig = async () => {
  cfgSaving.value = true
  try {
    await updateMcpConfigApi({
      enabled: cfg.enabled,
      rateLimitPerMin: cfg.rateLimitPerMin,
      logRetention: cfg.logRetention,
      allowedTools: cfg.allowedTools
    })
    ElMessage.success('已保存')
    loadConfig()
  } finally {
    cfgSaving.value = false
  }
}

onMounted(loadConfig)

const formatDateTime = (value?: string) => {
  if (!value) return '-'
  return formatTime(value, 'yyyy-MM-dd HH:mm:ss')
}

const statusTag = (status: string) => {
  const type = status === 'success' ? 'success' : status === 'denied' ? 'warning' : 'danger'
  return (
    <ElTag type={type} effect="light">
      {status}
    </ElTag>
  )
}

const columns = reactive<TableColumn[]>([
  {
    field: 'createdAt',
    label: '时间',
    minWidth: 170,
    formatter: (_row, __: TableColumn, value: string) => formatDateTime(value)
  },
  {
    field: 'tool',
    label: '工具',
    minWidth: 160
  },
  {
    field: 'status',
    label: '状态',
    width: 100,
    formatter: (_row, __: TableColumn, value: string) => statusTag(value)
  },
  {
    field: 'durationMs',
    label: '耗时(ms)',
    width: 100
  },
  {
    field: 'keyPrefix',
    label: 'Key 前缀',
    minWidth: 140
  },
  {
    field: 'clientIp',
    label: '客户端 IP',
    minWidth: 130
  },
  {
    field: 'error',
    label: '错误',
    minWidth: 200
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getMcpLogsApi({
      keyId: filters.keyId || undefined,
      tool: filters.tool || undefined,
      status: filters.status || undefined,
      from: filters.range?.[0] || undefined,
      to: filters.range?.[1] || undefined,
      pageIndex: currentPage.value,
      pageSize: pageSize.value
    })
    return {
      list: res.data.list || [],
      total: res.data.total || 0
    }
  }
})

const { loading, dataList, total, currentPage, pageSize } = tableState
pageSize.value = 20
const { getList } = tableMethods

// 最近 24 小时被拒情况：403/429 只能靠审计页发现，但没人会主动去看，
// 所以在页头直接给出「有 N 次被拒 + 最主要原因」（2026-11-20）。
// 1 小时窗口：24 小时太钝，「现在出事了」要的是最近这一小时。
const deniedCount = ref(0)
const deniedReason = ref('')
const deniedChecked = ref(false)
const loadDeniedSummary = async () => {
  try {
    const from = new Date(Date.now() - 3600 * 1000).toISOString()
    const res: any = await getMcpLogsApi({ status: 'denied', from, pageIndex: 1, pageSize: 5 })
    deniedCount.value = res?.data?.total || 0
    const first = (res?.data?.list || []).find((x: any) => x?.error)
    deniedReason.value = first?.error || ''
  } catch {
    deniedCount.value = 0
  } finally {
    deniedChecked.value = true
  }
}
onMounted(loadDeniedSummary)

const handleSearch = () => {
  currentPage.value = 1
  getList()
}

const handleReset = () => {
  filters.keyId = ''
  filters.tool = ''
  filters.status = ''
  filters.range = []
  currentPage.value = 1
  getList()
}

const handleExport = async () => {
  const url = exportMcpLogsApi({
    keyId: filters.keyId || undefined,
    tool: filters.tool || undefined,
    status: filters.status || undefined,
    from: filters.range?.[0] || undefined,
    to: filters.range?.[1] || undefined
  })
  const resp = await fetch(url, {
    headers: { Authorization: userStore.getToken || '' }
  })
  const blob = await resp.blob()
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = 'mcp-call-logs.csv'
  link.click()
  URL.revokeObjectURL(link.href)
}
</script>

<template>
  <ContentWrap>
    <ElAlert
      v-if="deniedCount > 0"
      type="warning"
      :closable="false"
      show-icon
      class="mb-12px"
      :title="`最近 1 小时有 ${deniedCount} 次调用被拒绝` + (deniedReason ? ' · ' + deniedReason : '')"
    >
      <ElButton link type="primary" size="small" @click="filters.status = 'denied'; handleSearch()">
        只看被拒的
      </ElButton>
    </ElAlert>
    <!-- 没黄条不代表「没人查过」：给一条明确的绿色正常态，避免把沉默当成没问题 -->
    <div v-else-if="deniedChecked" class="mcp-ok">
      <ElTag type="success" size="small" effect="plain">
        {{ '最近 1 小时没有被拒的调用' }}
      </ElTag>
    </div>
    <ElCard class="mb-12px" shadow="never" v-loading="cfgLoading">
      <template #header>
        <span>全局配置</span>
      </template>
      <div class="cfg-grid">
        <div class="cfg-field">
          <span class="cfg-label">启用 MCP</span>
          <ElSwitch v-model="cfg.enabled" />
        </div>
        <div class="cfg-field">
          <span class="cfg-label">全局限速/分</span>
          <ElInputNumber
            v-model="cfg.rateLimitPerMin"
            :min="0"
            :max="100000"
            controls-position="right"
            class="cfg-number"
          />
        </div>
        <div class="cfg-field">
          <span class="cfg-label">日志保留条数</span>
          <ElInputNumber
            v-model="cfg.logRetention"
            :min="1000"
            :max="1000000"
            :step="10000"
            controls-position="right"
            class="cfg-number"
          />
        </div>
        <div class="cfg-field cfg-field-wide">
          <span class="cfg-label">工具白名单</span>
          <ElSelect
            v-model="cfg.allowedTools"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="留空 = 全部工具"
            style="width: 100%"
          />
        </div>
        <div class="cfg-field cfg-actions">
          <ElButton type="primary" :loading="cfgSaving" @click="saveConfig">保存配置</ElButton>
        </div>
      </div>
    </ElCard>

    <div class="filter-grid">
      <ElInput v-model="filters.keyId" placeholder="Key ID" clearable />
      <ElInput v-model="filters.tool" placeholder="工具名，如 list_assets" clearable />
      <ElSelect v-model="filters.status" placeholder="状态" clearable>
        <ElOption v-for="opt in statusOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
      </ElSelect>
      <ElDatePicker
        v-model="filters.range"
        type="daterange"
        value-format="YYYY-MM-DD"
        start-placeholder="开始日期"
        end-placeholder="结束日期"
        class="filter-date"
      />
      <div class="filter-actions">
        <ElButton type="primary" @click="handleSearch">查询</ElButton>
        <ElButton @click="handleReset">重置</ElButton>
        <ElButton type="success" @click="handleExport">导出</ElButton>
      </div>
    </div>

    <Table
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      :columns="columns"
      :data="dataList"
      stripe
      :border="true"
      :loading="loading"
      :pagination="{ total, pageSizes: [20, 50, 100] }"
      @register="tableRegister"
    />
  </ContentWrap>
</template>

<style scoped>
.mcp-ok {
  margin-bottom: 10px;
}

.mb-10px {
  margin-bottom: 10px;
}

.mb-12px {
  margin-bottom: 12px;
}

/* 全局配置：标签在上、控件在下，等宽栅格，避免行内混排挤成一团 */
.cfg-grid {
  display: grid;
  grid-template-columns: 140px 160px 180px minmax(240px, 1fr) auto;
  gap: 16px 20px;
  align-items: end;
}

.cfg-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.cfg-field-wide {
  min-width: 0;
}

.cfg-label {
  font-size: 12px;
  line-height: 1;
  color: var(--el-text-color-secondary);
}

.cfg-number {
  width: 100%;
}

.cfg-actions {
  justify-content: flex-end;
}

/* 日志筛选：控件等宽排布，按钮单独一组，不再和日期选择器重叠 */
.filter-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  margin-bottom: 10px;
}

.filter-grid > * {
  flex: 1 1 180px;
  min-width: 0;
}

.filter-grid > :deep(.el-select) {
  width: 100%;
}

.filter-date {
  flex: 1 1 320px;
  max-width: 380px;
  width: 100%;
}

.filter-actions {
  display: flex;
  gap: 8px;
  flex: 0 0 auto;
  flex-wrap: nowrap;
}

.filter-actions :deep(.el-button) {
  margin-left: 0;
}

@media (max-width: 1400px) {
  .cfg-grid {
    grid-template-columns: repeat(2, minmax(180px, 1fr));
  }
  .cfg-actions {
    justify-content: flex-start;
  }
  .filter-grid > * {
    flex: 1 1 100%;
  }
}

.cfg-label {
  margin-right: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
