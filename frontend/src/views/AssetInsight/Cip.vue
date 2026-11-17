<script setup lang="tsx">
import { reactive, ref } from 'vue'
import {
  ElButton,
  ElCol,
  ElDescriptions,
  ElDescriptionsItem,
  ElDrawer,
  ElInput,
  ElLink,
  ElPagination,
  ElRow,
  ElTag
} from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { getCipDetailApi, getCipListApi } from '@/api/cip'
import type { CipDetailResult, CipDomain, CipRow } from '@/api/cip/types'
import { dateText } from '@/utils/timeText'
import ProjectSelect from './components/ProjectSelect.vue'

// ICP / 主体读模型（只读）：从 RootDomain 的 company/icp 字段按主体分组现算。
// 行里的 samples 是样例根域名，详情需要拿其中一个根域名反查同一主体下的全部域名。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const keyword = ref('')
const project = ref('')
const company = ref('')

const columns = reactive<TableColumn[]>([
  {
    field: 'company',
    label: t('cip.company'),
    minWidth: 240,
    formatter: (row: CipRow) => (
      <ElLink type="primary" onClick={() => openDetail(row)}>
        {row.company || t('cip.companyUnknown')}
      </ElLink>
    )
  },
  {
    field: 'icp',
    label: t('cip.icp'),
    minWidth: 220,
    formatter: (row: CipRow) => {
      const list = row.icps?.length ? row.icps : row.icp ? [row.icp] : []
      return list.length ? list.join(' , ') : '-'
    }
  },
  { field: 'domainCount', label: t('cip.domainCount'), width: 120 },
  {
    field: 'samples',
    label: t('cip.samples'),
    minWidth: 260,
    formatter: (row: CipRow) => (row.samples ?? []).join(' , ') || '-'
  },
  {
    field: 'lastTime',
    label: t('cip.lastTime'),
    minWidth: 150,
    formatter: (row: CipRow) => dateText(row.lastTime)
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getCipListApi({
      keyword: keyword.value.trim(),
      project: project.value,
      company: company.value.trim(),
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
  keyword.value = ''
  project.value = ''
  company.value = ''
  handleSearch()
}

// ---- 详情抽屉：以主体下的第一个样例域名为入口 ----
const drawerVisible = ref(false)
const detailLoading = ref(false)
const detail = ref<CipDetailResult | null>(null)
const entryDomain = ref('')
const domainPage = ref(1)
const domainPageSize = ref(20)

const domainColumns = reactive<TableColumn[]>([
  { field: 'domain', label: t('cip.domain'), minWidth: 260 },
  { field: 'icp', label: t('cip.icp'), minWidth: 220 },
  { field: 'project', label: t('cip.project'), minWidth: 150 },
  {
    field: 'tags',
    label: t('cip.tags'),
    minWidth: 160,
    formatter: (row: CipDomain) => (row.tags ?? []).join(' , ') || '-'
  },
  {
    field: 'time',
    label: t('cip.time'),
    minWidth: 150,
    formatter: (row: CipDomain) => dateText(row.time)
  }
])

const loadDetail = async () => {
  if (!entryDomain.value) {
    detail.value = null
    return
  }
  detailLoading.value = true
  try {
    const res = await getCipDetailApi({
      domain: entryDomain.value,
      project: project.value,
      pageIndex: domainPage.value,
      pageSize: domainPageSize.value
    })
    detail.value = res?.data ?? null
  } catch {
    detail.value = null
  } finally {
    detailLoading.value = false
  }
}

const openDetail = (row: CipRow) => {
  drawerVisible.value = true
  entryDomain.value = row.samples?.[0] ?? ''
  domainPage.value = 1
  loadDetail()
}

const onDomainPageChange = (page: number) => {
  domainPage.value = page
  loadDetail()
}

const onDomainSizeChange = (size: number) => {
  domainPageSize.value = size
  domainPage.value = 1
  loadDetail()
}
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="5">
        <ElInput
          v-model="keyword"
          :placeholder="t('cip.keywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="4">
        <ProjectSelect v-model="project" :placeholder="t('cip.project')" />
      </ElCol>
      <ElCol :span="4">
        <ElInput
          v-model="company"
          :placeholder="t('cip.companyPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="8">
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
      :pagination="{ total, pageSizes: [10, 20, 50, 100, 200] }"
      @register="tableRegister"
    >
      <template #empty>
        <div class="empty-state">{{ t('cip.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>

  <ElDrawer v-model="drawerVisible" :title="t('cip.detailTitle')" size="65%" direction="rtl">
    <div v-loading="detailLoading">
      <ElDescriptions v-if="detail" :column="2" border>
        <ElDescriptionsItem :label="t('cip.company')">
          {{ detail.company || t('cip.companyUnknown') }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cip.entryDomain')">
          {{ detail.domain || '-' }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cip.icp')" :span="2">
          <ElTag v-for="item in detail.icps ?? []" :key="item" size="small" class="icp-tag">
            {{ item }}
          </ElTag>
          <span v-if="!(detail.icps ?? []).length">{{ detail.icp || '-' }}</span>
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cip.domainCount')">
          {{ detail.total }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cip.project')">
          {{ project || t('cip.allProjects') }}
        </ElDescriptionsItem>
      </ElDescriptions>

      <div v-if="!entryDomain" class="empty-state detail-empty">
        {{ t('cip.detailNoDomain') }}
      </div>
      <template v-else>
        <div class="domains-title">{{ t('cip.domainsTitle') }}</div>
        <Table
          :columns="domainColumns"
          :data="detail?.list ?? []"
          stripe
          :border="true"
          :loading="detailLoading"
          :resizable="true"
        >
          <template #empty>
            <div class="empty-state">{{ t('cip.domainsEmpty') }}</div>
          </template>
        </Table>
        <ElPagination
          class="mt-3"
          layout="sizes, prev, pager, next, total"
          :page-sizes="[10, 20, 50, 100, 200]"
          :total="detail?.total ?? 0"
          :page-size="domainPageSize"
          :current-page="domainPage"
          @current-change="onDomainPageChange"
          @size-change="onDomainSizeChange"
        />
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

.detail-empty {
  padding: 40px 8px;
}

.icp-tag {
  margin: 0 6px 6px 0;
}

.domains-title {
  margin: 20px 0 10px;
  font-weight: 600;
}
</style>
