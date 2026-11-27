<script setup lang="tsx">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  ElAlert,
  ElButton,
  ElCol,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElInputNumber,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElRadio,
  ElRadioGroup,
  ElRow,
  ElSelect,
  ElSwitch,
  ElTag
} from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import {
  applyPolicyApi,
  createPolicyApi,
  deletePolicyApi,
  getPolicyDetailApi,
  getPolicyListApi,
  testPolicyApi,
  updatePolicyApi
} from '@/api/policy'
import type {
  ScanPolicy,
  ScanPolicyApplyResult,
  ScanPolicyConfig,
  ScanPolicyDicts,
  ScanPolicyTaskTemplate,
  ScanPolicyValidation
} from '@/api/policy/types'
import { getTemplateDataApi } from '@/api/task'
import {
  POLICY_MAX_DESCRIPTION_LENGTH,
  POLICY_MAX_NAME_LENGTH,
  POLICY_MAX_RATE_LIMIT,
  POLICY_MAX_THREAD_NUM,
  POLICY_MAX_TIMEOUT_SECONDS,
  POLICY_MIN_RATE_LIMIT,
  POLICY_MIN_THREAD_NUM,
  POLICY_MIN_TIMEOUT_SECONDS,
  formatOptionsText,
  parseOptionsText,
  parsePluginsText,
  policyFieldLabelKey,
  toNullableNumber
} from '@/utils/policy'
import { timeText } from '@/utils/timeText'

// 扫描策略（对标 ARL-Next policy.py）：把「模板 / 插件 / 并发 / 速率 / 超时 / 字典」
// 存成一组可复用参数，建任务时选策略即可套用。策略只是参数预设，不参与执行链路。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })
const refreshIcon = useIcon({ icon: 'iconoir:refresh' })
const plusIcon = useIcon({ icon: 'iconoir:plus' })

// ---- 列表 / 过滤 ----
const keyword = ref('')
// '' 全部 / 'true' 仅启用 / 'false' 仅停用：后端 enabled 指针不传即不过滤
const enabledFilter = ref('')

const enabledParam = (): boolean | undefined => {
  if (enabledFilter.value === 'true') return true
  if (enabledFilter.value === 'false') return false
  return undefined
}

// 模板名映射：列表里展示模板名而不是 24 位 ObjectID
const templateNames = ref<Record<string, string>>({})

const loadTemplates = async () => {
  try {
    const res = await getTemplateDataApi('', 1, 1000)
    const map: Record<string, string> = {}
    ;(res?.data?.list ?? []).forEach((item: { id: string; name: string }) => {
      if (item?.id) map[item.id] = item.name
    })
    templateNames.value = map
  } catch {
    // 模板名拿不到不影响策略本身的读写，退化成展示 ID
    templateNames.value = {}
  }
}

const templateName = (templateId?: string) => {
  const id = (templateId ?? '').trim()
  if (!id) return ''
  return templateNames.value[id] ?? id
}

const dictSummary = (row: ScanPolicy) => {
  const dicts = row.config?.dicts ?? {}
  const parts = [dicts.dir, dicts.subdomain, dicts.brute, dicts.port]
    .map((item) => (item ?? '').trim())
    .filter(Boolean)
  return parts.length ? parts.join(' · ') : '-'
}

const columns = reactive<TableColumn[]>([
  {
    field: 'name',
    label: t('policy.colName'),
    minWidth: 200,
    formatter: (row: ScanPolicy) => (
      <ElButton link type="primary" onClick={() => openEdit(row)}>
        {row.name || '-'}
      </ElButton>
    )
  },
  {
    field: 'enabled',
    label: t('policy.colEnabled'),
    width: 100,
    formatter: (row: ScanPolicy) => (
      <ElTag type={row.enabled ? 'success' : 'info'} size="small">
        {row.enabled ? t('policy.enabledOn') : t('policy.enabledOff')}
      </ElTag>
    )
  },
  {
    field: 'templateId',
    label: t('policy.colTemplate'),
    minWidth: 180,
    formatter: (row: ScanPolicy) => templateName(row.config?.templateId) || '-'
  },
  {
    field: 'plugins',
    label: t('policy.colPlugins'),
    width: 110,
    formatter: (row: ScanPolicy) => (row.config?.plugins ?? []).length
  },
  {
    field: 'threadNum',
    label: t('policy.colThread'),
    width: 100,
    formatter: (row: ScanPolicy) => row.config?.threadNum ?? '-'
  },
  {
    field: 'rateLimit',
    label: t('policy.colRate'),
    width: 100,
    formatter: (row: ScanPolicy) => row.config?.rateLimit ?? '-'
  },
  {
    field: 'timeoutSeconds',
    label: t('policy.colTimeout'),
    width: 110,
    formatter: (row: ScanPolicy) => row.config?.timeoutSeconds ?? '-'
  },
  {
    field: 'dicts',
    label: t('policy.colDicts'),
    minWidth: 200,
    formatter: (row: ScanPolicy) => dictSummary(row)
  },
  {
    field: 'updatedAt',
    label: t('policy.colUpdatedAt'),
    minWidth: 170,
    formatter: (row: ScanPolicy) => timeText(row.updatedAt)
  },
  {
    field: 'actions',
    label: t('policy.colActions'),
    width: 240,
    formatter: (row: ScanPolicy) => (
      <span class="row-actions">
        <ElButton link type="primary" size="small" onClick={() => openEdit(row)}>
          {t('policy.edit')}
        </ElButton>
        <ElButton link type="success" size="small" onClick={() => openValidate(row)}>
          {t('policy.validate')}
        </ElButton>
        <ElButton link type="primary" size="small" onClick={() => openPreview(row)}>
          {t('policy.preview')}
        </ElButton>
        <ElButton link type="danger" size="small" onClick={() => removePolicy(row)}>
          {t('policy.delete')}
        </ElButton>
      </span>
    )
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getPolicyListApi({
      keyword: keyword.value.trim(),
      enabled: enabledParam(),
      pageIndex: currentPage.value,
      pageSize: pageSize.value
    })
    return { list: res?.data?.list ?? [], total: res?.data?.total ?? 0 }
  }
})

const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList } = tableMethods

const handleSearch = () => {
  // currentPage 变化时 useTable 会自己拉一次，只有已经在第一页时才手动拉
  if (currentPage.value !== 1) {
    currentPage.value = 1
    return
  }
  getList()
}

const handleReset = () => {
  keyword.value = ''
  enabledFilter.value = ''
  handleSearch()
}

// ---- 新建 / 编辑弹窗 ----
const formVisible = ref(false)
const formLoading = ref(false)
const editingId = ref('')
const usageNote = ref('')
const validation = ref<ScanPolicyValidation | null>(null)
const validateLoading = ref(false)

const form = reactive({
  name: '',
  description: '',
  enabled: true,
  templateId: '',
  pluginsText: '',
  threadNum: null as number | null,
  rateLimit: null as number | null,
  timeoutSeconds: null as number | null,
  dictDir: '',
  dictSubdomain: '',
  dictBrute: '',
  dictPort: '',
  optionsText: ''
})

const resetForm = () => {
  editingId.value = ''
  validation.value = null
  usageNote.value = ''
  form.name = ''
  form.description = ''
  form.enabled = true
  form.templateId = ''
  form.pluginsText = ''
  form.threadNum = null
  form.rateLimit = null
  form.timeoutSeconds = null
  form.dictDir = ''
  form.dictSubdomain = ''
  form.dictBrute = ''
  form.dictPort = ''
  form.optionsText = ''
}

const fillForm = (row: ScanPolicy) => {
  const config = row.config ?? {}
  form.name = row.name ?? ''
  form.description = row.description ?? ''
  form.enabled = !!row.enabled
  form.templateId = config.templateId ?? ''
  form.pluginsText = (config.plugins ?? []).join('\n')
  form.threadNum = typeof config.threadNum === 'number' ? config.threadNum : null
  form.rateLimit = typeof config.rateLimit === 'number' ? config.rateLimit : null
  form.timeoutSeconds = typeof config.timeoutSeconds === 'number' ? config.timeoutSeconds : null
  form.dictDir = config.dicts?.dir ?? ''
  form.dictSubdomain = config.dicts?.subdomain ?? ''
  form.dictBrute = config.dicts?.brute ?? ''
  form.dictPort = config.dicts?.port ?? ''
  form.optionsText = formatOptionsText(config.options)
  validation.value = null
}

const openCreate = () => {
  resetForm()
  formVisible.value = true
}

// 编辑一律走 detail：列表里的 config 可能是旧快照，detail 还会带回 usage 说明
// （任务表没有 policyId，引用计数不可用，note 会如实说明这一点）。
const openEdit = async (row: ScanPolicy) => {
  resetForm()
  editingId.value = row.id
  fillForm(row)
  formVisible.value = true
  try {
    const res = await getPolicyDetailApi(row.id)
    if (res?.data) {
      fillForm(res.data)
      usageNote.value = res.data.usage?.note ?? ''
    }
  } catch {
    // 详情拿不到就用列表里的数据，不阻塞编辑
  }
}

// 表单 → 后端 config：留空 = 不覆盖模板默认（nil），rateLimit=0 表示不限速
const buildConfig = (): ScanPolicyConfig => {
  const config: ScanPolicyConfig = {}
  const templateId = form.templateId.trim()
  if (templateId) config.templateId = templateId
  const plugins = parsePluginsText(form.pluginsText)
  if (plugins.length) config.plugins = plugins
  const threadNum = toNullableNumber(form.threadNum)
  if (threadNum !== null) config.threadNum = threadNum
  const rateLimit = toNullableNumber(form.rateLimit)
  if (rateLimit !== null) config.rateLimit = rateLimit
  const timeoutSeconds = toNullableNumber(form.timeoutSeconds)
  if (timeoutSeconds !== null) config.timeoutSeconds = timeoutSeconds
  const dir = form.dictDir.trim()
  const subdomain = form.dictSubdomain.trim()
  const brute = form.dictBrute.trim()
  const port = form.dictPort.trim()
  if (dir || subdomain || brute || port) {
    const dicts: ScanPolicyDicts = {}
    if (dir) dicts.dir = dir
    if (subdomain) dicts.subdomain = subdomain
    if (brute) dicts.brute = brute
    if (port) dicts.port = port
    config.dicts = dicts
  }
  const parsed = parseOptionsText(form.optionsText)
  if (Object.keys(parsed.options).length) config.options = parsed.options
  return config
}

const optionsHint = computed(() => {
  const parsed = parseOptionsText(form.optionsText)
  const hints: string[] = []
  if (parsed.duplicates.length) {
    hints.push(t('policy.optionsDuplicates', { fields: parsed.duplicates.join(', ') }))
  }
  if (parsed.invalid.length) {
    hints.push(t('policy.optionsInvalid', { fields: parsed.invalid.join(', ') }))
  }
  return hints.join(' · ')
})

const submitForm = async () => {
  const name = form.name.trim()
  if (!name) {
    ElMessage.warning(t('policy.nameRequired'))
    return
  }
  formLoading.value = true
  try {
    const config = buildConfig()
    if (editingId.value) {
      await updatePolicyApi({
        id: editingId.value,
        name,
        description: form.description.trim(),
        config,
        enabled: form.enabled
      })
      ElMessage.success(t('policy.updateSuccess'))
    } else {
      await createPolicyApi({
        name,
        description: form.description.trim(),
        config,
        enabled: form.enabled
      })
      ElMessage.success(t('policy.createSuccess'))
    }
    formVisible.value = false
    getList()
  } catch {
    // 拦截器已提示
  } finally {
    formLoading.value = false
  }
}

// 校验：先按 test 端点试算，不合法也回 200，前端按 valid/errors 渲染
const runValidate = async () => {
  validateLoading.value = true
  try {
    const res = await testPolicyApi(buildConfig())
    validation.value = res?.data ?? null
  } catch {
    validation.value = null
  } finally {
    validateLoading.value = false
  }
}

// ---- 行内校验结果弹窗 ----
const validateVisible = ref(false)
const validateTarget = ref<ScanPolicy | null>(null)
const rowValidation = ref<ScanPolicyValidation | null>(null)
const rowValidating = ref(false)

const openValidate = async (row: ScanPolicy) => {
  validateTarget.value = row
  rowValidation.value = null
  validateVisible.value = true
  rowValidating.value = true
  try {
    const res = await testPolicyApi(row.config ?? {})
    rowValidation.value = res?.data ?? null
  } catch {
    rowValidation.value = null
  } finally {
    rowValidating.value = false
  }
}

// ---- 合并预览弹窗 ----
const previewVisible = ref(false)
const previewTarget = ref<ScanPolicy | null>(null)
const previewMode = ref<'template' | 'object'>('template')
const previewTemplateId = ref('')
const previewObjectText = ref('')
const previewLoading = ref(false)
const previewResult = ref<ScanPolicyApplyResult | null>(null)

const openPreview = (row: ScanPolicy) => {
  previewTarget.value = row
  previewMode.value = 'template'
  previewTemplateId.value = row.config?.templateId ?? ''
  previewObjectText.value = ''
  previewResult.value = null
  previewVisible.value = true
}

const mergedJson = computed(() => {
  if (!previewResult.value) return ''
  return JSON.stringify(previewResult.value.config ?? {}, null, 2)
})

const runPreview = async () => {
  const policy = previewTarget.value
  if (!policy) return
  let taskTemplate: ScanPolicyTaskTemplate
  if (previewMode.value === 'template') {
    const templateId = previewTemplateId.value.trim()
    if (!templateId) {
      ElMessage.warning(t('policy.previewTemplateRequired'))
      return
    }
    taskTemplate = templateId
  } else {
    const text = previewObjectText.value.trim()
    if (!text) {
      taskTemplate = {}
    } else {
      try {
        taskTemplate = JSON.parse(text) as ScanPolicyConfig
      } catch {
        ElMessage.warning(t('policy.previewObjectInvalid'))
        return
      }
    }
  }
  previewLoading.value = true
  try {
    const res = await applyPolicyApi(policy.id, taskTemplate)
    previewResult.value = res?.data ?? null
  } catch {
    previewResult.value = null
  } finally {
    previewLoading.value = false
  }
}

// ---- 删除 ----
const removePolicy = async (row: ScanPolicy) => {
  try {
    await ElMessageBox.confirm(
      t('policy.deleteConfirm', { name: row.name }),
      t('policy.confirmTitle'),
      {
        type: 'warning',
        confirmButtonText: t('common.ok'),
        cancelButtonText: t('common.cancel')
      }
    )
  } catch {
    return
  }
  try {
    await deletePolicyApi([row.id])
    ElMessage.success(t('policy.deleteSuccess'))
    getList()
  } catch {
    // 拦截器已提示
  }
}

const issueLabel = (field?: string) => {
  const key = policyFieldLabelKey(field)
  return key ? t(key) : field || '-'
}

onMounted(() => {
  loadTemplates()
})
</script>

<template>
  <ContentWrap :title="t('policy.title')" :message="t('policy.desc')">
    <template #header>
      <ElButton type="primary" :icon="plusIcon" @click="openCreate">
        {{ t('policy.create') }}
      </ElButton>
      <ElButton :icon="refreshIcon" :loading="loading" @click="getList">
        {{ t('policy.refresh') }}
      </ElButton>
    </template>

    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="6">
        <ElInput
          v-model="keyword"
          :placeholder="t('policy.keywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="4">
        <ElSelect
          v-model="enabledFilter"
          clearable
          :placeholder="t('policy.enabledAll')"
          class="full-width"
        >
          <ElOption :label="t('policy.enabledOn')" value="true" />
          <ElOption :label="t('policy.enabledOff')" value="false" />
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
        <div class="empty-state">{{ t('policy.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>

  <ElDialog
    v-model="formVisible"
    :title="editingId ? t('policy.dialogEditTitle') : t('policy.dialogCreateTitle')"
    width="720px"
  >
    <ElForm label-width="130px">
      <ElFormItem :label="t('policy.name')" required>
        <ElInput
          v-model="form.name"
          :maxlength="POLICY_MAX_NAME_LENGTH"
          show-word-limit
          :placeholder="t('policy.namePlaceholder')"
        />
      </ElFormItem>
      <ElFormItem :label="t('policy.description')">
        <ElInput
          v-model="form.description"
          type="textarea"
          :rows="2"
          :maxlength="POLICY_MAX_DESCRIPTION_LENGTH"
          show-word-limit
          :placeholder="t('policy.descriptionPlaceholder')"
        />
      </ElFormItem>
      <ElFormItem :label="t('policy.enabled')">
        <ElSwitch v-model="form.enabled" />
        <span class="form-hint inline-hint">{{ t('policy.enabledHint') }}</span>
      </ElFormItem>
      <ElFormItem :label="t('policy.template')">
        <ElSelect
          v-model="form.templateId"
          clearable
          filterable
          :placeholder="t('policy.templatePlaceholder')"
          class="full-width"
        >
          <ElOption
            v-for="(label, value) in templateNames"
            :key="value"
            :label="label"
            :value="value"
          />
        </ElSelect>
        <div class="form-hint">{{ t('policy.templateHint') }}</div>
      </ElFormItem>
      <ElFormItem :label="t('policy.plugins')">
        <ElInput
          v-model="form.pluginsText"
          type="textarea"
          :rows="4"
          :placeholder="t('policy.pluginsPlaceholder')"
        />
        <div class="form-hint">{{ t('policy.pluginsHint') }}</div>
      </ElFormItem>
      <ElRow :gutter="12">
        <ElCol :span="8">
          <ElFormItem :label="t('policy.threadNum')">
            <ElInputNumber
              v-model="form.threadNum"
              :min="POLICY_MIN_THREAD_NUM"
              :max="POLICY_MAX_THREAD_NUM"
              controls-position="right"
              class="full-width"
            />
          </ElFormItem>
        </ElCol>
        <ElCol :span="8">
          <ElFormItem :label="t('policy.rateLimit')">
            <ElInputNumber
              v-model="form.rateLimit"
              :min="POLICY_MIN_RATE_LIMIT"
              :max="POLICY_MAX_RATE_LIMIT"
              controls-position="right"
              class="full-width"
            />
          </ElFormItem>
        </ElCol>
        <ElCol :span="8">
          <ElFormItem :label="t('policy.timeoutSeconds')">
            <ElInputNumber
              v-model="form.timeoutSeconds"
              :min="POLICY_MIN_TIMEOUT_SECONDS"
              :max="POLICY_MAX_TIMEOUT_SECONDS"
              controls-position="right"
              class="full-width"
            />
          </ElFormItem>
        </ElCol>
      </ElRow>
      <div class="form-hint row-hint">
        {{ t('policy.numbersHint', { rateMin: POLICY_MIN_RATE_LIMIT }) }}
      </div>
      <ElFormItem :label="t('policy.dicts')">
        <ElRow :gutter="8" class="full-width">
          <ElCol :span="6">
            <ElInput v-model="form.dictDir" :placeholder="t('policy.dictDir')" />
          </ElCol>
          <ElCol :span="6">
            <ElInput v-model="form.dictSubdomain" :placeholder="t('policy.dictSubdomain')" />
          </ElCol>
          <ElCol :span="6">
            <ElInput v-model="form.dictBrute" :placeholder="t('policy.dictBrute')" />
          </ElCol>
          <ElCol :span="6">
            <ElInput v-model="form.dictPort" :placeholder="t('policy.dictPort')" />
          </ElCol>
        </ElRow>
        <div class="form-hint">{{ t('policy.dictsHint') }}</div>
      </ElFormItem>
      <ElFormItem :label="t('policy.options')">
        <ElInput
          v-model="form.optionsText"
          type="textarea"
          :rows="3"
          :placeholder="t('policy.optionsPlaceholder')"
        />
        <div v-if="optionsHint" class="form-hint warn-hint">{{ optionsHint }}</div>
        <div v-else class="form-hint">{{ t('policy.optionsHint') }}</div>
      </ElFormItem>
    </ElForm>

    <div v-if="usageNote" class="form-hint usage-hint">
      {{ t('policy.usageHint', { note: usageNote }) }}
    </div>

    <ElAlert
      v-if="validation"
      :type="validation.valid ? 'success' : 'error'"
      :closable="false"
      show-icon
    >
      <template #title>
        {{ validation.valid ? t('policy.validateOk') : t('policy.validateFailed') }}
      </template>
      <ul v-if="!validation.valid" class="issue-list">
        <li v-for="(issue, index) in validation.errors ?? []" :key="`${issue.code}-${index}`">
          <span class="issue-field">{{ issueLabel(issue.field) }}</span>
          <span class="issue-message">{{ issue.message }}</span>
        </li>
      </ul>
      <div v-else class="form-hint">{{ t('policy.validateOkHint') }}</div>
    </ElAlert>

    <template #footer>
      <ElButton :loading="validateLoading" @click="runValidate">
        {{ t('policy.validate') }}
      </ElButton>
      <ElButton @click="formVisible = false">{{ t('common.cancel') }}</ElButton>
      <ElButton type="primary" :loading="formLoading" @click="submitForm">
        {{ t('policy.save') }}
      </ElButton>
    </template>
  </ElDialog>

  <ElDialog v-model="validateVisible" :title="t('policy.validateResultTitle')" width="560px">
    <div class="section-title">{{ validateTarget?.name || '-' }}</div>
    <div v-loading="rowValidating">
      <ElAlert
        v-if="rowValidation"
        :type="rowValidation.valid ? 'success' : 'error'"
        :closable="false"
        show-icon
      >
        <template #title>
          {{ rowValidation.valid ? t('policy.validateOk') : t('policy.validateFailed') }}
        </template>
        <ul v-if="!rowValidation.valid" class="issue-list">
          <li v-for="(issue, index) in rowValidation.errors ?? []" :key="`${issue.code}-${index}`">
            <span class="issue-field">{{ issueLabel(issue.field) }}</span>
            <span class="issue-message">{{ issue.message }}</span>
          </li>
        </ul>
      </ElAlert>
      <div v-else-if="rowValidating" class="empty-state">{{ t('policy.validateHint') }}</div>
      <div v-else class="empty-state">{{ t('localization.requestFailed') }}</div>
    </div>
    <template #footer>
      <ElButton type="primary" @click="validateVisible = false">{{ t('common.ok') }}</ElButton>
    </template>
  </ElDialog>

  <ElDialog v-model="previewVisible" :title="t('policy.previewTitle')" width="760px">
    <div class="section-title">{{ previewTarget?.name || '-' }}</div>
    <ElForm label-width="130px">
      <ElFormItem :label="t('policy.previewSource')">
        <ElRadioGroup v-model="previewMode">
          <ElRadio value="template">{{ t('policy.previewSourceTemplate') }}</ElRadio>
          <ElRadio value="object">{{ t('policy.previewSourceObject') }}</ElRadio>
        </ElRadioGroup>
      </ElFormItem>
      <ElFormItem v-if="previewMode === 'template'" :label="t('policy.template')">
        <ElSelect
          v-model="previewTemplateId"
          clearable
          filterable
          :placeholder="t('policy.templatePlaceholder')"
          class="full-width"
        >
          <ElOption
            v-for="(label, value) in templateNames"
            :key="value"
            :label="label"
            :value="value"
          />
        </ElSelect>
        <div class="form-hint">{{ t('policy.previewTemplateHint') }}</div>
      </ElFormItem>
      <ElFormItem v-else :label="t('policy.previewSourceObject')">
        <ElInput
          v-model="previewObjectText"
          type="textarea"
          :rows="5"
          :placeholder="t('policy.previewObjectPlaceholder')"
        />
        <div class="form-hint">{{ t('policy.previewObjectHint') }}</div>
      </ElFormItem>
    </ElForm>
    <ElButton type="primary" :loading="previewLoading" @click="runPreview">
      {{ t('policy.previewRun') }}
    </ElButton>

    <template v-if="previewResult">
      <div class="section-title">
        {{ t('policy.previewMerged') }}
        <span class="section-hint">
          {{ t('policy.previewSourceLabel') }}: {{ previewResult.taskTemplateSource || '-' }}
        </span>
      </div>
      <pre class="json-panel">{{ mergedJson }}</pre>

      <div class="section-title">{{ t('policy.previewApplied') }}</div>
      <div v-if="(previewResult.appliedFields ?? []).length" class="tag-row">
        <ElTag v-for="field in previewResult.appliedFields" :key="field" size="small" type="info">
          {{ issueLabel(field) }}
        </ElTag>
      </div>
      <div v-else class="empty-state small">{{ t('policy.previewAppliedEmpty') }}</div>

      <div class="section-title">{{ t('policy.previewWarnings') }}</div>
      <ul v-if="(previewResult.warnings ?? []).length" class="issue-list">
        <li v-for="(warning, index) in previewResult.warnings" :key="`${warning.code}-${index}`">
          <span class="issue-field">{{ issueLabel(warning.field) }}</span>
          <span class="issue-message">{{ warning.message }}</span>
        </li>
      </ul>
      <div v-else class="empty-state small">{{ t('policy.previewNoWarnings') }}</div>
    </template>
    <div v-else class="empty-state">{{ t('policy.previewHint') }}</div>

    <template #footer>
      <ElButton @click="previewVisible = false">{{ t('policy.close') }}</ElButton>
    </template>
  </ElDialog>
</template>

<style scoped>
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

.inline-hint {
  margin-left: 10px;
}

.row-hint {
  margin: -8px 0 14px 130px;
}

.warn-hint {
  color: var(--el-color-warning);
}

.usage-hint {
  margin: -6px 0 12px;
}

.empty-state {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}

.empty-state.small {
  padding: 8px;
}

.row-actions {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 2px;
}

.section-title {
  display: flex;
  gap: 10px;
  align-items: baseline;
  margin: 18px 0 10px;
  font-weight: 600;
}

.section-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-weight: 400;
}

.json-panel {
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

.issue-list {
  margin: 6px 0 0;
  padding-left: 18px;
  line-height: 1.7;
}

.issue-field {
  margin-right: 8px;
  font-weight: 600;
}

.issue-message {
  word-break: break-all;
}

.tag-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
</style>
