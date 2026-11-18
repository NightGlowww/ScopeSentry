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
  ElAlert,
  ElTag,
  ElTable,
  ElTableColumn
} from 'element-plus'
import {
  getThreatConfigApi,
  updateThreatConfigApi,
  runThreatOnceApi,
  getGithubTokenStatusApi,
  getCveHistoryApi,
  listThreatTargetsApi,
  addThreatTargetsApi,
  removeThreatTargetsApi,
  type ThreatKind,
  type ThreatHistoryItem
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

// 监控目标（黑客 / 工具仓库）
const targets = reactive<Record<string, string[]>>({ hackers: [], tools: [] })
const targetInput = reactive<Record<string, string>>({ hackers: '', tools: '' })

const loadTargets = async (kind: 'hackers' | 'tools') => {
  const res = await listThreatTargetsApi(kind)
  targets[kind] = res.data.items || []
}

const addTarget = async (kind: 'hackers' | 'tools') => {
  const v = (targetInput[kind] || '').trim()
  if (!v) return
  const res = await addThreatTargetsApi(kind, [v])
  targets[kind] = res.data.items || []
  targetInput[kind] = ''
}

const removeTarget = async (kind: 'hackers' | 'tools', value: string) => {
  const res = await removeThreatTargetsApi(kind, [value])
  targets[kind] = res.data.items || []
}

// CVE 情报历史
const cveHistory = ref<ThreatHistoryItem[]>([])
const cveLoading = ref(false)
const cveKeyword = ref('')

const loadCveHistory = async () => {
  cveLoading.value = true
  try {
    const res = await getCveHistoryApi({ keyword: cveKeyword.value || undefined, limit: 100 })
    cveHistory.value = res.data.list || []
  } finally {
    cveLoading.value = false
  }
}

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
  loadTargets('hackers')
  loadTargets('tools')
  loadCveHistory()
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

  <ElRow :gutter="12" class="mb-12px">
    <ElCol v-for="item in [{ kind: 'hackers', title: '黑客监控目标' }, { kind: 'tools', title: '工具仓库监控目标' }]" :key="item.kind" :span="12">
      <ElCard shadow="never">
        <template #header><span>{{ item.title }}（{{ targets[item.kind].length }}）</span></template>
        <div class="target-add">
          <ElInput
            v-model="targetInput[item.kind]"
            :placeholder="item.kind === 'hackers' ? 'GitHub 用户名，如 @foo' : 'owner/repo'"
            @keyup.enter="addTarget(item.kind)"
          />
          <ElButton type="primary" @click="addTarget(item.kind)">添加</ElButton>
        </div>
        <div class="target-list">
          <ElTag
            v-for="t in targets[item.kind]"
            :key="t"
            closable
            class="target-tag"
            @close="removeTarget(item.kind, t)"
          >
            {{ t }}
          </ElTag>
          <span v-if="!targets[item.kind].length" class="hint">暂无目标</span>
        </div>
      </ElCard>
    </ElCol>
  </ElRow>

  <ElCard shadow="never">
    <template #header>
      <div class="flex items-center justify-between">
        <span>CVE 情报历史（{{ cveHistory.length }}）</span>
        <div class="history-actions">
          <ElInput v-model="cveKeyword" placeholder="关键字" clearable style="width: 200px" />
          <ElButton type="primary" @click="loadCveHistory">查询</ElButton>
        </div>
      </div>
    </template>
    <ElTable :data="cveHistory" v-loading="cveLoading" stripe border max-height="420">
      <ElTableColumn prop="discoveredAt" label="发现时间" width="170" />
      <ElTableColumn prop="key" label="编号" width="150" />
      <ElTableColumn label="标题" min-width="260">
        <template #default="scope">
          <a :href="scope.row.url" target="_blank" class="link">{{ scope.row.title || scope.row.url }}</a>
        </template>
      </ElTableColumn>
      <ElTableColumn prop="severity" label="严重度" width="100" />
      <ElTableColumn prop="repo" label="仓库" min-width="160" />
      <ElTableColumn prop="keyword" label="命中关键词" width="140" />
    </ElTable>
  </ElCard>
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
.target-add {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
}
.target-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.target-tag {
  margin: 0;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.history-actions {
  display: flex;
  gap: 8px;
}
.link {
  color: var(--el-color-primary);
  text-decoration: none;
}
</style>
