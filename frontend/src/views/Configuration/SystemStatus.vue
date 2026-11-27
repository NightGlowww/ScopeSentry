<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import {
  ElAlert,
  ElButton,
  ElCard,
  ElCol,
  ElRow,
  ElTable,
  ElTableColumn,
  ElTag,
  ElDescriptions,
  ElDescriptionsItem
} from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { getConsoleInfoApi, type ConsoleInfo } from '@/api/dashboard'
import request from '@/axios'
import { useVisibleInterval } from '@/hooks/web/useVisibleInterval'
import { useSingleTabLeader } from '@/hooks/web/useSingleTabLeader'

const { t } = useI18n('systemStatus')

const info = ref<ConsoleInfo | null>(null)
const loading = ref(false)
const error = ref('')
let timer: number | undefined

const load = async () => {
  loading.value = true
  try {
    const res = await getConsoleInfoApi()
    info.value = res.data
    error.value = ''
  } catch (e: any) {
    error.value = e?.message || t('loadFailed')
  } finally {
    loading.value = false
  }
}

// 存储占用：解释「库为什么这么大、备份为什么这么久」。
// 2026-11-28 实测 SensitiveBody 18.3GB / 1.9 万条，其余集合加起来不到 1GB。
type StorageCol = { name: string; bytes: number; docs: number }
const storage = ref<{ total: number; collections: StorageCol[]; collectedAt: string } | null>(null)
const loadStorage = async () => {
  try {
    const res: any = await request.get({ url: '/api/system/storage' })
    storage.value = res?.data || null
  } catch {
    storage.value = null
  }
}
const mb = (bytes: number) => {
  const v = Number(bytes) || 0
  if (v >= 1024 ** 3) return (v / 1024 ** 3).toFixed(2) + ' GB'
  if (v >= 1024 ** 2) return (v / 1024 ** 2).toFixed(1) + ' MB'
  return (v / 1024).toFixed(0) + ' KB'
}
const topShare = (col: any) => {
  const total = Number(storage.value?.total) || 0
  if (!total) return ''
  return ((Number(col.bytes) / total) * 100).toFixed(1) + '%'
}

const depTag = (dep?: { connected: boolean; latencyMs: number; error?: string }) => {
  if (!dep) return { type: 'info' as const, text: '-' }
  if (!dep.connected)
    return { type: 'danger' as const, text: t('depDisconnected', { error: dep.error || '' }) }
  return { type: 'success' as const, text: t('depOk', { latency: dep.latencyMs.toFixed(1) }) }
}

onMounted(() => {
  load()
  loadStorage()
  const { isLeader: statusLeader } = useSingleTabLeader('system-status')
  useVisibleInterval(() => {
    if (!statusLeader.value) return
    load()
  }, 15000)
})
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="mb-12px">
      <ElCol :span="18">
        <span class="hint"
          >{{ t('autoRefreshHint')
          }}{{ info ? t('collectedAtSuffix', { at: info.collectedAt }) : '' }}</span
        >
        <span v-if="error" class="err">{{ error }}</span>
      </ElCol>
      <ElCol :span="6" style="text-align: right">
        <ElButton type="primary" :loading="loading" @click="load">{{ t('refresh') }}</ElButton>
      </ElCol>
    </ElRow>

    <ElRow :gutter="12" class="mb-12px">
      <ElCol :span="6">
        <ElCard shadow="never">
          <div class="stat-label">{{ t('serviceVersion') }}</div>
          <div class="stat-value">{{ info?.version || '-' }}</div>
        </ElCard>
      </ElCol>
      <ElCol :span="6">
        <ElCard shadow="never">
          <div class="stat-label">{{ t('uptime') }}</div>
          <div class="stat-value">{{ info?.uptimeHuman || '-' }}</div>
        </ElCard>
      </ElCol>
      <ElCol :span="6">
        <ElCard shadow="never">
          <div class="stat-label">{{ t('nodes') }}</div>
          <div class="stat-value">{{ info ? info.nodeOnline + ' / ' + info.nodeCount : '-' }}</div>
        </ElCard>
      </ElCol>
      <ElCol :span="6">
        <ElCard shadow="never">
          <div class="stat-label">{{ t('tasks') }}</div>
          <div class="stat-value">{{ info ? info.taskRunning + ' / ' + info.taskCount : '-' }}</div>
        </ElCard>
      </ElCol>
    </ElRow>

    <ElRow :gutter="12" class="mb-12px" v-if="info && info.hostStats">
      <ElCol :span="8">
        <ElCard shadow="never">
          <div class="stat-label">{{ t('diskFree') }}</div>
          <div class="stat-value">{{ info.hostStats.diskFreePercent.toFixed(1) }}%</div>
          <div class="hint">
            {{ (info.hostStats.diskFreeBytes / 1024 / 1024 / 1024).toFixed(1) }} GB /
            {{ (info.hostStats.diskTotalBytes / 1024 / 1024 / 1024).toFixed(1) }} GB
          </div>
        </ElCard>
      </ElCol>
      <ElCol :span="8">
        <ElCard shadow="never">
          <div class="stat-label">{{ t('load1m') }}</div>
          <div class="stat-value">{{
            info.hostStats.load1 >= 0 ? info.hostStats.load1.toFixed(2) : '-'
          }}</div>
          <div class="hint">{{ t('cpuCores', { cpus: info.hostStats.cpus }) }}</div>
        </ElCard>
      </ElCol>
      <ElCol :span="8">
        <ElCard shadow="never">
          <div class="stat-label">{{ t('memAvailable') }}</div>
          <div class="stat-value">
            {{
              info.hostStats.memAvailablePercent >= 0
                ? info.hostStats.memAvailablePercent.toFixed(1) + '%'
                : '-'
            }}
          </div>
          <div class="hint">{{ t('memWarn') }}</div>
        </ElCard>
      </ElCol>
    </ElRow>
    <ElAlert
      v-if="info && info.hostWarnings && info.hostWarnings.length"
      type="warning"
      :closable="false"
      show-icon
      class="mb-12px"
      :title="t('hostWarning', { list: info.hostWarnings.join(', ') })"
    />
    <ElCard shadow="never">
      <template #header
        ><span>{{ t('runInfo') }}</span></template
      >
      <ElDescriptions :column="2" border>
        <ElDescriptionsItem :label="t('startTime')">{{
          info?.startTime || '-'
        }}</ElDescriptionsItem>
        <ElDescriptionsItem :label="t('timezone')">{{ info?.timezone || '-' }}</ElDescriptionsItem>
        <ElDescriptionsItem label="MongoDB">
          <ElTag :type="depTag(info?.mongo).type" effect="light">{{
            depTag(info?.mongo).text
          }}</ElTag>
        </ElDescriptionsItem>
        <ElDescriptionsItem label="Redis">
          <ElTag :type="depTag(info?.redis).type" effect="light">{{
            depTag(info?.redis).text
          }}</ElTag>
        </ElDescriptionsItem>
        <ElDescriptionsItem :label="t('uptimeSeconds')">{{
          info?.uptimeSeconds ?? '-'
        }}</ElDescriptionsItem>
        <ElDescriptionsItem :label="t('collectedTime')">{{
          info?.collectedAt || '-'
        }}</ElDescriptionsItem>
      </ElDescriptions>
    </ElCard>

    <ElCard shadow="never" class="mt-12px">
      <template #header>
        <span>{{ t('storageUsage') }}</span>
        <span v-if="storage" class="hint" style="margin-left: 8px">
          {{ t('storageSummary', { total: mb(storage.total) }) }}
        </span>
      </template>
      <ElTable v-if="storage?.collections?.length" :data="storage.collections" size="small">
        <ElTableColumn prop="name" :label="t('colCollection')" min-width="180" />
        <ElTableColumn :label="t('colUsage')" width="120">
          <template #default="{ row }">{{ mb(row.bytes) }}</template>
        </ElTableColumn>
        <ElTableColumn :label="t('colShare')" width="90">
          <template #default="{ row }">{{ topShare(row) }}</template>
        </ElTableColumn>
        <ElTableColumn prop="docs" :label="t('colDocs')" width="120" />
      </ElTable>
      <div v-else class="hint">{{ t('noData') }}</div>
    </ElCard>
  </ContentWrap>
</template>

<style scoped>
.mb-12px {
  margin-bottom: 12px;
}
.mt-12px {
  margin-top: 12px;
}
.stat-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-bottom: 6px;
}
.stat-value {
  font-size: 22px;
  font-weight: 600;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.err {
  margin-left: 12px;
  color: var(--el-color-danger);
  font-size: 12px;
}
</style>
