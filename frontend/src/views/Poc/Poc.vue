<script setup lang="tsx">
import { confirmDialog } from '@/utils/confirm'
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { ref, reactive, onUnmounted } from 'vue'
import {
  ElButton,
  ElCol,
  ElInput,
  ElRow,
  ElText,
  ElUpload,
  ElTooltip,
  ElMessage,
  UploadProps,
  UploadRawFile,
  UploadInstance,
  ElTag,

} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { syncPocApi, getImportStatusApi } from '@/api/poc'
import { Icon } from '@/components/Icon'
import { Upload, Refresh } from '@element-plus/icons-vue'
import { useIcon } from '@/hooks/web/useIcon'
import { BaseButton } from '@/components/Button'
import { getPocDataApi, deletePocDataApi, getPocDetailApi } from '@/api/poc'
import Detail from './components/Detail.vue'
import { Dialog } from '@/components/Dialog'
import { useUserStore } from '@/store/modules/user'
import { RowState } from '@/api/asset/types'
import { addTagApi, deleteTagApi } from '@/api/asset'
const searchicon = useIcon({ icon: 'iconoir:search' })
const { t } = useI18n()
const dialogVisible = ref(false)
const search = ref('')
const handleSearch = () => {
  getList()
}
const rowStateMap = reactive<Record<string, RowState>>({})
const nodeColums = reactive<TableColumn[]>([
  {
    field: 'selection',
    type: 'selection',
    width: 55
  },
  {
    field: 'name',
    label: t('poc.pocName'),
    minWidth: 70
  },
  {
    field: 'level',
    label: t('poc.level'),
    minWidth: 50,
    columnKey: 'level',
    formatter: (_: Recordable, __: TableColumn, levelValue: string) => {
      if (levelValue == null) {
        return <div></div>
      }
      let color = ''
      let flag = ''
      if (levelValue === 'critical') {
        color = 'red'
        flag = t('poc.critical')
      } else if (levelValue === 'high') {
        color = 'orange'
        flag = t('poc.high')
      } else if (levelValue === 'medium') {
        color = 'yellow'
        flag = t('poc.medium')
      } else if (levelValue === 'low') {
        color = 'blue'
        flag = t('poc.low')
      } else if (levelValue === 'info') {
        color = 'green'
        flag = t('poc.info')
      } else if (levelValue === 'unknown') {
        color = 'gray'
        flag = t('poc.unknown')
      }
      return (
        <ElRow gutter={20} style="width: 80%">
          <ElCol span={1}>
            <Icon icon="clarity:circle-solid" color={color} size={10} />
          </ElCol>
          <ElCol span={5}>
            <ElText type="info">{flag}</ElText>
          </ElCol>
        </ElRow>
      )
    },
    filters: [
      { text: t('poc.critical'), value: 'critical' },
      { text: t('poc.high'), value: 'high' },
      { text: t('poc.medium'), value: 'medium' },
      { text: t('poc.low'), value: 'low' },
      { text: t('poc.info'), value: 'info' },
      { text: t('poc.unknown'), value: 'unknown' }
    ]
  },
  {
    field: 'tags',
    label: t('localization.tag'),
    fit: 'true',
    formatter: (row: Recordable, __: TableColumn, tags: string[]) => {
      if (tags.length != 0) {
        return (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
            {tags.map((product) => (
              <div
                key={product}
                onClick={() => changeTags('app', product)}
                style={{ cursor: 'pointer' }}
              >
                <ElTag type={'success'}>{product}</ElTag>
              </div>
            ))}
          </div>
        )
        // if (ProductsValue.length > 1) {
        //   let contentTool = ''
        //   if (Array.isArray(ProductsValue)) {
        //     // It's an array, you can use forEach
        //     ProductsValue.forEach((item, _) => {
        //       contentTool += `<div>${item}</div>`
        //     })
        //   } else {
        //     console.error('ProductsValue is not an array')
        //   }
        //   return (
        //     <div class="flex">
        //       <ElTag type="success" effect="light" round>
        //         {ProductsValue[0]}
        //       </ElTag>
        //       <ElTooltip
        //         class="box-item"
        //         effect="dark"
        //         placement="top-start"
        //         content={contentTool}
        //         popper-class="tagtooltip"
        //         rawContent
        //       >
        //         <ElTag type="info" effect="plain" round style={'left:3px; position:relative'}>
        //           {t('asset.total')} {ProductsValue.length} {t('asset.p')}
        //         </ElTag>
        //       </ElTooltip>
        //     </div>
        //   )
        // } else {
        //   return (
        //     <div class="flex">
        //       <ElTag type="success" effect="light">
        //         {ProductsValue[0]}
        //       </ElTag>
        //     </div>
        //   )
        // }
      }
    }
  },
  {
    field: 'time',
    label: t('node.createTime'),
    minWidth: 50
  },

  {
    field: 'action',
    label: t('tableDemo.action'),
    minWidth: 30,
    formatter: (row, __: TableColumn, _: number) => {
      return (
        <>
          <BaseButton type="primary" onClick={() => edit(row)}>
            {t('common.edit')}
          </BaseButton>
          <BaseButton type="danger" onClick={() => del(row)}>
            {t('common.delete')}
          </BaseButton>
        </>
      )
    }
  }
])
const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getPocDataApi(search.value, currentPage.value, pageSize.value, filter)
    return {
      list: res.data.list,
      total: res.data.total
    }
  }
})
const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList, getElTableExpose } = tableMethods
function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}

const changeTags = (type, value) => {
  const key = `${type}=${value}`
  console.log(key)
  // dynamicTags.value = [...dynamicTags.value, key]
}

let pocForm = reactive({
  id: '',
  name: '',
  level: '',
  content: '',
  tags: []
})
const addPoc = async () => {
  pocForm.id = ''
  pocForm.name = ''
  pocForm.level = ''
  pocForm.content = ''
  pocForm.tags = []
  dialogVisible.value = true
}
const edit = async (data) => {
  pocForm.id = data.id
  pocForm.name = data.name
  pocForm.level = data.level
  pocForm.tags = data.tags
  const res = await getPocDetailApi(pocForm.id)
  pocForm.content = res.data.content
  dialogVisible.value = true
}

const closeDialog = () => {
  dialogVisible.value = false
}
const delLoading = ref(false)
// 没勾选任何行时批量删除按钮应禁用，避免点了弹确认却什么都不删
const selectedCount = ref(0)
const handleSelectionChange = (rows: unknown[]) => {
  selectedCount.value = rows?.length ?? 0
}
const del = async (data) => {
  delLoading.value = true
  try {
    const res = await deletePocDataApi([data.id])
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    delLoading.value = false
    getList()
  }
}
const ids = ref<string[]>([])
const delSelect = async () => {
  const elTableExpose = await getElTableExpose()
  const selectedRows = elTableExpose?.getSelectionRows() || []
  ids.value = selectedRows.map((row) => row.id)
  delLoading.value = true
  try {
    const res = await deletePocDataApi(ids.value)
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    delLoading.value = false
    getList()
  }
}
const confirmDelete = async () => {
  const confirmed = await confirmDialog(t('common.delMessage'))
  if (confirmed) {
    await delSelect()
  }
}
const userStore = useUserStore()
const uploadHeaders = ref({ Authorization: `${userStore.getToken}` })

// 「立即同步」：从上游仓库拉取 POC 并导入（后端复用每日定时任务的同一实现）
const syncLoading = ref(false)
const handleSync = async () => {
  syncLoading.value = true
  try {
    const res: any = await syncPocApi()
    startImportPoll()
    const d = res?.data || {}
    ElMessage.success(
      `同步完成：新增 ${d.success_num ?? 0}，更新 ${d.update_num ?? 0}，` +
        `无变化 ${d.repeat_num ?? 0}，失败 ${d.error_num ?? 0}`
    )
    getList()
  } catch (e: any) {
    ElMessage.error(`同步失败：${e?.message || '未知错误'}`)
  } finally {
    syncLoading.value = false
  }
}

const upload = ref<UploadInstance>()
const uploadSuccess = async () => {
  console.log('导入中')
  ElMessage.success(t('localization.importing'))
}

const handleExceed: UploadProps['onExceed'] = (files) => {
  upload.value!.clearFiles()
  const file = files[0] as UploadRawFile
  upload.value!.handleStart(file)
}

// 导入进度轮询：上传/同步后每 1 秒查一次，把「黑盒等待」变成进度反馈
const importStatus = ref<any>(null)
let importTimer: ReturnType<typeof setInterval> | null = null
const stopImportPoll = () => {
  if (importTimer) {
    clearInterval(importTimer)
    importTimer = null
  }
}
const startImportPoll = () => {
  stopImportPoll()
  importTimer = setInterval(async () => {
    try {
      const res: any = await getImportStatusApi()
      importStatus.value = res?.data || null
      if (importStatus.value && importStatus.value.running === false) {
        stopImportPoll()
        getList()
        const d = importStatus.value
        ElMessage.success(
          t('poc.importDone', {
            total: d.total ?? 0,
            update: d.update ?? 0,
            repeat: d.repeat ?? 0,
            failed: d.failed ?? 0
          })
        )
      }
    } catch {
      stopImportPoll()
    }
  }, 1000)
}
onUnmounted(stopImportPoll)

const handleUploadSuccess = (response) => {
  console.log(response)
  if (response.code === 200) {
    ElMessage.success(t('localization.uploadSuccess'))
    startImportPoll()
  } else {
    ElMessage.error(response.message)
  }
  if (response.code == 505) {
    localStorage.removeItem('plugin_key')
  }
  getList()
  upload.value?.clearFiles()
}
const handleFileChange = (file, fileList) => {
  if (fileList.length > 0) {
    upload.value!.submit()
  }
}
const filter = reactive<{ [key: string]: any }>({})
const filterChange = async (newFilters: any) => {
  Object.assign(filter, newFilters)
  getList()
}
</script>

<template>
  <ContentWrap>
    <div class="poc-toolbar">
      <span class="poc-toolbar__label">{{ t('poc.pocName') }}</span>
      <ElInput
        v-model="search"
        :placeholder="t('common.inputText')"
        class="poc-toolbar__input"
        style="height: 36px"
      />
      <ElButton type="primary" :icon="searchicon" style="height: 36px" @click="handleSearch">{{
        t('common.search')
      }}</ElButton>
    </div>
    <div class="poc-actions">
      <ElButton type="primary" @click="addPoc">{{ t('common.new') }}</ElButton>
      <BaseButton
        type="danger"
        :loading="delLoading"
        :disabled="selectedCount === 0"
        @click="confirmDelete"
      >
        {{ t('common.delete') }}
      </BaseButton>
      <ElButton :loading="syncLoading" :icon="Refresh" @click="handleSync">
        {{ t('poc.syncNow') }}
      </ElButton>
      <ElTooltip
        :content="t('common.uploadMsg')"
        placement="top-start"
        popper-class="poc-upload-tip"
      >
        <ElUpload
          ref="upload"
          class="flex items-center"
          action="/api/poc/data/import"
          :headers="uploadHeaders"
          :on-success="handleUploadSuccess"
          :limit="1"
          :on-exceed="handleExceed"
          :auto-upload="false"
          @change="handleFileChange"
        >
          <!-- 2026-11-18：这里原来用 BaseButton + #icon 插槽包文字，线上出现过整块空白按钮。
               换成标准 ElButton（图标走 :icon、文字走默认插槽），样式与同一行的「新建」一致，
               也不可能再渲染成一个没有内容的空盒子。 -->
          <template #trigger>
            <ElButton :icon="Upload">{{ t('plugin.import') }}</ElButton>
          </template>
        </ElUpload>
      </ElTooltip>
    </div>
    <Table
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      @selection-change="handleSelectionChange"
      @filter-change="filterChange"
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
  </ContentWrap>
  <Dialog
    v-model="dialogVisible"
    :title="pocForm.id ? $t('common.edit') : $t('common.new')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="800"
  >
    <Detail :closeDialog="closeDialog" :pocForm="pocForm" :getList="getList" />
  </Dialog>
</template>

<style scoped>
.poc-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 15px;
}
.poc-toolbar__label {
  flex: 0 0 auto;
  white-space: nowrap;
  color: var(--el-text-color-regular);
}
.poc-toolbar__input {
  width: 320px;
}
.poc-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
}
</style>

<style>
.poc-upload-tip.el-popper {
  max-width: 360px;
  line-height: 1.6;
  white-space: normal;
}
</style>
