<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from '@/hooks/web/useI18n'
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

const { t } = useI18n('github')

const kinds: { kind: ThreatKind; title: string }[] = [
  { kind: 'cve', title: t('kindCve') },
  { kind: 'hackers', title: t('kindHackers') },
  { kind: 'tools', title: t('kindTools') }
]

// 监控目标卡片：kind 必须是字面量联合，模板里才不会退化成 string
const targetKinds: { kind: 'hackers' | 'tools'; title: string }[] = [
  { kind: 'hackers', title: t('targetHackersTitle') },
  { kind: 'tools', title: t('targetToolsTitle') }
]

const sources = reactive<
  Record<string, { enabled: boolean; keywords: string[]; intervalHours: number; tokenRef: string }>
>({})
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
  } catch (e: any) {
    ElMessage.error(e?.message || t('cveHistoryLoadFailed'))
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
    ElMessage.success(t('saveSuccess'))
  } catch (e: any) {
    ElMessage.error(e?.message || t('saveFailed'))
  } finally {
    saving[kind] = false
  }
}

const runOnce = async (kind: ThreatKind) => {
  running[kind] = true
  try {
    await runThreatOnceApi(kind)
    ElMessage.success(t('runTriggered'))
  } catch (e: any) {
    ElMessage.error(e?.message || t('runFailed'))
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
    :type="
      tokenStatus.status === 'valid'
        ? 'success'
        : tokenStatus.status === 'missing'
          ? 'warning'
          : 'error'
    "
    :title="
      t('tokenStatusTitle', { status: tokenStatus.status }) +
      (tokenStatus.login ? t('tokenStatusLogin', { login: tokenStatus.login }) : '') +
      (tokenStatus.remaining !== undefined
        ? t('tokenStatusRemaining', { remaining: tokenStatus.remaining })
        : '')
    "
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
            <span class="label">{{ t('formKeywords') }}</span>
            <ElSelect
              v-model="sources[item.kind].keywords"
              multiple
              filterable
              allow-create
              default-first-option
              :placeholder="t('phOptional')"
              style="width: 100%"
            />
          </div>
          <div class="field">
            <span class="label">{{ t('formIntervalHours') }}</span>
            <ElInputNumber v-model="sources[item.kind].intervalHours" :min="1" :max="168" />
          </div>
          <div class="field">
            <span class="label">{{ t('formTokenRef') }}</span>
            <ElInput v-model="sources[item.kind].tokenRef" :placeholder="t('phTokenRef')" />
          </div>
          <div class="actions">
            <ElButton type="primary" :loading="saving[item.kind]" @click="save(item.kind)">
              {{ t('btnSave') }}
            </ElButton>
            <ElButton type="success" :loading="running[item.kind]" @click="runOnce(item.kind)">
              {{ t('btnRunOnce') }}
            </ElButton>
          </div>
        </div>
      </ElCard>
    </ElCol>
  </ElRow>

  <ElRow :gutter="12" class="mb-12px">
    <ElCol v-for="item in targetKinds" :key="item.kind" :span="12">
      <ElCard shadow="never">
        <template #header>
          <span>{{ item.title }}{{ t('parenCount', { count: targets[item.kind].length }) }}</span>
        </template>
        <div class="target-add">
          <ElInput
            v-model="targetInput[item.kind]"
            :placeholder="item.kind === 'hackers' ? t('phHackerTarget') : 'owner/repo'"
            @keyup.enter="addTarget(item.kind)"
          />
          <ElButton type="primary" @click="addTarget(item.kind)">{{ t('btnAdd') }}</ElButton>
        </div>
        <div class="target-list">
          <!-- disable-transitions 不是装饰：这些标签挂载时所在的 tab 是 display:none，
               进场过渡的 rAF 在隐藏面板里不会跑，el-zoom-in-center-enter-from 会永久留在
               元素上（scaleX(0) + opacity 0）—— 结果就是「已有目标一个都看不见，还点不到 ×」
               （2026-11-29 实测：切到威胁情报页，10 个标签全是 0 宽不可见）。 -->
          <ElTag
            v-for="tag in targets[item.kind]"
            :key="tag"
            closable
            disable-transitions
            class="target-tag"
            @close="removeTarget(item.kind, tag)"
          >
            {{ tag }}
          </ElTag>
          <span v-if="!targets[item.kind].length" class="hint">{{ t('noTargets') }}</span>
        </div>
      </ElCard>
    </ElCol>
  </ElRow>

  <ElCard shadow="never">
    <template #header>
      <div class="flex items-center justify-between">
        <span>{{ t('cveHistoryTitle') }}{{ t('parenCount', { count: cveHistory.length }) }}</span>
        <div class="history-actions">
          <ElInput
            v-model="cveKeyword"
            :placeholder="t('phKeyword')"
            clearable
            style="width: 200px"
          />
          <ElButton type="primary" @click="loadCveHistory">{{ t('btnQuery') }}</ElButton>
        </div>
      </div>
    </template>
    <ElTable :data="cveHistory" v-loading="cveLoading" stripe border max-height="420">
      <ElTableColumn prop="discoveredAt" :label="t('colDiscoveredAt')" width="170" />
      <ElTableColumn prop="key" :label="t('colKey')" width="150" />
      <ElTableColumn :label="t('colTitle')" min-width="260">
        <template #default="scope">
          <a :href="scope.row.url" target="_blank" class="link">{{
            scope.row.title || scope.row.url
          }}</a>
        </template>
      </ElTableColumn>
      <ElTableColumn prop="severity" :label="t('colSeverity')" width="100" />
      <ElTableColumn prop="repo" :label="t('colRepo')" min-width="160" />
      <ElTableColumn prop="keyword" :label="t('colKeyword')" width="140" />
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
