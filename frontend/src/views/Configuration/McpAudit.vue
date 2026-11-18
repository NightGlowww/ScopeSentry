<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { reactive, ref } from 'vue'
import {
  ElButton,
  ElCol,
  ElRow,
  ElInput,
  ElSelect,
  ElOption,
  ElDatePicker,
  ElTag
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { getMcpLogsApi, exportMcpLogsApi } from '@/api/mcp'
import { useUserStore } from '@/store/modules/user'
import { formatTime } from '@/utils'

const { t } = useI18n()
const userStore = useUserStore()

const statusOptions = [
  { value: 'success', label: 'success' },
  { value: 'denied', label: 'denied' },
  { value: 'error', label: 'error' }
]

const filters = reactive({
  keyId: '',
  tool: '',
  status: '',
  range: [] as string[]
})

const formatDateTime = (value?: string) => {
  if (!value) return '-'
  return formatTime(value, 'yyyy-MM-dd HH:mm:ss')
}

const statusTag = (status: string) => {
  const type = status === 'success' ? 'success' : status === 'denied' ? 'warning' : 'danger'
  return (
    <ElTag type={type} effect="light">
      {status}
    </ElTag>
  )
}

const columns = reactive<TableColumn[]>([
  {
    field: 'createdAt',
    label: '时间',
    minWidth: 170,
    formatter: (_row, __: TableColumn, value: string) => formatDateTime(value)
  },
  {
    field: 'tool',
    label: '工具',
    minWidth: 160
  },
  {
    field: 'status',
    label: '状态',
    width: 100,
    formatter: (_row, __: TableColumn, value: string) => statusTag(value)
  },
  {
    field: 'durationMs',
    label: '耗时(ms)',
    width: 100
  },
  {
    field: 'keyPrefix',
    label: 'Key 前缀',
    minWidth: 140
  },
  {
    field: 'clientIp',
    label: '客户端 IP',
    minWidth: 130
  },
  {
    field: 'error',
    label: '错误',
    minWidth: 200
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getMcpLogsApi({
      keyId: filters.keyId || undefined,
      tool: filters.tool || undefined,
      status: filters.status || undefined,
      from: filters.range?.[0] || undefined,
      to: filters.range?.[1] || undefined,
      pageIndex: currentPage.value,
      pageSize: pageSize.value
    })
    return {
      list: res.data.list || [],
      total: res.data.total || 0
    }
  }
})

const { loading, dataList, total, currentPage, pageSize } = tableState
pageSize.value = 20
const { getList } = tableMethods

const handleSearch = () => {
  currentPage.value = 1
  getList()
}

const handleReset = () => {
  filters.keyId = ''
  filters.tool = ''
  filters.status = ''
  filters.range = []
  currentPage.value = 1
  getList()
}

const handleExport = async () => {
  const url = exportMcpLogsApi({
    keyId: filters.keyId || undefined,
    tool: filters.tool || undefined,
    status: filters.status || undefined,
    from: filters.range?.[0] || undefined,
    to: filters.range?.[1] || undefined
  })
  const resp = await fetch(url, {
    headers: { Authorization: userStore.getToken || '' }
  })
  const blob = await resp.blob()
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = 'mcp-call-logs.csv'
  link.click()
  URL.revokeObjectURL(link.href)
}
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="mb-10px">
      <ElCol :span="5">
        <ElInput v-model="filters.keyId" placeholder="Key ID" clearable />
      </ElCol>
      <ElCol :span="5">
        <ElInput v-model="filters.tool" placeholder="工具名，如 list_assets" clearable />
      </ElCol>
      <ElCol :span="4">
        <ElSelect v-model="filters.status" placeholder="状态" clearable style="width: 100%">
          <ElOption v-for="opt in statusOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
        </ElSelect>
      </ElCol>
      <ElCol :span="6">
        <ElDatePicker
          v-model="filters.range"
          type="daterange"
          value-format="YYYY-MM-DD"
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          style="width: 100%"
        />
      </ElCol>
      <ElCol :span="4">
        <ElButton type="primary" @click="handleSearch">查询</ElButton>
        <ElButton @click="handleReset">重置</ElButton>
        <ElButton type="success" @click="handleExport">导出</ElButton>
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
