<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useSingleTabLeader } from '@/hooks/web/useSingleTabLeader'
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

const { t } = useI18n('backup')
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

const { isLeader: backupLeader } = useSingleTabLeader('backup')

const startPolling = () => {
  stopPolling()
  // 后台标签页不轮询：备份可能要几分钟，用户切走后没必要一直打接口
  timer = window.setInterval(() => {
    if (document.hidden) return
    // 多标签页只让一个轮询：2.5 秒的间隔多开时叠加最明显（2026-11-21）
    if (!backupLeader.value) return
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
  { field: 'name', label: t('colName'), minWidth: 240 },
  {
    field: 'size',
    label: t('colSize'),
    width: 110,
    formatter: (_row, __: TableColumn, value: number) => formatSize(value)
  },
  {
    field: 'createdAt',
    label: t('colCreatedAt'),
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
  { field: 'note', label: t('colNote'), minWidth: 160 },
  {
    field: 'action',
    label: t('colAction'),
    width: 250,
    formatter: (row) => (
      <div class="flex items-center" style={'gap:6px'}>
        <BaseButton type="primary" onClick={() => handleDownload(row.name)}>
          {t('download')}
        </BaseButton>
        <BaseButton type="warning" onClick={() => handleRestore(row.name)}>
          {t('restore')}
        </BaseButton>
        <BaseButton type="danger" onClick={() => handleDelete(row.name)}>
          {t('delete')}
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

// 备份列表汇总：份数 / 合计体积 / 最近一份的时间与大小
const backupSummary = computed(() => {
  const list = (dataList.value || []) as any[]
  if (!list.length) return { count: 0, text: '' }
  const total = list.reduce((sum, b) => sum + Number(b.size || 0), 0)
  const latest = list[0]
  const fmt = (bytes: number) => {
    const gb = bytes / 1024 / 1024 / 1024
    return gb >= 1 ? gb.toFixed(2) + ' GB' : (bytes / 1024 / 1024).toFixed(1) + ' MB'
  }
  return {
    count: list.length,
    text: t('summary', {
      count: list.length,
      total: fmt(total),
      latest: latest?.createdAt || '-',
      size: fmt(Number(latest?.size || 0))
    })
  }
})
const { getList } = tableMethods

const uploading = ref(false)

const handleCreate = async () => {
  const { value } = await ElMessageBox.prompt(t('createPromptMessage'), t('createTitle'), {
    confirmButtonText: t('startBackup'),
    cancelButtonText: t('cancel'),
    inputPlaceholder: t('notePlaceholder'),
    inputValidator: () => true
  }).catch(() => ({ value: null }) as { value: string | null })
  if (value === null) return
  await createBackupApi(value || '')
  ElMessage.success(t('backupSubmitted'))
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
    t('restoreConfirmMessage'),
    t('restoreTitle', { name }),
    {
      confirmButtonText: t('doRestore'),
      cancelButtonText: t('cancel'),
      inputPlaceholder: 'RESTORE',
      inputValidator: (v: string) => (v === 'RESTORE' ? true : t('restoreMustUpper'))
    }
  ).catch(() => ({ value: null }) as { value: string | null })
  if (value === null) return
  await restoreBackupApi(name, value)
  ElMessage.success(t('restoreSubmitted'))
  await loadStatus()
  getList()
}

const handleDelete = async (name: string) => {
  await ElMessageBox.confirm(t('deleteConfirm', { name }), t('deleteTitle'), {
    confirmButtonText: t('delete'),
    cancelButtonText: t('cancel'),
    type: 'warning'
  })
  await deleteBackupApi(name)
  getList()
}

const uploadHeaders = { Authorization: userStore.getToken || '' }

const handleUploadSuccess = () => {
  uploading.value = false
  ElMessage.success(t('uploadDone'))
  getList()
}

const handleUploadError = () => {
  uploading.value = false
  ElMessage.error(t('uploadFailed'))
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
        <span>{{ t('fullBackup') }}</span>
      </template>
      <ElRow :gutter="12" align="middle" class="mb-8px">
        <ElCol :span="4">
          <ElButton type="primary" @click="handleCreate">{{ t('createBackup') }}</ElButton>
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
            <ElButton type="success" :loading="uploading">{{ t('uploadPackage') }}</ElButton>
          </ElUpload>
        </ElCol>
        <ElCol :span="4">
          <ElButton @click="getList">{{ t('refreshList') }}</ElButton>
        </ElCol>
      </ElRow>

      <div v-if="status.running" class="mb-8px">
        <ElAlert type="info" :closable="false" show-icon>
          <template #title>
            {{
              t('runningNow', {
                current: status.current || t('backingUp'),
                progress: status.progress
              })
            }}
          </template>
          <ElProgress :percentage="status.progress" :stroke-width="8" />
        </ElAlert>
      </div>
      <ElAlert
        v-else-if="status.lastError"
        type="error"
        :closable="false"
        show-icon
        :title="t('lastTaskFailed', { error: status.lastError })"
      />
      <ElAlert v-else type="success" :closable="false" show-icon :title="t('idle')" />
    </ElCard>

    <!-- 备份单份可能几 GB：份数与合计占用要摆在列表上方，方便判断卷够不够 -->
    <div v-if="backupSummary.count" class="backup-summary">
      <span>{{ backupSummary.text }}</span>
    </div>
    <Table
      :columns="columns"
      :data="dataList"
      stripe
      :border="true"
      :loading="loading"
      :empty-text="t('emptyText')"
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
.backup-summary {
  margin-bottom: 8px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
</style>
