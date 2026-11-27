<script setup lang="tsx">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElAlert, ElButton, ElTag } from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { getProjectVulDataApi } from '@/api/ProjectAggregation'

// 项目详情「漏洞」页签。
// 之前项目页只有 子域名/端口/服务/任务，漏洞只在「概况」里画了个环形图——
// 一个项目有没有洞、有哪些洞、在哪个 URL 上，页面回答不了（2026-11-27 反馈）。
const { query } = useRoute()
const router = useRouter()
const projectId = String(query.id || "")

const loading = ref(false)
const rows = ref<any[]>([])
const total = ref(0)
const loadError = ref("")

const levelMap: Record<string, { text: string; type: any }> = {
  critical: { text: "严重", type: "danger" },
  high: { text: "高危", type: "danger" },
  medium: { text: "中危", type: "warning" },
  low: { text: "低危", type: "info" },
  info: { text: "信息", type: "info" }
}

const load = async () => {
  if (!projectId) return
  loading.value = true
  loadError.value = ""
  try {
    const res = await getProjectVulDataApi(projectId, { pageIndex: 1, pageSize: 200 })
    rows.value = res.data?.list || []
    total.value = res.data?.total || rows.value.length
  } catch (e: any) {
    loadError.value = e?.message || "加载失败"
  } finally {
    loading.value = false
  }
}
onMounted(load)

const openInVulnPage = (row: any) => {
  let host = String(row.url || "")
  try {
    host = new URL(host).host
  } catch {
    host = host.replace(/^https?:\/\//, "").split("/")[0]
  }
  if (!host) return
  router.push({ path: "/asset-information/nuclei-result", query: { host } })
}

const columns: TableColumn[] = [
  {
    field: "level",
    label: "等级",
    width: 90,
    formatter: (_r: any, _c: any, v: string) => {
      const m = levelMap[String(v || "").toLowerCase()] || { text: v || "-", type: "info" }
      return <ElTag type={m.type} effect="light">{m.text}</ElTag>
    }
  },
  { field: "vulname", label: "漏洞", minWidth: 320, showOverflowTooltip: true },
  { field: "url", label: "URL", minWidth: 300, showOverflowTooltip: true },
  { field: "matched", label: "命中位置", minWidth: 260, showOverflowTooltip: true },
  { field: "time", label: "发现时间", width: 170 },
  {
    field: "action",
    label: "操作",
    width: 150,
    formatter: (row: any) => (
      <ElButton link type="primary" onClick={() => openInVulnPage(row)}>
        在漏洞结果中查看
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
      :title="`漏洞列表加载失败：${loadError}`"
      style="margin-bottom: 12px"
    />
    <ElAlert
      v-else-if="!loading && total === 0"
      type="info"
      :closable="false"
      title="该项目范围内暂无漏洞记录。漏洞来自扫描任务写入的结果，命中项目根域名范围内的资产才会出现在这里。"
      style="margin-bottom: 12px"
    />
    <div style="margin-bottom: 8px; display: flex; align-items: center; gap: 8px">
      <span style="color: var(--el-text-color-secondary); font-size: 12px">共 {{ total }} 条</span>
      <ElButton link type="primary" @click="load">刷新</ElButton>
    </div>
    <Table
      :columns="columns"
      :data="rows"
      :loading="loading"
      :pagination="{ total, pageSize: 200, currentPage: 1 }"
      :show-pagination="false"
    />
  </div>
</template>
