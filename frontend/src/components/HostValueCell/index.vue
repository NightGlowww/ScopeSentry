<script setup lang="ts">
import { computed } from 'vue'
import { ElTooltip } from 'element-plus'
import { Icon } from '@/components/Icon'
import { copyText } from '@/utils/clipboard'
import { buildHref, splitHostValues, type HrefPolicy } from '@/utils/hostLink'

// 表格里的域名 / IP / 主机单元格（UI_FINDINGS #1/#2）：
// - 能确定 http(s) 才渲染 <a>，否则纯文本（不可点、无 pointer、无下划线）
// - 多值只显示第一个 + 「+N」徽标，tooltip 列全量
// - 单值超列宽由 CSS ellipsis 截断，title 给完整值
const props = withDefaults(
  defineProps<{
    value?: string | string[] | null
    /** 已解析好的链接；传 undefined 表示按 value + policy 自行推导，传 null 强制纯文本 */
    href?: string | null
    policy?: HrefPolicy
    copyLabel?: string
    icon?: string
    iconColor?: string
    iconSize?: number
  }>(),
  { policy: 'from-data', copyLabel: '复制', iconSize: 16 }
)

const values = computed(() => splitHostValues(props.value))
const first = computed(() => values.value[0] ?? '')
const rest = computed(() => Math.max(values.value.length - 1, 0))
const fullText = computed(() => values.value.join('\n'))
const link = computed(() =>
  props.href === undefined ? buildHref(first.value, props.policy) : props.href
)

const onCopy = () => copyText(values.value.length > 1 ? fullText.value : first.value)
</script>

<template>
  <div class="host-cell">
    <Icon
      v-if="icon"
      :icon="icon"
      :size="iconSize"
      :color="iconColor"
      class="host-cell__icon"
    />
    <a
      v-if="link"
      :href="link"
      target="_blank"
      rel="noopener noreferrer"
      class="host-cell__value host-cell__link"
      :title="fullText"
      >{{ first }}</a
    >
    <span v-else class="host-cell__value" :title="fullText">{{ first }}</span>
    <ElTooltip v-if="rest > 0" placement="top">
      <template #content>
        <div class="host-cell__list">{{ values.join('\n') }}</div>
      </template>
      <span class="host-cell__more" :aria-label="`共 ${values.length} 个值`">+{{ rest }}</span>
    </ElTooltip>
    <ElTooltip :content="copyLabel" placement="top">
      <Icon
        icon="ep:copy-document"
        role="button"
        tabindex="0"
        :aria-label="copyLabel"
        :size="13"
        color="var(--el-text-color-placeholder)"
        class="host-cell__copy"
        @click="onCopy"
        @keydown.enter.prevent="onCopy"
      />
    </ElTooltip>
  </div>
</template>

<style lang="less" scoped>
.host-cell {
  display: flex;
  align-items: center;
  min-width: 0;
  max-width: 100%;
}

.host-cell__icon {
  flex: 0 0 auto;
  transform: translateY(8%);
}

.host-cell__value {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.host-cell__link {
  color: var(--el-color-primary);
  text-decoration: none;
}

.host-cell__link:hover {
  text-decoration: underline;
}

.host-cell__more {
  flex: 0 0 auto;
  margin-left: 4px;
  padding: 0 5px;
  font-size: 11px;
  line-height: 16px;
  border-radius: 8px;
  background: var(--el-fill-color);
  color: var(--el-text-color-secondary);
  cursor: default;
}

.host-cell__copy {
  flex: 0 0 auto;
  margin-left: 4px;
  cursor: pointer;
}

.host-cell__list {
  max-width: 420px;
  white-space: pre-line;
}
</style>
