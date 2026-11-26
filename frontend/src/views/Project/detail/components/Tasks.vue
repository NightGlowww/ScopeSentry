<script setup lang="tsx">
import { onMounted, ref } from 'vue'
import { useI18n } from '@/hooks/web/useI18n'
import { useRoute } from 'vue-router'
import {
  ElAlert,
  ElButton,
  ElDescriptions,
  ElDescriptionsItem,
  ElDialog,
  ElTag
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { getTaskDataApi, getTaskDetailApi } from '@/api/task'

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
    width: 110,
    formatter: (row: any) => (
      <ElButton link type="primary" onClick={() => openDetail(row.id)}>
        详情
      </ElButton>
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
    <div style="margin-bottom: 8px; color: var(--el-text-color-secondary); font-size: 12px">
      共 {{ total }} 个任务（按 bindProject 关联）
      <ElButton link type="primary" style="margin-left: 8px" @click="load">刷新</ElButton>
    </div>
    <Table
      :columns="columns"
      :data="rows"
      :loading="loading"
      :pagination="{ total, pageSize: 100, currentPage: 1 }"
      :show-pagination="false"
    />

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
