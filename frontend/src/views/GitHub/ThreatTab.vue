<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import {
  ElButton,
  ElCard,
  ElCol,
  ElRow,
  ElSelect,
  ElSwitch,
  ElInputNumber,
  ElInput,
  ElMessage,
  ElAlert
} from 'element-plus'
import {
  getThreatConfigApi,
  updateThreatConfigApi,
  runThreatOnceApi,
  getGithubTokenStatusApi,
  type ThreatKind
} from '@/api/github'

const kinds: { kind: ThreatKind; title: string }[] = [
  { kind: 'cve', title: 'CVE 情报' },
  { kind: 'hackers', title: '黑客情报' },
  { kind: 'tools', title: '工具情报' }
]

const sources = reactive<Record<string, { enabled: boolean; keywords: string[]; intervalHours: number; tokenRef: string }>>({})
const saving = reactive<Record<string, boolean>>({})
const running = reactive<Record<string, boolean>>({})
const tokenStatus = ref<{ status: string; remaining?: number; login?: string } | null>(null)

const loadToken = async () => {
  try {
    const res = await getGithubTokenStatusApi()
    tokenStatus.value = res.data
  } catch {
    tokenStatus.value = null
  }
}

const loadOne = async (kind: ThreatKind) => {
  const res = await getThreatConfigApi(kind)
  const cfg = res.data.config || ({} as any)
  sources[kind] = {
    enabled: !!cfg.enabled,
    keywords: cfg.keywords || [],
    intervalHours: cfg.intervalHours || 6,
    tokenRef: cfg.tokenRef || ''
  }
}

const save = async (kind: ThreatKind) => {
  saving[kind] = true
  try {
    await updateThreatConfigApi(kind, {
      enabled: sources[kind].enabled,
      keywords: sources[kind].keywords,
      intervalHours: sources[kind].intervalHours,
      tokenRef: sources[kind].tokenRef
    })
    ElMessage.success('已保存')
  } finally {
    saving[kind] = false
  }
}

const runOnce = async (kind: ThreatKind) => {
  running[kind] = true
  try {
    await runThreatOnceApi(kind)
    ElMessage.success('已触发一次采集')
  } finally {
    running[kind] = false
  }
}

onMounted(async () => {
  await Promise.all([loadOne('cve'), loadOne('hackers'), loadOne('tools')])
  loadToken()
})
</script>

<template>
  <ElAlert
    v-if="tokenStatus"
    class="mb-10px"
    :closable="false"
    show-icon
    :type="tokenStatus.status === 'valid' ? 'success' : tokenStatus.status === 'missing' ? 'warning' : 'error'"
    :title="'GitHub Token 状态：' + tokenStatus.status + (tokenStatus.login ? '（' + tokenStatus.login + '）' : '') + (tokenStatus.remaining !== undefined ? '，剩余额度 ' + tokenStatus.remaining : '')"
  />

  <ElRow :gutter="12">
    <ElCol v-for="item in kinds" :key="item.kind" :span="8">
      <ElCard shadow="never" class="mb-12px">
        <template #header>
          <div class="flex items-center justify-between">
            <span>{{ item.title }}</span>
            <ElSwitch v-if="sources[item.kind]" v-model="sources[item.kind].enabled" />
          </div>
        </template>

        <div v-if="sources[item.kind]" class="threat-body">
          <div class="field">
            <span class="label">关键词</span>
            <ElSelect
              v-model="sources[item.kind].keywords"
              multiple
              filterable
              allow-create
              default-first-option
              placeholder="可留空"
              style="width: 100%"
            />
          </div>
          <div class="field">
            <span class="label">间隔(小时)</span>
            <ElInputNumber v-model="sources[item.kind].intervalHours" :min="1" :max="168" />
          </div>
          <div class="field">
            <span class="label">Token 引用</span>
            <ElInput v-model="sources[item.kind].tokenRef" placeholder="系统配置中的 name，可留空用环境变量" />
          </div>
          <div class="actions">
            <ElButton type="primary" :loading="saving[item.kind]" @click="save(item.kind)">保存</ElButton>
            <ElButton type="success" :loading="running[item.kind]" @click="runOnce(item.kind)">立即运行一次</ElButton>
          </div>
        </div>
      </ElCard>
    </ElCol>
  </ElRow>
</template>

<style scoped>
.mb-10px {
  margin-bottom: 10px;
}
.mb-12px {
  margin-bottom: 12px;
}
.threat-body .field {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}
.threat-body .label {
  flex: 0 0 72px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.actions {
  display: flex;
  gap: 8px;
}
</style>
