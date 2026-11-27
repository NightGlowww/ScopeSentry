<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElButton, ElCard, ElCol, ElRow, ElTooltip } from 'element-plus'
import { getAssetOverviewCountApi } from '@/api/assetOverview'
import type { AssetOverviewCount } from '@/api/assetOverview/types'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'

// 资产族数量总览：一行统计卡片，数据来自 /api/assets/asset_overview/count。
// 物理集合走 EstimatedDocumentCount，cert/cip 是读模型聚合计数，
// 所以这里的证书/主体数和对应列表页「全部项目」的 total 是同一口径。
const { t } = useI18n()
const refreshIcon = useIcon({ icon: 'iconoir:refresh' })

const emptyCount = (): AssetOverviewCount => ({
  asset: 0,
  subdomain: 0,
  url: 0,
  app: 0,
  mp: 0,
  vulnerability: 0,
  sensitive: 0,
  cert: 0,
  cip: 0
})

const counts = ref<AssetOverviewCount>(emptyCount())
const loading = ref(false)
const loaded = ref(false)

const cards: { key: keyof AssetOverviewCount; labelKey: string }[] = [
  { key: 'asset', labelKey: 'assetOverview.asset' },
  { key: 'subdomain', labelKey: 'assetOverview.subdomain' },
  { key: 'url', labelKey: 'assetOverview.url' },
  { key: 'app', labelKey: 'assetOverview.app' },
  { key: 'mp', labelKey: 'assetOverview.mp' },
  { key: 'vulnerability', labelKey: 'assetOverview.vulnerability' },
  { key: 'sensitive', labelKey: 'assetOverview.sensitive' },
  { key: 'cert', labelKey: 'assetOverview.cert' },
  { key: 'cip', labelKey: 'assetOverview.cip' }
]

const total = computed(() => cards.reduce((sum, card) => sum + (counts.value[card.key] ?? 0), 0))

const formatCount = (value: number | undefined) => Number(value ?? 0).toLocaleString()

const load = async () => {
  loading.value = true
  try {
    const res = await getAssetOverviewCountApi()
    counts.value = { ...emptyCount(), ...(res?.data ?? {}) }
  } catch {
    counts.value = emptyCount()
  } finally {
    loading.value = false
    loaded.value = true
  }
}

onMounted(load)
</script>

<template>
  <ElCard shadow="never" class="overview-card">
    <template #header>
      <div class="overview-header">
        <span class="overview-title">{{ t('assetOverview.title') }}</span>
        <ElButton link type="primary" :icon="refreshIcon" :loading="loading" @click="load">
          {{ t('assetOverview.refresh') }}
        </ElButton>
      </div>
    </template>
    <ElRow :gutter="12">
      <ElCol v-for="card in cards" :key="card.key" :xs="12" :sm="8" :md="6" :lg="4" :xl="4">
        <!-- 漏洞这一格后天就排除了「已忽略」（status=3），
             而漏洞列表页显示的是全部候选——两边数字天然差几条。
             不写清楚会被当成数据不一致（2026-11-27 实测 3 vs 4）。 -->
        <ElTooltip
          v-if="card.key === 'vulnerability'"
          content="不含「已忽略」的漏洞；漏洞结果列表里显示的是全部候选，所以两边会差几条"
          placement="top"
          effect="light"
        >
          <div class="overview-item">
            <div class="overview-label">{{ t(card.labelKey) }} <span class="overview-hint">?</span></div>
            <div class="overview-value">{{ formatCount(counts[card.key]) }}</div>
          </div>
        </ElTooltip>
        <div v-else class="overview-item">
          <div class="overview-label">{{ t(card.labelKey) }}</div>
          <div class="overview-value">{{ formatCount(counts[card.key]) }}</div>
        </div>
      </ElCol>
    </ElRow>
    <div v-if="loaded && total === 0" class="overview-empty">
      {{ t('assetOverview.empty') }}
    </div>
  </ElCard>
</template>

<style scoped>
.overview-hint {
  display: inline-block;
  width: 14px;
  height: 14px;
  line-height: 14px;
  text-align: center;
  border-radius: 50%;
  font-size: 10px;
  color: var(--el-text-color-secondary);
  border: 1px solid var(--el-border-color);
  cursor: help;
}

.overview-card {
  margin-bottom: 12px;
}

.overview-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.overview-title {
  font-size: 15px;
  font-weight: 600;
}

.overview-item {
  padding: 10px 12px;
  margin-bottom: 8px;
  background: var(--el-fill-color-lighter);
  border-radius: 6px;
}

.overview-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.overview-value {
  margin-top: 4px;
  color: var(--el-text-color-primary);
  font-size: 20px;
  font-weight: 600;
  line-height: 1.2;
}

.overview-empty {
  padding: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
