<script setup lang="tsx">
import { reactive, ref } from 'vue'
import { ElButton, ElCol, ElDrawer, ElInput, ElLink, ElPagination, ElRow } from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { getStatFingerAssetsApi, getStatFingerListApi } from '@/api/assetReadModel'
import type { StatFingerAssetRow, StatFingerRow } from '@/api/assetReadModel/types'
import ProjectSelect from './components/ProjectSelect.vue'

// 指纹统计（只读）：按技术名聚合出现次数/资产数/最近出现/样例 host，
// 点技术名在抽屉里反查使用该技术的资产。数据来自扫描结果里的 asset.technologies。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const keyword = ref('')
const project = ref('')

const columns = reactive<TableColumn[]>([
  {
    field: 'name',
    label: t('statFinger.name'),
    minWidth: 180,
    formatter: (row: StatFingerRow) => (
      <ElLink type="primary" onClick={() => openAssets(row.name)}>
        {row.name || '-'}
      </ElLink>
    )
  },
  { field: 'count', label: t('statFinger.count'), width: 120 },
  { field: 'assetCount', label: t('statFinger.assetCount'), width: 120 },
  { field: 'lastSeen', label: t('statFinger.lastSeen'), minWidth: 180 },
  {
    field: 'samples',
    label: t('statFinger.samples'),
    minWidth: 260,
    formatter: (row: StatFingerRow) => (row.samples ?? []).join(' , ')
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getStatFingerListApi({
      keyword: keyword.value.trim(),
      project: project.value,
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
  project.value = ''
  handleSearch()
}

// ---- 抽屉：按技术名反查资产 ----
const drawerVisible = ref(false)
const technology = ref('')
const assetLoading = ref(false)
const assetRows = ref<StatFingerAssetRow[]>([])
const assetTotal = ref(0)
const assetPage = ref(1)
const assetPageSize = ref(20)

const assetColumns = reactive<TableColumn[]>([
  { field: 'host', label: t('statFinger.host'), minWidth: 160 },
  { field: 'port', label: t('statFinger.port'), width: 90 },
  { field: 'ip', label: t('statFinger.ip'), minWidth: 130 },
  { field: 'url', label: t('statFinger.url'), minWidth: 240 },
  { field: 'title', label: t('statFinger.assetTitle'), minWidth: 160 },
  { field: 'statusCode', label: t('statFinger.statusCode'), width: 100 },
  { field: 'service', label: t('statFinger.service'), width: 120 },
  { field: 'project', label: t('statFinger.project'), minWidth: 140 }
])

const loadAssets = async () => {
  if (!technology.value) return
  assetLoading.value = true
  try {
    const res = await getStatFingerAssetsApi({
      technology: technology.value,
      project: project.value,
      pageIndex: assetPage.value,
      pageSize: assetPageSize.value
    })
    assetRows.value = res?.data?.list ?? []
    assetTotal.value = res?.data?.total ?? 0
  } catch {
    assetRows.value = []
    assetTotal.value = 0
  } finally {
    assetLoading.value = false
  }
}

const openAssets = (name: string) => {
  technology.value = name
  assetPage.value = 1
  drawerVisible.value = true
  loadAssets()
}

const onAssetPageChange = (page: number) => {
  assetPage.value = page
  loadAssets()
}

const onAssetSizeChange = (size: number) => {
  assetPageSize.value = size
  assetPage.value = 1
  loadAssets()
}
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="4">
        <ElInput
          v-model="keyword"
          :placeholder="t('statFinger.keywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="4">
        <ProjectSelect v-model="project" :placeholder="t('statFinger.project')" />
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
        <div class="empty-state">{{ t('statFinger.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>

  <ElDrawer
    v-model="drawerVisible"
    :title="t('statFinger.assetsTitle', { name: technology })"
    size="70%"
    direction="rtl"
  >
    <Table
      :columns="assetColumns"
      :data="assetRows"
      stripe
      :border="true"
      :loading="assetLoading"
      :resizable="true"
    >
      <template #empty>
        <div class="empty-state">{{ t('statFinger.assetsEmpty') }}</div>
      </template>
    </Table>
    <ElPagination
      class="mt-3"
      layout="sizes, prev, pager, next, total"
      :page-sizes="[10, 20, 50, 100, 200]"
      :total="assetTotal"
      :page-size="assetPageSize"
      :current-page="assetPage"
      @current-change="onAssetPageChange"
      @size-change="onAssetSizeChange"
    />
  </ElDrawer>
</template>

<style scoped>
.filter-row {
  margin-bottom: 15px;
}

.empty-state {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}
</style>
