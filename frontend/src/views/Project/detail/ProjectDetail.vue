<script setup lang="ts">
import { useI18n } from '@/hooks/web/useI18n'
import { ElTabs, ElTabPane } from 'element-plus'
import Dashboard from './components/Dashboard.vue'
import Subdomain from './components/Subdomain.vue'
import Port from './components/Port.vue'
import Service from './components/Service.vue'
const { t } = useI18n()
// 页面 id 从 URL query 取（ProjectList.vue: push('/project-management/project-detail?id=xxx')），
// 四个 Tab 组件各自 useRoute().query.id 组装请求。
</script>

<template>
  <ElTabs type="border-card">
    <ElTabPane :label="t('project.overview')"><Dashboard /></ElTabPane>
    <ElTabPane :label="t('subdomain.subdomainName')"><Subdomain /></ElTabPane>
    <ElTabPane :label="t('asset.port')"> <Port /></ElTabPane>
    <ElTabPane :label="t('asset.service')"><Service /></ElTabPane>
  </ElTabs>
</template>
