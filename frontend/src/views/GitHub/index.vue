<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import { reactive, ref } from 'vue'
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
  ElLink
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
  triageGithubResultsApi
} from '@/api/github'
import SchedulerTab from './SchedulerTab.vue'
import ThreatTab from './ThreatTab.vue'
import { formatTime } from '@/utils'

const activeTab = ref('task')

const taskStateMap: Record<number, { text: string; type: string }> = {
  0: { text: '待开始', type: 'info' },
  1: { text: '运行中', type: 'success' },
  2: { text: '已暂停', type: 'warning' },
  3: { text: '已完成', type: 'primary' }
}

const typeOptions = [
  { value: 'code_leak', label: '代码泄露' },
  { value: 'cve', label: 'CVE' },
  { value: 'hacker', label: '黑客' }
]

const taskFilters = reactive({ keyword: '', type: '', state: '' as string | number })
const resultFilters = reactive({ keyword: '', type: '', status: '', project: '' })

const fmt = (v?: string) => (v ? formatTime(v, 'yyyy-MM-dd HH:mm:ss') : '-')

// ---------------- 任务 ----------------
const taskColumns = reactive<TableColumn[]>([
  { field: 'name', label: '名称', minWidth: 160 },
  {
    field: 'type',
    label: '类型',
    width: 100,
    formatter: (_r, __: TableColumn, v: string) => typeOptions.find((t) => t.value === v)?.label || v
  },
  { field: 'query', label: '查询', minWidth: 200 },
  {
    field: 'state',
    label: '状态',
    width: 100,
    formatter: (_r, __: TableColumn, v: number) => (
      <ElTag type={(taskStateMap[v]?.type as any) || 'info'} effect="light">
        {taskStateMap[v]?.text || v}
      </ElTag>
    )
  },
  {
    field: 'stats',
    label: '已扫描/新增',
    width: 130,
    formatter: (_r, __: TableColumn, v?: { scanned: number; newFound: number }) =>
      v ? v.scanned + ' / ' + v.newFound : '-'
  },
  {
    field: 'lastRunAt',
    label: '最近运行',
    minWidth: 160,
    formatter: (_r, __: TableColumn, v?: string) => fmt(v)
  },
  {
    field: 'action',
    label: '操作',
    width: 170,
    formatter: (row) => (
      <div class="flex items-center" style={'gap:6px'}>
        <BaseButton
          type="warning"
          onClick={() => handleStopTask(row)}
          disabled={row.state !== 1 && row.state !== 0}
        >
          停止
        </BaseButton>
        <BaseButton type="danger" onClick={() => handleDeleteTask(row)}>
          删除
        </BaseButton>
      </div>
    )
  }
])

const { tableRegister: taskRegister, tableState: taskState, tableMethods: taskMethods } = useTable({
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
const { loading: taskLoading, dataList: taskList, total: taskTotal, currentPage: taskPage, pageSize: taskPageSize } = taskState
taskPageSize.value = 20
const { getList: getTaskList } = taskMethods

const handleStopTask = async (row: { id: string; name: string }) => {
  await stopGithubTasksApi([row.id])
  ElMessage.success('已暂停 ' + row.name)
  getTaskList()
}

const handleDeleteTask = async (row: { id: string; name: string }) => {
  await ElMessageBox.confirm('删除任务会同时删除其下的结果，确定删除 ' + row.name + ' 吗？', '删除任务', {
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning'
  })
  await deleteGithubTasksApi([row.id])
  getTaskList()
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
    ElMessage.warning('名称与查询语句不能为空')
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
    ElMessage.success('任务已创建')
    getTaskList()
  } finally {
    createLoading.value = false
  }
}

// ---------------- 结果 ----------------
const resultColumns = reactive<TableColumn[]>([
  {
    field: 'createdAt',
    label: '时间',
    minWidth: 160,
    formatter: (_r, __: TableColumn, v?: string) => fmt(v)
  },
  { field: 'repo', label: '仓库', minWidth: 180 },
  {
    field: 'path',
    label: '路径',
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
  { field: 'title', label: '标题', minWidth: 200 },
  { field: 'severity', label: '严重度', width: 100 },
  {
    field: 'status',
    label: '状态',
    width: 100,
    formatter: (_r, __: TableColumn, v: string) => (
      <ElTag type={v === 'triaged' ? 'success' : v === 'ignored' ? 'info' : 'warning'} effect="light">
        {v}
      </ElTag>
    )
  },
  {
    field: 'action',
    label: '操作',
    width: 190,
    formatter: (row) => (
      <div class="flex items-center" style={'gap:6px'}>
        <BaseButton type="primary" onClick={() => handleTriage(row, 'triaged')}>
          已研判
        </BaseButton>
        <BaseButton type="info" onClick={() => handleTriage(row, 'ignored')}>
          忽略
        </BaseButton>
      </div>
    )
  }
])

const { tableRegister: resultRegister, tableState: resultState, tableMethods: resultMethods } = useTable({
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
const { loading: resultLoading, dataList: resultList, total: resultTotal, currentPage: resultPage, pageSize: resultPageSize } = resultState
resultPageSize.value = 20
const { getList: getResultList } = resultMethods

const handleTriage = async (row: { id: string }, status: string) => {
  await triageGithubResultsApi({ ids: [row.id], status })
  ElMessage.success('已更新')
  getResultList()
}
</script>

<template>
  <ContentWrap>
    <ElTabs v-model="activeTab">
      <ElTabPane label="采集任务" name="task">
        <ElRow :gutter="12" class="mb-10px">
          <ElCol :span="5">
            <ElInput v-model="taskFilters.keyword" placeholder="名称/查询关键字" clearable />
          </ElCol>
          <ElCol :span="4">
            <ElSelect v-model="taskFilters.type" placeholder="类型" clearable style="width: 100%">
              <ElOption v-for="t in typeOptions" :key="t.value" :label="t.label" :value="t.value" />
            </ElSelect>
          </ElCol>
          <ElCol :span="4">
            <ElSelect v-model="taskFilters.state" placeholder="状态" clearable style="width: 100%">
              <ElOption label="待开始" :value="0" />
              <ElOption label="运行中" :value="1" />
              <ElOption label="已暂停" :value="2" />
              <ElOption label="已完成" :value="3" />
            </ElSelect>
          </ElCol>
          <ElCol :span="5">
            <ElButton type="primary" @click="getTaskList">查询</ElButton>
            <ElButton type="success" @click="openCreate">新建任务</ElButton>
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

      <ElTabPane label="泄露结果" name="result">
        <ElRow :gutter="12" class="mb-10px">
          <ElCol :span="5">
            <ElInput v-model="resultFilters.keyword" placeholder="仓库/路径/标题关键字" clearable />
          </ElCol>
          <ElCol :span="3">
            <ElSelect v-model="resultFilters.type" placeholder="类型" clearable style="width: 100%">
              <ElOption v-for="t in typeOptions" :key="t.value" :label="t.label" :value="t.value" />
            </ElSelect>
          </ElCol>
          <ElCol :span="3">
            <ElSelect v-model="resultFilters.status" placeholder="研判状态" clearable style="width: 100%">
              <ElOption label="new" value="new" />
              <ElOption label="triaged" value="triaged" />
              <ElOption label="ignored" value="ignored" />
            </ElSelect>
          </ElCol>
          <ElCol :span="4">
            <ElInput v-model="resultFilters.project" placeholder="项目" clearable />
          </ElCol>
          <ElCol :span="4">
            <ElButton type="primary" @click="getResultList">查询</ElButton>
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
      <ElTabPane label="周期任务" name="scheduler">
        <SchedulerTab />
      </ElTabPane>

      <ElTabPane label="威胁情报" name="threat">
        <ThreatTab />
      </ElTabPane>
    </ElTabs>

    <ElDialog v-model="createVisible" title="新建采集任务" width="620px">
      <ElForm label-width="100px">
        <ElFormItem label="名称" required>
          <ElInput v-model="createForm.name" placeholder="例如：某公司代码泄露" />
        </ElFormItem>
        <ElFormItem label="类型">
          <ElSelect v-model="createForm.type" style="width: 100%">
            <ElOption v-for="t in typeOptions" :key="t.value" :label="t.label" :value="t.value" />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="查询语句" required>
          <ElInput
            v-model="createForm.query"
            type="textarea"
            :rows="3"
            placeholder="GitHub 搜索语法，例如：'example.com' password"
          />
        </ElFormItem>
        <ElFormItem label="关键词">
          <ElSelect
            v-model="createForm.keywords"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="命中关键词，可留空"
            style="width: 100%"
          />
        </ElFormItem>
        <ElFormItem label="项目">
          <ElInput v-model="createForm.project" placeholder="可选，结果归属项目" />
        </ElFormItem>
        <ElFormItem label="间隔(分钟)">
          <ElInputNumber v-model="createForm.intervalMinutes" :min="0" :max="10080" />
          <span class="hint-text">0 = 只跑一次</span>
        </ElFormItem>
        <ElFormItem label="最大页数">
          <ElInputNumber v-model="createForm.maxPages" :min="1" :max="20" />
        </ElFormItem>
      </ElForm>
      <template #footer>
        <ElButton @click="createVisible = false">取消</ElButton>
        <ElButton type="primary" :loading="createLoading" @click="submitCreate">创建</ElButton>
      </template>
    </ElDialog>
  </ContentWrap>
</template>

<style scoped>
.mb-10px {
  margin-bottom: 10px;
}

.hint-text {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
