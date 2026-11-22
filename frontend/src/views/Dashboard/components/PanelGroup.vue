<script setup lang="ts">
import { ElCard, ElSkeleton } from 'element-plus'
import { CountTo } from '@/components/CountTo'
import { useDesign } from '@/hooks/web/useDesign'
import { useI18n } from '@/hooks/web/useI18n'
import { computed, ref, reactive } from 'vue'
import { getAssetStatisticsApi } from '@/api/dashboard/analysis'
import { getDashboardStatsApi, type DashboardStats } from '@/api/dashboard'
import type { DashboardTotalTypes } from '@/api/dashboard/analysis/types'

const { t } = useI18n()

const { getPrefixCls } = useDesign()

const prefixCls = getPrefixCls('panel')

const loading = ref(true)

const totalState = reactive<Omit<DashboardTotalTypes, 'vulnerabilityCount'>>({
  asetCount: 0,
  subdomainCount: 0,
  sensitiveCount: 0,
  urlCount: 0
})

const vulnLoading = ref(true)
const vulnStats = ref<DashboardStats | null>(null)
const vulnBreakdown = computed(() => {
  const stats = vulnStats.value
  if (!stats) return ''
  return t('dashboard.vulnBreakdown', {
    candidates: stats.vulnerability,
    pending: stats.vulnPending,
    ignored: stats.vulnIgnored
  })
})

const getVulnStats = async () => {
  try {
    const res = await getDashboardStatsApi()
    if (typeof res?.data?.vulnConfirmed === 'number') vulnStats.value = res.data
  } catch {
    // 保留不可用状态，避免把统计失败显示成确认数为零。
  } finally {
    vulnLoading.value = false
  }
}

const getCount = async () => {
  try {
    const res = await getAssetStatisticsApi()
    // 统计接口失败不应该把首页拖成白屏，保持 0 即可
    if (!res?.data) return
    totalState.asetCount = res.data.assetCount
    totalState.subdomainCount = res.data.subdomainCount
    totalState.sensitiveCount = res.data.sensitiveCount
    totalState.urlCount = res.data.urlCount
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

getCount()
getVulnStats()

// 五个指标卡：颜色只做“识别”，不做装饰
const cards = computed(() => [
  {
    key: 'asset',
    icon: 'zondicons:network',
    label: t('dashboard.totalAssets'),
    value: totalState.asetCount || 0,
    tone: '#0ea5a4',
    soft: 'rgb(14 165 164 / 12%)'
  },
  {
    key: 'subdomain',
    icon: 'formkit:url',
    label: t('dashboard.subDomain'),
    value: totalState.subdomainCount || 0,
    tone: '#2f6feb',
    soft: 'rgb(47 111 235 / 12%)'
  },
  {
    key: 'sensitive',
    icon: 'lucide:info',
    label: t('dashboard.informationLeakage'),
    value: totalState.sensitiveCount || 0,
    tone: '#e11d48',
    soft: 'rgb(225 29 72 / 12%)'
  },
  {
    key: 'url',
    icon: 'carbon:url',
    label: t('dashboard.URL'),
    value: totalState.urlCount || 0,
    tone: '#6366f1',
    soft: 'rgb(99 102 241 / 12%)'
  },
  {
    key: 'vul',
    icon: 'ant-design:bug-filled',
    label: t('dashboard.confirmedVulnerabilities'),
    value: vulnStats.value?.vulnConfirmed ?? null,
    tone: '#d97706',
    soft: 'rgb(217 119 6 / 12%)'
  }
])
</script>

<template>
  <!-- 用 grid 而不是 ElCol 的 5/5/5/5/4：栅格总和卡在 24，最后一列被压窄；
       而「漏洞」卡片多一行处置分布，比其它卡高，看着就不齐（2026-11-20 截图反馈）。
       grid 保证等宽等高，窄屏自动换行。 -->
  <div :class="prefixCls">
    <div v-for="card in cards" :key="card.key" :class="`${prefixCls}__col`">
      <ElCard shadow="never" :class="`${prefixCls}__card`">
        <ElSkeleton :loading="card.key === 'vul' ? vulnLoading : loading" animated :rows="2">
          <template #default>
            <div
              :class="`${prefixCls}__item`"
              :style="{ '--tone': card.tone, '--tone-soft': card.soft }"
            >
              <div :class="`${prefixCls}__icon`">
                <Icon :icon="card.icon" :size="22" />
              </div>
              <div :class="`${prefixCls}__meta`">
                <div :class="`${prefixCls}__label`">{{ card.label }}</div>
                <CountTo
                  v-if="card.value !== null"
                  :class="`${prefixCls}__value`"
                  :start-val="0"
                  :end-val="card.value"
                  :duration="1600"
                />
                <div v-else :class="`${prefixCls}__value`">
                  {{ t('dashboard.statsUnavailable') }}
                </div>
                <div v-if="card.key === 'vul' && vulnBreakdown" :class="`${prefixCls}__sub`">
                  {{ vulnBreakdown }}
                </div>
              </div>
            </div>
          </template>
        </ElSkeleton>
      </ElCard>
    </div>
  </div>
</template>

<style lang="less" scoped>
@prefix-cls: ~'@{namespace}-panel';

.@{prefix-cls} {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 16px;
  margin-bottom: 16px;

  @media (max-width: 1400px) {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  @media (max-width: 900px) {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  @media (max-width: 560px) {
    grid-template-columns: 1fr;
  }

  &__col {
    display: flex;
    min-width: 0;
  }

  &__card {
    width: 100%;
  }

  &__sub {
    margin-top: 2px;
    font-size: 11px;
    color: var(--el-text-color-secondary);
  }

  &__card {
    border: 1px solid var(--app-border);
    border-radius: var(--app-radius-md);
    transition:
      border-color 0.18s ease,
      transform 0.18s ease,
      box-shadow 0.18s ease;

    &:hover {
      border-color: var(--tone, var(--app-border-strong));
      box-shadow: var(--app-shadow-sm);
      transform: translateY(-1px);
    }

    :deep(.el-card__body) {
      padding: 16px 18px;
    }
  }

  &__item {
    display: flex;
    align-items: center;
    gap: 14px;
    min-height: 52px;
  }

  &__icon {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    color: var(--tone);
    background: var(--tone-soft);
    border-radius: 12px;
  }

  &__meta {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  &__label {
    overflow: hidden;
    font-size: 13px;
    color: var(--el-text-color-secondary);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__value {
    font-size: 26px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    line-height: 1.15;
    letter-spacing: -0.02em;
    color: var(--el-text-color-primary);
  }
}
</style>
