<script setup lang="tsx">
import { reactive } from 'vue'
import { useI18n } from '@/hooks/web/useI18n'
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

const { t } = useI18n('github')

const stateMap: Record<number, { text: string; type: string }> = {
  0: { text: t('statePending'), type: 'info' },
  1: { text: t('stateRunning'), type: 'success' },
  2: { text: t('statePaused'), type: 'warning' },
  3: { text: t('stateFinished'), type: 'primary' }
}

const filters = reactive({ keyword: '', state: '' as string | number })

const columns = reactive<TableColumn[]>([
  { field: 'name', label: t('colName'), minWidth: 160 },
  { field: 'query', label: t('colQuery'), minWidth: 200 },
  {
    field: 'intervalMinutes',
    label: t('formInterval'),
    width: 110,
    // 调度间隔存在 task.config.intervalMinutes 里，顶层没有这个字段：
    // 直接读 row.intervalMinutes 永远拿到 undefined，整列一直显示 '-'（2026-11-28 实测）。
    formatter: (row: { config?: { intervalMinutes?: number } }) => {
      const minutes = row?.config?.intervalMinutes
      return minutes ? String(minutes) : '-'
    }
  },
  {
    field: 'state',
    label: t('colState'),
    width: 100,
    formatter: (_r, __: TableColumn, v: number) => (
      <ElTag type={(stateMap[v]?.type as any) || 'info'} effect="light">
        {stateMap[v]?.text || v}
      </ElTag>
    )
  },
  {
    field: 'stats',
    label: t('colStats'),
    width: 130,
    formatter: (_r, __: TableColumn, v?: { scanned: number; newFound: number }) =>
      v ? v.scanned + ' / ' + v.newFound : '-'
  },
  {
    field: 'lastRunAt',
    label: t('colLastRun'),
    minWidth: 160,
    formatter: (_r, __: TableColumn, v?: string) => (v ? formatTime(v, 'yyyy-MM-dd HH:mm:ss') : '-')
  },
  {
    field: 'action',
    label: t('colAction'),
    width: 260,
    formatter: (row) => (
      <div class="flex items-center" style={'gap:6px'}>
        <BaseButton type="primary" onClick={() => act(row, 'run')}>
          {t('btnRunNow')}
        </BaseButton>
        <BaseButton type="warning" onClick={() => act(row, 'stop')}>
          {t('btnPause')}
        </BaseButton>
        <BaseButton type="success" onClick={() => act(row, 'recover')}>
          {t('btnRecover')}
        </BaseButton>
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
  try {
    if (kind === 'run') {
      const res = (await runSchedulersApi([row.id])) as { data?: { runs?: unknown[] } }
      // 后端这次一个任务都没跑（正在运行/不存在）时不能提示已提交
      if (!res?.data?.runs?.length) ElMessage.warning(t('runNoop', { name: row.name }))
      else ElMessage.success(t('runSubmitted', { name: row.name }))
    } else if (kind === 'stop') {
      const res = (await stopSchedulersApi([row.id])) as { data?: { updated?: number } }
      if (res?.data?.updated === 0) ElMessage.warning(t('stopNoop', { name: row.name }))
      else ElMessage.success(t('stopSuccess', { name: row.name }))
    } else {
      const res = (await recoverSchedulersApi([row.id])) as { data?: { recovered?: number } }
      if (res?.data?.recovered === 0) ElMessage.warning(t('recoverNoop', { name: row.name }))
      else ElMessage.success(t('recoverSuccess', { name: row.name }))
    }
    getList()
  } catch (e: any) {
    // 后端错误拦截器已提示过，只在没有响应体时补一条兜底
    if (!e?.response) ElMessage.error(e?.message || t('opFailed'))
  }
}
</script>

<template>
  <ElRow :gutter="12" class="mb-10px">
    <ElCol :span="6">
      <ElInput v-model="filters.keyword" :placeholder="t('phTaskKeyword')" clearable />
    </ElCol>
    <ElCol :span="4">
      <ElSelect v-model="filters.state" :placeholder="t('colState')" clearable style="width: 100%">
        <ElOption :label="t('statePending')" :value="0" />
        <ElOption :label="t('stateRunning')" :value="1" />
        <ElOption :label="t('statePaused')" :value="2" />
        <ElOption :label="t('stateFinished')" :value="3" />
      </ElSelect>
    </ElCol>
    <ElCol :span="6">
      <ElButton type="primary" @click="getList">{{ t('btnQuery') }}</ElButton>
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
