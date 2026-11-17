<script setup lang="ts">
import { useI18n } from '@/hooks/web/useI18n'
import { reactive, h, effect } from 'vue'
import { Table, TableColumn } from '@/components/Table'
import { getTaskProgressApi, pauseTaskTargetApi, resumeTaskTargetApi } from '@/api/task'
import { useTable } from '@/hooks/web/useTable'
import {
  ElButton,
  ElCard,
  ElMessage,
  ElMessageBox,
  ElPagination,
  ElTag,
  ElTooltip,
  ElTooltipProps
} from 'element-plus'
import { Icon } from '@/components/Icon'
const { t } = useI18n()

const props = defineProps<{
  closeDialog: () => void
  getProgressInfoID: string
  getProgressInfotype: string
  getProgressInforunnerid: string
}>()
const progressColums = reactive<TableColumn[]>([
  {
    field: 'target',
    label: t('task.taskTarget'),
    minWidth: 40,
    formatter: (row: Recordable, __: TableColumn, cellValue: string) => {
      const tooltipContent = row.node && row.node !== '' ? row.node : null
      const base = tooltipContent
        ? h(
            ElTooltip,
            { content: tooltipContent, placement: 'top', rawContent: true },
            {
              default: () => cellValue // 用 cellValue 作为原始数据显示
            }
          )
        : cellValue // 如果 row.node 是空字符串，则直接显示 cellValue
      if (row.paused === true) {
        return h('span', { class: 'paused-target' }, [
          base,
          h(ElTag, { type: 'info', size: 'small', class: 'ml-2' }, () => t('task.targetPaused'))
        ])
      }
      return base
    }
  },
  {
    field: 'TargetHandler',
    label: t('scanTemplate.TargetHandler'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'SubdomainScan',
    label: t('scanTemplate.SubdomainScan'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'SubdomainSecurity',
    label: t('scanTemplate.SubdomainSecurity'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'PortScanPreparation',
    label: t('scanTemplate.PortScanPreparation'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'PortScan',
    label: t('scanTemplate.PortScan'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'PortFingerprint',
    label: t('scanTemplate.PortFingerprint'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'AssetMapping',
    label: t('scanTemplate.AssetMapping'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'AssetHandle',
    label: t('scanTemplate.AssetHandle'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'URLScan',
    label: t('scanTemplate.URLScan'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'WebCrawler',
    label: t('scanTemplate.WebCrawler'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'URLSecurity',
    label: t('scanTemplate.URLSecurity'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'DirScan',
    label: t('scanTemplate.DirScan'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'VulnerabilityScan',
    label: t('scanTemplate.VulnerabilityScan'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'All',
    label: t('common.all'),
    minWidth: 30,
    formatter: (_: Recordable, __: TableColumn, cellValue: string[]) => {
      if (cellValue.length == 3) {
        return h(Icon, { icon: 'ph:prohibit' })
      }
      if (cellValue[0] == '') {
        return '-'
      }
      let cont = ''
      cont += `<div>Start:${cellValue[0]}</div>`
      cont += `<div>End:${cellValue[1]}</div>`
      if (cellValue[0] != '' && cellValue[1] != '') {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'success' }, () => 'Done')
        )
      } else {
        return h(ElTooltip, { content: cont, placement: 'top', rawContent: true }, () =>
          h(ElTag, { type: 'primary' }, () => 'Running')
        )
      }
    }
  },
  {
    field: 'pausedAction',
    label: t('common.operation'),
    width: 100,
    formatter: (row: Recordable) => {
      const paused = row.paused === true
      return h(
        ElButton,
        {
          link: true,
          type: paused ? 'primary' : 'warning',
          onClick: () => toggleTarget(row)
        },
        () => (paused ? t('task.resumeTarget') : t('task.pauseTarget'))
      )
    }
  }
])
const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const res = await getTaskProgressApi(props.getProgressInfoID, currentPage.value, pageSize.value)
    return {
      total: res.data.total,
      list: res.data.list
    }
  },
  immediate: false
})
const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList } = tableMethods

// 暂停/恢复单个目标（issue #277）：暂停前确认一次，恢复直接执行
const toggleTarget = async (row: Recordable) => {
  const paused = row.paused === true
  try {
    if (paused) {
      await resumeTaskTargetApi(props.getProgressInfoID, row.target)
    } else {
      await ElMessageBox.confirm(
        t('task.pauseTargetConfirm', { target: row.target }),
        t('common.delWarning'),
        { confirmButtonText: 'OK', cancelButtonText: t('common.cancel'), type: 'warning' }
      )
      await pauseTaskTargetApi(props.getProgressInfoID, row.target)
    }
    ElMessage.success(t('common.success'))
    getList()
  } catch {
    // 取消确认或请求失败：失败提示由 axios 拦截器统一弹出
  }
}

getList()
</script>
<template>
  <Table
    v-model:pageSize="pageSize"
    v-model:currentPage="currentPage"
    @register="tableRegister"
    :columns="progressColums"
    :data="dataList"
    rowKey="_id"
    :loading="loading"
    :resizable="true"
    max-height="600"
    :tooltip-options="{
      offset: 1,
      showArrow: false,
      effect: 'dark',
      enterable: true,
      showAfter: 0,
      popperOptions: {},
      popperClass: 'test',
      placement: 'top',
      hideAfter: 0,
      disabled: true
    }"
    :style="{
      fontFamily:
        '-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica Neue,Arial,Noto Sans,sans-serif,Apple Color Emoji,Segoe UI Emoji,Segoe UI Symbol,Noto Color Emoji'
    }"
  />
  <ElCard>
    <ElPagination
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      :page-sizes="[10, 20, 50, 100, 200, 500, 1000]"
      layout="total, sizes, prev, pager, next, jumper"
      :total="total"
    />
  </ElCard>
</template>
