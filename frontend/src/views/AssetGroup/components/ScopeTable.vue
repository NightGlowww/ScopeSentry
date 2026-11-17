<script setup lang="tsx">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { ElButton, ElInput, ElMessageBox, ElPagination, ElTag } from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { useTable } from '@/hooks/web/useTable'
import { formatTime } from '@/utils'
import {
  bindAssetScopeEnterpriseApi,
  clearAssetScopeUpdateErrorApi,
  deleteAssetScopeApi,
  getAssetScopeListApi,
  moveAssetScopeGroupApi
} from '@/api/assetGroup'
import { AssetGroup, AssetScope, AssetScopeType } from '@/api/assetGroup/types'
import ScopeFormDialog from './ScopeFormDialog.vue'
import MoveScopeDialog from './MoveScopeDialog.vue'
import BindEnterpriseDialog from './BindEnterpriseDialog.vue'

const props = defineProps<{
  groupId: string
  groupName: string
  groups: AssetGroup[]
}>()

const emit = defineEmits<{
  (event: 'changed'): void
}>()

const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const search = ref('')
const selectedRows = ref<AssetScope[]>([])
const selectedIds = computed(() => selectedRows.value.map((row) => row.id))

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const res = await getAssetScopeListApi({
      groupId: props.groupId,
      search: search.value,
      pageIndex: tableState.currentPage.value,
      pageSize: tableState.pageSize.value
    })
    const payload = res?.data
    return { list: payload?.list ?? [], total: payload?.total ?? 0 }
  }
})

const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList, getElTableExpose } = tableMethods

const scopeFormVisible = ref(false)
const editingScope = ref<AssetScope | null>(null)
const moveDialogVisible = ref(false)
const bindDialogVisible = ref(false)

const defaultEnterprise = computed(() =>
  selectedRows.value.length === 1 ? selectedRows.value[0].enterprise || '' : ''
)

const maxHeight = ref(480)
const setMaxHeight = () => {
  const screenHeight = window.innerHeight || document.documentElement.clientHeight
  maxHeight.value = Math.max(320, screenHeight * 0.6)
}

const handleSelectionChange = (rows: AssetScope[]) => {
  selectedRows.value = rows
}

// currentPage 变化时 useTable 内部会重新拉取，手动 getList 会重复请求，
// 所以只有已经在第一页时才直接刷新
const reload = (resetPage = true) => {
  if (!resetPage) {
    getList()
    return
  }
  if (currentPage.value === 1) {
    getList()
  } else {
    currentPage.value = 1
  }
}

const clearSelection = async () => {
  const table = await getElTableExpose()
  table?.clearSelection()
  selectedRows.value = []
}

const refreshAfterRemove = async (removed: number) => {
  await clearSelection()
  const remain = Math.max(0, total.value - removed)
  const lastPage = Math.max(1, Math.ceil(remain / pageSize.value))
  if (currentPage.value > lastPage) {
    currentPage.value = lastPage
  } else {
    getList()
  }
  emit('changed')
}

const openCreate = () => {
  editingScope.value = null
  scopeFormVisible.value = true
}

const openEdit = (row: AssetScope) => {
  editingScope.value = row
  scopeFormVisible.value = true
}

const removeRows = async (ids: string[], count: number) => {
  try {
    await ElMessageBox.confirm(t('assetGroup.deleteScopeTip', { count }), t('common.reminder'), {
      confirmButtonText: t('common.ok'),
      cancelButtonText: t('common.cancel'),
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    await deleteAssetScopeApi(ids)
  } catch {
    // 错误提示由 axios 拦截器统一弹出
    return
  }
  await refreshAfterRemove(count)
}

const handleBatchDelete = () => {
  if (!selectedIds.value.length) return
  removeRows(selectedIds.value, selectedIds.value.length)
}

const handleMoveConfirm = async (groupId: string) => {
  const ids = selectedIds.value
  if (!ids.length) return
  try {
    await moveAssetScopeGroupApi(ids, groupId)
  } catch {
    return
  }
  await refreshAfterRemove(ids.length)
}

// 绑定企业主体走后端单条接口，多选时逐条提交，前端不做并发轰炸
const handleBindConfirm = async (enterprise: string) => {
  const ids = selectedIds.value
  if (!ids.length) return
  try {
    for (const id of ids) {
      await bindAssetScopeEnterpriseApi(id, enterprise)
    }
  } catch {
    return
  }
  await clearSelection()
  getList()
}

const handleClearUpdateError = async () => {
  const ids = selectedIds.value
  if (!ids.length) return
  try {
    await clearAssetScopeUpdateErrorApi(ids)
  } catch {
    return
  }
  await clearSelection()
  getList()
}

const handleSaved = () => {
  getList()
  emit('changed')
}

const scopeTypeLabel = (type: AssetScopeType | '') => {
  switch (type) {
    case 'domain':
      return t('assetGroup.scopeTypeDomain')
    case 'ip':
      return t('assetGroup.scopeTypeIp')
    case 'mixed':
      return t('assetGroup.scopeTypeMixed')
    default:
      return t('assetGroup.scopeTypeAuto')
  }
}

const columns = reactive<TableColumn[]>([
  {
    field: 'selection',
    type: 'selection',
    minWidth: 55
  },
  {
    field: 'index',
    label: '#',
    type: 'index',
    minWidth: 55
  },
  {
    field: 'name',
    label: t('assetGroup.scopeName'),
    minWidth: 160
  },
  {
    field: 'scope',
    label: t('assetGroup.scopeContent'),
    minWidth: 260,
    showOverflowTooltip: true
  },
  {
    field: 'scopeType',
    label: t('assetGroup.scopeType'),
    minWidth: 110,
    formatter: (_row: AssetScope, __: TableColumn, value: AssetScopeType | '') => (
      <ElTag size="small" effect="plain">
        {scopeTypeLabel(value)}
      </ElTag>
    )
  },
  {
    field: 'domainArray',
    label: t('assetGroup.domainCount'),
    minWidth: 90,
    formatter: (_row: AssetScope, __: TableColumn, value: string[]) => String(value?.length ?? 0)
  },
  {
    field: 'ipArray',
    label: t('assetGroup.ipCount'),
    minWidth: 90,
    formatter: (_row: AssetScope, __: TableColumn, value: string[]) => String(value?.length ?? 0)
  },
  {
    field: 'enterprise',
    label: t('assetGroup.enterprise'),
    minWidth: 150,
    formatter: (_row: AssetScope, __: TableColumn, value: string) => value || '-'
  },
  {
    field: 'updateError',
    label: t('assetGroup.updateError'),
    minWidth: 160,
    formatter: (_row: AssetScope, __: TableColumn, value: string) =>
      value ? (
        <ElTag type="danger" effect="light" size="small">
          {value}
        </ElTag>
      ) : (
        '-'
      )
  },
  {
    field: 'createdAt',
    label: t('assetGroup.createdAt'),
    minWidth: 165,
    formatter: (_row: AssetScope, __: TableColumn, value: string) =>
      value ? formatTime(value, 'yyyy-MM-dd HH:mm:ss') : '-'
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    minWidth: 140,
    fixed: 'right',
    formatter: (row: AssetScope) => (
      <div>
        <ElButton type="primary" link onClick={() => openEdit(row)}>
          {t('common.edit')}
        </ElButton>
        <ElButton type="danger" link onClick={() => removeRows([row.id], 1)}>
          {t('common.delete')}
        </ElButton>
      </div>
    )
  }
])

function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}

watch(
  () => props.groupId,
  () => {
    handleSelectionChange([])
    reload()
  }
)

onMounted(() => {
  setMaxHeight()
  window.addEventListener('resize', setMaxHeight)
})

onUnmounted(() => {
  window.removeEventListener('resize', setMaxHeight)
})
</script>

<template>
  <ContentWrap :title="t('assetGroup.scopeTitle')">
    <template #header>
      <ElTag v-if="groupName" size="small" effect="plain">{{ groupName }}</ElTag>
    </template>

    <div class="scope-toolbar">
      <ElInput
        v-model="search"
        class="scope-toolbar__search"
        clearable
        :placeholder="t('assetGroup.searchPlaceholder')"
        @keyup.enter="reload()"
        @clear="reload()"
      />
      <ElButton type="primary" :icon="searchIcon" @click="reload()">
        {{ t('common.search') }}
      </ElButton>
      <ElButton type="primary" @click="openCreate">{{ t('assetGroup.addScope') }}</ElButton>
      <div class="scope-toolbar__spacer"></div>
      <span v-if="selectedRows.length" class="scope-toolbar__selected">
        {{ t('assetGroup.selected', { count: selectedRows.length }) }}
      </span>
      <ElButton :disabled="!selectedRows.length" @click="moveDialogVisible = true">
        {{ t('assetGroup.batchMove') }}
      </ElButton>
      <ElButton :disabled="!selectedRows.length" @click="bindDialogVisible = true">
        {{ t('assetGroup.bindEnterprise') }}
      </ElButton>
      <ElButton :disabled="!selectedRows.length" @click="handleClearUpdateError">
        {{ t('assetGroup.clearUpdateError') }}
      </ElButton>
      <ElButton type="danger" :disabled="!selectedRows.length" @click="handleBatchDelete">
        {{ t('common.delete') }}
      </ElButton>
    </div>

    <Table
      :columns="columns"
      :data="dataList"
      :border="true"
      :loading="loading"
      :max-height="maxHeight"
      stripe
      row-key="id"
      :headerCellStyle="tableHeaderColor"
      @register="tableRegister"
      @selection-change="handleSelectionChange"
    />

    <ElPagination
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      class="mt-10px"
      :page-sizes="[10, 20, 50, 100]"
      layout="total, sizes, prev, pager, next, jumper"
      :total="total"
    />

    <ScopeFormDialog
      v-model="scopeFormVisible"
      :group-id="groupId"
      :scope="editingScope"
      @saved="handleSaved"
    />
    <MoveScopeDialog
      v-model="moveDialogVisible"
      :groups="groups"
      :count="selectedRows.length"
      :current-group-id="groupId"
      @confirm="handleMoveConfirm"
    />
    <BindEnterpriseDialog
      v-model="bindDialogVisible"
      :count="selectedRows.length"
      :default-enterprise="defaultEnterprise"
      @confirm="handleBindConfirm"
    />
  </ContentWrap>
</template>

<style lang="less" scoped>
.scope-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  margin-bottom: 12px;
}

.scope-toolbar__search {
  width: 280px;
}

.scope-toolbar__spacer {
  flex: 1;
}

.scope-toolbar__selected {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
