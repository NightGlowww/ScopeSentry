<script setup lang="tsx">
import { confirmDialog } from '@/utils/confirm'
import { deleteResultMessage } from '@/utils/deleteResult'
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { computed, ref, reactive, onMounted } from 'vue'
import { ElButton, ElCol, ElInput, ElRow, ElText, ElBadge, ElMessageBox } from 'element-plus'
import { ElMessage } from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { Dialog } from '@/components/Dialog'
import { useTable } from '@/hooks/web/useTable'
import { useIcon } from '@/hooks/web/useIcon'
import { BaseButton } from '@/components/Button'
import {
  getFingerprintDataApi,
  deleteFingerprintDataApi,
  getFingerprintVersionApi,
  getCountByUpdateTimeApi,
  getFingerprintsByUpdateTimeApi,
  batchAddFingerprintApi
} from '@/api/Fingerprint'
import Detail from './components/Detail.vue'
const searchicon = useIcon({ icon: 'iconoir:search' })
const { t } = useI18n()
const search = ref('')
const emptyHint = computed(() => {
  const q = (search.value || '').trim()
  return q ? t('common.emptyFilteredHint', { q }) : t('common.noData')
})
const handleSearch = () => {
  // 搜索等于换结果集：不回第 1 页的话，停在第 N 页搜一个小结果会看到空表（2026-11-27）
  if (currentPage.value !== 1) {
    currentPage.value = 1
    return
  }
  getList()
}
const nodeColums = reactive<TableColumn[]>([
  {
    field: 'selection',
    type: 'selection',
    width: 55
  },
  {
    field: 'id',
    hidden: true
  },
  {
    field: 'name',
    label: t('fingerprint.name'),
    minWidth: 40
  },
  {
    field: 'category',
    label: t('fingerprint.category'),
    minWidth: 30
  },
  {
    field: 'parent_category',
    label: t('fingerprint.parentCategory'),
    minWidth: 30
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    minWidth: 40,
    formatter: (row, __: TableColumn, _: number) => {
      return (
        <>
          <BaseButton type="primary" onClick={() => edit(row)}>
            {t('common.edit')}
          </BaseButton>
          <BaseButton type="danger" onClick={() => del(row)}>
            {t('common.delete')}
          </BaseButton>
        </>
      )
    }
  }
])
const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getFingerprintDataApi(search.value, currentPage.value, pageSize.value)
    return {
      list: res.data.list,
      total: res.data.total
    }
  }
})
const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList, getElTableExpose } = tableMethods
function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}
const dialogVisible = ref(false)
const addSensitive = async () => {
  fingerprintForm.id = ''
  fingerprintForm.content = ''
  dialogVisible.value = true
}
const closeDialog = () => {
  dialogVisible.value = false
}
let fingerprintForm = reactive({
  id: '',
  content: ''
})
const edit = (data) => {
  fingerprintForm.id = data.id
  fingerprintForm.content = data.rule || data.content || ''
  dialogVisible.value = true
}
const delLoading = ref(false)
// 未勾选任何行时，工具条上的批量删除应禁用
const tableSelectedCount = ref(0)
const handleTableSelectionChange = (rows: unknown[]) => {
  tableSelectedCount.value = rows?.length ?? 0
}
const del = async (data) => {
  delLoading.value = true
  try {
    const res = await deleteFingerprintDataApi([data.id])
    ElMessage.success(deleteResultMessage(res, 1))
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    delLoading.value = false
    getList()
  }
}
const ids = ref<string[]>([])
const delSelect = async () => {
  const elTableExpose = await getElTableExpose()
  const selectedRows = elTableExpose?.getSelectionRows() || []
  ids.value = selectedRows.map((row) => row.id)
  delLoading.value = true
  try {
    const res = await deleteFingerprintDataApi(ids.value)
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    delLoading.value = false
    getList()
  }
}
const confirmDelete = async () => {
  const confirmed = await confirmDialog(t('common.delMessage'))
  if (confirmed) {
    await delSelect()
  }
}

// 更新相关的状态
const updateCount = ref(0)
const currentVersion = ref('')
const updateLoading = ref(false)

// 获取版本和数量
const fetchVersionAndCount = async () => {
  try {
    // 获取版本
    const versionRes = await getFingerprintVersionApi()
    const version = versionRes.data.version
    currentVersion.value = version

    // 使用版本获取数量
    const countRes = await getCountByUpdateTimeApi(version)
    updateCount.value = Number(countRes.data.count) || 0
  } catch (error) {
    console.error('Error fetching version and count:', error)
    updateCount.value = 0
  }
}

// 页面加载时获取版本和数量
onMounted(() => {
  fetchVersionAndCount()
})

// 更新按钮点击处理
const handleUpdate = async () => {
  try {
    // 二次确认
    await ElMessageBox.confirm(
      t('localization.fingerprintUpdateConfirm', { count: updateCount.value }),
      t('localization.confirmUpdate'),
      {
        confirmButtonText: t('common.ok'),
        cancelButtonText: t('common.cancel'),
        type: 'warning'
      }
    )

    updateLoading.value = true

    // 获取需要更新的指纹列表
    const fingerprintsRes = await getFingerprintsByUpdateTimeApi(currentVersion.value)
    const fingerprints = fingerprintsRes.data.data || []

    if (fingerprints.length === 0) {
      ElMessage.warning(t('localization.noUpdates'))
      updateLoading.value = false
      return
    }

    // 批量添加指纹
    await batchAddFingerprintApi(fingerprints)

    ElMessage.success(t('localization.fingerprintUpdated', { count: fingerprints.length }))

    // 刷新列表和数量
    await getList()
    await fetchVersionAndCount()
  } catch (error: any) {
    if (error !== 'cancel') {
      console.error('Error updating fingerprints:', error)
      ElMessage.error(error?.message || t('localization.updateFailed'))
    }
  } finally {
    updateLoading.value = false
  }
}
</script>

<template>
  <ContentWrap>
    <div class="fp-toolbar">
      <span class="fp-toolbar__label">{{ t('fingerprint.name') }}</span>
      <ElInput
        v-model="search"
        :placeholder="t('common.inputText')"
        class="fp-toolbar__input"
        style="height: 36px"
      />
      <ElButton type="primary" :icon="searchicon" style="height: 36px" @click="handleSearch">{{
        t('common.search')
      }}</ElButton>
    </div>
    <div class="fp-actions">
      <ElButton type="primary" @click="addSensitive">{{ t('common.new') }}</ElButton>
      <BaseButton
        type="danger"
        :loading="delLoading"
        :disabled="tableSelectedCount === 0"
        @click="confirmDelete"
      >
        {{ t('common.delete') }}
      </BaseButton>
      <ElBadge :value="updateCount" :hidden="updateCount === 0" :max="999999">
        <BaseButton type="success" :loading="updateLoading" @click="handleUpdate">
          {{ t('common.update') || '更新' }}
        </BaseButton>
      </ElBadge>
    </div>
    <Table
      :empty-text="emptyHint"
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      :columns="nodeColums"
      :data="dataList"
      stripe
      :border="true"
      :loading="loading"
      :resizable="true"
      :pagination="{
        total: total,
        pageSizes: [10, 20, 50, 100, 200, 500, 1000]
      }"
      @selection-change="handleTableSelectionChange"
      @register="tableRegister"
      :headerCellStyle="tableHeaderColor"
      :style="{
        fontFamily:
          '-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica Neue,Arial,Noto Sans,sans-serif,Apple Color Emoji,Segoe UI Emoji,Segoe UI Symbol,Noto Color Emoji'
      }"
    />
  </ContentWrap>
  <Dialog
    v-model="dialogVisible"
    :title="fingerprintForm.id ? $t('common.edit') : $t('common.new')"
    width="800px"
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="550"
  >
    <ElRow style="margin-bottom: 15px">
      <ElCol :span="24">
        <ElText class="mx-1" style="color: #409eff; font-size: 14px">
          {{ t('fingerprint.visualGeneratorTip') }}
          <a
            href="https://plugin.scope-sentry.top/fingers"
            target="_blank"
            style="color: #409eff; text-decoration: underline"
          >
            {{ t('fingerprint.visualGeneratorLink') }}
          </a>
          {{ t('fingerprint.visualGeneratorAction') }}
        </ElText>
      </ElCol>
    </ElRow>
    <Detail :closeDialog="closeDialog" :fingerprintForm="fingerprintForm" :getList="getList" />
  </Dialog>
</template>

<style scoped>
.fp-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 15px;
}
.fp-toolbar__label {
  flex: 0 0 auto;
  white-space: nowrap;
  color: var(--el-text-color-regular);
}
.fp-toolbar__input {
  width: 320px;
}
.fp-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
}
</style>
