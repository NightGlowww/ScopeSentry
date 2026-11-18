<script setup lang="ts">
import { ElTooltip } from 'element-plus'

/**
 * 给「禁用按钮」补一条可见的禁用原因。
 *
 * 为什么不能只加 title：Chrome/Firefox 对 disabled 的原生 button 不派发鼠标事件，
 * 原生 title 提示不会出现；EP 的 ElTooltip 也一样收不到 hover。
 * 因此这里包一层 span 承接指针事件，tooltip 由 span 触发，按钮本身仍保持 disabled。
 * 同时把 reason 透传到按钮的 title（无障碍/自动化断言可读），并用 aria-disabled 标注包裹层。
 */
withDefaults(
  defineProps<{
    disabled?: boolean
    reason?: string
  }>(),
  { disabled: false, reason: '' }
)
</script>

<template>
  <ElTooltip
    :disabled="!disabled || !reason"
    :content="reason"
    placement="top"
    :show-after="120"
    popper-class="disable-hint__popper"
  >
    <span class="disable-hint" :class="{ 'is-disabled': disabled }" :aria-disabled="disabled">
      <slot />
    </span>
  </ElTooltip>
</template>

<style scoped lang="less">
.disable-hint {
  display: inline-flex;
  align-items: center;
}

/* 禁用态下给出 not-allowed 光标，hover 任意位置都能触发外层 tooltip */
.disable-hint.is-disabled {
  cursor: not-allowed;
}

.disable-hint.is-disabled :deep(.el-button) {
  cursor: not-allowed;
  pointer-events: none;
}
</style>
