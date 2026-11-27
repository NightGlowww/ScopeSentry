<script setup lang="tsx">
import { confirmDialog } from '@/utils/confirm'
import { useI18n } from '@/hooks/web/useI18n'
import {
  ElTabs,
  ElTabPane,
  ElFormItem,
  ElForm,
  ElRadio,
  ElRadioGroup,
  ElInput,
  ElButton,
  ElTag,
  ElSpace,
  ElCheckbox,
  CheckboxValueType,
  ElCheckboxGroup,
  ElDatePicker,
  ElMessage
} from 'element-plus'
import { onMounted, onUnmounted, reactive, Ref, ref } from 'vue'
import BatchExportTab from './BatchExportTab.vue'
import {
  exportApi,
  getExportRecordApi,
  delExportApi,
  getFieldApi,
  downloadExportApi
} from '@/api/export'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { BaseButton } from '@/components/Button'
const { t } = useI18n()

const props = defineProps<{
  index: string
  searchParams: string
  getFilter: () => { [key: string]: any }
  // 页面当前的「发现时间」起点（YYYY-MM-DD HH:MM:SS）。
  // 不传的话导出默认不按时间过滤，用户会以为导出的是屏幕上那批（2026-11-27）。
  initialFrom?: string
}>()

const nowStr = () => {
  const d = new Date()
  const p = (x: number) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

const exportForm = reactive({
  type: 'all',
  // 默认 1000：后端 ExportRequest.Quantity 是 required，0 会被判参数非法，
  // 之前默认 0 导致点「Create」直接报 "Invalid request parameters"（2026-11-20）。
  quantity: 1000,
  // issue #261：可选的导出时间范围，Element 的 datetimerange 给的是 [start, end]
  // 有页面时间筛选时继承它，做到「默认导出的就是屏幕上看到的范围」
  timeRange: (props.initialFrom ? [props.initialFrom, nowStr()] : []) as string[]
})
const prefilledFromPage = !!props.initialFrom
const create = async () => {
  // 导出是异步任务：提交后要明确告诉用户「已提交，去列表等」，并刷新记录列表。
  // 之前只切 loading，成功失败都没有任何提示（2026-11-20）。
  if (!exportForm.quantity || exportForm.quantity < 1) {
    ElMessage.warning(t('export.quantityRequired'))
    return
  }
  createLoading.value = true
  try {
    const searchFilter = props.getFilter()
    const [startTime = '', endTime = ''] = exportForm.timeRange || []
    await exportApi(
      props.index,
      exportForm.quantity,
      exportForm.type,
      props.searchParams,
      searchFilter,
      startTime,
      endTime,
      checkedField.value,
      filetype.value
    )
    ElMessage.success(t('export.submitted'))
    refreshWithTimer()
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    createLoading.value = false
  }
}

const exportColums = reactive<TableColumn[]>([
  {
    field: 'selection',
    type: 'selection'
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    fixed: 'left',
    formatter: (row, __: TableColumn, _: number) => {
      return (
        <>
          <BaseButton
            type="success"
            disabled={!!row.file_missing}
            title={row.file_missing ? t('export.fileMissingHint') : ''}
            onClick={() => download(row.file_name)}
          >
            {t('export.download')}
          </BaseButton>
          <BaseButton type="danger" onClick={() => confirmDelete(row)}>
            {t('common.delete')}
          </BaseButton>
        </>
      )
    }
  },
  {
    field: 'file_name',
    label: t('export.fileName')
  },
  {
    field: 'state',
    label: t('export.state'),
    formatter: (row: Recordable, __: TableColumn, value: number) => {
      // 记录说成功、文件却不在（容器重建丢过文件）——这比「成功」更该被看见
      if (row?.file_missing) {
        return (
          <ElTooltip content={t('export.fileMissingHint')} placement="top">
            <ElTag type="warning">{t('export.fileMissing')}</ElTag>
          </ElTooltip>
        )
      }
      if (value == 0) {
        return <ElTag type="info">{t('export.run')}</ElTag>
      } else if (value == 1) {
        return <ElTag type="success">{t('export.success')}</ElTag>
      } else {
        return <ElTag type="danger">{t('export.fail')}</ElTag>
      }
    }
  },
  {
    field: 'create_time',
    label: t('export.createTime')
  },
  {
    field: 'end_time',
    label: t('export.endTime'),
    formatter: (_: Recordable, __: TableColumn, value: string) => {
      if (value == '') {
        return '-'
      } else {
        return value
      }
    }
  },
  {
    field: 'data_type',
    label: t('export.type')
  },
  {
    field: 'file_size',
    label: t('export.fileSize'),
    formatter: (_: Recordable, __: TableColumn, value: string) => {
      if (value == '') {
        return '-'
      } else {
        return value + ' MB'
      }
    }
  }
])
const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const res = await getExportRecordApi()
    return {
      list: res.data.list
    }
  },
  immediate: false
})
const { dataList, loading } = tableState
// 导出是异步任务：有记录处于「生成中」时，每 3 秒静默刷新一次，
// 用户不用手动点刷新就能看到文件变成可用（2026-11-20）。
let exportTimer: ReturnType<typeof setInterval> | null = null
const syncExportTimer = () => {
  const running = (dataList.value || []).some((r: any) => Number(r.state) === 0)
  if (running && !exportTimer) {
    exportTimer = setInterval(async () => {
      try {
        await getList()
      } catch {
        // 静默
      }
    }, 3000)
  } else if (!running && exportTimer) {
    clearInterval(exportTimer)
    exportTimer = null
  }
}
onUnmounted(() => {
  if (exportTimer) clearInterval(exportTimer)
})

const refreshWithTimer = async () => {
  await getList()
  syncExportTimer()
}

const { getList, getElTableExpose } = tableMethods

const download = async (id) => {
  try {
    const blob = await downloadExportApi(id)
    const url = window.URL.createObjectURL(blob)

    const a = document.createElement('a')
    a.href = url
    a.download = `${id}`
    a.click()
    window.URL.revokeObjectURL(url)
  } catch (err) {
    console.error('下载失败:', err)
  }
}
const createLoading = ref(false)
const onClick = (name) => {
  if (name == 'exportRecords') {
    getList()
  }
}
const delLoading = ref(false)
const confirmDelete = async (data) => {
  const confirmed = await confirmDialog(t('common.delMessage'))
  if (confirmed) {
    await del(data)
  }
}
const del = async (data) => {
  delLoading.value = true
  try {
    const res = await delExportApi([data.file_name])
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    delLoading.value = false
    getList()
  }
}
const confirmDeleteSelect = async () => {
  const confirmed = await confirmDialog(t('common.delMessage'))
  if (confirmed) {
    await delSelect()
  }
}
const ids = ref<string[]>([])
const delSelect = async () => {
  const elTableExpose = await getElTableExpose()
  const selectedRows = elTableExpose?.getSelectionRows() || []
  ids.value = selectedRows.map((row) => row.file_name)
  delLoading.value = true
  try {
    const res = await delExportApi(ids.value)
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    delLoading.value = false
    getList()
  }
}
const fields = ref([]) as Ref<string[]>
const getField = async () => {
  const res = await getFieldApi(props.index)
  fields.value = res.data.field || []
  // 默认全选：字段为空时后端直接 400（fields cannot be empty），
  // 用户不勾任何框就点 Create 必然失败——这里给一个可直接用的默认值。
  if (checkedField.value.length === 0) {
    checkedField.value = [...fields.value]
    checkAll.value = true
    isIndeterminate.value = false
  }
}

onMounted(() => {
  getField()
})
const isIndeterminate = ref(true)
const checkAll = ref(false)
const checkedField = ref([]) as Ref<string[]>
const handleCheckAllChange = (val: CheckboxValueType) => {
  checkedField.value = val ? fields.value : []
  isIndeterminate.value = false
}
const handleCheckedCitiesChange = (value: CheckboxValueType[]) => {
  const checkedCount = value.length
  checkAll.value = checkedCount === fields.value.length
  isIndeterminate.value = checkedCount > 0 && checkedCount < fields.value.length
}
// 后端只接受 xlsx / json（之前默认值写成 csv 会直接 400）
const filetype = ref('xlsx')
</script>

<template>
  <ElTabs tabPosition="left" @tab-change="onClick" model-value="export">
    <ElTabPane :label="t('asset.export')" name="export">
      <ElForm :model="exportForm" label-width="auto" style="position: relative">
        <ElFormItem :label="t('export.exportType')">
          <ElRadioGroup v-model="exportForm.type">
            <ElRadio value="all">{{ t('export.exportTypeAll') }}</ElRadio>
            <ElRadio value="search">{{ t('export.exportTypeSearch') }}</ElRadio>
          </ElRadioGroup>
        </ElFormItem>
        <ElFormItem :label="t('export.exportQuantity')">
          <ElInput v-model.number="exportForm.quantity" />
        </ElFormItem>
        <ElFormItem :label="t('export.timeRange')">
          <ElDatePicker
            v-model="exportForm.timeRange"
            type="datetimerange"
            value-format="YYYY-MM-DD HH:mm:ss"
            :start-placeholder="t('export.timeRangeStart')"
            :end-placeholder="t('export.timeRangeEnd')"
            clearable
            style="width: 100%"
          />
          <!-- 默认值来自页面时间筛选，必须写出来：否则用户以为导的是全部，或者以为筛选没生效 -->
          <div v-if="prefilledFromPage" class="prefill-hint">
            {{
              exportForm.timeRange.length
                ? '已继承页面「发现时间」筛选；清空时间即导出全部'
                : '时间已清空：将导出全部数据，不受页面时间筛选限制'
            }}
          </div>
        </ElFormItem>
        <ElFormItem :label="t('export.field')">
          <ElCheckbox
            v-model="checkAll"
            :indeterminate="isIndeterminate"
            @change="handleCheckAllChange"
          >
            {{ t('common.all') }}
          </ElCheckbox>
        </ElFormItem>
        <ElFormItem label=" ">
          <ElCheckboxGroup v-model="checkedField" @change="handleCheckedCitiesChange">
            <ElCheckbox v-for="field in fields" :key="field" :label="field" :value="field">
              {{ field }}
            </ElCheckbox>
          </ElCheckboxGroup>
        </ElFormItem>
        <ElFormItem :label="t('export.fileType')">
          <ElRadioGroup v-model="filetype">
            <ElRadio value="xlsx">xlsx</ElRadio>
            <ElRadio value="json">json</ElRadio>
          </ElRadioGroup>
        </ElFormItem>
        <ElFormItem>
          <ElButton
            type="primary"
            @click="create"
            style="left: 40%; position: relative"
            :loading="createLoading"
          >
            Create
          </ElButton>
        </ElFormItem>
      </ElForm>
    </ElTabPane>
    <ElTabPane :label="t('export.exportRecords')" name="exportRecords">
      <ElSpace direction="vertical" alignment="flex-start" :style="{ width: '100%' }">
        <BaseButton type="danger" :loading="delLoading" @click="confirmDeleteSelect">
          {{ t('common.delete') }}
        </BaseButton>
      </ElSpace>
      <Table
        @register="tableRegister"
        :columns="exportColums"
        :data="dataList"
        :loading="loading"
        max-height="500"
        :style="{
          fontFamily:
            '-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica Neue,Arial,Noto Sans,sans-serif,Apple Color Emoji,Segoe UI Emoji,Segoe UI Symbol,Noto Color Emoji',
          width: '100%'
        }"
      />
    </ElTabPane>
    <ElTabPane label="批量导出" name="batchExport">
      <BatchExportTab />
    </ElTabPane>
  </ElTabs>
</template>

<style scoped>
.prefill-hint {
  width: 100%;
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
</style>
