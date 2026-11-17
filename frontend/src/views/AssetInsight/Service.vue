<script setup lang="tsx">
import { reactive, ref } from 'vue'
import { ElButton, ElCol, ElInput, ElRow, ElTag, ElTooltip } from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { getAssetServiceListApi } from '@/api/assetService'
import type { AssetServiceRow } from '@/api/assetService/types'
import {
  clampServicePageSize,
  foldedHostCount,
  normalizeServiceKeyword,
  splitSamples
} from '@/utils/assetService'
import { timeText } from '@/utils/timeText'
import ProjectSelect from './components/ProjectSelect.vue'

// 服务维度读模型（只读）：一行是一个服务名，带资产/主机/端口数与样例 host。
// 服务来自端口识别与指纹探测，所以 counts 是扫描结果现算，不是人工录入。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const keyword = ref('')
const project = ref('')

// samples 只展示前 5 个 host，其余折叠成 "+N"。
// 后端只回前 5 个样例（$slice:5），所以折叠数要拿 hostCount 补，否则 "+N" 永远不出现。
// formatter 里的 JSX 不带 SFC 的 scoped 属性，所以标签布局用行内样式，不依赖 <style scoped>。
const renderSamples = (row: AssetServiceRow) => {
  const { shown, hidden, hiddenCount } = splitSamples(row.samples)
  const listed = shown.length + hiddenCount
  const folded = foldedHostCount(row.hostCount, listed, shown.length)
  if (!shown.length && !folded) return '-'
  const tips: string[] = []
  if (hidden.length) tips.push(hidden.join(', '))
  if (folded > hidden.length) {
    const total = Math.max(row.hostCount ?? 0, listed)
    tips.push(t('assetService.hostsTotal', { shown: shown.length, total }))
  }
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px' }}>
      {shown.map((host) => (
        <ElTag key={host} size="small" type="info">
          {host}
        </ElTag>
      ))}
      {folded ? (
        <ElTooltip content={tips.join(' · ')} placement="top">
          <ElTag size="small" type="primary">
            +{folded}
          </ElTag>
        </ElTooltip>
      ) : null}
    </div>
  )
}

const columns = reactive<TableColumn[]>([
  {
    field: 'service',
    label: t('assetService.service'),
    minWidth: 180,
    formatter: (row: AssetServiceRow) => (
      <span style={{ fontFamily: 'var(--el-font-family-mono, monospace)' }}>
        {row.service || '-'}
      </span>
    )
  },
  { field: 'assetCount', label: t('assetService.assetCount'), width: 120 },
  { field: 'hostCount', label: t('assetService.hostCount'), width: 120 },
  { field: 'portCount', label: t('assetService.portCount'), width: 120 },
  {
    field: 'samples',
    label: t('assetService.samples'),
    minWidth: 320,
    // 标签会换行，开了 overflow tooltip 反而整格被裁成一行
    showOverflowTooltip: false,
    formatter: renderSamples
  },
  {
    field: 'lastSeen',
    label: t('assetService.lastSeen'),
    minWidth: 180,
    formatter: (row: AssetServiceRow) => timeText(row.lastSeen)
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getAssetServiceListApi({
      keyword: normalizeServiceKeyword(keyword.value),
      project: project.value || undefined,
      pageIndex: currentPage.value,
      pageSize: clampServicePageSize(pageSize.value)
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
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="5">
        <ElInput
          v-model="keyword"
          :placeholder="t('assetService.keywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="4">
        <ProjectSelect v-model="project" :placeholder="t('assetService.project')" />
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
        <div class="empty-state">{{ t('assetService.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>
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
