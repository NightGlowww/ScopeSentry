<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ElButton, ElCard, ElCol, ElRow, ElTag, ElDescriptions, ElDescriptionsItem } from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { getConsoleInfoApi, type ConsoleInfo } from '@/api/dashboard'

const info = ref<ConsoleInfo | null>(null)
const loading = ref(false)
const error = ref('')
let timer: number | undefined

const load = async () => {
  loading.value = true
  try {
    const res = await getConsoleInfoApi()
    info.value = res.data
    error.value = ''
  } catch (e: any) {
    error.value = e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

const depTag = (dep?: { connected: boolean; latencyMs: number; error?: string }) => {
  if (!dep) return { type: 'info' as const, text: '-' }
  if (!dep.connected) return { type: 'danger' as const, text: '断连 ' + (dep.error || '') }
  return { type: 'success' as const, text: '正常 ' + dep.latencyMs.toFixed(1) + 'ms' }
}

onMounted(() => {
  load()
  timer = window.setInterval(load, 15000)
})
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<template>
  <ContentWrap>
    <ElRow :gutter="12" class="mb-12px">
      <ElCol :span="18">
        <span class="hint">每 15 秒自动刷新{{ info ? '，数据采集于 ' + info.collectedAt : '' }}</span>
        <span v-if="error" class="err">{{ error }}</span>
      </ElCol>
      <ElCol :span="6" style="text-align: right">
        <ElButton type="primary" :loading="loading" @click="load">刷新</ElButton>
      </ElCol>
    </ElRow>

    <ElRow :gutter="12" class="mb-12px">
      <ElCol :span="6">
        <ElCard shadow="never">
          <div class="stat-label">服务版本</div>
          <div class="stat-value">{{ info?.version || '-' }}</div>
        </ElCard>
      </ElCol>
      <ElCol :span="6">
        <ElCard shadow="never">
          <div class="stat-label">运行时长</div>
          <div class="stat-value">{{ info?.uptimeHuman || '-' }}</div>
        </ElCard>
      </ElCol>
      <ElCol :span="6">
        <ElCard shadow="never">
          <div class="stat-label">节点（在线/总数）</div>
          <div class="stat-value">{{ info ? info.nodeOnline + ' / ' + info.nodeCount : '-' }}</div>
        </ElCard>
      </ElCol>
      <ElCol :span="6">
        <ElCard shadow="never">
          <div class="stat-label">任务（运行/总数）</div>
          <div class="stat-value">{{ info ? info.taskRunning + ' / ' + info.taskCount : '-' }}</div>
        </ElCard>
      </ElCol>
    </ElRow>

    <ElCard shadow="never">
      <template #header><span>运行信息</span></template>
      <ElDescriptions :column="2" border>
        <ElDescriptionsItem label="启动时间">{{ info?.startTime || '-' }}</ElDescriptionsItem>
        <ElDescriptionsItem label="时区">{{ info?.timezone || '-' }}</ElDescriptionsItem>
        <ElDescriptionsItem label="MongoDB">
          <ElTag :type="depTag(info?.mongo).type" effect="light">{{ depTag(info?.mongo).text }}</ElTag>
        </ElDescriptionsItem>
        <ElDescriptionsItem label="Redis">
          <ElTag :type="depTag(info?.redis).type" effect="light">{{ depTag(info?.redis).text }}</ElTag>
        </ElDescriptionsItem>
        <ElDescriptionsItem label="运行秒数">{{ info?.uptimeSeconds ?? '-' }}</ElDescriptionsItem>
        <ElDescriptionsItem label="采集时间">{{ info?.collectedAt || '-' }}</ElDescriptionsItem>
      </ElDescriptions>
    </ElCard>
  </ContentWrap>
</template>

<style scoped>
.mb-12px {
  margin-bottom: 12px;
}
.stat-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-bottom: 6px;
}
.stat-value {
  font-size: 22px;
  font-weight: 600;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.err {
  margin-left: 12px;
  color: var(--el-color-danger);
  font-size: 12px;
}
</style>
