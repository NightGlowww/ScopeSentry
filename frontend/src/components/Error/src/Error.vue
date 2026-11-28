<script setup lang="ts">
import pageError from '@/assets/svgs/404.svg'
import networkError from '@/assets/svgs/500.svg'
import noPermission from '@/assets/svgs/403.svg'
import { propTypes } from '@/utils/propTypes'
import { useI18n } from '@/hooks/web/useI18n'

interface ErrorMap {
  url: string
  message: string
  buttonText: string
}

const { t } = useI18n()

const errorMap: {
  [key: string]: ErrorMap
} = {
  '404': {
    url: pageError,
    message: t('error.pageError'),
    buttonText: t('error.returnToHome')
  },
  '500': {
    url: networkError,
    message: t('error.networkError'),
    buttonText: t('error.returnToHome')
  },
  '403': {
    url: noPermission,
    message: t('error.noPermission'),
    buttonText: t('error.returnToHome')
  }
}

const props = defineProps({
  type: propTypes.string.validate((v: string) => ['404', '500', '403'].includes(v)).def('404'),
  // 用户实际访问的路径：只给「页面不存在」太没线索，复制这条路径才能去排查/反馈
  path: propTypes.string.def('')
})

const emit = defineEmits(['errorClick', 'backClick'])

const btnClick = () => {
  emit('errorClick', props.type)
}
const backClick = () => {
  emit('backClick', props.type)
}
</script>

<style scoped>
.error-path {
  margin-top: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  word-break: break-all;
}
.error-path code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  background: var(--el-fill-color-light);
  padding: 1px 6px;
  border-radius: 4px;
}
.error-actions {
  display: flex;
  gap: 10px;
  justify-content: center;
}
</style>

<template>
  <div class="flex justify-center">
    <div class="text-center">
      <img width="350" :src="errorMap[type].url" alt="" />
      <div class="text-14px text-[var(--el-color-info)]">{{ errorMap[type].message }}</div>
      <!-- 把访问的路径摆出来：否则用户只能截一张「页面不存在」的图，谁都没法定位 -->
      <div v-if="path" class="error-path" :title="t('error.requestedPathTip')">
        {{ t('error.requestedPath') }}<code>{{ path }}</code>
      </div>
      <div class="mt-20px error-actions">
        <BaseButton type="primary" @click="btnClick">{{ errorMap[type].buttonText }}</BaseButton>
        <BaseButton @click="backClick">{{ t('error.back') }}</BaseButton>
      </div>
    </div>
  </div>
</template>
