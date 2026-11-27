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

const mcpConfigExample = `{
  "mcpServers": {
    "scopesentry": {
      "url": "http://your-host:8082/mcp",
      "headers": {
        "X-API-Key": "ssk_你的密钥"
      }
    }
  }
}`

// 与后端 mcpguard 的 scope 常量一一对应
const SCOPE_OPTIONS = [
  { value: 'asset:read', label: 'asset:read（资产/POC/指纹/字典/看板/资产组/地契/ICP/审计 只读）' },
  {
    value: 'asset:write',
    label: 'asset:write（打标签/导出/规则启停/字典维护/建资产组/删资产组/建地契）'
  },
  { value: 'icp:write', label: 'icp:write（建 ICP 主体任务/停止 ICP 任务）' },
  { value: 'task:read', label: 'task:read（任务、扫描策略查询）' },
  { value: 'task:write', label: 'task:write（任务启停删、策略增删）' },
  { value: 'project:read', label: 'project:read' },
  { value: 'project:write', label: 'project:write' },
  { value: 'template:read', label: 'template:read' },
  { value: 'template:write', label: 'template:write' },
  { value: 'plugin:read', label: 'plugin:read' },
  { value: 'plugin:write', label: 'plugin:write（插件安装/重检/卸载）' },
  { value: 'node:read', label: 'node:read' },
  { value: 'node:write', label: 'node:write（节点重启/删除）' },
  { value: '*', label: '*' }
]

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
    for (const it of d.scopes || []) counts[it.scope] = it.tools
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
const scopeLabel = (o: { value: string; label: string }) => {
  const n = scopeTools.value[o.value]
  return n ? `${o.label} · ${n} 个工具` : o.label
}
const filteredCatalog = computed(() => {
  const q = catalogSearch.value.trim().toLowerCase()
  if (!q) return toolCatalog.value
  return toolCatalog.value.filter((it) => it.name.toLowerCase().includes(q) || it.scope.toLowerCase().includes(q))
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
      if (!value || value.length === 0) return '*（历史 key）'
      return value.join(', ')
    }
  },
  {
    field: 'rateLimitPerMin',
    label: '限速/分',
    width: 90,
    formatter: (_row, __: TableColumn, value: number) => (value ? String(value) : '不限')
  },
  {
    field: 'expiresAt',
    label: '过期',
    minWidth: 160,
    formatter: (_row, __: TableColumn, value?: string) => (value ? formatDateTime(value) : '长期')
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
        <BaseButton type="primary" onClick={() => openEditDialog(row)}>{t('apiKey.edit')}</BaseButton>
        <BaseButton type={row.enabled ? 'warning' : 'success'} onClick={() => handleToggle(row)}>
          {row.enabled ? '禁用' : '启用'}
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
const form = ref({ name: '', scope: ['asset:read'], rateLimitPerMin: 0 })
const createdKey = ref('')

const openCreateDialog = () => {
  form.value.name = ''
  form.value.scope = ['asset:read']
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
const editForm = ref({ name: '', scope: [], tools: '', rateLimitPerMin: 0, expiresAt: '' })

const openEditDialog = (row) => {
  editingId.value = row.id
  editForm.value = {
    name: row.name || '',
    scope: Array.isArray(row.scope) ? [...row.scope] : [],
    tools: Array.isArray(row.tools) ? row.tools.join(', ') : '',
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
      tools: String(editForm.value.tools || '').split(',').map((x) => x.trim()).filter(Boolean),
      rateLimitPerMin: Number(editForm.value.rateLimitPerMin) || 0,
      expiresAt: editForm.value.expiresAt || null
    })
    ElMessage.success(t('apiKey.saved'))
    editDialogVisible.value = false
    getList()
  } catch (e) {
    ElMessage.error((e && e.message) || t('common.operateFailed'))
  } finally {
    editLoading.value = false
  }
}

const handleCopyKey = () => {
  copy(createdKey.value)
}

const handleDelete = async (row: { id: string; name: string }) => {
  await ElMessageBox.confirm(t('apiKey.deleteConfirm', { name: row.name }), t('common.reminder'), {
    confirmButtonText: t('common.ok'),
    cancelButtonText: t('common.cancel'),
    type: 'warning'
  })
  const res = await deleteMcpKeyApi(row.id)
  ElMessage.success(deleteResultMessage(res, 1))
  getList()
}

const handleToggle = async (row: { id: string; name: string; enabled: boolean }) => {
  await setMcpKeyStatusApi(row.id, !row.enabled)
  getList()
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
        <ElButton @click="openToolCatalog">工具清单（{{ toolCatalog.length || '…' }}）</ElButton>
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
          <ElOption v-for="o in SCOPE_OPTIONS" :key="o.value" :label="scopeLabel(o)" :value="o.value" />
        </ElSelect>
      </ElFormItem>
      <ElFormItem label="tools">
        <ElInput v-model="editForm.tools" :placeholder="t('apiKey.toolsHint')" />
      </ElFormItem>
      <ElFormItem label="限速/分">
        <ElInputNumber v-model="editForm.rateLimitPerMin" :min="0" :max="100000" controls-position="right" />
      </ElFormItem>
      <ElFormItem :label="t('apiKey.expiresAt')">
        <ElDatePicker v-model="editForm.expiresAt" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
      </ElFormItem>
    </ElForm>
    <template #footer>
      <div style="text-align: right">
        <ElButton @click="editDialogVisible = false">{{ t('common.cancel') }}</ElButton>
        <ElButton type="primary" :loading="editLoading" @click="handleSaveEdit">{{ t('common.save') }}</ElButton>
      </div>
    </template>
  </Dialog>

  <Dialog v-model="catalogVisible" title="MCP 工具与权限对照" center :maxHeight="560">
    <div class="catalog-toolbar">
      <ElInput v-model="catalogSearch" placeholder="按工具名或 scope 搜索" clearable />
      <span class="catalog-hint">
        共 {{ toolCatalog.length }} 个工具；列表来自后端 mcpguard.ToolScopes，新增工具会在这里自动出现
      </span>
    </div>
    <ElTable :data="filteredCatalog" size="small" max-height="420" style="width: 100%">
      <ElTableColumn prop="name" label="工具" min-width="240" />
      <ElTableColumn prop="scope" label="所需 scope" width="160" />
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
          <ElCheckbox v-for="opt in SCOPE_OPTIONS" :key="opt.value" :value="opt.value">
            {{ scopeLabel(opt) }}
          </ElCheckbox>
        </ElCheckboxGroup>
      </ElFormItem>
      <ElFormItem label="限速/分">
        <ElInputNumber v-model="form.rateLimitPerMin" :min="0" :max="100000" />
        <span class="hint-text">0 = 跟随全局配置</span>
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
