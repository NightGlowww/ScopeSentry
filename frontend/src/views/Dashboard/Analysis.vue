<script setup lang="ts">
import PanelGroup from './components/PanelGroup.vue'
import { ElRow, ElCol, ElCard, ElProgress, ElText, ElTooltip, ElButton, ElForm, ElFormItem, ElInput, ElTag, ElPopconfirm } from 'element-plus'

import { Table, TableColumn } from '@/components/Table'

import { Dialog } from '@/components/Dialog'
import { ref, reactive, h, onBeforeUnmount, Ref, computed } from 'vue'
import { getNodeDataApi } from '@/api/node'
import { getTaskDataApi } from '@/api/task'
import { useI18n } from '@/hooks/web/useI18n'
import { UPDATEsYSTEMApi, getVersionDataApi } from '@/api/dashboard/analysis'
import { checkKeyApi } from '@/api/plugins'
import { useVisibleInterval } from "@/hooks/web/useVisibleInterval"
import { useSingleTabLeader } from "@/hooks/web/useSingleTabLeader"

const { t } = useI18n()

// 服务端自身健康：负载/内存/磁盘。之前这些只在 /healthz 里，界面上看不到——
// 宿主被别的容器吃满 CPU 时（load 19/4 核），服务端会被拖到探活失败还无人察觉（2026-11-20）。
const serverHealth = ref<any>(null)
const loadServerHealth = async () => {
  try {
    const res = await fetch('/healthz', { headers: { Accept: 'application/json' } })
    serverHealth.value = await res.json()
  } catch {
    serverHealth.value = null
  }
}
const healthTone = (v: number, warn: number, danger: number) =>
  v >= danger ? 'danger' : v >= warn ? 'warning' : 'success'
const loadRatio = computed(() => {
  const h = serverHealth.value?.host
  if (!h || !h.cpus) return 0
  return Number((h.load1 / h.cpus).toFixed(2))
})

const loading = ref(true)
// 首屏就取一次服务端健康（/healthz 是公开接口，不需要登录态）
loadServerHealth()
// 每 30 秒刷新：健康是要"盯"的指标，进来一次就不动等于没有
useVisibleInterval(() => {
  loadServerHealth()
}, 30000)

const nodeColumns = reactive<TableColumn[]>([
  {
    field: 'name',
    label: t('node.nodeName')
  },
  {
    field: 'running',
    label: t('node.taskCount'),
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      return h(
        ElTag,
        {
          round: true,
          effect: 'dark'
        },
        () => cellValue
      )
    }
  },
  {
    field: 'finished',
    label: t('node.finished'),
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      return h(
        ElTag,
        {
          round: true,
          effect: 'dark'
        },
        () => cellValue
      )
    }
  },
  {
    field: 'state',
    label: t('node.nodeStatus'),
    formatter: (_: Recordable, __: TableColumn, cellValue: string) => {
      return h(
        ElTag,
        {
          type: cellValue === '1' ? 'success' : cellValue === '2' ? 'warning' : 'danger',
          effect: 'dark'
        },
        () =>
          cellValue == '1'
            ? t('node.statusRun')
            : cellValue == '2'
              ? t('node.statusStop')
              : t('node.statusError')
      )
    }
  }
])

const taskColums = reactive<TableColumn[]>([
  {
    field: 'name',
    label: t('task.taskName')
  },
  {
    field: 'taskNum',
    label: t('task.taskCount'),
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      return h(
        ElTag,
        {
          round: true,
          effect: 'dark'
        },
        () => cellValue
      )
    }
  },
  {
    field: 'progress',
    label: t('task.taskProgress'),
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      return h(ElProgress, {
        percentage: cellValue,
        type: 'line',
        striped: true,
        status: cellValue < 100 ? '' : 'success',
        stripedFlow: cellValue < 100 ? true : false
      })
    }
  },
  {
    field: 'creatTime',
    label: t('task.createTime')
  }
])

const nodeUsageColumns = reactive<TableColumn[]>([
  {
    field: 'name',
    label: t('node.nodeName')
  },
  {
    field: 'cpuNum',
    label: t('node.nodeUsageCpu'),
    formatter: (_: Recordable, __: TableColumn, cellValue: string) => {
      let numericValue = parseFloat(cellValue)
      numericValue = parseFloat(numericValue.toFixed(2))
      return h(ElProgress, {
        percentage: numericValue,
        type: 'dashboard',
        /* 默认 126px 的仪表盘放在表格里会把行撑到 130px 高，缩到 56px */
        width: 56,
        strokeWidth: 5,
        color: numericValue < 50 ? '#26a33f' : numericValue <= 80 ? '#fe9900' : '#df2800'
      })
    }
  },
  {
    field: 'memNum',
    label: t('node.nodeUsageMemory'),
    formatter: (_: Recordable, __: TableColumn, cellValue: string) => {
      let numericValue = parseFloat(cellValue)
      numericValue = parseFloat(numericValue.toFixed(2))
      return h(ElProgress, {
        percentage: numericValue,
        type: 'dashboard',
        width: 56,
        strokeWidth: 5,
        color: numericValue < 50 ? '#26a33f' : numericValue < 80 ? '#fe9900' : '#df2800'
      })
    }
  }
])

const versionColumns = reactive<TableColumn[]>([
  {
    field: 'name',
    label: t('common.name')
  },
  {
    field: 'cversion',
    label: t('common.cversion')
  },
  {
    field: 'lversion',
    label: t('common.lversion'),
    formatter: (row: Recordable, __: TableColumn, cellValue: string) => {
      if (row.cversion != row.lversion) {
        updateFlag.value = true
        const msgArray = row.msg.split('\\n')
        let content = ''
        msgArray.forEach((line) => {
          content += `<div>${line}</div>`
        })
        return h(
          ElTooltip,
          {
            placement: 'top',
            content: content,
            rawContent: true
          },
          [
            h(
              ElText,
              {
                type: 'danger'
              },
              cellValue
            )
          ]
        )
      } else {
        return h(ElText, cellValue)
      }
    }
  }
])

let nodeUsageData: Ref<{ name: string; cpuNum: number; memNum: number }[]> = ref([])

const nodeData = ref<
  {
    name: string
    running: number
    finished: number
    state: number
    cpuNum: number
    memNum: number
  }[]
>([])

const getNodeState = async () => {
  try {
    const res = await getNodeDataApi()
    if (res && res.data && Array.isArray(res.data.list)) {
      nodeData.value = res.data.list.map((node) => ({
        name: node.name,
        running: node.running,
        state: node.state,
        finished: node.finished,
        cpuNum: node.cpuNum,
        memNum: node.memNum
      }))
      nodeUsageData.value = reactive(
        res.data.list.map((node) => ({
          name: node.name,
          cpuNum: node.cpuNum,
          memNum: node.memNum
        }))
      )
    }
  } catch (error) {
    console.error('Error fetching node data:', error)
  } finally {
    // 不论请求成功或失败，都会执行的代码块
    loading.value = false
  }
}

const taskData = ref<
  {
    name: string
    taskNum: string
    progress: string
    creatTime: string
  }[]
>([])

const getTaskData = async () => {
  const res = await getTaskDataApi('', 1, 10)
  console.log(res)
  taskData.value = reactive(
    res.data.list.map((task) => ({
      name: task.name,
      taskNum: task.taskNum,
      progress: task.progress,
      creatTime: task.creatTime
    }))
  )
}

const versionData = ref<
  {
    name: string
    cversion: string
    lversion: string
    msg: string
  }[]
>([])

const getVersionData = async () => {
  const res = await getVersionDataApi()
  console.log(res)
  versionData.value = reactive(
    res.data.list.map((v) => ({
      name: v.name,
      cversion: v.cversion,
      lversion: v.lversion,
      msg: v.msg
    }))
  )
}

const getAllApi = async () => {
  await Promise.all([getNodeState(), getTaskData()])
  loading.value = false
}
getVersionData()
getAllApi()
const { isLeader: dashLeader } = useSingleTabLeader('dashboard')
useVisibleInterval(() => {
  if (!dashLeader.value) return
  getAllApi()
}, 10000)



const pluginKey = ref('')
const updateSystem = async () => {
  const key = localStorage.getItem(`plugin_key`) as string
  if (!key) {
    keyDialogVisible.value = true
  } else {
    UpdatedialogVisible.value = true
  }
}
const savePluginKey = async () => {
  if (pluginKey.value) {
    const res = await checkKeyApi(pluginKey.value)
    if (res.code == 200) {
      localStorage.setItem('plugin_key', pluginKey.value)
      keyDialogVisible.value = false
      UpdatedialogVisible.value = true
    }
  }
}
const updateFlag = ref(false)

const form = ref({
  server: 'https://github.com/Autumn-27/ScopeSentry/archive/refs/heads/main.zip',
  scan: ''
})
const UpdatedialogVisible = ref(false)
const keyDialogVisible = ref(false)
async function handleSubmit() {
  const key = localStorage.getItem(`plugin_key`) as string
  const res = await UPDATEsYSTEMApi(form.value.server, form.value.scan, key)
  if (res.code == 505) {
    localStorage.removeItem('plugin_key')
  }
}
</script>

<template>
  <PanelGroup />
  <ElCard v-if="serverHealth" shadow="never" class="mb-12px server-health">
    <div class="server-health__row">
      <span class="server-health__title">{{ t('dashboard.serverHealth') }}</span>
      <ElTag size="small" effect="plain" type="info">v{{ serverHealth.version }}</ElTag>
      <ElTag size="small" effect="plain" :type="healthTone(loadRatio, 0.9, 1.5)">
        {{ t('dashboard.load') }} {{ serverHealth.host?.load1 }} / {{ serverHealth.host?.cpus }} 核
      </ElTag>
      <ElTag size="small" effect="plain" :type="healthTone(100 - (serverHealth.host?.memAvailablePercent || 100), 80, 92)">
        {{ t('dashboard.memAvailable') }} {{ (serverHealth.host?.memAvailablePercent || 0).toFixed(0) }}%
      </ElTag>
      <ElTag size="small" effect="plain" :type="healthTone(100 - (serverHealth.host?.diskFreePercent || 100), 80, 92)">
        {{ t('dashboard.diskFree') }} {{ (serverHealth.host?.diskFreePercent || 0).toFixed(0) }}%
      </ElTag>
      <!-- 依赖状态：宿主健康但 Mongo/Redis 挂了，服务端一样不能用 -->
      <ElTag size="small" effect="plain" :type="serverHealth.checks?.mongo?.ok ? 'success' : 'danger'">
        MongoDB {{ serverHealth.checks?.mongo?.ok ? t('dashboard.depOk') : t('dashboard.depDown') }}
      </ElTag>
      <ElTag size="small" effect="plain" :type="serverHealth.checks?.redis?.ok ? 'success' : 'danger'">
        Redis {{ serverHealth.checks?.redis?.ok ? t('dashboard.depOk') : t('dashboard.depDown') }}
      </ElTag>
    </div>
  </ElCard>
  <ElRow :gutter="20" justify="space-between">
    <ElCol :xl="12" :lg="12" :md="24" :sm="24" :xs="24">
      <ElCard shadow="hover" class="mb-20px">
        <template #header>
          <span>{{ t('dashboard.nodeInfo') }}</span>
        </template>
        <Table :columns="nodeColumns" :data="nodeData" stripe :border="false" :height="250" />
      </ElCard>
    </ElCol>
    <ElCol :xl="12" :lg="12" :md="24" :sm="24" :xs="24">
      <ElCard shadow="hover" class="mb-20px">
        <template #header>
          <span>{{ t('dashboard.taskInfo') }}</span>
        </template>
        <Table
          :columns="taskColums"
          :data="taskData"
          stripe
          :border="false"
          :height="250"
          :tooltip-options="{
            offset: 1,
            showArrow: false,
            effect: 'dark',
            enterable: false,
            showAfter: 0,
            popperOptions: {},
            popperClass: 'test',
            placement: 'bottom',
            hideAfter: 0,
            disabled: true
          }"
        />
      </ElCard>
    </ElCol>
    <ElCol :span="12">
      <ElCard shadow="hover" class="mb-25px">
        <template #header>
          <div>
            <span>{{ t('node.nodeUsageStatus') }}</span>
          </div>
        </template>
        <Table
          :columns="nodeUsageColumns"
          :data="nodeUsageData"
          :highlightCurrentRow="false"
          stripe
          :border="false"
          :height="600"
        />
      </ElCard>
    </ElCol>
    <ElCol :span="12">
      <ElCard shadow="hover" class="mb-25px">
        <template #header>
          <ElRow>
            <ElCol :span="12">
              <div>
                <span>{{ t('common.version') }}</span>
                <ElText
                  v-if="updateFlag"
                  type="danger"
                  size="small"
                  style="position: relative; left: 1rem"
                  >*{{ t('common.updatemsg') }}</ElText
                >
              </div>
            </ElCol>
            <!-- <ElCol :span="3" :offset="8" v-if="updateFlag">
              <ElPopconfirm title="Are you sure?" @confirm="updateSystem">
                <template #reference>
                  <ElButton color="#626aef">
                    {{ t('common.update') }}
                  </ElButton>
                </template>
              </ElPopconfirm>
            </ElCol> -->
          </ElRow>
        </template>
        <Table :columns="versionColumns" :data="versionData" stripe :border="false" :height="600" />
      </ElCard>
    </ElCol>
  </ElRow>
  <Dialog
    v-model="UpdatedialogVisible"
    :title="t('common.update')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="430"
  >
    <ElText type="danger" size="small" style="position: relative; left: 1rem">{{
      t('localization.updateHelp')
    }}</ElText>
    <ElForm :model="form" label-width="120px" class="upload-form">
      <ElFormItem :label="t('localization.serverUrl')">
        <ElInput v-model="form.server" :placeholder="t('localization.serverUrl')" />
      </ElFormItem>
      <ElFormItem :label="t('localization.scanUrl')">
        <ElInput
          v-model="form.scan"
          placeholder="scan url(https://github.com/Autumn-27/ScopeSentry-Scan/releases/download/vx.x.x/ScopeSentry-Scan_linux_amd64_vx.x.x.zip)"
        />
      </ElFormItem>
      <ElFormItem>
        <ElButton type="primary" @click="handleSubmit">{{ t('common.submit') }}</ElButton>
      </ElFormItem>
    </ElForm>
  </Dialog>
  <Dialog
    v-model="keyDialogVisible"
    :title="t('plugin.key')"
    center
    width="30%"
    style="max-width: 400px; height: 200px"
  >
    <div class="flex flex-col gap-2">
      <el-tooltip class="item" effect="dark" :content="t('plugin.keyMsg')" placement="top">
        <ElInput v-model="pluginKey" />
      </el-tooltip>
      <BaseButton @click="savePluginKey" type="primary" class="w-full">{{
        t('common.ok')
      }}</BaseButton>
    </div>
  </Dialog>
</template>

<style scoped>
.server-health__row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.server-health__title {
  font-weight: 600;
  margin-right: 4px;
}

.header-container {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100%;
}
.tooltip-content {
  white-space: pre-line !important;
}
</style>
