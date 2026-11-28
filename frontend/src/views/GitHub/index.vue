<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { onMounted, reactive, ref } from 'vue'
import {
  ElButton,
  ElCol,
  ElRow,
  ElInput,
  ElSelect,
  ElOption,
  ElTabs,
  ElTabPane,
  ElTag,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInputNumber,
  ElMessage,
  ElMessageBox,
  ElLink,
  ElDrawer,
  ElTable,
  ElTableColumn,
  ElCheckbox
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { BaseButton } from '@/components/Button'
import {
  listGithubTasksApi,
  createGithubTaskApi,
  stopGithubTasksApi,
  deleteGithubTasksApi,
  listGithubResultsApi,
  getResultOverviewApi,
  triageGithubResultsApi,
  listIgnoredReposApi,
  addIgnoredRepoApi,
  removeIgnoredRepoApi,
  type IgnoredRepo
} from '@/api/github'
import SchedulerTab from './SchedulerTab.vue'
import ThreatTab from './ThreatTab.vue'
import { formatTime } from '@/utils'

const { t } = useI18n('github')

const activeTab = ref('task')

const taskStateMap: Record<number, { text: string; type: string }> = {
  0: { text: t('statePending'), type: 'info' },
  1: { text: t('stateRunning'), type: 'success' },
  2: { text: t('statePaused'), type: 'warning' },
  3: { text: t('stateFinished'), type: 'primary' }
}

const typeOptions = [
  { value: 'code_leak', label: t('typeCodeLeak') },
  { value: 'cve', label: 'CVE' },
  { value: 'hacker', label: t('typeHacker') }
]

// 总览条与表格共用同一套类型标签：两处写法不一致时，同一个 code_leak 会出现两种叫法。
const typeLabel = (v?: string) => typeOptions.find((opt) => opt.value === v)?.label || v || ''

const taskFilters = reactive({ keyword: '', type: '', state: '' as string | number })
const resultFilters = reactive({ keyword: '', type: '', status: '', project: '' })

const fmt = (v?: string) => (v ? formatTime(v, 'yyyy-MM-dd HH:mm:ss') : '-')

// ---------------- 任务 ----------------
const taskColumns = reactive<TableColumn[]>([
  { field: 'name', label: t('colName'), minWidth: 160 },
  {
    field: 'type',
    label: t('colType'),
    width: 100,
    formatter: (_r, __: TableColumn, v: string) =>
      typeOptions.find((opt) => opt.value === v)?.label || v
  },
  { field: 'query', label: t('colQuery'), minWidth: 200 },
  {
    field: 'state',
    label: t('colState'),
    // 122：'Completed' 实测 117px，100 会裁成一个省略号
    width: 122,
    formatter: (_r, __: TableColumn, v: number) => (
      <ElTag type={(taskStateMap[v]?.type as any) || 'info'} effect="light">
        {taskStateMap[v]?.text || v}
      </ElTag>
    )
  },
  {
    field: 'stats',
    label: t('colStats'),
    width: 130,
    formatter: (_r, __: TableColumn, v?: { scanned: number; newFound: number }) =>
      v ? v.scanned + ' / ' + v.newFound : '-'
  },
  {
    field: 'lastRunAt',
    label: t('colLastRun'),
    minWidth: 160,
    formatter: (_r, __: TableColumn, v?: string) => fmt(v)
  },
  {
    field: 'action',
    label: t('colAction'),
    width: 170,
    formatter: (row) => (
      <div class="flex items-center" style={'gap:6px'}>
        <BaseButton
          type="warning"
          onClick={() => handleStopTask(row)}
          disabled={row.state !== 1 && row.state !== 0}
        >
          {t('btnStop')}
        </BaseButton>
        <BaseButton type="danger" onClick={() => handleDeleteTask(row)}>
          {t('btnDelete')}
        </BaseButton>
      </div>
    )
  }
])

const {
  tableRegister: taskRegister,
  tableState: taskState,
  tableMethods: taskMethods
} = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = taskState
    const res = await listGithubTasksApi({
      keyword: taskFilters.keyword || undefined,
      type: taskFilters.type || undefined,
      state: taskFilters.state === '' ? undefined : Number(taskFilters.state),
      pageIndex: currentPage.value,
      pageSize: pageSize.value
    })
    return { list: res.data.list || [], total: res.data.total || 0 }
  }
})
const {
  loading: taskLoading,
  dataList: taskList,
  total: taskTotal,
  currentPage: taskPage,
  pageSize: taskPageSize
} = taskState
taskPageSize.value = 20
const { getList: getTaskList } = taskMethods

const handleStopTask = async (row: { id: string; name: string }) => {
  try {
    const res = (await stopGithubTasksApi([row.id])) as { data?: { updated?: number } }
    // updated === 0 表示后端没改任何任务的状态，此时报“已暂停”是在说谎
    if (res?.data?.updated === 0) {
      ElMessage.warning(t('stopNoop', { name: row.name }))
    } else {
      ElMessage.success(t('stopSuccess', { name: row.name }))
    }
    getTaskList()
  } catch (e: any) {
    // 后端返回的错误已由 axios 拦截器统一提示过，这里只兜没有响应体的失败（超时/断网）
    if (!e?.response) ElMessage.error(e?.message || t('stopFailed'))
  }
}

const handleDeleteTask = async (row: { id: string; name: string }) => {
  try {
    await ElMessageBox.confirm(t('deleteConfirm', { name: row.name }), t('deleteTitle'), {
      confirmButtonText: t('btnDelete'),
      cancelButtonText: t('btnCancel'),
      type: 'warning'
    })
  } catch {
    // 用户点了取消：静默返回，不提示不报错
    return
  }
  try {
    await deleteGithubTasksApi([row.id])
    ElMessage.success(t('deleteSuccess', { name: row.name }))
    getTaskList()
  } catch (e: any) {
    if (!e?.response) ElMessage.error(e?.message || t('deleteFailed'))
  }
}

// ---------------- 新建任务 ----------------
const createVisible = ref(false)
const createLoading = ref(false)
const createForm = reactive({
  name: '',
  type: 'code_leak',
  query: '',
  keywords: [] as string[],
  project: '',
  intervalMinutes: 0,
  maxPages: 5
})

const openCreate = () => {
  createForm.name = ''
  createForm.type = 'code_leak'
  createForm.query = ''
  createForm.keywords = []
  createForm.project = ''
  createForm.intervalMinutes = 0
  createForm.maxPages = 5
  createVisible.value = true
}

const submitCreate = async () => {
  if (!createForm.name.trim() || !createForm.query.trim()) {
    ElMessage.warning(t('nameQueryRequired'))
    return
  }
  createLoading.value = true
  try {
    await createGithubTaskApi({
      name: createForm.name.trim(),
      type: createForm.type,
      query: createForm.query.trim(),
      keywords: createForm.keywords,
      project: createForm.project || undefined,
      intervalMinutes: createForm.intervalMinutes || undefined,
      maxPages: createForm.maxPages || undefined
    })
    createVisible.value = false
    ElMessage.success(t('createSuccess'))
    getTaskList()
  } catch (e: any) {
    // 创建失败此前只关 loading，对话框留在原地不给任何原因
    ElMessage.error(e?.message || t('createFailed'))
  } finally {
    createLoading.value = false
  }
}

// ---------------- 结果 ----------------
const resultColumns = reactive<TableColumn[]>([
  {
    field: 'createdAt',
    label: t('colTime'),
    minWidth: 160,
    formatter: (_r, __: TableColumn, v?: string) => fmt(v)
  },
  { field: 'repo', label: t('colRepo'), minWidth: 180 },
  {
    field: 'path',
    label: t('colPath'),
    minWidth: 200,
    formatter: (row) =>
      row.url ? (
        <ElLink href={row.url} target="_blank" type="primary">
          {row.path}
        </ElLink>
      ) : (
        row.path
      )
  },
  { field: 'title', label: t('colTitle'), minWidth: 200 },
  { field: 'severity', label: t('colSeverity'), width: 100 },
  {
    field: 'status',
    label: t('colState'),
    width: 100,
    formatter: (_r, __: TableColumn, v: string) => (
      <ElTag
        type={v === 'triaged' ? 'success' : v === 'ignored' ? 'info' : 'warning'}
        effect="light"
      >
        {v}
      </ElTag>
    )
  },
  {
    field: 'action',
    label: t('colAction'),
    width: 190,
    formatter: (row) => (
      <div class="flex items-center" style={'gap:6px'}>
        <BaseButton type="primary" onClick={() => handleTriage(row, 'triaged')}>
          {t('btnTriaged')}
        </BaseButton>
        <BaseButton type="info" onClick={() => handleTriage(row, 'ignored')}>
          {t('btnIgnore')}
        </BaseButton>
      </div>
    )
  }
])

const {
  tableRegister: resultRegister,
  tableState: resultState,
  tableMethods: resultMethods
} = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = resultState
    const res = await listGithubResultsApi({
      keyword: resultFilters.keyword || undefined,
      type: resultFilters.type || undefined,
      status: resultFilters.status || undefined,
      project: resultFilters.project || undefined,
      pageIndex: currentPage.value,
      pageSize: pageSize.value
    })
    return { list: res.data.list || [], total: res.data.total || 0 }
  }
})
const {
  loading: resultLoading,
  dataList: resultList,
  total: resultTotal,
  currentPage: resultPage,
  pageSize: resultPageSize
} = resultState
resultPageSize.value = 20
const { getList: getResultList } = resultMethods

// 结果总览：和列表用同一套筛选，保证「上面显示的数字」与「下面列出的行」口径一致。
// 只读聚合，失败静默（总览没了不该让列表也报错）。
const resultOverview = ref<{ total: number; byType: Record<string, number>; byStatus: Record<string, number> }>({
  total: 0,
  byType: {},
  byStatus: {}
})
const loadResultOverview = async () => {
  try {
    const res = await getResultOverviewApi({
      keyword: resultFilters.keyword || undefined,
      type: resultFilters.type || undefined,
      status: resultFilters.status || undefined,
      project: resultFilters.project || undefined
    })
    resultOverview.value = {
      total: res.data.total || 0,
      byType: res.data.byType || {},
      byStatus: res.data.byStatus || {}
    }
  } catch {
    resultOverview.value = { total: 0, byType: {}, byStatus: {} }
  }
}
// 总览与列表同源：查询、研判、翻页之后都要重算
const refreshResults = async () => {
  await getResultList()
  void loadResultOverview()
}
onMounted(() => {
  void loadResultOverview()
})

// ---------------- 忽略仓库（噪声黑名单） ----------------
// 后端 collect 时按这份名单跳过（任务与威胁源都会查），所以它是「治理噪声」的入口，
// 不是纯展示。加名单时可选择顺手清掉已入库的旧结果。
const ignoredVisible = ref(false)
const ignoredLoading = ref(false)
const ignoredList = ref<IgnoredRepo[]>([])
const ignoredForm = reactive({ repo: '', reason: '', purge: false })
const ignoredSaving = ref(false)

const loadIgnoredRepos = async () => {
  ignoredLoading.value = true
  try {
    const res = await listIgnoredReposApi()
    ignoredList.value = res.data.items || []
  } catch {
    ignoredList.value = []
  } finally {
    ignoredLoading.value = false
  }
}
const openIgnored = () => {
  ignoredVisible.value = true
  void loadIgnoredRepos()
}
const submitIgnored = async () => {
  const repo = ignoredForm.repo.trim()
  if (!repo) {
    ElMessage.warning(t('ignoredRepoRequired'))
    return
  }
  if (ignoredForm.purge) {
    // 删库不可恢复，二次确认
    await ElMessageBox.confirm(t('ignoredPurgeConfirm', { repo }), t('ignoredPurgeTitle'), {
      confirmButtonText: t('btnIgnore'),
      cancelButtonText: t('btnCancel'),
      type: 'warning'
    })
  }
  ignoredSaving.value = true
  try {
    const res = await addIgnoredRepoApi({ repo, reason: ignoredForm.reason.trim() || undefined, purge: ignoredForm.purge })
    const removed = res.data.removed || 0
    ElMessage.success(removed > 0 ? t('ignoredAddedPurged', { n: removed }) : t('ignoredAdded'))
    ignoredForm.repo = ''
    ignoredForm.reason = ''
    ignoredForm.purge = false
    await loadIgnoredRepos()
    // 清过旧结果就刷新列表与总览，否则界面上还留着已删的行
    if (removed > 0) refreshResults()
  } catch (e: any) {
    if (e !== 'cancel' && !e?.response) ElMessage.error(e?.message || t('opFailed'))
  } finally {
    ignoredSaving.value = false
  }
}
const dropIgnored = async (repo: string) => {
  try {
    await removeIgnoredRepoApi(repo)
    ElMessage.success(t('ignoredRemoved'))
    await loadIgnoredRepos()
  } catch (e: any) {
    if (!e?.response) ElMessage.error(e?.message || t('opFailed'))
  }
}

const handleTriage = async (row: { id: string }, status: string) => {
  await triageGithubResultsApi({ ids: [row.id], status })
  ElMessage.success(t('updateSuccess'))
  // 研判后总览里的 new/triaged 计数必须跟着动，否则「还有多少没处置」一直是旧数
  refreshResults()
}
</script>

<template>
  <ContentWrap>
    <ElTabs v-model="activeTab">
      <ElTabPane :label="t('tabTasks')" name="task">
        <ElRow :gutter="12" class="mb-10px">
          <ElCol :span="5">
            <ElInput v-model="taskFilters.keyword" :placeholder="t('phTaskKeyword')" clearable />
          </ElCol>
          <ElCol :span="4">
            <ElSelect
              v-model="taskFilters.type"
              :placeholder="t('colType')"
              clearable
              style="width: 100%"
            >
              <ElOption
                v-for="opt in typeOptions"
                :key="opt.value"
                :label="opt.label"
                :value="opt.value"
              />
            </ElSelect>
          </ElCol>
          <ElCol :span="4">
            <ElSelect
              v-model="taskFilters.state"
              :placeholder="t('colState')"
              clearable
              style="width: 100%"
            >
              <ElOption :label="t('statePending')" :value="0" />
              <ElOption :label="t('stateRunning')" :value="1" />
              <ElOption :label="t('statePaused')" :value="2" />
              <ElOption :label="t('stateFinished')" :value="3" />
            </ElSelect>
          </ElCol>
          <ElCol :span="5">
            <ElButton type="primary" @click="getTaskList">{{ t('btnQuery') }}</ElButton>
            <ElButton type="success" @click="openCreate">{{ t('btnNewTask') }}</ElButton>
          </ElCol>
        </ElRow>

        <Table
          v-model:pageSize="taskPageSize"
          v-model:currentPage="taskPage"
          :columns="taskColumns"
          :data="taskList"
          stripe
          border
          :loading="taskLoading"
          :pagination="{ total: taskTotal, pageSizes: [20, 50, 100] }"
          @register="taskRegister"
        />
      </ElTabPane>

      <ElTabPane :label="t('tabResults')" name="result">
        <!-- 总览条：整库口径，不随分页变化。之前结果页只能看到「这一页有什么」，
             看不到「还有多少条没处置」—— 而处置节奏正是要看这个数。 -->
        <div v-if="resultOverview.total > 0" class="ov-bar">
          <span class="ov-title">{{ t('overviewTitle') }}</span>
          <ElTag size="small" type="info" effect="plain">
            {{ t('overviewTotal', { n: resultOverview.total }) }}
          </ElTag>
          <ElTag
            v-if="resultOverview.byStatus.new"
            size="small"
            type="warning"
            effect="plain"
          >
            new {{ resultOverview.byStatus.new }}
          </ElTag>
          <ElTag
            v-if="resultOverview.byStatus.triaged"
            size="small"
            type="success"
            effect="plain"
          >
            triaged {{ resultOverview.byStatus.triaged }}
          </ElTag>
          <ElTag
            v-if="resultOverview.byStatus.ignored"
            size="small"
            type="info"
            effect="plain"
          >
            ignored {{ resultOverview.byStatus.ignored }}
          </ElTag>
          <span class="ov-sep" />
          <ElTag
            v-for="(n, kind) in resultOverview.byType"
            :key="kind"
            size="small"
            effect="plain"
          >
            {{ typeLabel(String(kind)) }} {{ n }}
          </ElTag>
        </div>
        <ElRow :gutter="12" class="mb-10px">
          <ElCol :span="5">
            <ElInput
              v-model="resultFilters.keyword"
              :placeholder="t('phResultKeyword')"
              clearable
            />
          </ElCol>
          <ElCol :span="3">
            <ElSelect
              v-model="resultFilters.type"
              :placeholder="t('colType')"
              clearable
              style="width: 100%"
            >
              <ElOption
                v-for="opt in typeOptions"
                :key="opt.value"
                :label="opt.label"
                :value="opt.value"
              />
            </ElSelect>
          </ElCol>
          <ElCol :span="3">
            <ElSelect
              v-model="resultFilters.status"
              :placeholder="t('phTriageStatus')"
              clearable
              style="width: 100%"
            >
              <ElOption label="new" value="new" />
              <ElOption label="triaged" value="triaged" />
              <ElOption label="ignored" value="ignored" />
            </ElSelect>
          </ElCol>
          <ElCol :span="4">
            <ElInput v-model="resultFilters.project" :placeholder="t('phProject')" clearable />
          </ElCol>
          <ElCol :span="4">
            <ElButton type="primary" @click="refreshResults">{{ t('btnQuery') }}</ElButton>
            <ElButton @click="openIgnored">{{ t('ignoredRepoManage') }}</ElButton>
          </ElCol>
        </ElRow>

        <Table
          v-model:pageSize="resultPageSize"
          v-model:currentPage="resultPage"
          :columns="resultColumns"
          :data="resultList"
          stripe
          border
          :loading="resultLoading"
          :pagination="{ total: resultTotal, pageSizes: [20, 50, 100] }"
          @register="resultRegister"
        />
      </ElTabPane>

      <!-- 忽略仓库抽屉：列表 + 添加 + 移除。加名单时可顺手清掉已入库的旧结果（需二次确认）。 -->
      <ElDrawer v-model="ignoredVisible" :title="t('ignoredRepoTitle')" size="520px">
        <div class="ignored-add">
          <ElInput v-model="ignoredForm.repo" :placeholder="t('ignoredRepoPh')" />
          <ElInput v-model="ignoredForm.reason" :placeholder="t('ignoredReasonPh')" />
          <ElCheckbox v-model="ignoredForm.purge">{{ t('ignoredPurge') }}</ElCheckbox>
          <ElButton type="primary" :loading="ignoredSaving" @click="submitIgnored">
            {{ t('btnIgnore') }}
          </ElButton>
          <div class="ignored-hint">{{ t('ignoredHint') }}</div>
        </div>
        <ElTable :data="ignoredList" v-loading="ignoredLoading" size="small" style="margin-top: 12px">
          <ElTableColumn prop="repo" :label="t('colRepo')" min-width="180" />
          <ElTableColumn prop="reason" :label="t('ignoredReason')" min-width="120" />
          <ElTableColumn :label="t('colAction')" width="90">
            <template #default="{ row }">
              <ElButton link type="danger" @click="dropIgnored(row.repo)">
                {{ t('btnRemove') }}
              </ElButton>
            </template>
          </ElTableColumn>
        </ElTable>
      </ElDrawer>
      <ElTabPane :label="t('tabScheduler')" name="scheduler">
        <SchedulerTab />
      </ElTabPane>

      <ElTabPane :label="t('tabThreat')" name="threat">
        <ThreatTab />
      </ElTabPane>
    </ElTabs>

    <ElDialog v-model="createVisible" :title="t('createTitle')" width="620px">
      <ElForm label-width="100px">
        <ElFormItem :label="t('colName')" required>
          <ElInput v-model="createForm.name" :placeholder="t('phName')" />
        </ElFormItem>
        <ElFormItem :label="t('colType')">
          <ElSelect v-model="createForm.type" style="width: 100%">
            <ElOption
              v-for="opt in typeOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </ElSelect>
        </ElFormItem>
        <ElFormItem :label="t('formQuery')" required>
          <ElInput
            v-model="createForm.query"
            type="textarea"
            :rows="3"
            :placeholder="t('phQuery')"
          />
        </ElFormItem>
        <ElFormItem :label="t('formKeywords')">
          <ElSelect
            v-model="createForm.keywords"
            multiple
            filterable
            allow-create
            default-first-option
            :placeholder="t('phKeywords')"
            style="width: 100%"
          />
        </ElFormItem>
        <ElFormItem :label="t('formProject')">
          <ElInput v-model="createForm.project" :placeholder="t('phProjectOptional')" />
        </ElFormItem>
        <ElFormItem :label="t('formInterval')">
          <ElInputNumber v-model="createForm.intervalMinutes" :min="0" :max="10080" />
          <span class="hint-text">{{ t('onceOnlyHint') }}</span>
        </ElFormItem>
        <ElFormItem :label="t('formMaxPages')">
          <ElInputNumber v-model="createForm.maxPages" :min="1" :max="20" />
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="createVisible = false">{{ t('btnCancel') }}</ElButton>
        <ElButton type="primary" :loading="createLoading" @click="submitCreate">
          {{ t('btnCreate') }}
        </ElButton>
      </template>
    </ElDialog>
  </ContentWrap>
</template>

<style scoped>
.ov-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  font-size: 13px;
}
.ov-title {
  color: var(--el-text-color-secondary);
}
.ignored-add {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.ignored-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
}
.ov-sep {
  width: 1px;
  height: 16px;
  background: var(--el-border-color);
  margin: 0 2px;
}
.mb-10px {
  margin-bottom: 10px;
}

.hint-text {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
