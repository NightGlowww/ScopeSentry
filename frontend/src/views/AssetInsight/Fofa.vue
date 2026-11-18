<script setup lang="tsx">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  ElAlert,
  ElButton,
  ElCol,
  ElDivider,
  ElForm,
  ElFormItem,
  ElInput,
  ElInputNumber,
  ElMessage,
  ElOption,
  ElRadio,
  ElRadioGroup,
  ElRow,
  ElSelect,
  ElTable,
  ElTableColumn,
  ElTag,
  ElTreeSelect
} from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import {
  createFofaTasksApi,
  getFofaStatusApi,
  listFofaImportsApi,
  saveFofaConfigApi,
  searchFofaApi
} from '@/api/fofa'
import type {
  FofaCreateMode,
  FofaImport,
  FofaImportResult,
  FofaProviderStatus,
  FofaSearchPage
} from '@/api/fofa/types'
import { getTemplateDataApi } from '@/api/task'
import { getProjectAllApi } from '@/api/project'
import {
  FOFA_MAX_IMPORT_SIZE,
  FOFA_MAX_PER_HOST_TASKS,
  FOFA_MAX_PREVIEW_SIZE,
  hostsToText,
  suggestFofaTaskName
} from '@/utils/policy'
import { timeText } from '@/utils/timeText'

// FOFA 查询 → 批量建任务（对标 ARL-Next taskFofa.py）。
// 未配置 fofa.key 时 status 会带 reason，页面照常可用（不隐藏），只是 search/task 会 400。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })
const refreshIcon = useIcon({ icon: 'iconoir:refresh' })
const plusIcon = useIcon({ icon: 'iconoir:plus' })

// ---- provider 状态 ----
const status = ref<FofaProviderStatus | null>(null)
const statusLoading = ref(false)

const loadStatus = async () => {
  statusLoading.value = true
  try {
    const res = await getFofaStatusApi()
    status.value = res?.data ?? null
  } catch {
    status.value = null
  } finally {
    statusLoading.value = false
  }
}

const statusAvailable = computed(() => !!status.value?.available)

// ---- FOFA Key 就地配置（写入 config.fofa.key，provider 下次调用即生效）----
const keyInput = ref('')
const keySaving = ref(false)

const saveKey = async () => {
  const key = keyInput.value.trim()
  if (!key) {
    ElMessage.warning(t('fofa.keyRequired'))
    return
  }
  keySaving.value = true
  try {
    const res = await saveFofaConfigApi({ key })
    status.value = res?.data ?? status.value
    keyInput.value = ''
    ElMessage.success(t('fofa.keySaved'))
  } finally {
    keySaving.value = false
  }
}

const clearKey = async () => {
  keySaving.value = true
  try {
    const res = await saveFofaConfigApi({ key: '' })
    status.value = res?.data ?? status.value
    ElMessage.success(t('fofa.keyCleared'))
  } finally {
    keySaving.value = false
  }
}
const balance = computed(() => status.value?.balance ?? null)

// ---- 查询 / 预览 ----
const query = ref('')
const previewSize = ref(20)
const previewLoading = ref(false)
const previewPage = ref<FofaSearchPage | null>(null)

const runPreview = async () => {
  const text = query.value.trim()
  if (!text) {
    ElMessage.warning(t('fofa.queryRequired'))
    return
  }
  if (previewSize.value > FOFA_MAX_PREVIEW_SIZE) {
    ElMessage.warning(t('fofa.previewSizeExceeded', { max: FOFA_MAX_PREVIEW_SIZE }))
    return
  }
  previewLoading.value = true
  try {
    const res = await searchFofaApi({ query: text, size: previewSize.value })
    previewPage.value = res?.data ?? null
  } catch {
    previewPage.value = null
  } finally {
    previewLoading.value = false
  }
}

const copyHosts = async () => {
  const text = hostsToText(previewPage.value?.hosts)
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(t('fofa.copySuccess'))
  } catch {
    ElMessage.warning(t('fofa.copyFailed'))
  }
}

// ---- 建任务 ----
const createMode = ref<FofaCreateMode>('single')
const createSize = ref(100)
const taskName = ref('')
const projectId = ref('')
const templateId = ref('')
const createLoading = ref(false)
const createResult = ref<FofaImportResult | null>(null)

// 任务名留空时用查询语句生成建议值（后端口径：换行/空白压成 '-'，截断 120 字符）
const effectiveTaskName = computed(() => taskName.value.trim() || suggestFofaTaskName(query.value))

const submitCreate = async () => {
  const text = query.value.trim()
  if (!text) {
    ElMessage.warning(t('fofa.queryRequired'))
    return
  }
  if (createSize.value > FOFA_MAX_IMPORT_SIZE) {
    ElMessage.warning(t('fofa.createSizeExceeded', { max: FOFA_MAX_IMPORT_SIZE }))
    return
  }
  const name = effectiveTaskName.value
  if (!name) {
    ElMessage.warning(t('fofa.taskNameRequired'))
    return
  }
  createLoading.value = true
  try {
    const res = await createFofaTasksApi({
      query: text,
      size: createSize.value,
      createMode: createMode.value,
      taskName: name,
      project: projectId.value || undefined,
      templateId: templateId.value || undefined
    })
    createResult.value = res?.data ?? null
    const result = createResult.value
    if (result) {
      ElMessage.success(
        t('fofa.createSuccess', {
          created: result.created,
          skipped: result.skipped,
          failed: result.failed
        })
      )
    }
    getList()
  } catch {
    // 拦截器已提示（未配置 key / 查询无结果 / per-host 超 50 都是 400）
  } finally {
    createLoading.value = false
  }
}

// ---- 项目 / 模板下拉 ----
interface ProjectNode {
  value: string
  label: string
  children?: ProjectNode[]
}

const projectList = reactive<ProjectNode[]>([])
const templateNames = ref<Record<string, string>>({})

const loadProjects = async () => {
  try {
    const res = await getProjectAllApi()
    projectList.length = 0
    ;(res?.data?.list ?? []).forEach((item: ProjectNode) => projectList.push(item))
  } catch {
    projectList.length = 0
  }
}

const loadTemplates = async () => {
  try {
    const res = await getTemplateDataApi('', 1, 1000)
    const map: Record<string, string> = {}
    ;(res?.data?.list ?? []).forEach((item: { id: string; name: string }) => {
      if (item?.id) map[item.id] = item.name
    })
    templateNames.value = map
  } catch {
    templateNames.value = {}
  }
}

// ---- 导入历史 ----
const historyKeyword = ref('')

const columns = reactive<TableColumn[]>([
  {
    field: 'query',
    label: t('fofa.historyQuery'),
    minWidth: 260,
    formatter: (row: FofaImport) => row.query || '-'
  },
  {
    field: 'createMode',
    label: t('fofa.historyMode'),
    width: 120,
    formatter: (row: FofaImport) =>
      row.createMode === 'per-host' ? t('fofa.modePerHost') : t('fofa.modeSingle')
  },
  {
    field: 'created',
    label: t('fofa.historyCreated'),
    width: 100,
    formatter: (row: FofaImport) => (
      <ElTag size="small" type="success">
        {row.created ?? 0}
      </ElTag>
    )
  },
  {
    field: 'skipped',
    label: t('fofa.historySkipped'),
    width: 100,
    formatter: (row: FofaImport) => (
      <ElTag size="small" type="info">
        {row.skipped ?? 0}
      </ElTag>
    )
  },
  {
    field: 'failed',
    label: t('fofa.historyFailed'),
    width: 100,
    formatter: (row: FofaImport) => (
      <ElTag size="small" type={row.failed ? 'danger' : 'info'}>
        {row.failed ?? 0}
      </ElTag>
    )
  },
  {
    field: 'hosts',
    label: t('fofa.historyHosts'),
    width: 110,
    formatter: (row: FofaImport) => (row.hosts ?? []).length
  },
  {
    field: 'createdAt',
    label: t('fofa.historyTime'),
    minWidth: 170,
    formatter: (row: FofaImport) => timeText(row.createdAt)
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await listFofaImportsApi({
      query: historyKeyword.value.trim(),
      pageIndex: currentPage.value,
      pageSize: pageSize.value
    })
    return { list: res?.data?.list ?? [], total: res?.data?.total ?? 0 }
  }
})

const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList } = tableMethods

const handleSearch = () => {
  if (currentPage.value !== 1) {
    currentPage.value = 1
    return
  }
  getList()
}

const handleReset = () => {
  historyKeyword.value = ''
  handleSearch()
}

onMounted(() => {
  loadStatus()
  loadProjects()
  loadTemplates()
})
</script>

<template>
  <ContentWrap :title="t('fofa.title')" :message="t('fofa.desc')">
    <template #header>
      <ElButton :icon="refreshIcon" :loading="statusLoading" @click="loadStatus">
        {{ t('fofa.statusRefresh') }}
      </ElButton>
    </template>

    <div class="status-bar">
      <span class="label">{{ t('fofa.statusTitle') }}:</span>
      <ElTag v-if="status" :type="statusAvailable ? 'success' : 'warning'" size="small">
        {{ status.provider }}
        {{ statusAvailable ? t('fofa.statusAvailable') : t('fofa.statusUnavailable') }}
      </ElTag>
      <span v-if="status && !status.configured" class="hint">{{
        t('fofa.statusNotConfigured')
      }}</span>
      <span v-if="status?.keySource" class="hint">
        {{ t('fofa.statusKeySource') }}: {{ status.keySource }}
      </span>
      <span v-if="balance" class="balance">
        <ElTag size="small" type="info">
          {{ t('fofa.balancePoint') }} {{ balance.fofaPoint ?? '-' }}
        </ElTag>
        <ElTag size="small" type="info">
          {{ t('fofa.balanceFreePoint') }} {{ balance.remainFreePoint ?? '-' }}
        </ElTag>
        <ElTag size="small" type="info">
          {{ t('fofa.balanceApiQuery') }} {{ balance.remainApiQuery ?? '-' }}
        </ElTag>
        <ElTag v-if="balance.username" size="small">
          {{ t('fofa.balanceUsername') }} {{ balance.username }}
        </ElTag>
        <ElTag v-if="balance.isVIP" size="small" type="success">
          {{ t('fofa.balanceVip') }} {{ balance.vipLevel ?? '-' }}
        </ElTag>
      </span>
    </div>

    <ElAlert
      v-if="status && !statusAvailable"
      type="warning"
      :closable="false"
      show-icon
      class="status-alert"
      :title="t('fofa.statusReasonHint', { reason: status.reason || '-' })"
    />

    <div class="key-config">
      <span class="label">{{ t('fofa.keyLabel') }}</span>
      <ElInput
        v-model="keyInput"
        type="password"
        show-password
        clearable
        class="key-input"
        :placeholder="
          status?.configured ? t('fofa.keyPlaceholderSet') : t('fofa.keyPlaceholderEmpty')
        "
      />
      <ElButton type="primary" :loading="keySaving" @click="saveKey">
        {{ t('fofa.keySave') }}
      </ElButton>
      <ElButton v-if="status?.configured" :loading="keySaving" @click="clearKey">
        {{ t('fofa.keyClear') }}
      </ElButton>
    </div>
    <div class="key-hint">{{ t('fofa.keyHint') }}</div>

    <ElDivider content-position="left">{{ t('fofa.searchTitle') }}</ElDivider>
    <ElForm label-width="120px">
      <ElFormItem :label="t('fofa.query')">
        <ElInput
          v-model="query"
          type="textarea"
          :rows="3"
          :placeholder="t('fofa.queryPlaceholder')"
        />
        <div class="form-hint">{{ t('fofa.queryHint') }}</div>
      </ElFormItem>
      <ElRow :gutter="12">
        <ElCol :span="8">
          <ElFormItem :label="t('fofa.previewSize')">
            <ElInputNumber
              v-model="previewSize"
              :min="1"
              :max="FOFA_MAX_PREVIEW_SIZE"
              controls-position="right"
              class="full-width"
            />
          </ElFormItem>
        </ElCol>
        <ElCol :span="16">
          <ElButton
            type="primary"
            :icon="searchIcon"
            :loading="previewLoading"
            :disabled="!statusAvailable"
            @click="runPreview"
          >
            {{ t('fofa.preview') }}
          </ElButton>
          <span v-if="!statusAvailable" class="form-hint warn-hint">
            {{ t('fofa.statusReasonHint', { reason: status?.reason || '-' }) }}
          </span>
          <span class="form-hint inline-hint">
            {{ t('fofa.previewSizeHint', { max: FOFA_MAX_PREVIEW_SIZE }) }}
          </span>
        </ElCol>
      </ElRow>
    </ElForm>

    <div v-if="previewPage" v-loading="previewLoading" class="preview-panel">
      <div class="section-title">
        {{ t('fofa.previewHostsTitle') }}
        <span class="section-hint">
          {{ t('fofa.previewTotal') }} {{ previewPage.total ?? 0 }} · {{ t('fofa.previewCount') }}
          {{ previewPage.size ?? 0 }}
          <template v-if="typeof previewPage.consumedFPoint === 'number'">
            · {{ t('fofa.previewConsumed') }} {{ previewPage.consumedFPoint }}
          </template>
        </span>
        <ElButton link type="primary" size="small" @click="copyHosts">
          {{ t('fofa.copyHosts') }}
        </ElButton>
      </div>
      <div v-if="(previewPage.hosts ?? []).length" class="host-box">
        <ElTag v-for="host in previewPage.hosts" :key="host" size="small" class="host-tag">
          {{ host }}
        </ElTag>
      </div>
      <div v-else class="empty-state">{{ t('fofa.previewHostsEmpty') }}</div>
    </div>

    <ElDivider content-position="left">{{ t('fofa.createTitle') }}</ElDivider>
    <ElForm label-width="120px">
      <ElFormItem :label="t('fofa.query')">
        <ElInput :model-value="query" type="textarea" :rows="2" readonly />
        <div class="form-hint">{{ t('fofa.createQueryHint') }}</div>
      </ElFormItem>
      <ElRow :gutter="12">
        <ElCol :span="10">
          <ElFormItem :label="t('fofa.createSize')">
            <ElInputNumber
              v-model="createSize"
              :min="1"
              :max="FOFA_MAX_IMPORT_SIZE"
              controls-position="right"
              class="full-width"
            />
            <div class="form-hint">
              {{ t('fofa.createSizeHint', { max: FOFA_MAX_IMPORT_SIZE }) }}
            </div>
          </ElFormItem>
        </ElCol>
        <ElCol :span="14">
          <ElFormItem :label="t('fofa.createMode')">
            <ElRadioGroup v-model="createMode">
              <ElRadio value="single">{{ t('fofa.modeSingle') }}</ElRadio>
              <ElRadio value="per-host">{{ t('fofa.modePerHost') }}</ElRadio>
            </ElRadioGroup>
            <div class="form-hint">
              {{
                createMode === 'per-host'
                  ? t('fofa.modePerHostHint', { limit: FOFA_MAX_PER_HOST_TASKS })
                  : t('fofa.modeSingleHint')
              }}
            </div>
          </ElFormItem>
        </ElCol>
      </ElRow>
      <ElFormItem :label="t('fofa.taskName')">
        <ElInput
          v-model="taskName"
          :placeholder="effectiveTaskName || t('fofa.taskNamePlaceholder')"
        />
        <div class="form-hint">{{ t('fofa.taskNameHint') }}</div>
      </ElFormItem>
      <ElRow :gutter="12">
        <ElCol :span="12">
          <ElFormItem :label="t('fofa.project')">
            <ElTreeSelect
              v-model="projectId"
              :data="projectList"
              :placeholder="t('fofa.projectPlaceholder')"
              filterable
              check-strictly
              clearable
              :render-after-expand="false"
              class="full-width"
            />
          </ElFormItem>
        </ElCol>
        <ElCol :span="12">
          <ElFormItem :label="t('fofa.template')">
            <ElSelect
              v-model="templateId"
              clearable
              filterable
              :placeholder="t('fofa.templatePlaceholder')"
              class="full-width"
            >
              <ElOption
                v-for="(label, value) in templateNames"
                :key="value"
                :label="label"
                :value="value"
              />
            </ElSelect>
            <div class="form-hint">{{ t('fofa.templateHint') }}</div>
          </ElFormItem>
        </ElCol>
      </ElRow>
      <ElFormItem>
        <ElButton
          type="primary"
          :icon="plusIcon"
          :loading="createLoading"
          :disabled="!statusAvailable"
          @click="submitCreate"
        >
          {{ t('fofa.create') }}
        </ElButton>
        <span class="form-hint inline-hint">{{ t('fofa.createHint') }}</span>
      </ElFormItem>
    </ElForm>

    <div v-if="createResult" class="result-panel">
      <div class="section-title">{{ t('fofa.resultTitle') }}</div>
      <div class="tag-row">
        <ElTag size="small" type="success">
          {{ t('fofa.created') }} {{ createResult.created }}
        </ElTag>
        <ElTag size="small" type="info">{{ t('fofa.skipped') }} {{ createResult.skipped }}</ElTag>
        <ElTag size="small" :type="createResult.failed ? 'danger' : 'info'">
          {{ t('fofa.failed') }} {{ createResult.failed }}
        </ElTag>
        <ElTag size="small" type="info">{{ t('fofa.resultSize') }} {{ createResult.size }}</ElTag>
      </div>
      <div v-if="(createResult.taskNames ?? []).length" class="task-names">
        <span class="label">{{ t('fofa.resultTasks') }}:</span>
        <ElTag v-for="name in createResult.taskNames" :key="name" size="small" class="host-tag">
          {{ name }}
        </ElTag>
      </div>
      <template v-if="(createResult.errors ?? []).length">
        <div class="section-title">{{ t('fofa.resultErrorsTitle') }}</div>
        <ElTable :data="createResult.errors" stripe border size="small" max-height="260">
          <ElTableColumn prop="host" :label="t('fofa.resultErrorHost')" min-width="200" />
          <ElTableColumn prop="error" :label="t('fofa.resultErrorReason')" min-width="320" />
        </ElTable>
      </template>
      <div v-else-if="!createResult.created" class="empty-state">
        {{ t('fofa.resultEmpty') }}
      </div>
    </div>

    <ElDivider content-position="left">{{ t('fofa.historyTitle') }}</ElDivider>
    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="6">
        <ElInput
          v-model="historyKeyword"
          :placeholder="t('fofa.historyKeywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
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
        <div class="empty-state">{{ t('fofa.historyEmpty') }}</div>
      </template>
    </Table>
  </ContentWrap>
</template>

<style scoped>
/* FOFA Key 行：标签 + 定宽输入 + 按钮同一行，说明文字单独一行 */
.key-config {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 10px 0 2px;
  flex-wrap: wrap;
}

.key-config .label {
  flex: 0 0 auto;
  min-width: 64px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.key-config .key-input {
  flex: 0 1 360px;
  width: 360px;
  max-width: 100%;
}

.key-hint {
  margin: 0 0 2px 72px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
}

@media (max-width: 900px) {
  .key-hint {
    margin-left: 0;
  }
  .key-config .key-input {
    flex: 1 1 100%;
    width: 100%;
  }
}

.status-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-bottom: 12px;
}

.label {
  color: var(--el-text-color-secondary);
}

.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.balance {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-left: auto;
}

.status-alert {
  margin-bottom: 12px;
}

.full-width {
  width: 100%;
}

.form-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
}

.inline-hint {
  margin-left: 10px;
}

.preview-panel {
  margin: 4px 0 8px;
}

.host-box {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-height: 220px;
  padding: 10px;
  overflow: auto;
  background: var(--el-fill-color-light);
  border-radius: 4px;
}

.host-tag {
  font-family: var(--el-font-family-mono, monospace);
}

.filter-row {
  margin-bottom: 15px;
}

.empty-state {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}

.section-title {
  display: flex;
  gap: 10px;
  align-items: baseline;
  margin: 12px 0 10px;
  font-weight: 600;
}

.section-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-weight: 400;
}

.tag-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.task-names {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  margin-top: 8px;
}

.result-panel {
  margin-bottom: 8px;
}
</style>
