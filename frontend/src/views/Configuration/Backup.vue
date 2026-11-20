<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  ElButton,
  ElCol,
  ElRow,
  ElCard,
  ElProgress,
  ElAlert,
  ElMessage,
  ElMessageBox,
  ElUpload
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { BaseButton } from '@/components/Button'
import {
  listBackupsApi,
  createBackupApi,
  getBackupStatusApi,
  deleteBackupApi,
  restoreBackupApi,
  backupDownloadUrl
} from '@/api/backup'
import { useUserStore } from '@/store/modules/user'
import { formatTime } from '@/utils'

const userStore = useUserStore()

const formatSize = (size: number) => {
  if (!size) return '-'
  const mb = size / 1024 / 1024
  return mb >= 1024 ? (mb / 1024).toFixed(2) + ' GB' : mb.toFixed(1) + ' MB'
}

const status = reactive({ running: false, current: '', progress: 0, lastError: '' })
let timer: number | undefined

let prevRunning = false
const loadStatus = async () => {
  try {
    const res = await getBackupStatusApi()
    Object.assign(status, res.data)
    // 任务从「运行中」变为「空闲」时刷新一次列表，避免要手动点刷新
    if (prevRunning && !res.data.running) {
      getList()
    }
    prevRunning = res.data.running
  } catch {
    // 忽略状态轮询错误
  }
}

const startPolling = () => {
  stopPolling()
  // 后台标签页不轮询：备份可能要几分钟，用户切走后没必要一直打接口
  timer = window.setInterval(() => {
    if (document.hidden) return
    loadStatus()
  }, 2500)
}

const stopPolling = () => {
  if (timer) {
    window.clearInterval(timer)
    timer = undefined
  }
}

const columns = reactive<TableColumn[]>([
  { field: 'name', label: '备份文件', minWidth: 240 },
  {
    field: 'size',
    label: '大小',
    width: 110,
    formatter: (_row, __: TableColumn, value: number) => formatSize(value)
  },
  {
    field: 'createdAt',
    label: '创建时间',
    minWidth: 170,
    formatter: (_row, __: TableColumn, value: string) =>
      value ? formatTime(value, 'yyyy-MM-dd HH:mm:ss') : '-'
  },
  {
    field: 'sha256',
    label: 'SHA256',
    minWidth: 140,
    formatter: (_row, __: TableColumn, value: string) => (value ? value.slice(0, 12) + '…' : '-')
  },
  { field: 'note', label: '备注', minWidth: 160 },
  {
    field: 'action',
    label: '操作',
    width: 250,
    formatter: (row) => (
      <div class="flex items-center" style={'gap:6px'}>
        <BaseButton type="primary" onClick={() => handleDownload(row.name)}>
          下载
        </BaseButton>
        <BaseButton type="warning" onClick={() => handleRestore(row.name)}>
          恢复
        </BaseButton>
        <BaseButton type="danger" onClick={() => handleDelete(row.name)}>
          删除
        </BaseButton>
      </div>
    )
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const res = await listBackupsApi()
    // 后端直接返回数组；兼容 {list:[...]} 形态
    const raw: any = res.data
    const list = Array.isArray(raw) ? raw : raw?.list || []
    return { list }
  }
})
const { loading, dataList } = tableState
const { getList } = tableMethods

const uploading = ref(false)

const handleCreate = async () => {
  const { value } = await ElMessageBox.prompt('可填写备注（可留空）', '创建全量备份', {
    confirmButtonText: '开始备份',
    cancelButtonText: '取消',
    inputPlaceholder: '例如：升级前快照',
    inputValidator: () => true
  }).catch(() => ({ value: null }) as { value: string | null })
  if (value === null) return
  await createBackupApi(value || '')
  ElMessage.success('备份任务已提交')
  await loadStatus()
  getList()
}

const handleDownload = async (name: string) => {
  const resp = await fetch(backupDownloadUrl(name), {
    headers: { Authorization: userStore.getToken || '' }
  })
  const blob = await resp.blob()
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = name
  link.click()
  URL.revokeObjectURL(link.href)
}

const handleRestore = async (name: string) => {
  const { value } = await ElMessageBox.prompt(
    '恢复会先用当前数据做一次 pre-restore 备份，然后覆盖现有数据。请输入 RESTORE 确认。',
    '恢复备份 ' + name,
    {
      confirmButtonText: '执行恢复',
      cancelButtonText: '取消',
      inputPlaceholder: 'RESTORE',
      inputValidator: (v: string) => (v === 'RESTORE' ? true : '请输入大写的 RESTORE')
    }
  ).catch(() => ({ value: null }) as { value: string | null })
  if (value === null) return
  await restoreBackupApi(name, value)
  ElMessage.success('恢复任务已提交')
  await loadStatus()
  getList()
}

const handleDelete = async (name: string) => {
  await ElMessageBox.confirm('确定删除备份 ' + name + ' 吗？', '删除备份', {
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning'
  })
  await deleteBackupApi(name)
  getList()
}

const uploadHeaders = { Authorization: userStore.getToken || '' }

const handleUploadSuccess = () => {
  uploading.value = false
  ElMessage.success('上传完成')
  getList()
}

const handleUploadError = () => {
  uploading.value = false
  ElMessage.error('上传失败')
}

onMounted(async () => {
  await loadStatus()
  getList()
  startPolling()
})
onBeforeUnmount(stopPolling)
</script>

<template>
  <ContentWrap>
    <ElCard class="mb-12px" shadow="never">
      <template #header>
        <span>全量备份</span>
      </template>
      <ElRow :gutter="12" align="middle" class="mb-8px">
        <ElCol :span="4">
          <ElButton type="primary" @click="handleCreate">创建备份</ElButton>
        </ElCol>
        <ElCol :span="6">
          <ElUpload
            :action="'/api/backup/upload'"
            :headers="uploadHeaders"
            :show-file-list="false"
            :limit="1"
            accept=".tar.gz,.tgz,.zip"
            @success="handleUploadSuccess"
            @error="handleUploadError"
          >
            <ElButton type="success" :loading="uploading">上传备份包</ElButton>
          </ElUpload>
        </ElCol>
        <ElCol :span="4">
          <ElButton @click="getList">刷新列表</ElButton>
        </ElCol>
      </ElRow>

      <div v-if="status.running" class="mb-8px">
        <ElAlert type="info" :closable="false" show-icon>
          <template #title>
            正在执行：{{ status.current || '备份中' }}（{{ status.progress }}%）
          </template>
          <ElProgress :percentage="status.progress" :stroke-width="8" />
        </ElAlert>
      </div>
      <ElAlert v-else-if="status.lastError" type="error" :closable="false" show-icon
        :title="'上次任务失败：' + status.lastError" />
      <ElAlert v-else type="success" :closable="false" show-icon title="当前没有进行中的备份/恢复任务" />
    </ElCard>

    <Table
      :columns="columns"
      :data="dataList"
      stripe
      :border="true"
      :loading="loading"
      @register="tableRegister"
    />
  </ContentWrap>
</template>

<style scoped>
.mb-8px {
  margin-bottom: 8px;
}
.mb-12px {
  margin-bottom: 12px;
}
</style>
