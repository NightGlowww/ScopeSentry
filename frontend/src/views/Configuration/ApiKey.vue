<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import { deleteResultMessage } from '@/utils/deleteResult'
import { useI18n } from '@/hooks/web/useI18n'
import { ref, reactive, computed, onMounted } from 'vue'
import {
  ElButton,
  ElCol,
  ElRow,
  ElInput,
  ElForm,
  ElFormItem,
  ElAlert,
  ElTag,
  ElMessage,
  ElMessageBox,
  ElCheckboxGroup,
  ElCheckbox,
  ElInputNumber,
  ElSelect,
  ElOption,
  ElDatePicker,
  ElTable,
  ElTableColumn
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { Dialog } from '@/components/Dialog'
import { useTable } from '@/hooks/web/useTable'
import { BaseButton } from '@/components/Button'
import {
  createMcpKeyApi,
  deleteMcpKeyApi,
  getMcpKeysApi,
  setMcpKeyStatusApi,
  updateMcpKeyApi
} from '@/api/apikey'
import { getMcpToolsApi } from '@/api/mcp'
import { useClipboard } from '@/hooks/web/useClipboard'
import { formatTime } from '@/utils'

const { t } = useI18n()
const { copy, copied } = useClipboard()

const mcpConfigExample = computed(
  () => `{
  "mcpServers": {
    "scopesentry": {
      "url": "http://your-host:8082/mcp",
      "headers": {
        "X-API-Key": "ssk_${t('apiKey.exampleApiKey')}"
      }
    }
  }
}`
)

// 后端实际在用的 scope（从 /api/mcp/tools 回来）。硬编码列表漏项时会自动补上，
// 否则新加的 scope（如 system:read）在界面上根本选不到 —— 只能建 * 全权 key 或直接调接口。
const backendScopes = ref<string[]>([])

// 与后端 mcpguard 的 scope 常量一一对应
const SCOPE_OPTIONS = computed<{ value: string; label: string }[]>(() => [
  { value: 'asset:read', label: t('apiKey.scopeAssetRead') },
  { value: 'asset:write', label: t('apiKey.scopeAssetWrite') },
  { value: 'icp:write', label: t('apiKey.scopeIcpWrite') },
  { value: 'task:read', label: t('apiKey.scopeTaskRead') },
  { value: 'task:write', label: t('apiKey.scopeTaskWrite') },
  { value: 'project:read', label: 'project:read' },
  { value: 'project:write', label: 'project:write' },
  { value: 'template:read', label: 'template:read' },
  { value: 'template:write', label: 'template:write' },
  { value: 'plugin:read', label: 'plugin:read' },
  { value: 'plugin:write', label: t('apiKey.scopePluginWrite') },
  { value: 'node:read', label: 'node:read' },
  { value: 'node:write', label: t('apiKey.scopeNodeWrite') },
  { value: 'system:read', label: t('apiKey.scopeSystemRead') },
  { value: '*', label: '*' }
])

// 工具 ↔ scope 对照：来自后端 mcpguard.ToolScopes（唯一真相源）。
// 只写死在界面上的「asset:read（资产/…只读）」解释不了「这个 scope 到底能调几个工具」。
const scopeTools = ref<Record<string, number>>({})
const toolCatalog = ref<{ name: string; scope: string }[]>([])
const catalogVisible = ref(false)
const catalogSearch = ref('')
const loadToolCatalog = async () => {
  try {
    const res: any = await getMcpToolsApi()
    const d = res?.data || {}
    toolCatalog.value = d.list || []
    const counts: Record<string, number> = {}
    const names: string[] = []
    for (const it of d.scopes || []) {
      counts[it.scope] = it.tools
      if (it.scope && it.scope !== '*') names.push(it.scope)
    }
    backendScopes.value = names
    // '*' 是超权，覆盖全部工具
    counts['*'] = d.total || toolCatalog.value.length
    scopeTools.value = counts
  } catch {
    scopeTools.value = {}
    toolCatalog.value = []
  }
}
const openToolCatalog = async () => {
  if (!toolCatalog.value.length) await loadToolCatalog()
  catalogVisible.value = true
}
// 硬编码清单 + 后端多出来的 scope（后端加了新 scope 而界面还没跟上时兜底）
const allScopeOptions = computed(() => {
  const known = new Set(SCOPE_OPTIONS.value.map((o) => o.value))
  const extra = backendScopes.value
    .filter((s) => !known.has(s))
    .map((s) => ({ value: s, label: s }))
  return [...SCOPE_OPTIONS.value, ...extra]
})
const scopeLabel = (o: { value: string; label: string }) => {
  const n = scopeTools.value[o.value]
  return n ? `${o.label} · ${t('apiKey.scopeToolCount', { n })}` : o.label
}
// 工具白名单候选：只列当前已选 scope 下的工具（选了 * 就给全部）。
// 空选 = 不限制，按 scope 放行全部 —— 与后端 ToolAllowed 的语义一致。
const toolsForScopes = (scopes: string[]) => {
  const set = new Set(scopes || [])
  const all = set.size === 0 || set.has('*')
  return toolCatalog.value.filter((t) => all || set.has(t.scope)).map((t) => t.name)
}
const filteredCatalog = computed(() => {
  const q = catalogSearch.value.trim().toLowerCase()
  if (!q) return toolCatalog.value
  return toolCatalog.value.filter(
    (it) => it.name.toLowerCase().includes(q) || it.scope.toLowerCase().includes(q)
  )
})

const formatDateTime = (value?: string) => {
  if (!value) return '-'
  return formatTime(value, 'yyyy-MM-dd HH:mm:ss')
}

const columns = reactive<TableColumn[]>([
  {
    field: 'name',
    label: t('common.name'),
    minWidth: 120
  },
  {
    field: 'keyPrefix',
    label: t('apiKey.keyPrefix'),
    minWidth: 140
  },
  {
    field: 'scope',
    label: 'Scope',
    minWidth: 200,
    formatter: (_row, __: TableColumn, value: string[]) => {
      if (!value || value.length === 0) return t('apiKey.scopeHistory')
      return value.join(', ')
    }
  },
  {
    field: 'rateLimitPerMin',
    label: t('apiKey.rateLimitPerMin'),
    width: 90,
    formatter: (_row, __: TableColumn, value: number) =>
      value ? String(value) : t('apiKey.rateUnlimited')
  },
  {
    field: 'expiresAt',
    label: t('apiKey.expiresColumn'),
    minWidth: 160,
    formatter: (_row, __: TableColumn, value?: string) =>
      value ? formatDateTime(value) : t('apiKey.expiresNever')
  },
  {
    field: 'createdBy',
    label: t('apiKey.createdBy'),
    minWidth: 100
  },
  {
    field: 'createdAt',
    label: t('apiKey.createdAt'),
    minWidth: 160,
    formatter: (_row, __: TableColumn, value: string) => formatDateTime(value)
  },
  {
    field: 'lastUsedAt',
    label: t('apiKey.lastUsedAt'),
    minWidth: 160,
    formatter: (_row, __: TableColumn, value: string) => formatDateTime(value)
  },
  {
    field: 'enabled',
    label: t('apiKey.status'),
    minWidth: 80,
    formatter: (_row, __: TableColumn, value: boolean) => (
      <ElTag type={value ? 'success' : 'info'} effect="light">
        {value ? t('apiKey.enabled') : t('apiKey.disabled')}
      </ElTag>
    )
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    width: 190,
    formatter: (row) => (
      <div class="flex items-center" style={'gap:6px'}>
        <BaseButton type="primary" onClick={() => openEditDialog(row)}>
          {t('apiKey.edit')}
        </BaseButton>
        <BaseButton type={row.enabled ? 'warning' : 'success'} onClick={() => handleToggle(row)}>
          {row.enabled ? t('apiKey.actionDisable') : t('apiKey.actionEnable')}
        </BaseButton>
        <BaseButton type="danger" onClick={() => handleDelete(row)}>
          {t('common.delete')}
        </BaseButton>
      </div>
    )
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const res = await getMcpKeysApi({ pageIndex: 1, pageSize: 200 })
    return {
      list: res.data.list || []
    }
  }
})

const { loading, dataList } = tableState
const { getList } = tableMethods
// 进页面就把工具↔scope 对照表拉下来：scope 选择器要显示「N 个工具」
onMounted(() => {
  loadToolCatalog()
})

function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}

const createDialogVisible = ref(false)
const keyDialogVisible = ref(false)
const createLoading = ref(false)
const form = ref({ name: '', scope: ['asset:read'], tools: [] as string[], rateLimitPerMin: 0 })
const createdKey = ref('')

const openCreateDialog = () => {
  form.value.name = ''
  form.value.scope = ['asset:read']
  form.value.tools = []
  form.value.rateLimitPerMin = 0
  createDialogVisible.value = true
}

const handleCreate = async () => {
  const name = form.value.name.trim()
  if (!name) {
    // 静默 return 会让用户以为点了没反应
    ElMessage.warning(t('apiKey.nameRequired'))
    return
  }
  createLoading.value = true
  try {
    const res = await createMcpKeyApi({
      name,
      scope: form.value.scope.length ? form.value.scope : ['*'],
      tools: form.value.tools,
      rateLimitPerMin: form.value.rateLimitPerMin || 0
    })
    createDialogVisible.value = false
    createdKey.value = res.data.key
    keyDialogVisible.value = true
    getList()
  } catch (e: any) {
    // 创建失败必须给提示：此前只复位 loading，失败时对话框留在原地、无任何反馈
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    createLoading.value = false
  }
}

// 编辑治理字段：建好后原本只能启停/删除，改权限得重建一把 key（2026-11-21）
const editDialogVisible = ref(false)
const editLoading = ref(false)
const editingId = ref('')
// tools 改成数组：原来是一串逗号文本，用户得自己敲工具名，敲错一个就是「明明给了权限却调不动」
const editForm = ref({
  name: '',
  scope: [] as string[],
  tools: [] as string[],
  rateLimitPerMin: 0,
  expiresAt: ''
})

const openEditDialog = (row) => {
  editingId.value = row.id
  editForm.value = {
    name: row.name || '',
    scope: Array.isArray(row.scope) ? [...row.scope] : [],
    tools: Array.isArray(row.tools) ? [...row.tools] : [],
    rateLimitPerMin: Number(row.rateLimitPerMin) || 0,
    expiresAt: row.expiresAt ? String(row.expiresAt).slice(0, 10) : ''
  }
  editDialogVisible.value = true
}

const handleSaveEdit = async () => {
  if (!editingId.value) return
  editLoading.value = true
  try {
    await updateMcpKeyApi(editingId.value, {
      name: editForm.value.name,
      scope: editForm.value.scope,
      tools: editForm.value.tools,
      rateLimitPerMin: Number(editForm.value.rateLimitPerMin) || 0,
      expiresAt: editForm.value.expiresAt || undefined
    })
    ElMessage.success(t('apiKey.saved'))
    editDialogVisible.value = false
    getList()
  } catch (e) {
    ElMessage.error((e as any)?.message || t('common.operateFailed'))
  } finally {
    editLoading.value = false
  }
}

const handleCopyKey = () => {
  copy(createdKey.value)
}

const handleDelete = async (row: { id: string; name: string }) => {
  try {
    await ElMessageBox.confirm(t('apiKey.deleteConfirm', { name: row.name }), t('common.reminder'), {
      confirmButtonText: t('common.ok'),
      cancelButtonText: t('common.cancel'),
      type: 'warning'
    })
  } catch {
    // 用户点取消：ElMessageBox 以 reject 表达，别当成失败弹错误
    return
  }
  try {
    const res = await deleteMcpKeyApi(row.id)
    ElMessage.success(deleteResultMessage(res, 1))
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    // 无论成败都对齐服务端：删失败多半是这条已经在别处没了，界面不能继续留着它
    getList()
  }
}

const handleToggle = async (row: { id: string; name: string; enabled: boolean }) => {
  const next = !row.enabled
  const stateLabel = next ? t('apiKey.enabled') : t('apiKey.disabled')
  try {
    await setMcpKeyStatusApi(row.id, next)
    ElMessage.success(t('apiKey.statusChanged', { name: row.name, state: stateLabel }))
  } catch (e: any) {
    // 以前这里没有任何分支：接口报错（比如这条 key 已不存在）时按钮点下去毫无反应，
    // 只有控制台里一条 unhandled rejection，界面上永远停在旧状态。
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    getList()
  }
}

const closeKeyDialog = () => {
  keyDialogVisible.value = false
  createdKey.value = ''
}
</script>

<template>
  <ContentWrap>
    <ElAlert
      :title="t('apiKey.usageTitle')"
      type="info"
      :closable="false"
      show-icon
      class="mb-16px"
    >
      <template #default>
        <div class="api-key-usage">
          <p>{{ t('apiKey.usageDesc') }}</p>
          <pre class="api-key-example">{{ mcpConfigExample }}</pre>
        </div>
      </template>
    </ElAlert>

    <ElRow :gutter="16" class="mb-10px">
      <ElCol :span="4">
        <ElButton type="primary" @click="openCreateDialog">{{ t('apiKey.createTitle') }}</ElButton>
      </ElCol>
      <ElCol :span="4">
        <ElButton @click="openToolCatalog">{{
          t('apiKey.toolCatalogButton', { count: toolCatalog.length || '…' })
        }}</ElButton>
      </ElCol>
    </ElRow>

    <Table
      :columns="columns"
      :data="dataList"
      stripe
      :border="true"
      :loading="loading"
      :resizable="true"
      @register="tableRegister"
      :headerCellStyle="tableHeaderColor"
    />
  </ContentWrap>

  <Dialog
    v-model="editDialogVisible"
    :title="t('apiKey.edit')"
    center
    style="border-radius: 15px"
    :maxHeight="560"
  >
    <ElForm label-width="110px" @submit.prevent>
      <ElFormItem :label="t('common.name')">
        <ElInput v-model="editForm.name" maxlength="64" show-word-limit />
      </ElFormItem>
      <ElFormItem label="Scope">
        <ElSelect v-model="editForm.scope" multiple filterable style="width: 100%">
          <ElOption
            v-for="o in allScopeOptions"
            :key="o.value"
            :label="scopeLabel(o)"
            :value="o.value"
          />
        </ElSelect>
      </ElFormItem>
      <ElFormItem :label="t('apiKey.toolsWhitelist')">
        <ElSelect
          v-model="editForm.tools"
          multiple
          filterable
          allow-create
          default-first-option
          collapse-tags
          collapse-tags-tooltip
          style="width: 100%"
          :placeholder="t('apiKey.toolsPlaceholder')"
        >
          <ElOption v-for="n in toolsForScopes(editForm.scope)" :key="n" :label="n" :value="n" />
        </ElSelect>
        <div class="hint-text">{{ t('apiKey.toolsSelected', { n: editForm.tools.length }) }}</div>
      </ElFormItem>
      <ElFormItem :label="t('apiKey.rateLimitPerMin')">
        <ElInputNumber
          v-model="editForm.rateLimitPerMin"
          :min="0"
          :max="100000"
          controls-position="right"
        />
      </ElFormItem>
      <ElFormItem :label="t('apiKey.expiresAt')">
        <ElDatePicker
          v-model="editForm.expiresAt"
          type="date"
          value-format="YYYY-MM-DD"
          style="width: 100%"
        />
      </ElFormItem>
    </ElForm>
    <template #footer>
      <div style="text-align: right">
        <ElButton @click="editDialogVisible = false">{{ t('common.cancel') }}</ElButton>
        <ElButton type="primary" :loading="editLoading" @click="handleSaveEdit">{{
          t('common.save')
        }}</ElButton>
      </div>
    </template>
  </Dialog>

  <Dialog v-model="catalogVisible" :title="t('apiKey.catalogTitle')" center :maxHeight="560">
    <div class="catalog-toolbar">
      <ElInput
        v-model="catalogSearch"
        :placeholder="t('apiKey.catalogSearchPlaceholder')"
        clearable
      />
      <span class="catalog-hint">
        {{ t('apiKey.catalogSummary', { n: toolCatalog.length }) }}
      </span>
    </div>
    <ElTable :data="filteredCatalog" size="small" max-height="420" style="width: 100%">
      <ElTableColumn prop="name" :label="t('apiKey.catalogToolColumn')" min-width="240" />
      <ElTableColumn prop="scope" :label="t('apiKey.catalogScopeColumn')" width="160" />
    </ElTable>
    <template #footer>
      <div style="text-align: right">
        <ElButton @click="catalogVisible = false">{{ t('common.ok') }}</ElButton>
      </div>
    </template>
  </Dialog>

  <Dialog
    v-model="createDialogVisible"
    :title="t('apiKey.createTitle')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="520"
  >
    <ElForm :model="form" label-width="80px" @submit.prevent>
      <ElFormItem :label="t('common.name')" required>
        <ElInput
          v-model="form.name"
          :placeholder="t('apiKey.namePlaceholder')"
          maxlength="64"
          show-word-limit
        />
      </ElFormItem>
      <ElFormItem label="Scope">
        <ElCheckboxGroup v-model="form.scope">
          <ElCheckbox v-for="opt in allScopeOptions" :key="opt.value" :value="opt.value">
            {{ scopeLabel(opt) }}
          </ElCheckbox>
        </ElCheckboxGroup>
      </ElFormItem>
      <ElFormItem :label="t('apiKey.toolsWhitelist')">
        <ElSelect
          v-model="form.tools"
          multiple
          filterable
          allow-create
          default-first-option
          collapse-tags
          collapse-tags-tooltip
          style="width: 100%"
          :placeholder="t('apiKey.toolsPlaceholder')"
        >
          <ElOption v-for="n in toolsForScopes(form.scope)" :key="n" :label="n" :value="n" />
        </ElSelect>
        <div class="hint-text">{{ t('apiKey.toolsSelected', { n: form.tools.length }) }}</div>
      </ElFormItem>
      <ElFormItem :label="t('apiKey.rateLimitPerMin')">
        <ElInputNumber v-model="form.rateLimitPerMin" :min="0" :max="100000" />
        <span class="hint-text">{{ t('apiKey.rateFollowGlobal') }}</span>
      </ElFormItem>
      <ElFormItem>
        <ElButton type="primary" :loading="createLoading" @click="handleCreate">
          {{ t('common.ok') }}
        </ElButton>
        <ElButton @click="createDialogVisible = false">{{ t('common.cancel') }}</ElButton>
      </ElFormItem>
    </ElForm>
  </Dialog>

  <Dialog
    v-model="keyDialogVisible"
    :title="t('apiKey.keyCreatedTitle')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="360"
    @close="closeKeyDialog"
  >
    <ElAlert
      :title="t('apiKey.keyShowOnce')"
      type="warning"
      :closable="false"
      show-icon
      class="mb-16px"
    />
    <ElForm label-width="80px">
      <ElFormItem :label="t('apiKey.fullKey')">
        <ElInput v-model="createdKey" readonly>
          <template #append>
            <ElButton @click="handleCopyKey">
              {{ copied ? t('apiKey.copied') : t('apiKey.copyKey') }}
            </ElButton>
          </template>
        </ElInput>
      </ElFormItem>
    </ElForm>
    <div class="dialog-footer">
      <ElButton type="primary" @click="closeKeyDialog">{{ t('common.ok') }}</ElButton>
    </div>
  </Dialog>
</template>

<style scoped>
.api-key-usage p {
  margin: 0 0 8px;
  line-height: 1.6;
}

.api-key-example {
  margin: 0;
  padding: 12px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
  white-space: pre-wrap;
  word-break: break-all;
  font-size: 12px;
  line-height: 1.5;
}

.hint-text {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
}

.mb-16px {
  margin-bottom: 16px;
}

.mb-10px {
  margin-bottom: 10px;
}
</style>
