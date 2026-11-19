<script setup lang="tsx">
import { confirmDialog } from '@/utils/confirm'
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { ref, reactive, h } from 'vue'
import { ElScrollbar, ElTag, ElTooltip } from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
// import { useIcon } from '@/hooks/web/useIcon'
import { onMounted } from 'vue'
import { Dialog } from '@/components/Dialog'
import { BaseButton } from '@/components/Button'
import Configuration from './components/Configuration.vue'
import plugin from './components/plugin.vue'
import { getNodeDataApi, deleteNodeApi, getNodeLogApi, restartNodeApi, clearNodeQueueApi } from '@/api/node'
const { t } = useI18n()
const nodeColums = reactive<TableColumn[]>([
  {
    field: 'selection',
    type: 'selection',
    width: 55
  },
  {
    field: 'name',
    label: t('node.nodeName'),
    minWidth: 180
  },
  {
    field: 'maxTaskNum',
    label: t('configuration.maxTaskNum'),
    width: 110,
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      return h(
        ElTag,
        {
          type: 'info'
        },
        () => cellValue
      )
    }
  },
  {
    field: 'running',
    label: t('node.taskCount'),
    width: 110,
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      return h(
        ElTag,
        {
          round: true,
          effect: 'plain',
          hit: true
        },
        () => cellValue
      )
    }
  },
  {
    field: 'finished',
    label: t('node.finished'),
    width: 110,
    formatter: (_: Recordable, __: TableColumn, cellValue: string) => {
      return h(
        ElTag,
        {
          round: true,
          effect: 'plain',
          hit: true
        },
        () => cellValue
      )
    }
  },
  {
    field: 'cpuNum',
    label: t('node.nodeUsageCpu'),
    width: 100,
    formatter: (_: Recordable, __: TableColumn, cellValue: string) => {
      let numericValue = parseFloat(cellValue)
      numericValue = parseFloat(numericValue.toFixed(2))
      return h(
        ElTag,
        {
          round: true,
          effect: 'plain',
          hit: true,
          type: numericValue < 50 ? '' : numericValue < 80 ? 'warning' : 'danger'
        },
        () => numericValue + '%'
      )
    }
  },
  {
    field: 'memNum',
    label: t('node.nodeUsageMemory'),
    width: 110,
    formatter: (_: Recordable, __: TableColumn, cellValue: string) => {
      let numericValue = parseFloat(cellValue)
      numericValue = parseFloat(numericValue.toFixed(2))
      return h(
        ElTag,
        {
          round: true,
          effect: 'plain',
          hit: true,
          type: numericValue < 50 ? '' : numericValue < 80 ? 'warning' : 'danger'
        },
        () => numericValue + '%'
      )
    }
  },
  {
    field: 'state',
    label: t('node.nodeStatus'),
    width: 110,
    formatter: (_: Recordable, __: TableColumn, cellValue: string) => {
      return h(
        ElTag,
        {
          type: cellValue === '1' ? 'success' : cellValue === '2' ? 'warning' : 'danger',
          effect: 'light',
          hit: true
        },
        () =>
          cellValue === '1'
            ? t('node.statusRun')
            : cellValue === '2'
              ? t('node.statusStop')
              : t('node.statusError')
      )
    }
  },
  {
    field: 'queueLen',
    label: t('node.queueLen'),
    width: 100,
    formatter: (_: Recordable, __: TableColumn, cellValue: number) => {
      // 队列积压说明有任务在排队（节点一次只跑一个），>5 用警告色提示
      const v = cellValue || 0
      return h(ElTag, { type: v > 5 ? 'danger' : v > 0 ? 'warning' : 'info' }, () => v)
    }
  },
  {
    field: 'updateTime',
    label: t('node.updateTime'),
    // 时间戳需要约 175px，小于这个值右侧会被截断
    width: 180,
    showOverflowTooltip: true
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    /* 按钮实际 ~176px；不再固定，避免脱离文档流压住上一列 */
    width: 220,
    formatter: (row, __: TableColumn, _: number) => {
      console.log(row)
      return (
        <>
          <BaseButton type="warning" size="small" onClick={() => openPlugin(row.name)}>
            {t('node.plugin')}
          </BaseButton>
          <BaseButton type="success" size="small" onClick={() => openLogDialogVisible(row)}>
            {t('node.log')}
          </BaseButton>
          <BaseButton type="primary" size="small" onClick={() => openConfig(row)}>
            {t('common.config')}
          </BaseButton>
          <ElTooltip content={t('node.clearQueueMsg')}>
            <BaseButton type="info" size="small" onClick={() => clearQueue(row.name)}>
              {t('node.clearQueue')}
            </BaseButton>
          </ElTooltip>
          <ElTooltip content={t('node.restartMsg')}>
            <BaseButton type="danger" size="small" onClick={() => restartNode(row.name)}>
              {t('node.restart')}
            </BaseButton>
          </ElTooltip>
        </>
      )
    }
  }
])
// 清空节点待领取队列：任务洪泛时不用再登服务器 redis-cli
const clearQueue = async (name: string) => {
  try {
    await ElMessageBox.confirm(
      t('node.clearQueueConfirm', { name }),
      t('common.delWarning'),
      { confirmButtonText: 'OK', cancelButtonText: t('common.cancel'), type: 'warning' }
    )
  } catch {
    return
  }
  try {
    const res: any = await clearNodeQueueApi(name)
    ElMessage.success(t('node.clearQueueDone', { n: res?.data?.cleared ?? 0 }))
    getList()
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  }
}

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const res = await getNodeDataApi()
    return {
      list: res.data.list
    }
  }
})
const { loading, dataList, currentPage, pageSize } = tableState
const { getList, getElTableExpose } = tableMethods
function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}
const dialogVisible = ref(false)
const closeDialog = () => {
  dialogVisible.value = false
}
const detailData = reactive({
  name: '',
  maxTaskNum: '',
  state: '',
  ModulesConfig: ''
})
const openConfig = async (data) => {
  detailData.name = data.name
  detailData.maxTaskNum = data.maxTaskNum
  detailData.ModulesConfig = data.modulesConfig
  detailData.state = data.state
  dialogVisible.value = true
}
const restartNode = async (name) => {
  await restartNodeApi(name)
}
const delLoading = ref(false)
// 未勾选任何行时，工具条上的批量删除应禁用
const tableSelectedCount = ref(0)
const handleTableSelectionChange = (rows: unknown[]) => {
  tableSelectedCount.value = rows?.length ?? 0
}
const names = ref<string[]>([])
const delSelect = async () => {
  const elTableExpose = await getElTableExpose()
  const selectedRows = elTableExpose?.getSelectionRows() || []
  names.value = selectedRows.map((row) => row.name)
  delLoading.value = true
  try {
    const res = await deleteNodeApi(names.value)
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    delLoading.value = false
    getList()
  }
}
const confirmDelete = async () => {
  const confirmed = await confirmDialog(t('common.delMessage'))
  if (confirmed) {
    await delSelect()
  }
}
const logDialogVisible = ref(false)
const closeLogDialogVisible = () => {
  logDialogVisible.value = false
}
const logContent = ref('')
const scrollbarRef = ref<InstanceType<typeof ElScrollbar>>()
const logLoading = ref(false)
const openLogDialogVisible = async (data) => {
  // 大日志接口要几百毫秒到几秒：先开弹窗显示 loading，再填内容，
  // 否则点完「日志」会有一段毫无反应的空白（2026-11-20）。
  logContent.value = ''
  logLoading.value = true
  logDialogVisible.value = true
  try {
    const res = await getNodeLogApi(data.name)
    logContent.value = res.data.logs || ''
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    logLoading.value = false
  }
}
onMounted(() => {
  setMaxHeight()
  window.addEventListener('resize', setMaxHeight)
})

const maxHeight = ref(0)

const setMaxHeight = () => {
  const screenHeight = window.innerHeight || document.documentElement.clientHeight
  maxHeight.value = screenHeight * 0.7
}

const nodeName = ref('')
const pluginDialogVisible = ref(false)
const openPlugin = async (data) => {
  nodeName.value = data
  pluginDialogVisible.value = true
}
const closepluginDialogVisible = () => {
  pluginDialogVisible.value = false
}
</script>

<template>
  <ContentWrap>
    <!-- <ElRow :gutter="20" style="margin-bottom: 15px">
      <ElCol :span="1.5">
        <ElText class="mx-1" style="position: relative; top: 8px">{{ t('node.nodeName') }}:</ElText>
      </ElCol>
      <ElCol :span="5">
        <ElInput v-model="search" :placeholder="t('common.inputText')" style="height: 36px" />
      </ElCol>
      <ElCol :span="5" style="position: relative; left: 16px">
        <ElButton type="primary" :icon="searchicon" @click="handleSearch"
          >{{ t('common.search') }}</ElButton
        >
      </ElCol>
    </ElRow> -->
    <div class="node-toolbar">
      <BaseButton
        type="danger"
        :loading="delLoading"
        :disabled="tableSelectedCount === 0"
        @click="confirmDelete"
      >
        {{ t('common.delete') }}
      </BaseButton>
    </div>
    <div>
      <Table
        v-model:pageSize="pageSize"
        v-model:currentPage="currentPage"
        :columns="nodeColums"
        :data="dataList"
        stripe
        :border="true"
        :loading="loading"
        :resizable="true"
        @selection-change="handleTableSelectionChange"
      @register="tableRegister"
        :headerCellStyle="tableHeaderColor"
        :style="{
          fontFamily:
            '-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica Neue,Arial,Noto Sans,sans-serif,Apple Color Emoji,Segoe UI Emoji,Segoe UI Symbol,Noto Color Emoji'
        }"
      />
    </div>
  </ContentWrap>
  <Dialog
    v-model="dialogVisible"
    :title="$t('common.config')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="maxHeight"
  >
    <Configuration :closeDialog="closeDialog" :nodeConfForm="detailData" :getList="getList" />
  </Dialog>
  <Dialog
    v-model="logDialogVisible"
    :title="t('node.log')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="maxHeight"
  >
    <ElScrollbar ref="scrollbarRef">
      <div v-if="logLoading" class="log-loading">
        <span class="spinner" />
        <span>{{ t('node.logLoading') }}</span>
      </div>
      <pre v-else-if="logContent">{{ logContent }}</pre>
      <div v-else class="log-empty">{{ t('node.logEmpty') }}</div>
    </ElScrollbar>
    <template #footer>
      <BaseButton @click="closeLogDialogVisible">{{ t('common.off') }}</BaseButton>
    </template>
  </Dialog>

  <Dialog
    v-model="pluginDialogVisible"
    :title="t('node.plugin')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
    :maxHeight="maxHeight"
  >
    <plugin :closeDialog="closepluginDialogVisible" :name="nodeName" />
  </Dialog>
</template>

<style scoped>
/* 日志弹窗的加载与空态：此前点击后是一片空白，看不出在加载还是没日志 */
.log-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 24px 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.spinner {
  width: 14px;
  height: 14px;
  border: 2px solid var(--el-border-color);
  border-top-color: var(--el-color-primary);
  border-radius: 50%;
  animation: node-log-spin 0.8s linear infinite;
}
@keyframes node-log-spin {
  to {
    transform: rotate(360deg);
  }
}
.log-empty {
  padding: 24px 0;
  text-align: center;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.node-toolbar {
  margin-bottom: 12px;
}
</style>
