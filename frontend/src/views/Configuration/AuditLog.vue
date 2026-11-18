<script setup lang="tsx">
import { reactive } from 'vue'
import { ElButton, ElCol, ElRow, ElInput, ElSelect, ElOption, ElTag } from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { listAuditLogsApi } from '@/api/audit'
import { formatTime } from '@/utils'

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
  { field: 'at', label: '时间', minWidth: 165, formatter: (_r, __: TableColumn, v?: string) => fmt(v) },
  { field: 'user', label: '操作者', width: 120 },
  {
    field: 'method',
    label: '方法',
    width: 90,
    formatter: (_r, __: TableColumn, v: string) => (
      <ElTag type={v === 'DELETE' ? 'danger' : v === 'POST' || v === 'PUT' ? 'warning' : 'info'} effect="light">
        {v}
      </ElTag>
    )
  },
  { field: 'path', label: '接口', minWidth: 240 },
  {
    field: 'status',
    label: '状态',
    width: 90,
    formatter: (_r, __: TableColumn, v: number) => (
      <ElTag type={v >= 500 ? 'danger' : v >= 400 ? 'warning' : 'success'} effect="light">
        {v}
      </ElTag>
    )
  },
  { field: 'durationMs', label: '耗时(ms)', width: 100 },
  { field: 'ip', label: 'IP', minWidth: 130 },
  { field: 'ua', label: 'User-Agent', minWidth: 200 }
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
        <ElInput v-model="filters.user" placeholder="操作者" clearable />
      </ElCol>
      <ElCol :span="5">
        <ElInput v-model="filters.path" placeholder="接口路径关键字，如 /api/task" clearable />
      </ElCol>
      <ElCol :span="3">
        <ElSelect v-model="filters.method" placeholder="方法" clearable style="width: 100%">
          <ElOption v-for="m in methodOptions" :key="m" :label="m" :value="m" />
        </ElSelect>
      </ElCol>
      <ElCol :span="3">
        <ElSelect v-model="filters.statusMin" placeholder="状态" clearable style="width: 100%">
          <ElOption v-for="s in statusOptions" :key="s.value" :label="s.label" :value="s.value" />
        </ElSelect>
      </ElCol>
      <ElCol :span="6">
        <ElButton type="primary" @click="getList">查询</ElButton>
        <ElButton @click="reset">重置</ElButton>
      </ElCol>
    </ElRow>

    <Table
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      :columns="columns"
      :data="dataList"
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
