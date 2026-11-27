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
import { getTaskDataApi, getTaskDetailApi, ingestToProjectApi, assignTaskProjectApi } from '@/api/task'

// 项目详情的「任务」页签。
//
// 之前项目详情只有概览/子域名/端口/服务四个页签，任务属于哪个项目完全看不出来，
// 项目页点进去像个摆设（2026-11-27 反馈）。这个页签按 bindProject 拉任务，
// 让「一个项目 = 一串任务」这件事在界面上成立。
const { t } = useI18n()
const { query } = useRoute()
const projectId = String(query.id || "")

const loading = ref(false)
const rows = ref<any[]>([])
const total = ref(0)
const loadError = ref("")

const statusMap: Record<number, { text: string; type: any }> = {
  0: { text: "待开始", type: "primary" },
  1: { text: "运行中", type: "info" },
  2: { text: "暂停", type: "warning" },
  3: { text: "完成", type: "success" },
  4: { text: "失败", type: "danger" }
}

const load = async () => {
  if (!projectId) return
  loading.value = true
  loadError.value = ""
  try {
    const res = await getTaskDataApi("", 1, 100, undefined, undefined, projectId)
    rows.value = res.data?.list || []
    total.value = res.data?.total || 0
  } catch (e: any) {
    // 失败要说出来：空表格会被当成「这个项目确实没有任务」
    loadError.value = e?.message || "加载失败"
  } finally {
    loading.value = false
  }
}
onMounted(load)

// 任务详情弹窗
// 「往这个项目里加一批目标」：直接走后端 ingest，不再需要「先建任务再同步到项目」。
const addVisible = ref(false)
const addTargets = ref("")
const addAutoStart = ref(true)
const addSubmitting = ref(false)

const submitAdd = async () => {
  const targets = addTargets.value.trim()
  if (!targets) {
    ElMessage.warning("请先填入目标（一行一个）")
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
    ElMessage.success(
      `已在项目下创建任务 ${d.taskName || ""}（新增目标 ${d.addedTargets ?? d.targetCount ?? 0} 个）` +
        (d.startError ? `；启动失败：${d.startError}` : d.started ? "，已开始执行" : "")
    )
    addVisible.value = false
    addTargets.value = ""
    await load()
  } catch (e: any) {
    // 建任务失败要说清楚原因（比如模板 id 不对、节点离线），不要静默
    ElMessage.error(e?.message || "创建任务失败")
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
    ElMessage.error(e?.message || '任务检索失败')
  } finally {
    linkLoading.value = false
  }
}

const submitLink = async () => {
  if (linkSelected.value.length === 0) {
    ElMessage.warning('请先勾选要关联的任务')
    return
  }
  try {
    const res = await assignTaskProjectApi(linkSelected.value, projectId)
    ElMessage.success(`已关联 ${res.data?.updated ?? linkSelected.value.length} 个任务`)
    linkVisible.value = false
    await load()
  } catch (e: any) {
    ElMessage.error(e?.message || '关联失败')
  }
}

const unlinkTask = async (id: string) => {
  try {
    await assignTaskProjectApi([id], '')
    ElMessage.success('已移出本项目')
    await load()
  } catch (e: any) {
    ElMessage.error(e?.message || '移出失败')
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
  { field: "name", label: "任务名", minWidth: 260 },
  {
    field: "status",
    label: "状态",
    width: 100,
    formatter: (_row: any, _col: any, val: number) => {
      const s = statusMap[Number(val)] || { text: String(val ?? "-"), type: "default" }
      return <ElTag type={s.type}>{s.text}</ElTag>
    }
  },
  { field: "progress", label: "进度", width: 90, formatter: (_r: any, _c: any, v: number) => `${v ?? 0}%` },
  { field: "creatTime", label: "创建时间", width: 170 },
  { field: "endTime", label: "结束时间", width: 170 },
  {
    field: "action",
    label: "操作",
    width: 170,
    formatter: (row: any) => (
      <div>
        <ElButton link type="primary" onClick={() => openDetail(row.id)}>
          详情
        </ElButton>
        <ElButton link type="danger" onClick={() => unlinkTask(row.id)}>
          移出项目
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
      :title="`任务列表加载失败：${loadError}`"
      style="margin-bottom: 12px"
    />
    <ElAlert
      v-else-if="!loading && total === 0"
      type="info"
      :closable="false"
      title="这个项目下还没有任务。外部集成可以通过 POST /api/task/ingest 以「往项目里加一批目标」的方式创建任务，任务会自动绑定到本项目。"
      style="margin-bottom: 12px"
    />
    <div style="margin-bottom: 8px; display: flex; align-items: center; gap: 8px">
      <span style="color: var(--el-text-color-secondary); font-size: 12px">
        共 {{ total }} 个任务（按 bindProject 关联）
      </span>
      <ElButton link type="primary" @click="load">刷新</ElButton>
      <ElButton type="primary" size="small" @click="addVisible = true">添加目标并扫描</ElButton>
      <ElButton size="small" @click="(linkVisible = true), searchTasks()">关联已有任务</ElButton>
    </div>
    <Table
      :columns="columns"
      :data="rows"
      :loading="loading"
      :pagination="{ total, pageSize: 100, currentPage: 1 }"
      :show-pagination="false"
    />

    <!-- 项目优先：往这个项目里加一批目标，服务端负责建任务并绑定本项目 -->
    <ElDialog v-model="addVisible" title="向本项目添加目标并创建任务" width="620px">
      <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-bottom: 8px">
        一行一个目标（域名 / IP / CIDR / URL）。任务会自动绑定到当前项目，之后在这个页签里能看到它的全部历史。
      </div>
      <ElInput
        v-model="addTargets"
        type="textarea"
        :rows="8"
        placeholder="example.com&#10;api.example.com&#10;https://staging.example.com"
      />
      <div style="margin-top: 10px">
        <ElCheckbox v-model="addAutoStart">创建后立即开始扫描</ElCheckbox>
      </div>
      <template #footer>
        <ElButton @click="addVisible = false">取消</ElButton>
        <ElButton type="primary" :loading="addSubmitting" @click="submitAdd">创建</ElButton>
      </template>
    </ElDialog>

    <!-- 归属补录：把历史任务挂到当前项目（bindProject 是后加的字段，老任务全空） -->
    <ElDialog v-model="linkVisible" title="关联已有任务到本项目" width="720px">
      <div style="display: flex; gap: 8px; margin-bottom: 10px">
        <ElInput v-model="linkSearch" placeholder="按任务名搜索（留空列出最近 50 条）" @keyup.enter="searchTasks" />
        <ElButton type="primary" :loading="linkLoading" @click="searchTasks">搜索</ElButton>
      </div>
      <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-bottom: 8px">
        勾选后会把任务的项目归属改为当前项目；已经属于别的项目的任务会被改挂过来。
      </div>
      <ElTable :data="linkRows" size="small" height="360" @selection-change="(v) => (linkSelected = v.map((r) => r.id))">
        <ElTableColumn type="selection" width="46" />
        <ElTableColumn prop="name" label="任务名" min-width="320" show-overflow-tooltip />
        <ElTableColumn label="状态" width="90">
          <template #default="{ row }">
            <ElTag size="small">{{ (statusMap[Number(row.status)] || {}).text || row.status }}</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="当前项目" width="200" show-overflow-tooltip>
          <template #default="{ row }">{{ row.bindProject || "未归属" }}</template>
        </ElTableColumn>
      </ElTable>
      <template #footer>
        <ElButton @click="linkVisible = false">取消</ElButton>
        <ElButton type="primary" @click="submitLink">关联所选（{{ linkSelected.length }}）</ElButton>
      </template>
    </ElDialog>

    <ElDialog v-model="detailVisible" title="任务详情" width="720px">
      <ElDescriptions v-if="detail" :column="1" border>
        <ElDescriptionsItem label="任务名">{{ detail.name }}</ElDescriptionsItem>
        <ElDescriptionsItem label="状态">
          {{ (statusMap[Number(detail.status)] || {}).text || detail.status }}
        </ElDescriptionsItem>
        <ElDescriptionsItem label="进度">{{ detail.progress }}%</ElDescriptionsItem>
        <ElDescriptionsItem label="节点">{{ (detail.node || []).join(", ") || "全部" }}</ElDescriptionsItem>
        <ElDescriptionsItem label="目标">
          <pre style="white-space: pre-wrap; margin: 0; max-height: 200px; overflow: auto">{{ detail.target }}</pre>
        </ElDescriptionsItem>
        <ElDescriptionsItem v-if="detail.failReason" label="失败原因">
          {{ detail.failReason }}
        </ElDescriptionsItem>
      </ElDescriptions>
    </ElDialog>
  </div>
</template>

<style scoped></style>
