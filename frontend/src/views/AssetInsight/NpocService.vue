<script setup lang="tsx">
import { computed, reactive, ref } from 'vue'
import { ElButton, ElCol, ElInput, ElMessage, ElMessageBox, ElRow } from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { deleteNpocServiceApi, getNpocServiceListApi } from '@/api/npocService'
import type { NpocServiceRow } from '@/api/npocService/types'
import { clampPageSize, normalizeKeyword, normalizeScheme, portText } from '@/utils/assetList'
import { timeText } from '@/utils/timeText'

// NPoC 服务（只读 + 批量删除）：一行是扫描端 NPoC 识别出的一个服务。
// 数据是节点侧模块落库的，空态必须说清楚「节点跑完对应模块才会出现」。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const keyword = ref('')
// 协议按精确值过滤，页面统一小写；扫描端把协议写成 "HTTP" 时也能对上
const schemeInput = ref('')
const scheme = computed({
  get: () => schemeInput.value,
  set: (value: string) => {
    schemeInput.value = String(value ?? '')
      .trim()
      .toLowerCase()
  }
})

// formatter 里的 JSX 不带 SFC 的 scoped 属性，所以等宽样式走行内，不依赖 <style scoped>
const monoStyle = {
  fontFamily: 'var(--el-font-family-mono, monospace)'
}

const columns = reactive<TableColumn[]>([
  {
    field: 'selection',
    type: 'selection',
    width: 50
  },
  {
    field: 'scheme',
    label: t('npocService.scheme'),
    width: 120,
    formatter: (row: NpocServiceRow) => <span style={monoStyle}>{row.scheme || '-'}</span>
  },
  {
    field: 'host',
    label: t('npocService.host'),
    minWidth: 220,
    formatter: (row: NpocServiceRow) => <span style={monoStyle}>{row.host || '-'}</span>
  },
  {
    field: 'port',
    label: t('npocService.port'),
    width: 100,
    // 老数据里端口可能存成字符串，也可能没有；0 不是合法端口，同样显示成 "-"
    formatter: (row: NpocServiceRow) => <span style={monoStyle}>{portText(row.port)}</span>
  },
  {
    field: 'target',
    label: t('npocService.target'),
    minWidth: 320,
    formatter: (row: NpocServiceRow) => (
      <span style={{ ...monoStyle, wordBreak: 'break-all' }}>{row.target || '-'}</span>
    )
  },
  {
    field: 'insertTime',
    label: t('npocService.insertTime'),
    width: 180,
    formatter: (row: NpocServiceRow) => timeText(row.insertTime)
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getNpocServiceListApi({
      keyword: normalizeKeyword(keyword.value),
      scheme: normalizeScheme(schemeInput.value),
      pageIndex: currentPage.value,
      pageSize: clampPageSize(pageSize.value)
    })
    return { list: res?.data?.list ?? [], total: res?.data?.total ?? 0 }
  }
})

const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList, getElTableExpose } = tableMethods

const handleSearch = () => {
  // currentPage 变化时 useTable 会自己拉一次，只有已在第一页时才手动拉，避免重复请求
  if (currentPage.value !== 1) {
    currentPage.value = 1
    return
  }
  getList()
}

const handleReset = () => {
  keyword.value = ''
  schemeInput.value = ''
  handleSearch()
}

const selectedRows = ref<NpocServiceRow[]>([])
const selectedIds = computed(() => selectedRows.value.map((row) => row.id))
const deleting = ref(false)

const handleSelectionChange = (rows: NpocServiceRow[]) => {
  selectedRows.value = rows ?? []
}

// 删除后清空勾选：数据换了但勾选还在的话，下一次删除会带上已经删掉的 id
const clearSelection = async () => {
  const elTableExpose = await getElTableExpose()
  elTableExpose?.clearSelection?.()
  selectedRows.value = []
}

const handleDelete = async () => {
  const ids = selectedIds.value
  if (!ids.length) {
    ElMessage.warning(t('npocService.selectFirst'))
    return
  }
  // 二次确认：删除不可恢复，确认框里带上条数，避免误删整页
  try {
    await ElMessageBox.confirm(
      t('npocService.deleteConfirm', { count: ids.length }),
      t('common.reminder'),
      {
        confirmButtonText: t('common.ok'),
        cancelButtonText: t('common.cancel'),
        type: 'warning'
      }
    )
  } catch {
    // 用户取消：正常选择，不提示也不改列表
    return
  }
  deleting.value = true
  try {
    await deleteNpocServiceApi(ids)
    // 整页被删空时回退一页，否则用户停在空白页会以为数据被清光了
    const emptiedPage = ids.length >= dataList.value.length
    await clearSelection()
    if (emptiedPage && currentPage.value > 1) {
      currentPage.value -= 1 // 分页 watch 会重新拉列表
    } else {
      getList()
    }
  } catch {
    // 失败提示由 axios 拦截器统一弹出，这里只恢复按钮状态
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="5">
        <ElInput
          v-model="keyword"
          :placeholder="t('npocService.keywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="3">
        <ElInput
          v-model="scheme"
          :placeholder="t('npocService.schemePlaceholder')"
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
    <div class="toolbar">
      <ElButton
        type="danger"
        :disabled="!selectedIds.length"
        :loading="deleting"
        @click="handleDelete"
      >
        {{ t('common.delete') }}
      </ElButton>
      <span v-if="selectedIds.length" class="selected-hint">
        {{ t('npocService.selectedCount', { count: selectedIds.length }) }}
      </span>
    </div>
    <Table
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      rowKey="id"
      :columns="columns"
      :data="dataList"
      stripe
      :border="true"
      :loading="loading"
      :resizable="true"
      :pagination="{ total, pageSizes: [10, 20, 50, 100, 200] }"
      @register="tableRegister"
      @selection-change="handleSelectionChange"
    >
      <template #empty>
        <div class="empty-state">{{ t('npocService.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>
</template>

<style scoped>
.filter-row {
  margin-bottom: 12px;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}

.selected-hint {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.empty-state {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}
</style>
