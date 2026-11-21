<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/hooks/web/useI18n'
import { ElRadioGroup, ElRadioButton, ElTooltip } from 'element-plus'

// 发现时间筛选条（可复用）：对齐 ARL-Next v1.5.1
// 用法：<TimeRangeFilter v-model="timeRange" v-model:sort="timeSort" @change="handleSearch" />
const props = defineProps<{
  modelValue: string
  sort?: string
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
  (e: 'update:sort', v: string): void
  (e: 'change'): void
}>()
const { t } = useI18n()

const days = computed(() => props.modelValue || 'all')
const sortDir = computed(() => props.sort || 'desc')

const setDays = (v: string | number | boolean | undefined) => {
  emit('update:modelValue', String(v))
  emit('change')
}
const setSort = (v: string | number | boolean | undefined) => {
  emit('update:sort', String(v))
  emit('change')
}

// 把「最近 N 天」换算成后端能比较的起始时间串
const from = computed(() => {
  const v = days.value
  if (v === 'all') return ''
  const n = Number(v)
  if (!n) return ''
  const d = new Date(Date.now() - n * 24 * 3600 * 1000)
  const pad = (x: number) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} 00:00:00`
})
defineExpose({ from })
</script>

<template>
  <div class="time-range-row">
    <span class="time-range-label">{{ t('asset.discoveryTime') }}</span>
    <ElRadioGroup :model-value="days" size="small" @change="setDays">
      <ElRadioButton label="all">{{ t('asset.timeAll') }}</ElRadioButton>
      <ElRadioButton label="1">{{ t('asset.timeToday') }}</ElRadioButton>
      <ElRadioButton label="7">{{ t('asset.time7d') }}</ElRadioButton>
      <ElRadioButton label="30">{{ t('asset.time30d') }}</ElRadioButton>
    </ElRadioGroup>
    <ElTooltip :content="t('asset.sortHint')" placement="top">
      <ElRadioGroup :model-value="sortDir" size="small" class="sort-group" @change="setSort">
        <ElRadioButton label="desc">{{ t('asset.sortNewest') }}</ElRadioButton>
        <ElRadioButton label="asc">{{ t('asset.sortOldest') }}</ElRadioButton>
      </ElRadioGroup>
    </ElTooltip>
  </div>
</template>

<style scoped>
.time-range-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}
.time-range-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.sort-group {
  margin-left: auto;
}
</style>
