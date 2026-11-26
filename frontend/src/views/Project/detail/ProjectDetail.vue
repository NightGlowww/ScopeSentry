<script setup lang="ts">
import { useI18n } from '@/hooks/web/useI18n'
import { ElTabs, ElTabPane } from 'element-plus'
import Dashboard from './components/Dashboard.vue'
import Subdomain from './components/Subdomain.vue'
import Port from './components/Port.vue'
import Service from './components/Service.vue'
import Tasks from './components/Tasks.vue'
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
    <!-- 任务页签：项目不能只是资产聚合，它得能回答「这个项目跑过哪些任务、结果如何」 -->
    <ElTabPane :label="t('task.projectTasks')"><Tasks /></ElTabPane>
  </ElTabs>
</template>
