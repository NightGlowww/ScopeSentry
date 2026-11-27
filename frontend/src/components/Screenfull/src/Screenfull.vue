<script setup lang="ts">
import { Icon } from '@/components/Icon'
import { useFullscreen } from '@vueuse/core'
import { propTypes } from '@/utils/propTypes'
import { useDesign } from '@/hooks/web/useDesign'
import { useI18n } from '@/hooks/web/useI18n'

const { getPrefixCls } = useDesign()

const { t } = useI18n()

const prefixCls = getPrefixCls('screenfull')

defineProps({
  color: propTypes.string.def('')
})

const { toggle, isFullscreen } = useFullscreen()

const toggleFullscreen = () => {
  toggle()
}
</script>

<template>
  <div
    :class="prefixCls"
    role="button"
    tabindex="0"
    :aria-label="t('common.toggleFullscreen')"
    @click="toggleFullscreen"
    @keydown.enter="toggleFullscreen"
  >
    <Icon
      :size="18"
      :icon="isFullscreen ? 'zmdi:fullscreen-exit' : 'zmdi:fullscreen'"
      :color="color"
    />
  </div>
</template>
