<script setup lang="tsx">
import { onMounted, ref } from 'vue'
import { useI18n } from '@/hooks/web/useI18n'
import { useRoute } from 'vue-router'
import {
  ElAlert,
  ElButton,
  ElCheckbox,
  ElDescriptions,
  ElDescriptionsItem,
  ElDialog,
  ElInput,
  ElMessage,
  ElTable,
  ElTableColumn,
  ElTag
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import {
  getTaskDataApi,
  getTaskDetailApi,
  ingestToProjectApi,
  assignTaskProjectApi
} from '@/api/task'

// 项目详情的「任务」页签。
//
// 之前项目详情只有概览/子域名/端口/服务四个页签，任务属于哪个项目完全看不出来，
// 项目页点进去像个摆设（2026-11-27 反馈）。这个页签按 bindProject 拉任务，
// 让「一个项目 = 一串任务」这件事在界面上成立。
const { t } = useI18n('projectDetail')
const { query } = useRoute()
const projectId = String(query.id || '')

const loading = ref(false)
const rows = ref<any[]>([])
const total = ref(0)
const loadError = ref('')

const statusMap: Record<number, { key: string; type: any }> = {
  0: { key: 'statusPending', type: 'primary' },
  1: { key: 'statusRunning', type: 'info' },
  2: { key: 'statusPaused', type: 'warning' },
  3: { key: 'statusDone', type: 'success' },
  4: { key: 'statusFailed', type: 'danger' }
}

const statusLabel = (val: unknown) => {
  const s = statusMap[Number(val)]
  return s ? t(s.key) : ''
}
const statusTagType = (val: unknown) => statusMap[Number(val)]?.type || 'default'

const load = async () => {
  if (!projectId) return
  loading.value = true
  loadError.value = ''
  try {
    const res = await getTaskDataApi('', 1, 100, undefined, undefined, projectId)
    rows.value = res.data?.list || []
    total.value = res.data?.total || 0
  } catch (e: any) {
    // 失败要说出来：空表格会被当成「这个项目确实没有任务」
    loadError.value = e?.message || t('loadFailed')
  } finally {
    loading.value = false
  }
}
onMounted(load)

// 任务详情弹窗
// 「往这个项目里加一批目标」：直接走后端 ingest，不再需要「先建任务再同步到项目」。
const addVisible = ref(false)
const addTargets = ref('')
const addAutoStart = ref(true)
const addSubmitting = ref(false)

const submitAdd = async () => {
  const targets = addTargets.value.trim()
  if (!targets) {
    ElMessage.warning(t('noTargets'))
    return
  }
  addSubmitting.value = true
  try {
    const res = await ingestToProjectApi({
      projectId,
      targets,
      autoStart: addAutoStart.value
    })
    const d: any = res.data || {}
    let msg = t('ingestSuccess', {
      name: d.taskName || '',
      added: d.addedTargets ?? d.targetCount ?? 0
    })
    msg += d.startError
      ? t('ingestStartFailed', { reason: d.startError })
      : d.started
        ? t('ingestStarted')
        : ''
    ElMessage.success(msg)
    addVisible.value = false
    addTargets.value = ''
    await load()
  } catch (e: any) {
    // 建任务失败要说清楚原因（比如模板 id 不对、节点离线），不要静默
    ElMessage.error(e?.message || t('createTaskFailed'))
  } finally {
    addSubmitting.value = false
  }
}

// 关联已有任务：bindProject 是后来才加的字段，历史任务全是空值，
// 没有补录通道的话项目页永远只有零星几条新任务（2026-11-27）。
const linkVisible = ref(false)
const linkSearch = ref('')
const linkLoading = ref(false)
const linkRows = ref<any[]>([])
const linkSelected = ref<string[]>([])

const searchTasks = async () => {
  linkLoading.value = true
  try {
    const res = await getTaskDataApi(linkSearch.value.trim(), 1, 50)
    linkRows.value = res.data?.list || []
    linkSelected.value = []
  } catch (e: any) {
    ElMessage.error(e?.message || t('taskSearchFailed'))
  } finally {
    linkLoading.value = false
  }
}

const submitLink = async () => {
  if (linkSelected.value.length === 0) {
    ElMessage.warning(t('selectTasksFirst'))
    return
  }
  try {
    const res = await assignTaskProjectApi(linkSelected.value, projectId)
    ElMessage.success(t('linkSuccess', { n: res.data?.updated ?? linkSelected.value.length }))
    linkVisible.value = false
    await load()
  } catch (e: any) {
    ElMessage.error(e?.message || t('linkFailed'))
  }
}

const unlinkTask = async (id: string) => {
  try {
    await assignTaskProjectApi([id], '')
    ElMessage.success(t('unlinkSuccess'))
    await load()
  } catch (e: any) {
    ElMessage.error(e?.message || t('unlinkFailed'))
  }
}

const detail = ref<any>(null)
const detailVisible = ref(false)
const openDetail = async (id: string) => {
  const res = await getTaskDetailApi(id)
  detail.value = res.data
  detailVisible.value = true
}

const columns: TableColumn[] = [
  { field: 'name', label: t('taskName'), minWidth: 260 },
  {
    field: 'status',
    label: t('status'),
    width: 100,
    formatter: (_row: any, _col: any, val: number) => (
      <ElTag type={statusTagType(val)}>{statusLabel(val) || String(val ?? '-')}</ElTag>
    )
  },
  {
    field: 'progress',
    label: t('progress'),
    width: 90,
    formatter: (_r: any, _c: any, v: number) => `${v ?? 0}%`
  },
  { field: 'creatTime', label: t('createTime'), width: 170 },
  { field: 'endTime', label: t('endTime'), width: 170 },
  {
    field: 'action',
    label: t('action'),
    width: 170,
    formatter: (row: any) => (
      <div>
        <ElButton link type="primary" onClick={() => openDetail(row.id)}>
          {t('detail')}
        </ElButton>
        <ElButton link type="danger" onClick={() => unlinkTask(row.id)}>
          {t('unlink')}
        </ElButton>
      </div>
    )
  }
]
</script>

<template>
  <div>
    <ElAlert
      v-if="loadError"
      type="error"
      :closable="false"
      :title="t('tasksLoadFailedAlert', { message: loadError })"
      style="margin-bottom: 12px"
    />
    <ElAlert
      v-else-if="!loading && total === 0"
      type="info"
      :closable="false"
      :title="t('tasksEmptyHint')"
      style="margin-bottom: 12px"
    />
    <div style="margin-bottom: 8px; display: flex; align-items: center; gap: 8px">
      <span style="color: var(--el-text-color-secondary); font-size: 12px">
        {{ t('totalTasks', { n: total }) }}
      </span>
      <ElButton link type="primary" @click="load">{{ t('refresh') }}</ElButton>
      <ElButton type="primary" size="small" @click="addVisible = true">{{
        t('addTargetsScan')
      }}</ElButton>
      <ElButton size="small" @click="((linkVisible = true), searchTasks())">{{
        t('linkExisting')
      }}</ElButton>
    </div>
    <Table
      :columns="columns"
      :data="rows"
      :loading="loading"
      :pagination="{ total, pageSize: 100, currentPage: 1 }"
      :show-pagination="false"
    />

    <!-- 项目优先：往这个项目里加一批目标，服务端负责建任务并绑定本项目 -->
    <ElDialog v-model="addVisible" :title="t('addDialogTitle')" width="620px">
      <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-bottom: 8px">
        {{ t('addHint') }}
      </div>
      <ElInput
        v-model="addTargets"
        type="textarea"
        :rows="8"
        placeholder="example.com&#10;api.example.com&#10;https://staging.example.com"
      />
      <div style="margin-top: 10px">
        <ElCheckbox v-model="addAutoStart">{{ t('autoStart') }}</ElCheckbox>
      </div>
      <template #footer>
        <ElButton @click="addVisible = false">{{ t('cancel') }}</ElButton>
        <ElButton type="primary" :loading="addSubmitting" @click="submitAdd">{{
          t('create')
        }}</ElButton>
      </template>
    </ElDialog>

    <!-- 归属补录：把历史任务挂到当前项目（bindProject 是后加的字段，老任务全空） -->
    <ElDialog v-model="linkVisible" :title="t('linkDialogTitle')" width="720px">
      <div style="display: flex; gap: 8px; margin-bottom: 10px">
        <ElInput
          v-model="linkSearch"
          :placeholder="t('linkSearchPlaceholder')"
          @keyup.enter="searchTasks"
        />
        <ElButton type="primary" :loading="linkLoading" @click="searchTasks">{{
          t('search')
        }}</ElButton>
      </div>
      <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-bottom: 8px">
        {{ t('linkHint') }}
      </div>
      <ElTable
        :data="linkRows"
        size="small"
        height="360"
        @selection-change="(v) => (linkSelected = v.map((r) => r.id))"
      >
        <ElTableColumn type="selection" width="46" />
        <ElTableColumn prop="name" :label="t('taskName')" min-width="320" show-overflow-tooltip />
        <ElTableColumn :label="t('status')" width="90">
          <template #default="{ row }">
            <ElTag size="small">{{ statusLabel(row.status) || row.status }}</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('currentProject')" width="200" show-overflow-tooltip>
          <template #default="{ row }">{{ row.bindProject || t('unassigned') }}</template>
        </ElTableColumn>
      </ElTable>
      <template #footer>
        <ElButton @click="linkVisible = false">{{ t('cancel') }}</ElButton>
        <ElButton type="primary" @click="submitLink">{{
          t('linkSelected', { n: linkSelected.length })
        }}</ElButton>
      </template>
    </ElDialog>

    <ElDialog v-model="detailVisible" :title="t('detailTitle')" width="720px">
      <ElDescriptions v-if="detail" :column="1" border>
        <ElDescriptionsItem :label="t('taskName')">{{ detail.name }}</ElDescriptionsItem>
        <ElDescriptionsItem :label="t('status')">
          {{ statusLabel(detail.status) || detail.status }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('progress')">{{ detail.progress }}%</ElDescriptionsItem>
        <ElDescriptionsItem :label="t('node')">{{
          (detail.node || []).join(', ') || t('all')
        }}</ElDescriptionsItem>
        <ElDescriptionsItem :label="t('target')">
          <pre style="white-space: pre-wrap; margin: 0; max-height: 200px; overflow: auto">{{
            detail.target
          }}</pre>
        </ElDescriptionsItem>
        <ElDescriptionsItem v-if="detail.failReason" :label="t('failReason')">
          {{ detail.failReason }}
        </ElDescriptionsItem>
      </ElDescriptions>
    </ElDialog>
  </div>
</template>

<style scoped></style>
