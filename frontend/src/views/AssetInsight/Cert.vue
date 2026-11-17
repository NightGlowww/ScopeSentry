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
  ElOption,
  ElRow,
  ElSelect,
  ElTag
} from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { getCertDetailApi, getCertListApi } from '@/api/cert'
import type { CertRow } from '@/api/cert/types'
import { timeText } from '@/utils/timeText'
import ProjectSelect from './components/ProjectSelect.vue'

// 证书读模型（只读）：从 asset 的 TLS 字段现算，同一张证书（指纹或 serial+issuer）
// 在多台资产上出现时合并成一行；hostCount 是完整关联数，hosts 只展示前 50 条。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const keyword = ref('')
const project = ref('')
// 三态过滤：'' 全部，'true' 只看已过期，'false' 只看未过期
const expired = ref('')

const columns = reactive<TableColumn[]>([
  {
    field: 'subject',
    label: t('cert.subject'),
    minWidth: 240,
    formatter: (row: CertRow) => (
      <ElLink type="primary" onClick={() => openDetail(row)}>
        {row.subjectCN || row.subject || '-'}
      </ElLink>
    )
  },
  {
    field: 'issuer',
    label: t('cert.issuer'),
    minWidth: 200,
    formatter: (row: CertRow) => row.issuerCN || row.issuer || '-'
  },
  {
    field: 'notAfter',
    label: t('cert.notAfter'),
    minWidth: 190,
    formatter: (row: CertRow) => (
      <span class="expire-cell">
        <span>{timeText(row.notAfter)}</span>
        {row.expired ? (
          <ElTag type="danger" size="small">
            {t('cert.expired')}
          </ElTag>
        ) : null}
      </span>
    )
  },
  { field: 'hostCount', label: t('cert.hostCount'), width: 110 },
  {
    field: 'san',
    label: t('cert.san'),
    minWidth: 220,
    formatter: (row: CertRow) => (row.san ?? []).slice(0, 3).join(' , ') || '-'
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    // 清空下拉时 v-model 会变成 undefined，不能当成「未过期」传下去
    const expiredParam =
      expired.value === 'true' ? true : expired.value === 'false' ? false : undefined
    const res = await getCertListApi({
      keyword: keyword.value.trim(),
      project: project.value,
      expired: expiredParam,
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
  expired.value = ''
  handleSearch()
}

// ---- 详情抽屉 ----
const hostColumns = reactive<TableColumn[]>([
  { field: 'host', label: t('cert.host'), minWidth: 220 },
  { field: 'ip', label: t('cert.ip'), minWidth: 150 },
  { field: 'port', label: t('cert.port'), width: 110 }
])

const drawerVisible = ref(false)
const detailLoading = ref(false)
const detail = ref<CertRow | null>(null)

const openDetail = async (row: CertRow) => {
  drawerVisible.value = true
  detail.value = row
  detailLoading.value = true
  try {
    const res = await getCertDetailApi({ id: row.id, project: project.value })
    if (res?.data) detail.value = res.data
  } finally {
    detailLoading.value = false
  }
}
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="5">
        <ElInput
          v-model="keyword"
          :placeholder="t('cert.keywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="4">
        <ProjectSelect v-model="project" :placeholder="t('cert.project')" />
      </ElCol>
      <ElCol :span="3">
        <ElSelect v-model="expired" :placeholder="t('cert.expiredFilter')" class="full-width">
          <ElOption :label="t('cert.expiredAll')" value="" />
          <ElOption :label="t('cert.expiredOnly')" value="true" />
          <ElOption :label="t('cert.validOnly')" value="false" />
        </ElSelect>
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
        <div class="empty-state">{{ t('cert.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>

  <ElDrawer v-model="drawerVisible" :title="t('cert.detailTitle')" size="60%" direction="rtl">
    <div v-loading="detailLoading">
      <ElDescriptions v-if="detail" :column="2" border>
        <ElDescriptionsItem :label="t('cert.subject')">
          {{ detail.subject || '-' }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.subjectCN')">
          {{ detail.subjectCN || '-' }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.issuer')">
          {{ detail.issuer || '-' }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.issuerCN')">
          {{ detail.issuerCN || '-' }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.serial')">
          {{ detail.serial || '-' }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.hostCount')">
          {{ detail.hostCount }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.notBefore')">
          {{ timeText(detail.notBefore) }}
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.notAfter')">
          {{ timeText(detail.notAfter) }}
          <ElTag v-if="detail.expired" type="danger" size="small">{{ t('cert.expired') }}</ElTag>
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.fingerprint')" :span="2">
          <span class="mono">{{ detail.fingerprint || '-' }}</span>
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.fingerprintSha1')" :span="2">
          <span class="mono">{{ detail.fingerprintSha1 || '-' }}</span>
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.fingerprintMd5')" :span="2">
          <span class="mono">{{ detail.fingerprintMd5 || '-' }}</span>
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('cert.san')" :span="2">
          <ElTag v-for="item in detail.san ?? []" :key="item" size="small" class="san-tag">
            {{ item }}
          </ElTag>
          <span v-if="!(detail.san ?? []).length">-</span>
        </ElDescriptionsItem>
      </ElDescriptions>

      <div class="hosts-header">
        <span class="hosts-title">{{ t('cert.hostsTitle') }}</span>
        <span v-if="detail && detail.hostCount > (detail.hosts ?? []).length" class="hosts-hint">
          {{
            t('cert.hostsTruncated', {
              shown: (detail.hosts ?? []).length,
              total: detail.hostCount
            })
          }}
        </span>
      </div>
      <Table :columns="hostColumns" :data="detail?.hosts ?? []" stripe :border="true">
        <template #empty>
          <div class="empty-state">{{ t('cert.hostsEmpty') }}</div>
        </template>
      </Table>
    </div>
  </ElDrawer>
</template>

<style scoped>
.filter-row {
  margin-bottom: 15px;
}

.full-width {
  width: 100%;
}

.empty-state {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}

.expire-cell {
  display: inline-flex;
  gap: 8px;
  align-items: center;
}

.mono {
  font-family: var(--el-font-family-mono, monospace);
  word-break: break-all;
}

.san-tag {
  margin: 0 6px 6px 0;
}

.hosts-header {
  display: flex;
  gap: 12px;
  align-items: baseline;
  margin: 20px 0 10px;
}

.hosts-title {
  font-weight: 600;
}

.hosts-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
