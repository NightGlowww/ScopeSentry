<script setup lang="tsx">
import { computed, reactive, ref } from 'vue'
import { ElButton, ElCol, ElInput, ElLink, ElOption, ElRow, ElSelect, ElTag } from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { getHttpStatusListApi } from '@/api/httpStatus'
import type { HttpStatusRow } from '@/api/httpStatus/types'
import { httpStatusTagType, normalizeStatusFilter, safeHttpUrl } from '@/utils/httpStatus'
import { timeText } from '@/utils/timeText'
import ProjectSelect from './components/ProjectSelect.vue'

// HTTP 状态读模型（只读）：一行是某个 host:port 最近一次探测到的响应码。
// 分档筛选与精确码同时存在时精确码优先——填了 404 就只看 404。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const keyword = ref('')
const project = ref('')
const band = ref('')
// 输入框只收数字：除分档外还允许三位精确码，非数字与超长部分直接裁掉
const exactCodeInput = ref('')
const exactCode = computed({
  get: () => exactCodeInput.value,
  set: (value: string) => {
    exactCodeInput.value = String(value ?? '')
      .replace(/\D/g, '')
      .slice(0, 3)
  }
})

const columns = reactive<TableColumn[]>([
  {
    field: 'host',
    label: t('httpStatus.host'),
    minWidth: 200,
    formatter: (row: HttpStatusRow) => <span class="mono">{row.host || '-'}</span>
  },
  {
    field: 'statusCode',
    label: t('httpStatus.statusCode'),
    width: 110,
    formatter: (row: HttpStatusRow) =>
      row.statusCode ? (
        <ElTag type={httpStatusTagType(row.statusCode)}>{row.statusCode}</ElTag>
      ) : (
        '-'
      )
  },
  {
    field: 'title',
    label: t('httpStatus.title'),
    minWidth: 220,
    formatter: (row: HttpStatusRow) => row.title || '-'
  },
  {
    field: 'url',
    label: t('httpStatus.url'),
    minWidth: 280,
    formatter: (row: HttpStatusRow) => {
      const href = safeHttpUrl(row.url)
      if (!href) return <span class="mono">{row.url || '-'}</span>
      return (
        <ElLink href={href} target="_blank" rel="noopener noreferrer" type="primary" class="mono">
          {row.url}
        </ElLink>
      )
    }
  },
  {
    field: 'service',
    label: t('httpStatus.service'),
    width: 140,
    formatter: (row: HttpStatusRow) => row.service || '-'
  },
  {
    field: 'time',
    label: t('httpStatus.time'),
    width: 180,
    formatter: (row: HttpStatusRow) => timeText(row.time)
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getHttpStatusListApi({
      status: normalizeStatusFilter(band.value, exactCode.value),
      keyword: keyword.value.trim() || undefined,
      project: project.value || undefined,
      pageIndex: currentPage.value,
      pageSize: Math.min(pageSize.value, 200)
    })
    return { list: res?.data?.list ?? [], total: res?.data?.total ?? 0 }
  }
})

const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList } = tableMethods

const handleSearch = () => {
  // currentPage 变化时 useTable 会自己拉一次，只有在第一页时才手动拉，避免重复请求
  if (currentPage.value !== 1) {
    currentPage.value = 1
    return
  }
  getList()
}

const handleReset = () => {
  keyword.value = ''
  project.value = ''
  band.value = ''
  exactCodeInput.value = ''
  handleSearch()
}
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="3">
        <ElSelect v-model="band" class="full-width">
          <ElOption :label="t('httpStatus.bandAll')" value="" />
          <ElOption :label="t('httpStatus.band2xx')" value="2xx" />
          <ElOption :label="t('httpStatus.band3xx')" value="3xx" />
          <ElOption :label="t('httpStatus.band4xx')" value="4xx" />
          <ElOption :label="t('httpStatus.band5xx')" value="5xx" />
        </ElSelect>
      </ElCol>
      <ElCol :span="3">
        <ElInput
          v-model="exactCode"
          :placeholder="t('httpStatus.exactCodePlaceholder')"
          maxlength="3"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="5">
        <ElInput
          v-model="keyword"
          :placeholder="t('httpStatus.keywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="4">
        <ProjectSelect v-model="project" :placeholder="t('httpStatus.project')" />
      </ElCol>
      <ElCol :span="6">
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
        <div class="empty-state">{{ t('httpStatus.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>
</template>

<style scoped>
.filter-row {
  margin-bottom: 15px;
}

.full-width {
  width: 100%;
}

.empty-state {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}

.mono {
  font-family: var(--el-font-family-mono, monospace);
  word-break: break-all;
}
</style>
