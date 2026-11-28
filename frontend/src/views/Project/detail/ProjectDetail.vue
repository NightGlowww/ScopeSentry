<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from '@/hooks/web/useI18n'
import {
  ElTabs,
  ElTabPane,
  ElTag,
  ElButton,
  ElDialog,
  ElMessage,
  ElMessageBox,
  ElPopover,
} from 'element-plus'
import { Edit, Delete } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import AddProject from '../components/AddProject.vue'
import { deleteProjectApi } from '@/api/project'
import Dashboard from './components/Dashboard.vue'
import Subdomain from './components/Subdomain.vue'
import Port from './components/Port.vue'
import Service from './components/Service.vue'
import Tasks from './components/Tasks.vue'
import Vulns from './components/Vulns.vue'
import { getProjectInfoApi, getProjectAssetCountApi } from '@/api/ProjectAggregation'
const { t } = useI18n()
const { t: tp } = useI18n('projectDetail')
// 页面 id 从 URL query 取（ProjectList.vue: push('/project-management/project-detail?id=xxx')），
// 各 Tab 组件各自 useRoute().query.id 组装请求。
import { useRoute } from 'vue-router'
const { query } = useRoute()
const projectId = String(query.id || '')

// 项目头：之前进详情页没有任何「这是哪个项目」的标识，
// 只有一排页签，用户不知道自己站在哪儿（2026-11-27 反馈：项目管理像残废）。
const info = ref<any>(null)
const counts = ref<any>(null)
// 4 位以上数字上分隔符：45363 → 45,363，扫一眼就能比大小
const fmtNum = (n?: number) => (n ?? 0).toLocaleString()
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
      tp('deleteConfirm', { name: info.value?.name || projectId }),
      tp('deleteTitle'),
      { type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await deleteProjectApi([projectId], false)
    ElMessage.success(tp('deleteSuccess'))
    router.push('/project-management/index')
  } catch (e: any) {
    ElMessage.error(e?.message || tp('deleteFailed'))
  }
}
onMounted(refresh)
</script>

<template>
  <div>
    <div v-if="info" class="project-head">
      <!-- 头部只有两行：第一行「项目是谁 + 能做什么」，第二行「扫哪儿」，第三行数字条。
           之前编辑/删除按钮被挤到第三行左侧、删除还是实心红框，头部像三块补丁（2026-11-28 反馈）。 -->
      <div class="head-top">
        <div class="head-title">
          <span class="pname">{{ info.name }}</span>
          <ElTag v-if="info.tag" size="small" effect="plain" class="tag-src">{{ info.tag }}</ElTag>
          <ElTag v-if="info.scheduledTasks" size="small" type="success" effect="plain">
            {{ tp('scheduledTag', { hour: info.hour }) }}
          </ElTag>
          <ElTag v-else size="small" type="info" effect="plain">{{ tp('manual') }}</ElTag>
          <span v-if="info.scheduledTasks && info.next_time" class="next-time">
            {{ tp('nextRun', { time: info.next_time }) }}
          </span>
        </div>
        <div class="head-actions">
          <ElButton size="small" :icon="Edit" @click="editVisible = true">{{
            tp('editProject')
          }}</ElButton>
          <ElButton
            size="small"
            type="danger"
            text
            :icon="Delete"
            class="btn-del"
            @click="onDelete"
          >
            {{ tp('deleteProject') }}
          </ElButton>
        </div>
      </div>
      <!-- 扫描范围最多几十个域名，全铺开会把头部撑成一堵墙：给 4 个 + 「+N」，全量进浮层。 -->
      <div class="head-scope">
        <span class="scope-label">{{ tp('scopeLabel') }}</span>
        <span class="scope-count">{{
          tp('rootDomainCount', { n: (info.root_domains || []).length })
        }}</span>
        <template v-if="(info.root_domains || []).length">
          <ElTag
            v-for="d in (info.root_domains || []).slice(0, 4)"
            :key="d"
            size="small"
            effect="plain"
            class="scope-tag"
          >
            {{ d }}
          </ElTag>
          <ElPopover
            v-if="(info.root_domains || []).length > 4"
            placement="bottom-start"
            :width="460"
            trigger="click"
          >
            <template #reference>
              <ElButton link type="primary" size="small" class="scope-more">
                {{ tp('moreCount', { n: (info.root_domains || []).length - 4 }) }}
              </ElButton>
            </template>
            <div class="scope-pop">
              <div v-for="d in info.root_domains" :key="d">{{ d }}</div>
            </div>
          </ElPopover>
        </template>
        <span v-else class="muted">{{ tp('scopeEmpty') }}</span>
      </div>
      <div v-if="counts" class="head-stats">
        <div class="stat">
          <b>{{ fmtNum(counts.assetCount) }}</b><span>{{ tp('statAsset') }}</span>
        </div>
        <div class="stat">
          <b>{{ fmtNum(counts.subdomainCount) }}</b><span>{{ tp('statSubdomain') }}</span>
        </div>
        <div class="stat">
          <b>{{ fmtNum(counts.urlCount) }}</b><span>{{ tp('statUrl') }}</span>
        </div>
        <div class="stat">
          <b :class="{ hot: (counts.vulCount ?? 0) > 0 }">{{ fmtNum(counts.vulCount) }}</b><span>{{
            tp('statVuln')
          }}</span>
        </div>
        <div class="stat">
          <b>{{ fmtNum(counts.sensitiveCount) }}</b><span>{{ tp('statSensitive') }}</span>
        </div>
      </div>
    </div>
    <!-- 编辑复用列表页那个表单，避免两处表单逻辑漂移 -->
    <ElDialog v-model="editVisible" :title="tp('editProject')" width="640px">
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
      <!-- 漏洞页签：项目页要能直接回答「这个项目有哪些洞」（2026-11-27 反馈） -->
      <ElTabPane :label="t('assetOverview.vulnerability')"><Vulns /></ElTabPane>
      <!-- 任务页签：项目不能只是资产聚合，它得能回答「这个项目跑过哪些任务、结果如何」 -->
      <ElTabPane :label="tp('tasksTab')"><Tasks /></ElTabPane>
    </ElTabs>
  </div>
</template>

<style scoped>
.project-head {
  padding: 16px 20px 14px;
  margin-bottom: 12px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
}
/* 标题与操作同一行、基线对齐：按钮不再被挤到下面单独一行 */
.head-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.head-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex-wrap: wrap;
}
.pname {
  font-size: 19px;
  font-weight: 700;
  letter-spacing: 0.2px;
  max-width: 640px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tag-src {
  font-weight: 500;
}
.next-time {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}
.head-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: none;
}
/* 删除是不可逆操作，用文字按钮，不做实心红框抢主视觉 */
.btn-del {
  padding: 5px 8px;
}
.btn-del:hover {
  background: var(--el-color-danger-light-9);
}

.head-scope {
  margin-top: 10px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}
.scope-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
.scope-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-right: 2px;
}
.scope-tag {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
.scope-more {
  font-size: 12px;
  height: auto;
}
.scope-pop {
  max-height: 260px;
  overflow: auto;
  font-size: 12px;
  line-height: 1.9;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.muted {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

/* 数字条：一条分隔线 + 竖线分栏，横向读，别再竖排右对齐 */
.head-stats {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
}
.stat {
  display: flex;
  align-items: baseline;
  gap: 6px;
  padding: 0 20px;
  border-left: 1px solid var(--el-border-color-lighter);
}
.stat:first-child {
  padding-left: 0;
  border-left: none;
}
.stat b {
  font-size: 17px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1.2;
}
.stat b.hot {
  color: var(--el-color-danger);
}
.stat span {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
