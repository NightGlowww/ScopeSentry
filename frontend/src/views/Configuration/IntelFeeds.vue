<script setup lang="ts">
import { ContentWrap } from '@/components/ContentWrap'
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
    ElMessage.error(e?.message || '加载失败')
  } finally {
    loading.value = false
  }
}

const toggle = async (row: IntelFeed, val: boolean) => {
  try {
    await updateIntelFeedApi(row._id, { enabled: val })
    row.enabled = val
    ElMessage.success(val ? '已启用' : '已停用')
  } catch (e: any) {
    row.enabled = !val
    ElMessage.error(e?.message || '操作失败')
  }
}

const setInterval = async (row: IntelFeed, val: number | undefined) => {
  if (!val) return
  try {
    await updateIntelFeedApi(row._id, { intervalHours: val })
    row.intervalHours = val
    ElMessage.success('间隔已更新')
  } catch (e: any) {
    ElMessage.error(e?.message || '操作失败')
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
    ElMessage.success(`刷新完成：新增 ${added} 条${failed.length ? `，失败 ${failed.length} 个源` : ''}`)
    await load()
  } catch (e: any) {
    ElMessage.error(e?.message || '刷新失败')
  } finally {
    running[key] = false
  }
}

const fmt = (s?: string) => (s ? new Date(s).toLocaleString('zh-CN', { hour12: false }) : '—')

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-16px">
    <ContentWrap title="多源情报订阅">
      <div class="mb-12px flex items-center gap-12px">
        <ElTag type="info" effect="plain">源 {{ feeds.length }} 个 / 启用 {{ feeds.filter((f) => f.enabled).length }}</ElTag>
        <ElTag type="success" effect="plain">累计情报 {{ total }} 条</ElTag>
        <div class="flex-grow"></div>
        <ElButton type="primary" :loading="running['__all__']" @click="runOne()">全部立即刷新</ElButton>
        <ElButton :loading="loading" @click="load">刷新列表</ElButton>
      </div>
      <ElTable :data="feeds" v-loading="loading" size="small" border style="width: 100%">
        <ElTableColumn prop="name" label="情报源" min-width="180" />
        <ElTableColumn prop="kind" label="类型" width="90" align="center" />
        <ElTableColumn label="启用" width="80" align="center">
          <template #default="{ row }">
            <ElSwitch :model-value="row.enabled" @change="(v: any) => toggle(row, !!v)" />
          </template>
        </ElTableColumn>
        <ElTableColumn label="间隔(h)" width="120" align="center">
          <template #default="{ row }">
            <ElInputNumber
              :model-value="row.intervalHours"
              :min="1"
              :max="720"
              size="small"
              controls-position="right"
              style="width: 88px"
              @change="(v: any) => setInterval(row, v)"
            />
          </template>
        </ElTableColumn>
        <ElTableColumn prop="itemCount" label="条目" width="80" align="center" />
        <ElTableColumn label="上次抓取" width="170" align="center">
          <template #default="{ row }">{{ fmt(row.lastRunAt) }}</template>
        </ElTableColumn>
        <ElTableColumn label="状态" min-width="220">
          <template #default="{ row }">
            <ElTag v-if="!row.lastError" type="success" effect="plain" size="small">正常</ElTag>
            <ElTooltip v-else :content="row.lastError" placement="top">
              <ElTag type="danger" effect="plain" size="small">失败</ElTag>
            </ElTooltip>
            <span v-if="row.lastError" class="ml-8px text-12px op-60">{{ row.lastError.slice(0, 60) }}</span>
          </template>
        </ElTableColumn>
        <ElTableColumn label="操作" width="110" align="center">
          <template #default="{ row }">
            <ElButton size="small" :loading="running[row._id]" @click="runOne(row)">立即抓取</ElButton>
          </template>
        </ElTableColumn>
      </ElTable>
    </ContentWrap>

    <ContentWrap title="最新情报（20 条）">
      <ElTable :data="items" size="small" border style="width: 100%">
        <ElTableColumn prop="source" label="来源" width="150" />
        <ElTableColumn label="标题" min-width="420">
          <template #default="{ row }">
            <ElLink :href="row.url" target="_blank" type="primary">{{ row.title }}</ElLink>
          </template>
        </ElTableColumn>
        <ElTableColumn prop="published" label="发布" width="180" />
        <ElTableColumn label="入库" width="170">
          <template #default="{ row }">{{ fmt(row.addedAt) }}</template>
        </ElTableColumn>
      </ElTable>
    </ContentWrap>
  </div>
</template>
