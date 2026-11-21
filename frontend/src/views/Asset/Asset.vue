<script setup lang="ts">
import { useI18n } from '@/hooks/web/useI18n'
import { ElTabs } from 'element-plus'
import { ElTabPane } from 'element-plus'
import AssetInfo2 from './components/AssetInfo2.vue'
import Subdomain from './components/Subdomain.vue'
import URL from './components/URL.vue'
import Crawler from './components/Crawler.vue'
import SensitiveInformation from './components/SensitiveInformation.vue'
import DirScan from './components/DirScan.vue'
import PageMonitoring from './components/PageMonitoring.vue'
import vul from './components/vul.vue'
import SubdomainTakeover from './components/SubdomainTakeover.vue'
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getProjectAllApi } from '@/api/project'
import { getTaskNamesApi } from '@/api/task'
import RootDomain from './components/RootDomain.vue'
import APP from './components/APP.vue'
import MP from './components/MP.vue'
import IP from './components/IP.vue'
import AssetOverviewCards from '@/views/AssetInsight/components/AssetOverviewCards.vue'
const { t } = useI18n()
interface Project {
  value: string
  label: string
  children?: Project[]
}
const projectList = reactive<Project[]>([])
const getProjectList = async () => {
  const res = await getProjectAllApi()
  res.data.list.forEach((item: Project) => {
    projectList.push(item)
  })
}
getProjectList()

interface TaskNameData {
  id: string
  name: string
}
const taskList = reactive<TaskNameData[]>([])
const getTaskList = async () => {
  const res = await getTaskNamesApi()
  taskList.splice(0, taskList.length, ...(res.data || []))
}
getTaskList()

// Tab 改为受控 + 支持 query 指定（?tab=subdomain&search=task=="x"）。
// 子组件用 sessionStorage('asset:<tab>:search') 记忆搜索词，父组件 setup 先写进去，
// 子组件挂载时自己读到并触发首查——不用逐个改 props。
const route = useRoute()
const router = useRouter()
const TAB_KEYS: Record<string, string> = {
  asset: 'assetinfo2', ip: 'ip', rootdomain: 'rootdomain', subdomain: 'subdomain',
  takeover: 'subdomaintakeover', app: 'app', mp: 'mp', url: 'url', crawler: 'crawler',
  sensitive: 'sensitiveinformation', dirscan: 'dirscan', vul: 'vul', pagemonitoring: 'pagemonitoring'
}
const activeTab = ref<string>((route.query.tab as string) || 'asset')
const initialSearch = route.query.search as string
if (initialSearch && TAB_KEYS[activeTab.value]) {
  sessionStorage.setItem('asset:' + TAB_KEYS[activeTab.value] + ':search', initialSearch)
}
const onTabChange = () => {
  // Tab 变化时清掉 query，避免刷新后又跳回旧 Tab
  if (route.query.tab) {
    router.replace({ path: route.path, query: {} })
  }
}

const handleTabClick = (tab: any) => {
  if (tab.paneName === 'map') {
    // 跳转到同一网站的map项目
    window.location.href = '/map'
  }
}
</script>

<template>
  <!-- 资产族数量总览：一行统计卡片（asset/subdomain/url/app/mp/vulnerability/sensitive/cert/cip） -->
  <AssetOverviewCards />
  <ElTabs v-model="activeTab" type="border-card" @tab-click="handleTabClick" @tab-change="onTabChange">
    <ElTabPane lazy name="asset" :label="t('asset.assetName')"
      ><AssetInfo2 :projectList="projectList" :taskList="taskList"
    /></ElTabPane>
    <ElTabPane lazy name="ip" :label="t('asset.IP')"
      ><IP :projectList="projectList" :taskList="taskList"
    /></ElTabPane>
    <ElTabPane lazy name="rootdomain" :label="t('rootDomain.rootDomainName')">
      <RootDomain :projectList="projectList" :taskList="taskList" />
    </ElTabPane>
    <ElTabPane lazy name="subdomain" :label="t('subdomain.subdomainName')">
      <Subdomain :projectList="projectList" :taskList="taskList" />
    </ElTabPane>
    <ElTabPane lazy name="takeover" :label="t('task.subdomainTakeover')">
      <SubdomainTakeover :projectList="projectList" :taskList="taskList" />
    </ElTabPane>
    <ElTabPane lazy name="app" :label="t('app.appName')">
      <APP :projectList="projectList" :taskList="taskList" />
    </ElTabPane>
    <ElTabPane lazy name="mp" :label="t('miniProgram.miniProgramName')">
      <MP :projectList="projectList" :taskList="taskList" />
    </ElTabPane>
    <ElTabPane lazy name="url" :label="t('URL.URLName')"
      ><URL :projectList="projectList" :taskList="taskList"
    /></ElTabPane>
    <ElTabPane lazy name="crawler" :label="t('crawler.crawlerName')"
      ><Crawler :projectList="projectList" :taskList="taskList"
    /></ElTabPane>
    <ElTabPane lazy name="sensitive" :label="t('sensitiveInformation.sensitiveInformationName')">
      <SensitiveInformation :projectList="projectList" :taskList="taskList" />
    </ElTabPane>
    <ElTabPane lazy name="dirscan" :label="t('dirScan.dirScanName')"
      ><DirScan :projectList="projectList" :taskList="taskList"
    /></ElTabPane>
    <ElTabPane lazy name="vul" :label="t('vulnerability.vulnerabilityName')">
      <vul :projectList="projectList" :taskList="taskList" />
    </ElTabPane>
    <ElTabPane lazy name="pagemonitoring" :label="t('PageMonitoring.pageMonitoringName')">
      <PageMonitoring :projectList="projectList" :taskList="taskList" />
    </ElTabPane>
    <ElTabPane lazy :label="t('localization.map')" name="map" />
  </ElTabs>
</template>
