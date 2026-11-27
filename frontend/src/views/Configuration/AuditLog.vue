<script setup lang="tsx">
import { reactive } from 'vue'
import { ElButton, ElCol, ElRow, ElInput, ElSelect, ElOption, ElTag } from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { listAuditLogsApi } from '@/api/audit'
import { formatTime } from '@/utils'

const { t } = useI18n('auditLog')

const filters = reactive({
  user: '',
  path: '',
  method: '',
  statusMin: '' as string | number
})

const methodOptions = ['POST', 'PUT', 'DELETE', 'PATCH']
const statusOptions = [
  { value: 200, label: '≥200' },
  { value: 400, label: '≥400' },
  { value: 500, label: '≥500' }
]

const fmt = (v?: string) => (v ? formatTime(v, 'yyyy-MM-dd HH:mm:ss') : '-')

const columns = reactive<TableColumn[]>([
  {
    field: 'at',
    label: t('time'),
    minWidth: 165,
    formatter: (_r, __: TableColumn, v?: string) => fmt(v)
  },
  { field: 'user', label: t('operator'), width: 120 },
  {
    field: 'method',
    label: t('method'),
    width: 90,
    formatter: (_r, __: TableColumn, v: string) => (
      <ElTag
        type={v === 'DELETE' ? 'danger' : v === 'POST' || v === 'PUT' ? 'warning' : 'info'}
        effect="light"
      >
        {v}
      </ElTag>
    )
  },
  { field: 'path', label: t('path'), minWidth: 240 },
  {
    field: 'status',
    label: t('status'),
    width: 90,
    formatter: (_r, __: TableColumn, v: number) => (
      <ElTag type={v >= 500 ? 'danger' : v >= 400 ? 'warning' : 'success'} effect="light">
        {v}
      </ElTag>
    )
  },
  { field: 'durationMs', label: t('durationMs'), width: 100 },
  { field: 'ip', label: t('ip'), minWidth: 130 },
  { field: 'ua', label: t('userAgent'), minWidth: 200 }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await listAuditLogsApi({
      user: filters.user || undefined,
      path: filters.path || undefined,
      method: filters.method || undefined,
      statusMin: filters.statusMin === '' ? undefined : Number(filters.statusMin),
      pageIndex: currentPage.value,
      pageSize: pageSize.value
    })
    return { list: res.data.list || [], total: res.data.total || 0 }
  }
})
const { loading, dataList, total, currentPage, pageSize } = tableState
pageSize.value = 20
const { getList } = tableMethods

const reset = () => {
  filters.user = ''
  filters.path = ''
  filters.method = ''
  filters.statusMin = ''
  currentPage.value = 1
  getList()
}
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="mb-10px">
      <ElCol :span="4">
        <ElInput v-model="filters.user" :placeholder="t('operator')" clearable />
      </ElCol>
      <ElCol :span="5">
        <ElInput v-model="filters.path" :placeholder="t('pathPlaceholder')" clearable />
      </ElCol>
      <ElCol :span="3">
        <ElSelect v-model="filters.method" :placeholder="t('method')" clearable style="width: 100%">
          <ElOption v-for="m in methodOptions" :key="m" :label="m" :value="m" />
        </ElSelect>
      </ElCol>
      <ElCol :span="3">
        <ElSelect
          v-model="filters.statusMin"
          :placeholder="t('status')"
          clearable
          style="width: 100%"
        >
          <ElOption v-for="s in statusOptions" :key="s.value" :label="s.label" :value="s.value" />
        </ElSelect>
      </ElCol>
      <ElCol :span="6">
        <ElButton type="primary" @click="getList">{{ t('query') }}</ElButton>
        <ElButton @click="reset">{{ t('reset') }}</ElButton>
      </ElCol>
    </ElRow>

    <Table
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      :columns="columns"
      :data="dataList"
      :empty-text="t('emptyText')"
      stripe
      border
      :loading="loading"
      :pagination="{ total, pageSizes: [20, 50, 100] }"
      @register="tableRegister"
    />
  </ContentWrap>
</template>

<style scoped>
.mb-10px {
  margin-bottom: 10px;
}
</style>
