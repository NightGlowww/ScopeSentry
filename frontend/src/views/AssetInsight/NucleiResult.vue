<script setup lang="tsx">
import { reactive, ref } from 'vue'
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
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { getNucleiResultDetailApi, getNucleiResultListApi } from '@/api/assetReadModel'
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
const templateId = ref('')
const keyword = ref('')

const columns = reactive<TableColumn[]>([
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
  { field: 'matchedAt', label: t('nucleiResult.matchedAt'), minWidth: 170 }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getNucleiResultListApi({
      project: project.value,
      severity: severity.value,
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
  handleSearch()
}

// ---- 详情抽屉 ----
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

        <div class="detail-block">
          <div class="detail-block__title">{{ t('nucleiResult.request') }}</div>
          <pre v-if="detail.request" class="detail-block__code">{{ detail.request }}</pre>
          <ElText v-else type="info">{{ t('nucleiResult.noRequest') }}</ElText>
        </div>

        <div class="detail-block">
          <div class="detail-block__title">{{ t('nucleiResult.response') }}</div>
          <pre v-if="detail.response" class="detail-block__code">{{ detail.response }}</pre>
          <ElText v-else type="info">{{ t('nucleiResult.noResponse') }}</ElText>
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
