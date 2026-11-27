<script setup lang="tsx">
import { onMounted, ref } from 'vue'
import { useI18n } from '@/hooks/web/useI18n'
import { useRoute, useRouter } from 'vue-router'
import { ElAlert, ElButton, ElTag } from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { getProjectVulDataApi } from '@/api/ProjectAggregation'

// 项目详情「漏洞」页签。
// 之前项目页只有 子域名/端口/服务/任务，漏洞只在「概况」里画了个环形图——
// 一个项目有没有洞、有哪些洞、在哪个 URL 上，页面回答不了（2026-11-27 反馈）。
const { t } = useI18n('projectDetail')
const { query } = useRoute()
const router = useRouter()
const projectId = String(query.id || '')

const loading = ref(false)
const rows = ref<any[]>([])
const total = ref(0)
const loadError = ref('')

const levelMap: Record<string, { key: string; type: any }> = {
  critical: { key: 'levelCritical', type: 'danger' },
  high: { key: 'levelHigh', type: 'danger' },
  medium: { key: 'levelMedium', type: 'warning' },
  low: { key: 'levelLow', type: 'info' },
  info: { key: 'levelInfo', type: 'info' }
}

const levelLabel = (val: unknown) => {
  const m = levelMap[String(val || '').toLowerCase()]
  return m ? t(m.key) : ''
}
const levelTagType = (val: unknown) => levelMap[String(val || '').toLowerCase()]?.type || 'info'

const load = async () => {
  if (!projectId) return
  loading.value = true
  loadError.value = ''
  try {
    const res = await getProjectVulDataApi(projectId, { pageIndex: 1, pageSize: 200 })
    rows.value = res.data?.list || []
    total.value = res.data?.total || rows.value.length
  } catch (e: any) {
    loadError.value = e?.message || t('loadFailed')
  } finally {
    loading.value = false
  }
}
onMounted(load)

const openInVulnPage = (row: any) => {
  let host = String(row.url || '')
  try {
    host = new URL(host).host
  } catch {
    host = host.replace(/^https?:\/\//, '').split('/')[0]
  }
  if (!host) return
  router.push({ path: '/asset-information/nuclei-result', query: { host } })
}

const columns: TableColumn[] = [
  {
    field: 'level',
    label: t('level'),
    width: 90,
    formatter: (_r: any, _c: any, v: string) => (
      <ElTag type={levelTagType(v)} effect="light">
        {levelLabel(v) || v || '-'}
      </ElTag>
    )
  },
  { field: 'vulname', label: t('vulname'), minWidth: 320, showOverflowTooltip: true },
  { field: 'url', label: t('url'), minWidth: 300, showOverflowTooltip: true },
  { field: 'matched', label: t('matched'), minWidth: 260, showOverflowTooltip: true },
  { field: 'time', label: t('time'), width: 170 },
  {
    field: 'action',
    label: t('action'),
    width: 150,
    formatter: (row: any) => (
      <ElButton link type="primary" onClick={() => openInVulnPage(row)}>
        {t('viewInVulnResult')}
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
      :title="t('vulnsLoadFailedAlert', { message: loadError })"
      style="margin-bottom: 12px"
    />
    <ElAlert
      v-else-if="!loading && total === 0"
      type="info"
      :closable="false"
      :title="t('vulnsEmptyHint')"
      style="margin-bottom: 12px"
    />
    <div style="margin-bottom: 8px; display: flex; align-items: center; gap: 8px">
      <span style="color: var(--el-text-color-secondary); font-size: 12px">{{
        t('totalCount', { n: total })
      }}</span>
      <ElButton link type="primary" @click="load">{{ t('refresh') }}</ElButton>
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
