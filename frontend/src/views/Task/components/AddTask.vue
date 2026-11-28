<script setup lang="ts">
import {
  ElCheckbox,
  ElDivider,
  ElForm,
  ElFormItem,
  ElInput,
  ElRow,
  ElCol,
  ElSwitch,
  FormRules,
  ElTooltip,
  ElRadioGroup,
  ElRadio,
  ElSelectV2,
  ElOption,
  ElButton,
  FormInstance,
  ElSelect,
  ElMessage,
  CheckboxValueType,
  ElTreeSelect,
  ElDrawer,
  ElAlert,
  ElTag
} from 'element-plus'
import { useI18n } from '@/hooks/web/useI18n'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { getNodeDataOnlineApi, getNodeDataApi } from '@/api/node'
import {
  addScheduledTaskApi,
  addTaskApi,
  getScheduleDetailApi,
  getTaskDetailApi,
  getTemplateDataApi,
  updateScheduleApi
} from '@/api/task'
import DetailTemplate from './DetailTemplate.vue'
import { getProjectAllApi } from '@/api/project'
import { applyPolicyApi, getPolicyListApi } from '@/api/policy'
import type { ScanPolicyApplyResult, ScanPolicyConfig } from '@/api/policy/types'
import { POLICY_MAX_PAGE_SIZE, applyPolicyToTaskForm, policyFieldLabelKey } from '@/utils/policy'
import type { PolicyConflict } from '@/utils/policy'
const { t } = useI18n()
// 策略为空时引导去策略页；用 router 而不是改 hash，避免 keep-alive 下弹窗残留
const router = useRouter()

const props = defineProps<{
  closeDialog: () => void
  getList: () => void
  create: boolean
  schedule: boolean
  taskid: string
  tp: string
  targetIds: string[]
  getFilter?: () => { [key: string]: any }
  searchParams?: string
}>()

interface RuleForm {
  name: string
  target: string
  node: []
  template: string
  day: number
  hour: number
  minute: number
}
const sourceTp = props.tp.includes('Source') ? true : false

const rules = reactive<FormRules<RuleForm>>({
  name: [{ required: true, message: t('task.msgTaskName'), trigger: 'blur' }],
  target: [
    {
      required: sourceTp ? false : true,
      message: t('task.msgTarget'),
      trigger: 'blur'
    }
  ],
  // 勾了「自动加入全部在线节点」就不必再手选：服务端会补齐全部节点。
  // 原来的 required:true 会让这个开关在建任务时被前端直接拦死。
  node: [
    {
      trigger: 'change',
      validator: (_rule, value, callback) => {
        if (taskData.allNode || (Array.isArray(value) && value.length > 0)) return callback()
        callback(new Error(t('task.nodeMsg')))
      }
    }
  ],
  template: [{ required: true, message: t('localization.selectTemplate'), trigger: 'blur' }],
  day: [
    {
      message: '1-31',
      trigger: 'change',
      validator: (_, value, callback) => {
        console.log(value)
        if (!value) {
          callback(new Error('1-31'))
        } else if (!/^\d+$/.test(value)) {
          callback(new Error('1-31'))
        } else if (value < 1 || value > 31) {
          callback(new Error('1-31'))
        } else {
          callback() // 验证通过
        }
      }
    }
  ],
  hour: [
    {
      message: '0-24',
      trigger: 'change',
      validator: (_rule, value, callback) => {
        console.log(value)
        if (!value) {
          callback(new Error('0-24'))
        } else if (!/^\d+$/.test(value)) {
          callback(new Error('0-24'))
        } else if (value < 0 || value > 24) {
          callback(new Error('0-24'))
        } else {
          callback() // 验证通过
        }
      }
    }
  ],
  minute: [
    {
      message: '0-60',
      trigger: 'change',
      validator: (_rule, value, callback) => {
        console.log(value)
        if (!value) {
          callback(new Error('0-60'))
        } else if (!/^\d+$/.test(value)) {
          callback(new Error('0-60'))
        } else if (value < 0 || value > 60) {
          callback(new Error('0-60'))
        } else {
          callback() // 验证通过
        }
      }
    }
  ]
})

const saveLoading = ref(false)
const ruleFormRef = ref<FormInstance>()
const submitForm = async (formEl: FormInstance | undefined) => {
  saveLoading.value = true
  try {
    if (!formEl) return

    const valid = await formEl.validate() // 使用 Promise 风格的 validate
    if (valid) {
      let res
      let searchFilter = reactive<{ [key: string]: any }>({})
      if (targetTp.value == 'search') {
        if (props.getFilter) {
          searchFilter = props.getFilter()
        }
        if (props.searchParams) {
          taskData.search = props.searchParams
        }
      }
      if (sourceTp) {
        taskData.targetSource = props.tp
      }
      if (props.taskid) {
        // 修改计划任务
        res = await updateScheduleApi(
          props.taskid,
          taskData.name,
          taskData.target,
          taskData.ignore,
          taskData.node,
          taskData.allNode,
          taskData.duplicates,
          taskData.scheduledTasks,
          taskData.hour,
          taskData.template,
          targetTp.value,
          taskData.search,
          searchFilter,
          targetNumber.value,
          props.targetIds,
          taskData.project,
          taskData.targetSource,
          taskData.day,
          taskData.minute,
          taskData.week,
          taskData.bindProject,
          taskData.cycleType
        )
      } else {
        // 创建新任务
        if (props.schedule) {
          res = await addScheduledTaskApi(
            taskData.name,
            taskData.target,
            taskData.ignore,
            taskData.node,
            taskData.allNode,
            taskData.duplicates,
            taskData.scheduledTasks,
            taskData.hour,
            taskData.template,
            targetTp.value,
            taskData.search,
            searchFilter,
            targetNumber.value,
            props.targetIds,
            taskData.project,
            taskData.targetSource,
            taskData.day,
            taskData.minute,
            taskData.week,
            taskData.bindProject,
            taskData.cycleType,
            policyId.value,
            scanProfile.value
          )
        } else {
          res = await addTaskApi(
            taskData.name,
            taskData.target,
            taskData.ignore,
            taskData.node,
            taskData.allNode,
            taskData.duplicates,
            taskData.scheduledTasks,
            taskData.hour,
            taskData.template,
            targetTp.value,
            taskData.search,
            searchFilter,
            targetNumber.value,
            props.targetIds,
            taskData.project,
            taskData.targetSource,
            taskData.day,
            taskData.minute,
            taskData.week,
            taskData.bindProject,
            taskData.cycleType,
            // 选中的扫描策略随任务一起提交：后端据此在生成扫描载荷时收窄插件清单
            policyId.value,
            scanProfile.value
          )
        }
      }
      if (res.code === 200) {
        // 业务成功文案是通用的 api.success，全局拦截器按噪音规则不弹，这里补一个确认
        ElMessage.success(t('common.success'))
        props.closeDialog()
        props.getList()
      }
    }
  } catch (error) {
    console.error('提交表单时发生错误:', error)
  } finally {
    saveLoading.value = false // 确保无论成功或失败都重置加载状态
  }
}
const nodeOptions = reactive<{ value: string; label: string; disabled?: boolean }[]>([])
// 同时取「全部节点」和「在线节点」：离线节点也列出来但禁用，
// 否则用户只看到节点消失，不知道是自己选错了还是节点掉了。
const getNodeList = async () => {
  const [onlineRes, allRes] = await Promise.all([
    getNodeDataOnlineApi().catch(() => ({ data: { list: [] } }) as any),
    getNodeDataApi().catch(() => ({ data: { list: [] } }) as any)
  ])
  const online: string[] = onlineRes?.data?.list || []
  const allRaw = allRes?.data?.list || []
  const all: string[] = allRaw.map((n: any) => (typeof n === 'string' ? n : n.name)).filter(Boolean)

  nodeOptions.length = 0
  // 在线优先，离线排后面并标注
  online.forEach((item) => nodeOptions.push({ value: item, label: item }))
  all
    .filter((n) => !online.includes(n))
    .forEach((n) =>
      nodeOptions.push({ value: n, label: n + ' ' + t('node.offlineSuffix'), disabled: true })
    )

  if (online.length > 0) {
    isCheckboxDisabledNode.value = false
  } else {
    isCheckboxDisabledNode.value = true
    ElMessage.warning(t('node.onlineNodeMsg'))
  }
}

const templateOptions = reactive<{ value: string; label: string }[]>([])

const getTemplateList = async () => {
  templateOptions.length = 0
  const res = await getTemplateDataApi('', 1, 1000)
  if (res.data.list.length > 0) {
    res.data.list.forEach((item) => {
      templateOptions.push({ value: item.id, label: item.name })
    })
  }
}

onMounted(() => {
  getNodeList()
  getTemplateList()
})
const indeterminate = ref(false)
const isCheckboxDisabledNode = ref(false)
const taskData = reactive({
  name: '',
  target: '',
  ignore: '',
  node: [] as string[],
  allNode: true,
  scheduledTasks: false,
  duplicates: 'None',
  template: '',
  cycleType: 'daily',
  search: '',
  project: [] as string[],
  targetSource: 'general',
  day: 1,
  hour: 1,
  minute: 30,
  week: 1,
  bindProject: null,
  // 扫描档位：'' = 按模板；'recon' = 只做资产测绘（后端跳过 nuclei 与目录扫描）
  scanProfile: ''
})
const handleCheckAll = (val: CheckboxValueType) => {
  indeterminate.value = false
  if (val) {
    taskData.node = []
    nodeOptions.forEach((option) => {
      return taskData.node.push(option.value)
    })
  } else {
    taskData.node = []
  }
}
const templateId = ref('')
const dialogVisible = ref(false)

let DialogTitle = t('task.addTemplate')
const editTemplate = async (data) => {
  templateId.value = data
  if (data != '') {
    DialogTitle = t('task.editTemplate')
  }
  dialogVisible.value = true
}
const closeTemplateDialog = () => {
  dialogVisible.value = false
}

const loadTaskData = async (id) => {
  const res = await getTaskDetailApi(id)
  taskData.name = res.data.name
  taskData.target = res.data.target
  taskData.ignore = res.data.ignore
  taskData.node = res.data.node
  taskData.allNode = res.data.allNode
  taskData.scheduledTasks = res.data.scheduledTasks
  taskData.hour = res.data.hour
  taskData.duplicates = res.data.duplicates
  taskData.template = res.data.template
  // 任务上挂过策略时回显选择（后端 task.policyId，空 = 没套用策略）
  policyId.value = res.data.policyId || ''
  scanProfile.value = res.data.scanProfile || ''
  taskData.day = res.data.day
  taskData.minute = res.data.minute
  taskData.week = res.data.week
  taskData.cycleType = res.data.cycleType
}

const loadScheduleData = async (id) => {
  const res = await getScheduleDetailApi(id)
  taskData.name = res.data.name
  taskData.target = res.data.target
  taskData.ignore = res.data.ignore
  taskData.node = res.data.node
  taskData.allNode = res.data.allNode
  taskData.scheduledTasks = res.data.scheduledTasks
  taskData.hour = res.data.hour
  taskData.duplicates = res.data.duplicates
  taskData.template = res.data.template
  taskData.project = res.data.project
  taskData.targetSource = res.data.targetSource
  taskData.day = res.data.day
  taskData.minute = res.data.minute
  taskData.week = res.data.week
  taskData.cycleType = res.data.cycleType
  // 计划任务文档里同样存了 policyId（ScheduledTasks 存的就是 models.Task），一并回显
  policyId.value = res.data.policyId || ''
  scanProfile.value = res.data.scanProfile || ''
}

// 扫描策略选择。声明必须在下面这个 immediate watch 之前：taskid 存在时会同步走
// loadTaskData() 回显 policyId，晚声明会命中 TDZ（Cannot access before initialization）。
const policyId = ref('')
// 扫描档位：空 = 按模板完整扫描；recon = 只做资产测绘（后端 scanprofile.Apply 剔除 nuclei 与目录扫描）。
// 这个开关以前只有 MCP 能设 —— 界面上看不到、选不了，等于「机制在、人手用不到」。
const scanProfile = ref('')

watch(
  () => props.taskid, // 监听 props.taskid 的变化
  async (newId) => {
    if (newId) {
      // 如果传入了 ID，则加载已有数据
      if (props.schedule) {
        // 如果是计划任务则从计划任务中加载数据
        await loadScheduleData(newId)
      } else {
        // 从任务中加载数据
        await loadTaskData(newId)
      }
    } else {
      if (props.schedule) {
        taskData.scheduledTasks = true
      } else {
        taskData.scheduledTasks = false
      }
      taskData.name = ''
      taskData.target = ''
      taskData.ignore = ''
      taskData.node = []
      taskData.allNode = true
      taskData.duplicates = 'None'
      taskData.template = ''
    }
  },
  { immediate: true } // 确保组件挂载时立即触发
)
const targetTp = ref('select')
const targetNumber = ref(0)
const targetSourceOptions = [
  {
    label: t('task.general'),
    value: 'general'
  },
  {
    label: t('task.fromProject'),
    value: 'project'
  },
  {
    label: t('task.fromAsset'),
    value: 'asset'
  },
  {
    label: t('task.fromRootDomain'),
    value: 'RootDomain'
  },
  {
    label: t('task.fromSubdomain'),
    value: 'subdomain'
  }
]
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

// ---- 扫描策略（建任务弹窗内的「一键套用」入口） ----
// 下拉只列 enabled=true 的策略；选中后调 apply 把策略覆盖模板默认后的 merged config 带回来。
// 规则：只填表单里还空着的字段（目前是扫描模板），已填的一律不覆盖，冲突用 warning 提示。
// policyId 本身声明在上面（immediate watch 需要先有它）。
const policyOptions = ref<{ id: string; name: string }[]>([])
const policyApplied = ref<ScanPolicyApplyResult | null>(null)
const policyConflicts = ref<PolicyConflict[]>([])
const policyLoading = ref(false)

const loadPolicies = async () => {
  try {
    const res = await getPolicyListApi({
      enabled: true,
      pageIndex: 1,
      pageSize: POLICY_MAX_PAGE_SIZE
    })
    policyOptions.value = (res?.data?.list ?? []).map((item) => ({ id: item.id, name: item.name }))
  } catch {
    // 策略列表拿不到不影响建任务本身
    policyOptions.value = []
  }
}

// 去策略页新建：先关掉建任务弹窗，再跳转——否则弹窗会盖在策略页上（keep-alive 下还会带回来）
const goCreatePolicy = () => {
  props.closeDialog()
  router.push('/asset-information/policy')
}

const resetPolicy = () => {
  policyId.value = ''
  scanProfile.value = ''
  policyApplied.value = null
  policyConflicts.value = []
}

const policyFieldText = (field?: string) => {
  const key = policyFieldLabelKey(field)
  return key ? t(key) : field || '-'
}

// 后端 warnings 描述的是「策略合并到模板默认」，本地冲突描述的是「我们没覆盖表单值」；
// 同一个字段同时出现时只留后者，避免两句话互相打脸。
const policyWarnings = computed(() => {
  const warnings = policyApplied.value?.warnings ?? []
  if (!policyConflicts.value.some((item) => item.field === 'template')) return warnings
  return warnings.filter((item) => item.field !== 'templateId')
})

const onPolicyChange = async () => {
  policyApplied.value = null
  policyConflicts.value = []
  if (!policyId.value) return
  policyLoading.value = true
  try {
    // 把当前表单当作 taskTemplate 带上去：后端只在用户没填的字段上合并
    const taskTemplate: ScanPolicyConfig = { templateId: taskData.template }
    const res = await applyPolicyApi(policyId.value, taskTemplate)
    const applied = res?.data ?? null
    policyApplied.value = applied
    const fill = applyPolicyToTaskForm({ template: taskData.template }, applied?.config)
    if (fill.values.template) {
      taskData.template = fill.values.template
    }
    policyConflicts.value = fill.conflicts
  } catch {
    policyApplied.value = null
    policyConflicts.value = []
  } finally {
    policyLoading.value = false
  }
}

// 换任务（或切回「新建」）时清掉上一条策略的合并结果。
// 注意注册在 policy 的 ref 之后：immediate 的上一个 watch 触发时这些 ref 还没初始化。
watch(
  () => props.taskid,
  () => {
    resetPolicy()
  }
)

loadPolicies()
</script>
<template>
  <ElForm
    :model="taskData"
    label-width="auto"
    :rules="rules"
    status-icon
    ref="ruleFormRef"
    :disabled="create ? false : true"
  >
    <ElFormItem :label="t('task.taskName')" prop="name">
      <ElInput v-model="taskData.name" :placeholder="t('task.msgTaskName')" />
    </ElFormItem>
    <ElFormItem :label="t('task.targetSource')" v-if="!sourceTp">
      <ElSelectV2
        style="width: 50%"
        v-model="taskData.targetSource"
        :options="targetSourceOptions"
      />
    </ElFormItem>
    <ElRow
      v-if="taskData.targetSource != 'project' && taskData.targetSource != 'general' && !sourceTp"
    >
      <ElCol :span="12">
        <ElFormItem :label="t('task.search')">
          <ElInput v-model="taskData.search" :placeholder="t('form.input')" v-if="!sourceTp" />
        </ElFormItem>
      </ElCol>
      <ElCol :span="12">
        <ElFormItem :label="t('task.targetProject')">
          <ElTreeSelect
            v-model="taskData.project"
            :data="projectList"
            :placeholder="t('project.project')"
            multiple
            filterable
            show-checkbox
            collapse-tags
            :max-collapse-tags="1"
          />
        </ElFormItem>
      </ElCol>
    </ElRow>
    <ElFormItem :label="t('task.targetProject')" v-if="taskData.targetSource == 'project'">
      <ElTreeSelect
        v-model="taskData.project"
        :data="projectList"
        :placeholder="t('project.project')"
        multiple
        filterable
        show-checkbox
        collapse-tags
        :max-collapse-tags="1"
      />
    </ElFormItem>
    <ElFormItem
      :label="t('task.taskTarget')"
      prop="target"
      v-if="taskData.targetSource == 'general'"
    >
      <ElInput
        v-model="taskData.target"
        :placeholder="t('task.msgTarget')"
        type="textarea"
        :rows="10"
        v-if="!sourceTp"
      />
      <ElRadioGroup v-model="targetTp" v-if="sourceTp">
        <ElRadio value="select">{{ t('task.select') }}</ElRadio>
        <ElRadio value="search">{{ t('export.exportTypeSearch') }}</ElRadio>
      </ElRadioGroup>
    </ElFormItem>
    <ElFormItem :label="t('task.targetNumber')" v-if="targetTp == 'search' && sourceTp">
      <ElInput v-model.number="targetNumber" />
    </ElFormItem>
    <ElFormItem :label="t('task.ignore')" prop="ignore" v-if="taskData.targetSource != 'project'">
      <ElInput
        v-model="taskData.ignore"
        :placeholder="t('task.ignoreMsg')"
        type="textarea"
        :rows="5"
      />
    </ElFormItem>
    <!-- <ElFormItem :label="t('task.bindProject')" v-if="taskData.targetSource != 'project'">
      <ElTreeSelect
        v-model="taskData.bindProject"
        :data="projectList"
        :placeholder="t('project.project')"
        filterable
        show-checkbox
      />
    </ElFormItem> -->
    <ElRow>
      <ElCol :span="12">
        <ElFormItem :label="t('task.nodeSelect')" prop="node">
          <ElSelectV2
            v-model="taskData.node"
            filterable
            :options="nodeOptions"
            :placeholder="t('task.nodeMsg')"
            style="width: 80%"
            multiple
            tag-type="success"
            collapse-tags
            collapse-tags-tooltip
            :max-collapse-tags="7"
          >
            <template #header>
              <ElCheckbox
                :disabled="isCheckboxDisabledNode"
                :indeterminate="indeterminate"
                @change="handleCheckAll"
              >
                {{ t('common.all') }}
              </ElCheckbox>
            </template>
          </ElSelectV2>
        </ElFormItem>
      </ElCol>
      <ElCol :span="12">
        <ElFormItem :label="t('task.autoNode')">
          <ElTooltip effect="dark" :content="t('task.selectNodeMsg')" placement="top">
            <ElSwitch
              v-model="taskData.allNode"
              inline-prompt
              :active-text="t('common.switchAction')"
              :inactive-text="t('common.switchInactive')"
            />
          </ElTooltip>
        </ElFormItem>
      </ElCol>
    </ElRow>

    <ElFormItem :label="t('project.scheduledTasks')">
      <ElTooltip effect="dark" :content="t('project.msgScheduledTasks')" placement="top">
        <ElSwitch
          v-model="taskData.scheduledTasks"
          inline-prompt
          :active-text="t('common.switchAction')"
          :inactive-text="t('common.switchInactive')"
        />
      </ElTooltip>
    </ElFormItem>
    <ElFormItem :label="t('project.cycle')" prop="type" v-if="taskData.scheduledTasks">
      <ElRow :gutter="10" style="width: 100%">
        <ElCol :span="5">
          <ElSelect v-model="taskData.cycleType" style="width: 100%">
            <ElOption :label="t('task.daily')" value="daily" />
            <ElOption :label="t('task.ndays')" value="ndays" />
            <ElOption :label="t('task.nhours')" value="nhours" />
            <ElOption :label="t('task.weekly')" value="weekly" />
            <ElOption :label="t('task.monthly')" value="monthly" />
          </ElSelect>
        </ElCol>
        <ElCol :span="5" v-if="taskData.cycleType == 'weekly'">
          <ElSelect v-model.number="taskData.week" style="width: 100%">
            <ElOption :label="t('task.monday')" :value="0" />
            <ElOption :label="t('task.tuesday')" :value="1" />
            <ElOption :label="t('task.wednesday')" :value="2" />
            <ElOption :label="t('task.thursday')" :value="3" />
            <ElOption :label="t('task.friday')" :value="4" />
            <ElOption :label="t('task.saturday')" :value="5" />
            <ElOption :label="t('task.sunday')" :value="6" />
          </ElSelect>
        </ElCol>
        <ElCol :span="5" v-if="taskData.cycleType === 'ndays' || taskData.cycleType == 'monthly'">
          <ElFormItem prop="day">
            <ElInput style="width: 100%" v-model.number="taskData.day">
              <template #append>{{ t('task.day') }}</template>
            </ElInput>
          </ElFormItem>
        </ElCol>
        <ElCol
          :span="5"
          v-if="
            taskData.cycleType === 'daily' ||
            taskData.cycleType === 'ndays' ||
            taskData.cycleType == 'nhours' ||
            taskData.cycleType == 'weekly' ||
            taskData.cycleType == 'monthly'
          "
        >
          <ElFormItem prop="hour">
            <ElInput style="width: 100%" v-model.number="taskData.hour">
              <template #append>{{ t('task.hour') }}</template>
            </ElInput>
          </ElFormItem>
        </ElCol>
        <ElCol
          :span="5"
          v-if="
            taskData.cycleType === 'daily' ||
            taskData.cycleType === 'ndays' ||
            taskData.cycleType == 'nhours' ||
            taskData.cycleType == 'weekly' ||
            taskData.cycleType == 'monthly'
          "
        >
          <ElFormItem prop="minute">
            <ElInput style="width: 100%" v-model.number="taskData.minute">
              <template #append>{{ t('task.minute') }}</template>
            </ElInput>
          </ElFormItem>
        </ElCol>
      </ElRow>
    </ElFormItem>
    <ElDivider content-position="center" style="width: 60%; left: 20%">{{
      t('task.duplicates')
    }}</ElDivider>
    <ElRow>
      <ElCol :span="24">
        <ElFormItem :label="t('task.duplicates')" prop="type">
          <ElRadioGroup v-model="taskData.duplicates">
            <ElRadio label="None" name="duplicates" :checked="true" value="None">{{
              t('localization.none')
            }}</ElRadio>
            <ElTooltip effect="dark" :content="t('task.duplicatesMsg')" placement="top">
              <ElRadio :label="t('task.duplicatesSubdomain')" name="duplicates" value="subdomain" />
            </ElTooltip>
          </ElRadioGroup>
        </ElFormItem>
      </ElCol>
    </ElRow>
    <ElDivider content-position="center" style="width: 60%; left: 20%">{{
      t('policy.pickTitle')
    }}</ElDivider>
    <!-- 扫描档位：决定这次任务跑不跑 nuclei（实测 nuclei 占单任务耗时的九成）。
         recon 档几分钟出资产面，需要漏洞面时再完整跑一遍 —— 排队紧的时候这是唯一的省时旋钮。 -->
    <ElFormItem :label="t('task.scanProfile')">
      <ElSelect v-model="scanProfile" clearable style="width: 40%">
        <ElOption :label="t('task.scanProfileFull')" value="" />
        <ElOption :label="t('task.scanProfileRecon')" value="recon" />
      </ElSelect>
      <span class="policy-hint inline">{{ t('task.scanProfileHint') }}</span>
    </ElFormItem>
    <ElFormItem :label="t('policy.pickLabel')">
      <ElSelect
        v-model="policyId"
        clearable
        filterable
        :loading="policyLoading"
        :placeholder="t('policy.pickPlaceholder')"
        style="width: 40%"
        @change="onPolicyChange"
      >
        <ElOption
          v-for="item in policyOptions"
          :key="item.id"
          :label="item.name"
          :value="item.id"
        />
      </ElSelect>
      <span class="policy-hint inline">{{ t('policy.pickHint') }}</span>
      <!-- 一条策略都没有时给出口：否则下拉是空的，用户不知道去哪建 -->
      <div v-if="!policyLoading && !policyOptions.length" class="policy-hint policy-empty">
        {{ t('policy.noneYet') }}
        <ElButton link type="primary" size="small" @click="goCreatePolicy">
          {{ t('policy.goCreate') }}
        </ElButton>
      </div>
    </ElFormItem>
    <ElFormItem v-if="policyApplied" :label="t('policy.pickSummaryTitle')">
      <div class="policy-summary">
        <div v-if="(policyApplied.appliedFields ?? []).length" class="policy-tags">
          <ElTag v-for="field in policyApplied.appliedFields" :key="field" size="small" type="info">
            {{ policyFieldText(field) }}
          </ElTag>
        </div>
        <div v-else class="policy-hint">{{ t('policy.pickAppliedEmpty') }}</div>
        <div class="policy-hint">{{ t('policy.pickSummaryHint') }}</div>
        <div class="policy-hint">{{ t('policy.pickEffectHint') }}</div>
      </div>
    </ElFormItem>
    <ElAlert
      v-if="policyConflicts.length"
      type="warning"
      :closable="false"
      show-icon
      class="policy-alert"
      :title="t('policy.pickConflictTitle')"
    >
      <ul class="policy-conflict-list">
        <li v-for="(conflict, index) in policyConflicts" :key="`${conflict.field}-${index}`">
          {{
            t('policy.pickConflict', {
              field: policyFieldText(conflict.field),
              current: conflict.current,
              next: conflict.next
            })
          }}
        </li>
      </ul>
    </ElAlert>
    <ElAlert
      v-if="policyWarnings.length"
      type="warning"
      :closable="false"
      show-icon
      class="policy-alert"
      :title="t('policy.previewWarnings')"
    >
      <ul class="policy-conflict-list">
        <li v-for="(warning, index) in policyWarnings" :key="`${warning.code}-${index}`">
          {{ policyFieldText(warning.field) }}{{ t('common.labelSep') }}{{ warning.message }}
        </li>
      </ul>
    </ElAlert>
    <ElDivider content-position="center" style="width: 60%; left: 20%">{{
      t('router.scanTemplate')
    }}</ElDivider>
    <ElFormItem :label="t('router.scanTemplate')" prop="template">
      <!-- <ElSelectV2 v-model="taskData.template" :placeholder="t('task.nodeMsg')" style="width: 50%" /> -->
      <ElSelect
        v-model="taskData.template"
        :placeholder="t('localization.selectTemplate')"
        style="width: 30%"
      >
        <ElOption
          v-for="item in templateOptions"
          :key="item.value"
          :label="item.label"
          :value="item.value"
        >
          <ElRow>
            <ElCol :span="16">
              <ElTooltip :content="item.label" placement="top">
                <span
                  style="
                    float: left;
                    overflow: hidden;
                    text-overflow: ellipsis;
                    white-space: nowrap;
                    width: 100%;
                  "
                >
                  {{ item.label }}
                </span>
              </ElTooltip>
            </ElCol>
            <ElCol :span="8">
              <ElButton
                type="primary"
                size="small"
                style="margin-left: 15px"
                @click.stop="editTemplate(item.value)"
              >
                {{ t('common.edit') }}
              </ElButton>
            </ElCol>
          </ElRow>
        </ElOption>
        <template #footer>
          <ElButton
            type="success"
            size="small"
            plain
            style="margin-left: 15px"
            @click.stop="editTemplate('')"
          >
            {{ t('common.new') }}
          </ElButton>
        </template>
      </ElSelect>
    </ElFormItem>
    <ElFormItem :label="t('task.scanProfile')">
      <ElSelect v-model="taskData.scanProfile" style="width: 30%">
        <ElOption :label="t('task.scanProfileFull')" value="" />
        <ElOption :label="t('task.scanProfileRecon')" value="recon" />
      </ElSelect>
      <div class="scan-profile-hint">{{ t('task.scanProfileHint') }}</div>
    </ElFormItem>
    <ElDivider />
    <ElRow>
      <ElCol :span="2" :offset="10">
        <ElFormItem>
          <ElButton type="primary" @click="submitForm(ruleFormRef)" :loading="saveLoading">
            {{ t('task.save') }}
          </ElButton>
        </ElFormItem>
      </ElCol>
    </ElRow>
  </ElForm>
  <ElDrawer v-model="dialogVisible" :title="DialogTitle" direction="rtl" size="80%">
    <DetailTemplate
      :closeDialog="closeTemplateDialog"
      :getList="getTemplateList"
      :id="templateId"
    />
  </ElDrawer>
</template>

<style scoped>
.scan-profile-hint {
  width: 100%;
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-secondary);
}
.policy-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
}

.policy-hint.inline {
  margin-left: 10px;
}

.policy-summary {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.policy-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.policy-alert {
  margin: 0 0 14px;
}

.policy-conflict-list {
  margin: 4px 0 0;
  padding-left: 18px;
  line-height: 1.7;
}
</style>
