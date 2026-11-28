<script setup lang="ts">
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { ref, onMounted, reactive } from 'vue'
import {
  ElButton,
  ElTable,
  ElTableColumn,
  ElSwitch,
  ElInputNumber,
  ElTag,
  ElMessage,
  ElTooltip,
  ElLink
} from 'element-plus'
import {
  getIntelFeedsApi,
  runIntelFeedApi,
  updateIntelFeedApi,
  type IntelFeed,
  type IntelItem
} from '@/api/system/intel'

const { t } = useI18n('intelFeeds')

const loading = ref(false)
const feeds = ref<IntelFeed[]>([])
const items = ref<IntelItem[]>([])
const total = ref(0)
const running = reactive<Record<string, boolean>>({})

const load = async () => {
  loading.value = true
  try {
    const res = await getIntelFeedsApi()
    feeds.value = (res.data?.feeds ?? []) as IntelFeed[]
    items.value = (res.data?.items ?? []) as IntelItem[]
    total.value = res.data?.total ?? 0
  } catch (e: any) {
    ElMessage.error(e?.message || t('loadFailed'))
  } finally {
    loading.value = false
  }
}

const toggle = async (row: IntelFeed, val: boolean) => {
  try {
    await updateIntelFeedApi(row._id, { enabled: val })
    row.enabled = val
    ElMessage.success(val ? t('enabledMsg') : t('disabledMsg'))
  } catch (e: any) {
    row.enabled = !val
    ElMessage.error(e?.message || t('operateFailed'))
  }
}

const setInterval = async (row: IntelFeed, val: number | undefined) => {
  if (!val) return
  try {
    await updateIntelFeedApi(row._id, { intervalHours: val })
    row.intervalHours = val
    ElMessage.success(t('intervalUpdated'))
  } catch (e: any) {
    ElMessage.error(e?.message || t('operateFailed'))
  }
}

const runOne = async (row?: IntelFeed) => {
  const key = row?._id ?? '__all__'
  running[key] = true
  try {
    const res = await runIntelFeedApi(row?._id)
    const results = (res.data?.results ?? []) as any[]
    const added = results.reduce((s, r) => s + (r.added ?? 0), 0)
    const failed = results.filter((r) => r.error)
    ElMessage.success(
      t('refreshDone', { added }) +
        (failed.length ? t('refreshFailedSuffix', { count: failed.length }) : '')
    )
    await load()
  } catch (e: any) {
    ElMessage.error(e?.message || t('refreshFailed'))
  } finally {
    running[key] = false
  }
}

const fmt = (s?: string) => (s ? new Date(s).toLocaleString('zh-CN', { hour12: false }) : '—')

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-16px">
    <ContentWrap :title="t('pageTitle')">
      <div class="mb-12px flex items-center gap-12px">
        <ElTag type="info" effect="plain">{{
          t('sourceCount', { total: feeds.length, enabled: feeds.filter((f) => f.enabled).length })
        }}</ElTag>
        <ElTag type="success" effect="plain">{{ t('totalItems', { total }) }}</ElTag>
        <div class="flex-grow"></div>
        <ElButton type="primary" :loading="running['__all__']" @click="runOne()">{{
          t('refreshAll')
        }}</ElButton>
        <ElButton :loading="loading" @click="load">{{ t('refreshList') }}</ElButton>
      </div>
      <ElTable :data="feeds" v-loading="loading" size="small" border style="width: 100%">
        <ElTableColumn prop="name" :label="t('colSource')" min-width="180" />
        <ElTableColumn prop="kind" :label="t('colKind')" width="90" align="center" />
        <ElTableColumn :label="t('colEnabled')" width="80" align="center">
          <template #default="{ row }">
            <ElSwitch :model-value="row.enabled" @change="(v: any) => toggle(row as any, !!v)" />
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('colInterval')" width="120" align="center">
          <template #default="{ row }">
            <ElInputNumber
              :model-value="row.intervalHours"
              :min="1"
              :max="720"
              size="small"
              controls-position="right"
              style="width: 88px"
              @change="(v: any) => setInterval(row as any, v)"
            />
          </template>
        </ElTableColumn>
        <ElTableColumn prop="itemCount" :label="t('colItemCount')" width="80" align="center" />
        <ElTableColumn :label="t('colLastRun')" width="170" align="center">
          <template #default="{ row }">{{ fmt(row.lastRunAt) }}</template>
        </ElTableColumn>
        <ElTableColumn :label="t('colStatus')" min-width="220">
          <template #default="{ row }">
            <ElTag v-if="!row.lastError" type="success" effect="plain" size="small">{{
              t('ok')
            }}</ElTag>
            <ElTooltip v-else :content="row.lastError" placement="top">
              <ElTag type="danger" effect="plain" size="small">{{ t('failed') }}</ElTag>
            </ElTooltip>
            <span v-if="row.lastError" class="ml-8px text-12px op-60">{{
              row.lastError.slice(0, 60)
            }}</span>
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('colAction')" width="110" align="center">
          <template #default="{ row }">
            <ElButton size="small" :loading="running[row._id]" @click="runOne(row as any)">{{
              t('runNow')
            }}</ElButton>
          </template>
        </ElTableColumn>
      </ElTable>
    </ContentWrap>

    <ContentWrap :title="t('latestTitle')">
      <ElTable :data="items" size="small" border style="width: 100%">
        <ElTableColumn prop="source" :label="t('colFrom')" width="150" />
        <ElTableColumn :label="t('colTitle')" min-width="420">
          <template #default="{ row }">
            <ElLink :href="row.url" target="_blank" type="primary">{{ row.title }}</ElLink>
          </template>
        </ElTableColumn>
        <ElTableColumn prop="published" :label="t('colPublished')" width="180" />
        <ElTableColumn :label="t('colAddedAt')" width="170">
          <template #default="{ row }">{{ fmt(row.addedAt) }}</template>
        </ElTableColumn>
      </ElTable>
    </ContentWrap>
  </div>
</template>
