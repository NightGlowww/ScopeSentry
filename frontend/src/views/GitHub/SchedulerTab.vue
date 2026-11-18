<script setup lang="tsx">
import { reactive } from 'vue'
import { ElButton, ElCol, ElRow, ElInput, ElSelect, ElOption, ElTag, ElMessage } from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { BaseButton } from '@/components/Button'
import {
  listSchedulersApi,
  runSchedulersApi,
  stopSchedulersApi,
  recoverSchedulersApi
} from '@/api/github'
import { formatTime } from '@/utils'

const stateMap: Record<number, { text: string; type: string }> = {
  0: { text: '待开始', type: 'info' },
  1: { text: '运行中', type: 'success' },
  2: { text: '已暂停', type: 'warning' },
  3: { text: '已完成', type: 'primary' }
}

const filters = reactive({ keyword: '', state: '' as string | number })

const columns = reactive<TableColumn[]>([
  { field: 'name', label: '名称', minWidth: 160 },
  { field: 'query', label: '查询', minWidth: 200 },
  {
    field: 'intervalMinutes',
    label: '间隔(分钟)',
    width: 110,
    formatter: (_r, __: TableColumn, v?: number) => (v ? String(v) : '-')
  },
  {
    field: 'state',
    label: '状态',
    width: 100,
    formatter: (_r, __: TableColumn, v: number) => (
      <ElTag type={(stateMap[v]?.type as any) || 'info'} effect="light">
        {stateMap[v]?.text || v}
      </ElTag>
    )
  },
  {
    field: 'stats',
    label: '已扫描/新增',
    width: 130,
    formatter: (_r, __: TableColumn, v?: { scanned: number; newFound: number }) =>
      v ? v.scanned + ' / ' + v.newFound : '-'
  },
  {
    field: 'lastRunAt',
    label: '最近运行',
    minWidth: 160,
    formatter: (_r, __: TableColumn, v?: string) => (v ? formatTime(v, 'yyyy-MM-dd HH:mm:ss') : '-')
  },
  {
    field: 'action',
    label: '操作',
    width: 260,
    formatter: (row) => (
      <div class="flex items-center" style={'gap:6px'}>
        <BaseButton type="primary" onClick={() => act(row, 'run')}>立即运行</BaseButton>
        <BaseButton type="warning" onClick={() => act(row, 'stop')}>暂停</BaseButton>
        <BaseButton type="success" onClick={() => act(row, 'recover')}>恢复</BaseButton>
      </div>
    )
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await listSchedulersApi({
      keyword: filters.keyword || undefined,
      state: filters.state === '' ? undefined : Number(filters.state),
      pageIndex: currentPage.value,
      pageSize: pageSize.value
    })
    return { list: res.data.list || [], total: res.data.total || 0 }
  }
})
const { loading, dataList, total, currentPage, pageSize } = tableState
pageSize.value = 20
const { getList } = tableMethods

const act = async (row: { id: string; name: string }, kind: 'run' | 'stop' | 'recover') => {
  if (kind === 'run') await runSchedulersApi([row.id])
  if (kind === 'stop') await stopSchedulersApi([row.id])
  if (kind === 'recover') await recoverSchedulersApi([row.id])
  ElMessage.success('已提交：' + row.name)
  getList()
}
</script>

<template>
  <ElRow :gutter="12" class="mb-10px">
    <ElCol :span="6">
      <ElInput v-model="filters.keyword" placeholder="名称/查询关键字" clearable />
    </ElCol>
    <ElCol :span="4">
      <ElSelect v-model="filters.state" placeholder="状态" clearable style="width: 100%">
        <ElOption label="待开始" :value="0" />
        <ElOption label="运行中" :value="1" />
        <ElOption label="已暂停" :value="2" />
        <ElOption label="已完成" :value="3" />
      </ElSelect>
    </ElCol>
    <ElCol :span="6">
      <ElButton type="primary" @click="getList">查询</ElButton>
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
</template>

<style scoped>
.mb-10px {
  margin-bottom: 10px;
}
</style>
