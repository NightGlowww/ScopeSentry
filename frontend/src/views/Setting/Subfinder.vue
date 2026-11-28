<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { ref, reactive } from 'vue'
import { ElButton, ElCol, ElInput, ElRow, ElText } from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useIcon } from '@/hooks/web/useIcon'
import { BaseButton } from '@/components/Button'
import { getNodeDataApi } from '@/api/node'
const searchicon = useIcon({ icon: 'iconoir:search' })
const { t } = useI18n()
const search = ref('')
const handleSearch = () => {
  console.log('as')
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
    field: 'pocName',
    label: t('poc.pocName')
  },
  {
    field: 'CreateTime',
    label: t('node.createTime')
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    formatter: (row, __: TableColumn, _: number) => {
      console.log(row)
      return (
        <>
          <BaseButton type="primary">{t('common.edit')}</BaseButton>
        </>
      )
    }
  }
])
const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    // getNodeDataApi() 不接受参数、也不返回 total（节点列表是一次性取全量）；
    // 之前按分页接口传了三个参数并读 res.data.total，表格 total 永远是 undefined。
    // 这里在前端做本地过滤+分页，别去改接口契约。
    const res = await getNodeDataApi()
    const all = (res?.data?.list || []) as any[]
    const kw = (search.value || '').trim().toLowerCase()
    const filtered = kw ? all.filter((n) => String(n?.name || n).toLowerCase().includes(kw)) : all
    const start = (currentPage.value - 1) * pageSize.value
    return {
      list: filtered.slice(start, start + pageSize.value),
      total: filtered.length
    }
  }
})
const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList } = tableMethods
function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}
</script>

<template>
  <ContentWrap>
    <ElRow>
      <ElCol :span="1">
        <ElText class="mx-1" style="position: relative; top: 8px">{{ t('poc.pocName') }}:</ElText>
      </ElCol>
      <ElCol :span="5">
        <ElInput v-model="search" :placeholder="t('common.inputText')" style="height: 36px" />
      </ElCol>
      <ElCol :span="5" style="position: relative; left: 16px">
        <ElButton type="primary" :icon="searchicon" @click="handleSearch">{{
          t('common.search')
        }}</ElButton>
      </ElCol>
    </ElRow>
    <ElRow>
      <ElCol style="position: relative; top: 16px">
        <div class="mb-10px">
          <BaseButton type="danger">
            {{ t('common.delete') }}
          </BaseButton>
        </div>
      </ElCol>
    </ElRow>
    <div style="position: relative; top: 12px">
      <Table
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
        @register="tableRegister"
        :headerCellStyle="tableHeaderColor"
        :style="{
          fontFamily:
            '-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica Neue,Arial,Noto Sans,sans-serif,Apple Color Emoji,Segoe UI Emoji,Segoe UI Symbol,Noto Color Emoji'
        }"
      />
    </div>
  </ContentWrap>
</template>
