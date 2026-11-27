<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { onMounted, reactive, ref, computed } from 'vue'
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
import { useVisibleInterval } from '@/hooks/web/useVisibleInterval'
import { useSingleTabLeader } from '@/hooks/web/useSingleTabLeader'
import { formatTime } from '@/utils'

const { t } = useI18n()
const { t: tc } = useI18n('mcpAudit')
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
    ElMessage.success(tc('saved'))
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
    label: tc('colTime'),
    minWidth: 170,
    formatter: (_row, __: TableColumn, value: string) => formatDateTime(value)
  },
  {
    field: 'tool',
    label: tc('colTool'),
    minWidth: 160
  },
  {
    field: 'status',
    label: tc('colStatus'),
    width: 100,
    formatter: (_row, __: TableColumn, value: string) => statusTag(value)
  },
  {
    field: 'durationMs',
    label: tc('colDuration'),
    width: 100
  },
  {
    field: 'keyPrefix',
    label: tc('colKeyPrefix'),
    minWidth: 140
  },
  {
    field: 'clientIp',
    label: tc('colClientIp'),
    minWidth: 130
  },
  {
    field: 'error',
    label: tc('colError'),
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

// 三种客户端的接入片段（端点取当前站点，密钥用占位符）
const mcpEndpoint = computed(
  () => (typeof window !== 'undefined' ? window.location.origin : '') + '/mcp'
)
const clientGuides = computed(() => [
  {
    name: 'Claude Code / Claude Desktop',
    body: JSON.stringify(
      {
        mcpServers: {
          scopesentry: {
            type: 'http',
            url: mcpEndpoint.value,
            headers: { 'X-API-Key': tc('apiKeyPlaceholder') }
          }
        }
      },
      null,
      2
    )
  },
  {
    name: 'Cursor / Windsurf',
    body: JSON.stringify(
      {
        mcpServers: {
          scopesentry: {
            url: mcpEndpoint.value,
            headers: { 'X-API-Key': tc('apiKeyPlaceholder') }
          }
        }
      },
      null,
      2
    )
  },
  {
    name: tc('curlGuideName'),
    body: [
      'curl -s ' + mcpEndpoint.value + ' \\',
      '  -H "Content-Type: application/json" \\',
      '  -H "Accept: application/json, text/event-stream" \\',
      '  -H "X-API-Key: ' + tc('apiKeyPlaceholder') + '" \\',
      '  -d \'{"jsonrpc":"2.0","id":1,"method":"tools/list"}\''
    ].join('\n')
  }
])
// 三段可直接粘给 AI Agent 的提示词（工具名与 scope 对应我们的 MCP）
const promptTemplates = computed(() => [
  {
    name: tc('promptAssetTitle'),
    body: [
      tc('promptAssetLine1'),
      tc('promptAssetLine2'),
      tc('promptAssetLine3'),
      tc('promptAssetLine4'),
      tc('promptAssetLine5')
    ].join('\n')
  },
  {
    name: tc('promptSearchTitle'),
    body: [tc('promptSearchLine1'), tc('promptSearchLine2'), tc('promptSearchLine3')].join('\n')
  },
  {
    name: tc('promptTriageTitle'),
    body: [
      tc('promptTriageLine1'),
      tc('promptTriageLine2'),
      tc('promptTriageLine3'),
      tc('promptTriageLine4'),
      tc('promptTriageLine5')
    ].join('\n')
  }
])

const copiedName = ref('')
const copyGuide = async (c: { name: string; body: string }) => {
  try {
    await navigator.clipboard.writeText(c.body)
    copiedName.value = c.name
    setTimeout(() => (copiedName.value = ''), 1500)
  } catch {
    copiedName.value = ''
  }
}

// 实时监控流（对齐 ARL-Next 的 MCP 调用监控）：8 秒静默刷新，后台不刷，回前台补一次。
// 审计是「看发生了什么」的页面，手动点刷新会错过突发调用（2026-11-21）。
const autoRefresh = ref(true)
const lastRefreshAt = ref('')
// 多标签页只让一个轮询（其余标签显示「其他标签页正在刷新」），避免请求叠加
const { isLeader } = useSingleTabLeader('mcp-audit')
const stampNow = () => {
  lastRefreshAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
}
useVisibleInterval(() => {
  if (!autoRefresh.value) return
  if (!isLeader.value) {
    // 让位：另一个标签页在刷，这里不重复请求
    return
  }
  getList()
  stampNow()
}, 8000)

const handleSearch = () => {
  currentPage.value = 1
  getList()
  stampNow()
}

// 模板里的多语句事件处理器一律收成具名函数：
// prettier 会把 `@click="a(); b()"` 折成多行把分号吃掉，构建期直接 SyntaxError（2026-11-28 实测）。
const refreshNow = () => {
  getList()
  stampNow()
}

const onlyDenied = () => {
  filters.status = 'denied'
  handleSearch()
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
    <!-- 提示词模板：对齐 ARL-Next 的 prompt drawer——接好之后「让 Agent 干什么」才是关键 -->
    <ElCard class="mb-12px" shadow="never">
      <template #header>
        <span>{{ t('mcp.promptTitle') }}</span>
      </template>
      <div class="guide-grid">
        <div v-for="p in promptTemplates" :key="p.name" class="guide-item">
          <div class="guide-head">
            <span class="guide-name">{{ p.name }}</span>
            <ElButton link type="primary" size="small" @click="copyGuide(p)">
              {{ copiedName === p.name ? t('mcp.copied') : t('mcp.copy') }}
            </ElButton>
          </div>
          <pre class="guide-code">{{ p.body }}</pre>
        </div>
      </div>
    </ElCard>

    <!-- 客户端接入指引：对齐 ARL-Next 的 MCP 中心——光有工具列表不够，
         还得告诉人「怎么把它接到 Cursor / Claude Code 里」-->
    <ElCard class="mb-12px" shadow="never">
      <template #header>
        <span>{{ t('mcp.guideTitle') }}</span>
      </template>
      <div class="guide-grid">
        <div v-for="c in clientGuides" :key="c.name" class="guide-item">
          <div class="guide-head">
            <span class="guide-name">{{ c.name }}</span>
            <ElButton link type="primary" size="small" @click="copyGuide(c)">
              {{ copiedName === c.name ? t('mcp.copied') : t('mcp.copy') }}
            </ElButton>
          </div>
          <pre class="guide-code">{{ c.body }}</pre>
        </div>
      </div>
    </ElCard>

    <div class="stream-bar">
      <ElSwitch v-model="autoRefresh" size="small" />
      <span class="stream-hint">
        <template v-if="!isLeader">{{ t('mcp.autoRefreshFollower') }}</template>
        <template v-else>
          {{ t('mcp.autoRefresh')
          }}{{ lastRefreshAt ? ' · ' + t('task.listRefreshHintShort', { t: lastRefreshAt }) : '' }}
        </template>
      </span>
      <ElButton
        link
        type="primary"
        size="small"
        @click="refreshNow"
      >
        {{ t('mcp.refreshNow') }}
      </ElButton>
    </div>

    <ElAlert
      v-if="deniedCount > 0"
      type="warning"
      :closable="false"
      show-icon
      class="mb-12px"
      :title="
        tc('deniedAlertTitle', { count: deniedCount }) + (deniedReason ? ' · ' + deniedReason : '')
      "
    >
      <ElButton
        link
        type="primary"
        size="small"
        @click="onlyDenied"
      >
        {{ tc('onlyDenied') }}
      </ElButton>
    </ElAlert>
    <!-- 没黄条不代表「没人查过」：给一条明确的绿色正常态，避免把沉默当成没问题 -->
    <div v-else-if="deniedChecked" class="mcp-ok">
      <ElTag type="success" size="small" effect="plain">
        {{ tc('noDenied') }}
      </ElTag>
    </div>
    <ElCard class="mb-12px" shadow="never" v-loading="cfgLoading">
      <template #header>
        <span>{{ tc('globalConfig') }}</span>
      </template>
      <div class="cfg-grid">
        <div class="cfg-field">
          <span class="cfg-label">{{ tc('enableMcp') }}</span>
          <ElSwitch v-model="cfg.enabled" />
        </div>
        <div class="cfg-field">
          <span class="cfg-label">{{ tc('rateLimitPerMin') }}</span>
          <ElInputNumber
            v-model="cfg.rateLimitPerMin"
            :min="0"
            :max="100000"
            controls-position="right"
            class="cfg-number"
          />
        </div>
        <div class="cfg-field">
          <span class="cfg-label">{{ tc('logRetention') }}</span>
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
          <span class="cfg-label">{{ tc('toolAllowlist') }}</span>
          <ElSelect
            v-model="cfg.allowedTools"
            multiple
            filterable
            allow-create
            default-first-option
            :placeholder="tc('allowlistPlaceholder')"
            style="width: 100%"
          />
        </div>
        <div class="cfg-field cfg-actions">
          <ElButton type="primary" :loading="cfgSaving" @click="saveConfig">{{
            tc('saveConfig')
          }}</ElButton>
        </div>
      </div>
    </ElCard>

    <div class="filter-grid">
      <ElInput v-model="filters.keyId" placeholder="Key ID" clearable />
      <ElInput v-model="filters.tool" :placeholder="tc('toolNamePlaceholder')" clearable />
      <ElSelect v-model="filters.status" :placeholder="tc('statusPlaceholder')" clearable>
        <ElOption
          v-for="opt in statusOptions"
          :key="opt.value"
          :label="opt.label"
          :value="opt.value"
        />
      </ElSelect>
      <ElDatePicker
        v-model="filters.range"
        type="daterange"
        value-format="YYYY-MM-DD"
        :start-placeholder="tc('startDatePlaceholder')"
        :end-placeholder="tc('endDatePlaceholder')"
        class="filter-date"
      />
      <div class="filter-actions">
        <ElButton type="primary" @click="handleSearch">{{ tc('search') }}</ElButton>
        <ElButton @click="handleReset">{{ tc('reset') }}</ElButton>
        <ElButton type="success" @click="handleExport">{{ tc('exportBtn') }}</ElButton>
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
.stream-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 12px;
}
.stream-hint {
  color: var(--el-text-color-secondary);
}

.guide-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 12px;
}
.guide-item {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  padding: 8px 10px;
}
.guide-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}
.guide-name {
  font-size: 13px;
  font-weight: 600;
}
.guide-code {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--el-text-color-regular);
}

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
