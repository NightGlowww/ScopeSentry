<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from '@/hooks/web/useI18n'
import { ElTabs, ElTabPane, ElTag, ElButton, ElDialog, ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import AddProject from '../components/AddProject.vue'
import { deleteProjectApi } from '@/api/project'
import Dashboard from './components/Dashboard.vue'
import Subdomain from './components/Subdomain.vue'
import Port from './components/Port.vue'
import Service from './components/Service.vue'
import Tasks from './components/Tasks.vue'
import { getProjectInfoApi, getProjectAssetCountApi } from '@/api/ProjectAggregation'
const { t } = useI18n()
// 页面 id 从 URL query 取（ProjectList.vue: push('/project-management/project-detail?id=xxx')），
// 各 Tab 组件各自 useRoute().query.id 组装请求。
import { useRoute } from 'vue-router'
const { query } = useRoute()
const projectId = String(query.id || '')

// 项目头：之前进详情页没有任何「这是哪个项目」的标识，
// 只有一排页签，用户不知道自己站在哪儿（2026-11-27 反馈：项目管理像残废）。
const info = ref<any>(null)
const counts = ref<any>(null)
const router = useRouter()
const editVisible = ref(false)
const refresh = async () => {
  if (!projectId) return
  try {
    const [i, c] = await Promise.all([
      getProjectInfoApi(projectId),
      getProjectAssetCountApi(projectId)
    ])
    info.value = i.data
    counts.value = c.data
  } catch (e) {
    // 头信息失败不该阻断页签，静默即可
  }
}
const closeEdit = () => {
  editVisible.value = false
  refresh()
}
const onDelete = async () => {
  try {
    await ElMessageBox.confirm(
      `删除项目「${info.value?.name || projectId}」？项目下的任务不会被删除，但会失去归属。`,
      '删除项目',
      { type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await deleteProjectApi([projectId], false)
    ElMessage.success('项目已删除')
    router.push('/project-management/index')
  } catch (e: any) {
    ElMessage.error(e?.message || '删除失败')
  }
}
onMounted(refresh)
</script>

<template>
  <div>
    <div v-if="info" class="project-head">
      <div class="head-main">
        <div class="head-title">
          <span class="pname">{{ info.name }}</span>
          <ElTag v-if="info.tag" size="small" effect="plain">{{ info.tag }}</ElTag>
          <ElTag v-if="info.scheduledTasks" size="small" type="success" effect="plain">
            定时 {{ info.hour }}h
          </ElTag>
          <ElTag v-else size="small" type="info" effect="plain">手动</ElTag>
        </div>
        <div class="head-scope">
          扫描范围：
          <span v-if="(info.root_domains || []).length">
            {{ (info.root_domains || []).join(' / ') }}
          </span>
          <span v-else class="muted">未设置根域名</span>
        </div>
        <div class="head-actions">
          <ElButton size="small" @click="editVisible = true">编辑项目</ElButton>
          <ElButton size="small" type="danger" plain @click="onDelete">删除项目</ElButton>
        </div>
      </div>
      <div v-if="counts" class="head-stats">
        <div class="stat"><b>{{ counts.assetCount ?? 0 }}</b><span>资产</span></div>
        <div class="stat"><b>{{ counts.subdomainCount ?? 0 }}</b><span>子域名</span></div>
        <div class="stat"><b>{{ counts.urlCount ?? 0 }}</b><span>URL</span></div>
        <div class="stat"><b>{{ counts.vulCount ?? 0 }}</b><span>漏洞</span></div>
      </div>
    </div>
    <!-- 编辑复用列表页那个表单，避免两处表单逻辑漂移 -->
    <ElDialog v-model="editVisible" title="编辑项目" width="640px">
      <AddProject
        v-if="editVisible"
        :closeDialog="closeEdit"
        :projectid="projectId"
        :getProjectData="() => {}"
        :schedule="true"
      />
    </ElDialog>
    <ElTabs type="border-card">
      <ElTabPane :label="t('project.overview')"><Dashboard /></ElTabPane>
      <ElTabPane :label="t('subdomain.subdomainName')"><Subdomain /></ElTabPane>
      <ElTabPane :label="t('asset.port')"> <Port /></ElTabPane>
      <ElTabPane :label="t('asset.service')"><Service /></ElTabPane>
      <!-- 任务页签：项目不能只是资产聚合，它得能回答「这个项目跑过哪些任务、结果如何」 -->
      <ElTabPane :label="t('task.projectTasks')"><Tasks /></ElTabPane>
    </ElTabs>
  </div>
</template>

<style scoped>
.project-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 14px 18px;
  margin-bottom: 12px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  flex-wrap: wrap;
}
.head-title { display: flex; align-items: center; gap: 8px; }
.pname { font-size: 18px; font-weight: 700; }
.head-scope { margin-top: 6px; font-size: 12px; color: var(--el-text-color-secondary); }
.muted { color: var(--el-text-color-placeholder); }
.head-actions { display: flex; gap: 8px; }
.head-stats { display: flex; gap: 24px; }
.stat { display: flex; flex-direction: column; align-items: flex-end; line-height: 1.2; }
.stat b { font-size: 18px; }
.stat span { font-size: 11px; color: var(--el-text-color-secondary); }
</style>
