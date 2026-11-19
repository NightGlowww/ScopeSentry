<script setup lang="tsx">
import { computed, reactive, ref } from 'vue'
import {
  ElButton,
  ElCol,
  ElDescriptions,
  ElDescriptionsItem,
  ElDrawer,
  ElInput,
  ElRow,
  ElTag,
  ElText,
  ElMessage
} from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { getWihDetailApi, getWihListApi } from '@/api/assetReadModel'
import type { WihRow } from '@/api/assetReadModel/types'
import { getSensitiveResultBodyApi } from '@/api/asset'
import { tagColorProps } from '@/utils/tagColor'
import ProjectSelect from './components/ProjectSelect.vue'

// WIH（敏感信息，只读）：type（规则名）/ project / keyword 过滤，
// 行点击进详情；正文不进列表，按 bodyId 走现成的 sensitive/body 接口按需加载。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const typeFilter = ref('')
const project = ref('')
const keyword = ref('')

const columns = reactive<TableColumn[]>([
  {
    field: 'type',
    label: t('wih.type'),
    width: 180,
    formatter: (row: WihRow) => (
      <ElTag
        {...tagColorProps(row.type, row.type && row.color ? { [row.type]: row.color } : null)}
        size="small"
      >
        {row.type || '-'}
      </ElTag>
    )
  },
  { field: 'url', label: t('wih.url'), minWidth: 260 },
  {
    field: 'match',
    label: t('wih.match'),
    minWidth: 240,
    formatter: (row: WihRow) => (row.match ?? []).join(' , ')
  },
  { field: 'project', label: t('wih.project'), minWidth: 140 },
  { field: 'rootDomain', label: t('wih.rootDomain'), minWidth: 160 },
  { field: 'time', label: t('wih.time'), minWidth: 170 }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getWihListApi({
      type: typeFilter.value.trim(),
      project: project.value,
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
  typeFilter.value = ''
  project.value = ''
  keyword.value = ''
  handleSearch()
}

// ---- 详情抽屉 ----
const drawerVisible = ref(false)
const detailLoading = ref(false)
const detail = ref<WihRow | null>(null)
const body = ref('')
const bodyLoaded = ref(false)
const bodyLoading = ref(false)

const openDetail = async (row: WihRow) => {
  drawerVisible.value = true
  detail.value = row
  body.value = ''
  bodyLoaded.value = false
  detailLoading.value = true
  try {
    const res = await getWihDetailApi(row.id)
    if (res?.data) detail.value = res.data
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    detailLoading.value = false
  }
}

const loadBody = async () => {
  const bodyId = detail.value?.bodyId
  if (!bodyId) return
  bodyLoading.value = true
  try {
    const res = await getSensitiveResultBodyApi(bodyId)
    body.value = res?.data?.body ?? ''
    bodyLoaded.value = true
  } finally {
    bodyLoading.value = false
  }
}

const onRowClick = (row: WihRow) => {
  openDetail(row)
}

const rowStyle = () => ({ cursor: 'pointer' })

// 详情里的规则标签：模板里不能写 {...obj}，先算成对象再 v-bind。
const detailTypeTagProps = computed(() => {
  const current = detail.value
  if (!current?.type) return { type: 'info' as const }
  return tagColorProps(current.type, current.color ? { [current.type]: current.color } : null)
})
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="4">
        <ElInput
          v-model="typeFilter"
          :placeholder="t('wih.typePlaceholder')"
          clearable
          @keyup.enter="handleSearch"
        />
      </ElCol>
      <ElCol :span="4">
        <ProjectSelect v-model="project" :placeholder="t('wih.project')" />
      </ElCol>
      <ElCol :span="5">
        <ElInput
          v-model="keyword"
          :placeholder="t('wih.keywordPlaceholder')"
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
        <div class="empty-state">{{ t('wih.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>

  <ElDrawer v-model="drawerVisible" :title="t('wih.detailTitle')" size="70%" direction="rtl">
    <div v-loading="detailLoading" class="detail-body">
      <template v-if="detail">
        <ElDescriptions :border="true" :column="2" class="detail-descriptions">
          <ElDescriptionsItem :label="t('wih.type')">
            <ElTag v-bind="detailTypeTagProps" size="small">
              {{ detail.type }}
            </ElTag>
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('wih.project')">
            {{ detail.project }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('wih.url')" :span="2">
            {{ detail.url }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('wih.host')">
            {{ detail.host }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('wih.rootDomain')">
            {{ detail.rootDomain }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('wih.taskName')">
            {{ detail.taskName }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('wih.time')">
            {{ detail.time }}
          </ElDescriptionsItem>
          <ElDescriptionsItem :label="t('wih.tags')" :span="2">
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
          <ElDescriptionsItem :label="t('wih.match')" :span="2">
            <div class="match-list">
              <ElTag
                v-for="(item, index) in detail.match ?? []"
                :key="`${item}-${index}`"
                size="small"
                type="danger"
                class="match-item"
              >
                {{ item }}
              </ElTag>
            </div>
          </ElDescriptionsItem>
        </ElDescriptions>

        <div class="detail-block">
          <div class="detail-block__header">
            <span class="detail-block__title">{{ t('wih.body') }}</span>
            <ElButton
              v-if="detail.bodyId && !bodyLoaded"
              size="small"
              :loading="bodyLoading"
              @click="loadBody"
            >
              {{ t('wih.loadBody') }}
            </ElButton>
          </div>
          <pre v-if="bodyLoaded" class="detail-block__code">{{ body }}</pre>
          <ElText v-else-if="!detail.bodyId" type="info">{{ t('wih.bodyEmpty') }}</ElText>
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

.match-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.match-item {
  max-width: 100%;
  height: auto;
  white-space: normal;
  word-break: break-all;
}

.detail-block {
  margin-bottom: 16px;
}

.detail-block__header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.detail-block__title {
  font-weight: 600;
}

.detail-block__code {
  margin: 0;
  padding: 8px 12px;
  max-height: 420px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.5;
}
</style>
