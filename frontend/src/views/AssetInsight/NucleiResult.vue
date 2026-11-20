<script setup lang="tsx">
import { reactive, ref, h, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ElButton,
  ElCol,
  ElDescriptions,
  ElDescriptionsItem,
  ElDrawer,
  ElInput,
  ElOption,
  ElRow,
  ElSelect,
  ElTag,
  ElText
} from 'element-plus'
import { ElMessage } from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { updateNucleiStatusApi, batchUpdateNucleiStatusApi } from '@/api/assetReadModel'
import { getNucleiResultDetailApi, getNucleiResultListApi, getNucleiStatusStatsApi } from '@/api/assetReadModel'
import type { NucleiResultDetail, NucleiResultItem } from '@/api/assetReadModel/types'
import { normalizeSeverity, SEVERITY_LEVELS, severityTagType } from '@/utils/severity'
import type { SeverityLevel } from '@/utils/severity'
import ProjectSelect from './components/ProjectSelect.vue'

// Nuclei 结果（只读）：project / severity / templateId / keyword 过滤，
// 行点击打开抽屉看请求/响应原文。数据来自扫描任务里的 Nuclei 模板命中。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const LEVEL_LABEL_KEYS: Record<SeverityLevel, string> = {
  critical: 'nucleiResult.levelCritical',
  high: 'nucleiResult.levelHigh',
  medium: 'nucleiResult.levelMedium',
  low: 'nucleiResult.levelLow',
  info: 'nucleiResult.levelInfo',
  unknown: 'nucleiResult.levelUnknown'
}

const levelLabel = (value?: string) => t(LEVEL_LABEL_KEYS[normalizeSeverity(value)])

const project = ref('')
const severity = ref('')
// 处置状态过滤：0=全部
const statusFilter = ref(0)
// 处置进度：顶部展示各状态条数，让「还有多少没处理」一眼可见
const statusStats = ref<Record<string, number>>({})
const totalVuln = computed(() =>
  Object.values(statusStats.value || {}).reduce((a: number, b: any) => a + Number(b || 0), 0)
)
// 从任务跳过来后要能回去：带上任务名，任务页会自动搜索
// 从 URL 里取主机名，用于跳「这个 host 的全部资产」
const detailHost = computed(() => {
  const u = (detail.value as any)?.url || ''
  if (!u) return ''
  try {
    return new URL(u.startsWith('http') ? u : 'http://' + u).hostname
  } catch {
    return u.split('/')[0]
  }
})
const goHostAssets = () => {
  if (!detailHost.value) return
  router.push({
    path: '/asset-information/index',
    query: { tab: 'asset', search: 'domain=="' + detailHost.value + '"' }
  })
  drawerVisible.value = false
}

const goTask = () => {
  router.push({ path: '/task-management/ScanTask', query: { search: taskNameFilter.value } })
}

const loadStatusStats = async () => {
  try {
    const res: any = await getNucleiStatusStatsApi({
      project: project.value,
      severity: severity.value,
      taskName: taskNameFilter.value.trim(),
      status: 0
    })
    statusStats.value = res?.data || {}
  } catch {
    // 统计失败不影响列表主流程
  }
}
// 按任务筛选：一个任务扫完想直接看它的产出
// 从任务详情跳转过来时，用 query.taskName 预填筛选（闭环：任务 → 它的漏洞）
const route = useRoute()
const router = useRouter()
const taskNameFilter = ref((route.query.taskName as string) || '')
const templateId = ref('')
const keyword = ref('')

const columns = reactive<TableColumn[]>([
  { field: 'selection', type: 'selection', width: 44 },
  {
    field: 'severity',
    label: t('nucleiResult.severity'),
    width: 110,
    formatter: (row: NucleiResultItem) => (
      <ElTag type={severityTagType(row.severity)} effect="light" size="small">
        {levelLabel(row.severity)}
      </ElTag>
    )
  },
  { field: 'templateId', label: t('nucleiResult.templateId'), minWidth: 200 },
  { field: 'name', label: t('nucleiResult.templateName'), minWidth: 180 },
  { field: 'url', label: t('nucleiResult.url'), minWidth: 260 },
  { field: 'project', label: t('nucleiResult.project'), minWidth: 140 },
  {
    field: 'status',
    label: t('nucleiResult.status'),
    width: 110,
    formatter: (_: any, __: any, v: number) => {
      const map: Record<number, { type: any; key: string }> = {
        1: { type: 'info', key: 'nucleiResult.statusNew' },
        2: { type: 'warning', key: 'nucleiResult.statusDoing' },
        3: { type: 'info', key: 'nucleiResult.statusIgnored' },
        4: { type: 'warning', key: 'nucleiResult.statusSuspect' },
        5: { type: 'danger', key: 'nucleiResult.statusConfirmed' },
        6: { type: 'success', key: 'nucleiResult.statusDone' }
      }
      const m = map[v] || map[1]
      return h(ElTag, { type: m.type }, () => t(m.key))
    }
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    width: 180,
    formatter: (row: any) => {
      return h('div', { style: 'display:flex;gap:6px' }, [
        h(
          ElButton,
          { size: 'small', type: 'danger', link: true, onClick: () => setStatus(row.id, 5) },
          () => t('nucleiResult.markConfirmed')
        ),
        h(
          ElButton,
          { size: 'small', type: 'info', link: true, onClick: () => setStatus(row.id, 3) },
          () => t('nucleiResult.markFalsePositive')
        )
      ])
    }
  },
  { field: 'matchedAt', label: t('nucleiResult.matchedAt'), minWidth: 170 }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    // 每次刷新列表顺带更新处置进度（统计失败不影响列表）
    loadStatusStats()
    const { currentPage, pageSize } = tableState
    const res = await getNucleiResultListApi({
      project: project.value,
      severity: severity.value,
      status: statusFilter.value || 0,
      taskName: taskNameFilter.value.trim(),
      templateId: templateId.value.trim(),
      keyword: keyword.value.trim(),
      pageIndex: currentPage.value,
      pageSize: pageSize.value
    })
    return { list: res?.data?.list ?? [], total: res?.data?.total ?? 0 }
  }
})

const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList } = tableMethods

const handleSearch = () => {
  // currentPage 变化时 useTable 会自己拉一次，只有已经在第一页时才手动拉，避免重复请求
  if (currentPage.value !== 1) {
    currentPage.value = 1
    return
  }
  getList()
}

const handleReset = () => {
  project.value = ''
  severity.value = ''
  templateId.value = ''
  keyword.value = ''
  statusFilter.value = 0
  taskNameFilter.value = ''
  handleSearch()
}

// 单条处置：确认存在(5) / 标记误报(3)
const setStatus = async (id: string, status: number) => {
  if (!id) return
  try {
    await updateNucleiStatusApi(id, status)
    ElMessage.success(t('common.success'))
    getList()
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  }
}

// 批量处置：勾选多条一次改状态
const selectedRows = ref<any[]>([])
const batchLoading = ref(false)
const handleSelectionChange = (rows: any[]) => {
  selectedRows.value = rows || []
}
const batchSetStatus = async (status: number) => {
  if (selectedRows.value.length === 0) {
    ElMessage.warning(t('nucleiResult.selectFirst'))
    return
  }
  batchLoading.value = true
  try {
    const ids = selectedRows.value.map((r) => r.id)
    await batchUpdateNucleiStatusApi(ids, status)
    ElMessage.success(t('nucleiResult.batchDone', { n: ids.length }))
    selectedRows.value = []
    getList()
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    batchLoading.value = false
  }
}

// ---- 详情抽屉 ----
// 大报文保护 + 一键复制：详情页内容常被贴进报告，
// 但有的响应体几百 KB，直接渲染会把抽屉卡死（2026-11-20）。
const MAX_PREVIEW = 200 * 1024
const clipText = (t?: string) => {
  if (!t) return ''
  return t.length > MAX_PREVIEW ? t.slice(0, MAX_PREVIEW) : t
}
const isClipped = (t?: string) => !!t && t.length > MAX_PREVIEW
const copyBlock = async (text?: string) => {
  if (!text) return
  // 优先用异步剪贴板；不可用（非安全上下文 / 无用户激活）时退回 textarea + execCommand，
  // 否则在没有 focus 的后台标签页里会直接报「复制失败」（2026-11-20 实测）。
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      ElMessage.success(t('common.copySuccess'))
      return
    }
  } catch {
    // 落到下面的兜底
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    ok ? ElMessage.success(t('common.copySuccess')) : ElMessage.error(t('common.copyFailed'))
  } catch {
    ElMessage.error(t('common.copyFailed'))
  }
}
const drawerVisible = ref(false)
const detailLoading = ref(false)
const detail = ref<NucleiResultDetail | null>(null)

const openDetail = async (row: NucleiResultItem) => {
  drawerVisible.value = true
  detail.value = null
  detailLoading.value = true
  try {
    const res = await getNucleiResultDetailApi(row.id)
    detail.value = res?.data ?? null
  } finally {
    detailLoading.value = false
  }
}

const onRowClick = (row: NucleiResultItem) => {
  openDetail(row)
}

const rowStyle = () => ({ cursor: 'pointer' })
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="4">
        <ProjectSelect v-model="project" :placeholder="t('nucleiResult.project')" />
      </ElCol>
      <ElCol :span="3">
        <ElSelect v-model="severity" :placeholder="t('nucleiResult.severity')" clearable>
          <ElOption :label="t('nucleiResult.allSeverity')" value="" />
          <ElOption
            v-for="level in SEVERITY_LEVELS"
            :key="level"
            :label="t(LEVEL_LABEL_KEYS[level])"
            :value="level"
          />
        </ElSelect>
      </ElCol>
      <ElCol :span="3">
        <ElSelect v-model="statusFilter" :placeholder="t('nucleiResult.status')" clearable>
          <ElOption :label="t('nucleiResult.statusAll')" :value="0" />
          <ElOption :label="t('nucleiResult.statusNew')" :value="1" />
          <ElOption :label="t('nucleiResult.statusIgnored')" :value="3" />
          <ElOption :label="t('nucleiResult.statusConfirmed')" :value="5" />
        </ElSelect>
      </ElCol>
      <ElCol :span="4">
        <ElInput
          v-model="templateId"
          :placeholder="t('nucleiResult.templatePlaceholder')"
          clearable
          @keyup.enter="handleSearch"
        />
      </ElCol>
      <ElCol :span="4">
        <ElInput
          v-model="taskNameFilter"
          :placeholder="t('nucleiResult.filterByTask')"
          clearable
          @keyup.enter="handleSearch"
        />
      </ElCol>
      <ElCol :span="4">
        <ElInput
          v-model="keyword"
          :placeholder="t('nucleiResult.keywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
        />
      </ElCol>
      <ElCol :span="9">
        <ElButton type="primary" :icon="searchIcon" @click="handleSearch">
          {{ t('common.search') }}
        </ElButton>
        <ElButton @click="handleReset">{{ t('common.reset') }}</ElButton>
      </ElCol>
    </ElRow>

    <div v-if="taskNameFilter" class="task-link-bar">
      <span class="task-link-label">{{ t('nucleiResult.filteredByTask') }}</span>
      <ElTag type="primary" effect="plain" size="small">{{ taskNameFilter }}</ElTag>
      <ElButton link type="primary" size="small" @click="goTask">{{ t('nucleiResult.backToTask') }}</ElButton>
    </div>

    <div class="triage-bar">
      <span class="triage-total">{{ t('nucleiResult.totalVuln', { n: totalVuln }) }}</span>
      <ElTag v-if="statusStats['1']" type="info" size="small">
        {{ t('nucleiResult.statusNew') }} {{ statusStats['1'] }}
      </ElTag>
      <ElTag v-if="statusStats['3']" type="info" size="small" effect="plain">
        {{ t('nucleiResult.statusIgnored') }} {{ statusStats['3'] }}
      </ElTag>
      <ElTag v-if="statusStats['5']" type="danger" size="small">
        {{ t('nucleiResult.statusConfirmed') }} {{ statusStats['5'] }}
      </ElTag>
    </div>

    <div class="batch-bar">
      <ElButton :loading="batchLoading" type="danger" link @click="batchSetStatus(5)">
        {{ t('nucleiResult.batchConfirm') }}
      </ElButton>
      <ElButton :loading="batchLoading" type="info" link @click="batchSetStatus(3)">
        {{ t('nucleiResult.batchFalsePositive') }}
      </ElButton>
      <span class="batch-count">{{ selectedRows.length }}</span>
    </div>
    <Table
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      :columns="columns"
      :data="dataList"
      stripe
      :border="true"
      :loading="loading"
      :resizable="true"
      :row-style="rowStyle"
      :pagination="{ total, pageSizes: [10, 20, 50, 100, 200] }"
      @register="tableRegister"
      @selection-change="handleSelectionChange"
      @row-click="onRowClick"
    >
      <template #empty>
        <div class="empty-state">{{ t('nucleiResult.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>

  <ElDrawer
    v-model="drawerVisible"
    :title="t('nucleiResult.detailTitle')"
    size="70%"
    direction="rtl"
  >
    <div v-loading="detailLoading" class="detail-body">
      <template v-if="detail">
        <ElDescriptions :border="true" :column="2" class="detail-descriptions">
          <ElDescriptionsItem :label="t('nucleiResult.severity')">
            <ElTag :type="severityTagType(detail.severity)" effect="light" size="small">
              {{ levelLabel(detail.severity) }}
            </ElTag>
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('nucleiResult.templateId')">
            {{ detail.templateId }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('nucleiResult.templateName')">
            {{ detail.name }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('nucleiResult.project')">
            {{ detail.project }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('nucleiResult.url')" :span="2">
            {{ detail.url }}
            <ElButton
              v-if="detailHost"
              link
              type="primary"
              size="small"
              style="margin-left: 8px"
              @click="goHostAssets"
            >
              {{ t('nucleiResult.viewHostAssets') }}
            </ElButton>
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('nucleiResult.rootDomain')">
            {{ detail.rootDomain }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('nucleiResult.taskName')">
            {{ detail.taskName }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('nucleiResult.matched')" :span="2">
            {{ detail.matched }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('nucleiResult.matchedAt')">
            {{ detail.matchedAt }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('nucleiResult.tags')">
            <ElTag
              v-for="tag in detail.tags ?? []"
              :key="tag"
              size="small"
              type="info"
              style="margin-right: 4px"
            >
              {{ tag }}
            </ElTag>
          </ElDescriptionsItem>
        </ElDescriptions>

        <div v-if="detail.templateContent" class="detail-block">
          <div class="detail-block__head">
            <span class="detail-block__title">{{ t('nucleiResult.pocTemplate') }}</span>
            <ElButton link type="primary" size="small" @click="copyBlock(detail.templateContent)">
              {{ t('common.copy') }}
            </ElButton>
          </div>
          <pre class="detail-block__code">{{ clipText(detail.templateContent) }}</pre>
          <ElText v-if="isClipped(detail.templateContent)" type="warning" size="small">
            {{ t('nucleiResult.truncatedHint') }}
          </ElText>
        </div>

        <div class="detail-block">
          <div class="detail-block__head">
            <span class="detail-block__title">{{ t('nucleiResult.request') }}</span>
            <ElButton
              v-if="detail.request"
              link
              type="primary"
              size="small"
              @click="copyBlock(detail.request)"
            >
              {{ t('common.copy') }}
            </ElButton>
          </div>
          <pre v-if="detail.request" class="detail-block__code">{{ clipText(detail.request) }}</pre>
          <ElText v-else type="info">{{ t('nucleiResult.noRequest') }}</ElText>
          <ElText v-if="isClipped(detail.request)" type="warning" size="small">
            {{ t('nucleiResult.truncatedHint') }}
          </ElText>
        </div>

        <div class="detail-block">
          <div class="detail-block__head">
            <span class="detail-block__title">{{ t('nucleiResult.response') }}</span>
            <ElButton
              v-if="detail.response"
              link
              type="primary"
              size="small"
              @click="copyBlock(detail.response)"
            >
              {{ t('common.copy') }}
            </ElButton>
          </div>
          <pre v-if="detail.response" class="detail-block__code">{{ clipText(detail.response) }}</pre>
          <ElText v-else type="info">{{ t('nucleiResult.noResponse') }}</ElText>
          <ElText v-if="isClipped(detail.response)" type="warning" size="small">
            {{ t('nucleiResult.truncatedHint') }}
          </ElText>
        </div>
      </template>
    </div>
  </ElDrawer>
</template>

<style scoped>
.filter-row {
  margin-bottom: 15px;
}

.empty-state {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}

.detail-body {
  min-height: 200px;
}

.detail-descriptions {
  margin-bottom: 16px;
  word-break: break-all;
}

.detail-block {
  margin-bottom: 16px;
}

.task-link-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 13px;
}
.task-link-label {
  color: var(--el-text-color-secondary);
}

.triage-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.triage-total {
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.batch-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.batch-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.detail-block__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.detail-block__title {
  font-weight: 600;
  margin-bottom: 6px;
}

.detail-block__code {
  margin: 0;
  padding: 8px 12px;
  max-height: 360px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.5;
}
</style>
