<script setup lang="ts">
import { ref } from 'vue'
import { ElTabPane, ElTabs } from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import DictionaryPanel from './components/DictionaryPanel.vue'

// 字典管理：按 kind 分 tab，每个 tab 都是同一套「左列表 + 右内容」组件。
// dir / subdomain / other 走通用 /api/dictionary/*；brute / cdn 的 list/save 由
// api/dictionary 内部路由到 /api/dict_brute、/api/dict_cdn，组件本身不区分。
const { t } = useI18n()

const activeKind = ref('dir')

const tabs = [
  { kind: 'dir', label: 'dictManager.kindDir' },
  { kind: 'subdomain', label: 'dictManager.kindSubdomain' },
  { kind: 'brute', label: 'dictManager.kindBrute' },
  { kind: 'cdn', label: 'dictManager.kindCdn' },
  { kind: 'other', label: 'dictManager.kindOther' }
]
</script>

<template>
  <ContentWrap>
    <ElTabs v-model="activeKind" type="border-card">
      <ElTabPane v-for="tab in tabs" :key="tab.kind" :label="t(tab.label)" :name="tab.kind" lazy>
        <DictionaryPanel :kind="tab.kind" />
      </ElTabPane>
    </ElTabs>
  </ContentWrap>
</template>
