<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { ref, reactive, onMounted, h, inject, computed, type Ref } from 'vue'
import { ArrowDown, Search } from '@element-plus/icons-vue'
import {
  ElButton,
  ElCol,
  ElInput,
  ElRow,
  ElText,
  ElMessageBox,
  ElTag,
  ElTooltip,
  ElScrollbar,
  UploadInstance,
  UploadProps,
  UploadRawFile,
  ElUpload,
  ElMessage,
  ElDropdownItem,
  ElDropdownMenu,
  ElDropdown,
  ElIcon,
  ElBadge,
  ElDrawer,
  ElSpace,
  ElSwitch
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useIcon } from '@/hooks/web/useIcon'
import { Dialog } from '@/components/Dialog'
import { BaseButton } from '@/components/Button'
import {
  checkKeyApi,
  cleanAllPluginLogApi,
  cleanPluginLogApi,
  deletePluginDataApi,
  getPluginDataApi,
  getPluginLogApi,
  reCheckPluginApi,
  reInstallPluginApi,
  uninstallPluginApi
} from '@/api/plugins'
import detail from './detail.vue'
import { useUserStore } from '@/store/modules/user'

const searchicon = useIcon({ icon: 'iconoir:search' })
const { t } = useI18n()
const search = ref('')
const handleSearch = () => {
  // 搜索等于换结果集：不回第 1 页的话，停在第 N 页搜一个小结果会看到空表（2026-11-27）
  if (currentPage.value !== 1) {
    currentPage.value = 1
    return
  }
  getList()
}

// 从父组件注入插件市场相关方法
const openMarketDialog = inject<() => void>('openMarketDialog', () => {})
const pendingPluginsCount = inject<Ref<number>>('pendingPluginsCount', ref(0))
const pendingPluginsCountValue = computed(() => pendingPluginsCount.value)

const taskColums = reactive<TableColumn[]>([
  {
    field: 'index',
    label: t('tableDemo.index'),
    type: 'index',
    width: 50
  },
  {
    field: 'selection',
    type: 'selection',
    width: 44,
    selectable: (row) => !row.isSystem
  },
  {
    field: 'name',
    label: t('plugin.name'),
    minWidth: 170,
    showOverflowTooltip: true,
    formatter: (row, __: TableColumn, value: string) => {
      return (
        <a
          href={`https://plugin.scope-sentry.top/plugin/${row.hash}`}
          style="color: #409EFF; text-decoration: none;"
          target="_blank"
        >
          {value}
        </a>
      )
    }
  },
  {
    field: 'module',
    label: t('plugin.module'),
    /* 模块是定长枚举（PortScanPreparation 最长 19 字符），按最长值给宽度，避免出现省略号 */
    width: 172,
    formatter: (_, __: TableColumn, value: string) => {
      const color = moduleColorMap[value] || '#FFFFFF' // 默认白色
      return <ElTag style={{ backgroundColor: color, color: '#000' }}>{value}</ElTag>
    }
  },
  {
    field: 'version',
    label: t('plugin.version'),
    width: 70
  },
  {
    field: 'action',
    label: t('tableDemo.action'),
    /* 按内容收口：操作 / 日志 / 编辑 / 更多 四个控件。
       实测 56 + 44 + 44 + 44 + 3×6(gap) = 206，加 24 单元格内边距 = 230 < 260，留 30px 余量；
       之前是「操作 + 日志 + 清除日志 + 编辑 + 删除」5 个控件共 304px，挤出单元格 6px 被裁。
       「清除日志 / 删除」收进「更多」下拉，不再占行内宽度。 */
    width: 260,
    formatter: (row, __: TableColumn, _: number) => {
      const handleCommand = (command) => {
        switch (command) {
          case 'reinstall':
            reInstallPluginApi('all', row.hash, row.module)
            break
          case 'recheck':
            reCheckPluginApi('all', row.hash, row.module)
            break
          case 'uninstall':
            uninstallPluginApi('all', row.hash, row.module)
            break
        }
      }
      const handleMoreCommand = (command) => {
        switch (command) {
          case 'cleanLog':
            confirmCleanLog(row.hash, row.module)
            break
          case 'delete':
            // 系统插件不可删，菜单项已置灰，这里再兜一层
            if (!row.isSystem) confirmDelete(row.hash, row.module)
            break
        }
      }
      const renderDropdown = (label: string, items, onCommand: (cmd: string) => void) =>
        h(
          ElDropdown,
          { onCommand },
          {
            default: () =>
              h(
                ElButton,
                { style: { outline: 'none', boxShadow: 'none' } },
                () => [label, h(ElIcon, {}, () => h(ArrowDown))]
              ),
            dropdown: () => h(ElDropdownMenu, null, () => items)
          }
        )
      return (
        <div style="display: flex; align-items: center; gap: 6px">
          {renderDropdown(
            t('common.operation'),
            [
              h(ElDropdownItem, { command: 'reinstall' }, () => t('plugin.reInstall')),
              h(ElDropdownItem, { command: 'recheck' }, () => t('plugin.reCheck')),
              h(ElDropdownItem, { command: 'uninstall' }, () => t('plugin.uninstall'))
            ],
            handleCommand
          )}
          <BaseButton onClick={() => openLogDialogVisible(row)}>{t('common.log')}</BaseButton>
          <BaseButton type="primary" onClick={() => editPlugin(row.id)}>
            {t('common.edit')}
          </BaseButton>
          {renderDropdown(
            t('common.more'),
            [
              h(ElDropdownItem, { command: 'cleanLog' }, () => t('common.cleanLog')),
              h(
                ElDropdownItem,
                { command: 'delete', disabled: row.isSystem, divided: true },
                () => t('common.delete')
              )
            ],
            handleMoreCommand
          )}
        </div>
      )
    }
  }
])

const moduleColorMap = {
  TargetHandler: '#2243dda6', // 浅红色
  SubdomainScan: '#FF9B85', // 更深的浅橙色
  SubdomainSecurity: '#FFFFBA', // 浅黄色
  PortScanPreparation: '#BAFFB3', // 浅绿色
  PortScan: '#BAE1FF', // 浅蓝色
  AssetMapping: '#e3ffba', // 浅粉红色
  URLScan: '#D1BAFF', // 浅紫色
  WebCrawler: '#FFABAB', // 浅红
  DirScan: '#3ccde6', // 选择浅桃色
  VulnerabilityScan: '#FF677D', // 浅粉色
  AssetHandle: '#B2E1FF', // 浅青色
  PortFingerprint: '#ffb5e4', // 更亮的浅橙色
  URLSecurity: '#FFE4BA', // 浅米色
  PassiveScan: '#A2DFF7'
}

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    // 不传递 type 参数，默认为扫描端插件
    const res = await getPluginDataApi(search.value, currentPage.value, pageSize.value, 'scan')
    return {
      list: res.data.list,
      total: res.data.total
    }
  },
  immediate: true
})
const { loading, dataList, total, currentPage, pageSize } = tableState
pageSize.value = 20
const { getList, getElTableExpose } = tableMethods
function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}
const dialogVisible = ref(false)

let DialogTitle = t('plugin.new')
const closeDialog = () => {
  dialogVisible.value = false
}

const confirmDeleteSelect = async () => {
  ElMessageBox({
    showCancelButton: true,
    cancelButtonText: t('common.cancel'),
    message: t('common.delMessage'),
    title: t('common.delete'),
    draggable: true
  }).then(async () => {
    await delSelect()
  })
}

const confirmDelete = async (hash: string, module: string) => {
  ElMessageBox({
    showCancelButton: true,
    cancelButtonText: t('common.cancel'),
    title: t('common.delete'),
    message: t('common.delMessage'),
    draggable: true
  }).then(async () => {
    await del(hash, module)
  })
}

const confirmCleanLog = async (hash: string, module: string) => {
  ElMessageBox({
    showCancelButton: true,
    cancelButtonText: t('common.cancel'),
    title: t('common.cleanLog'),
    message: t('localization.cleanLogsConfirm'),
    draggable: true
  })
    .then(async () => {
      await cleanPluginLogApi(module, hash)
      ElMessage.success(t('common.cleanLog') + ' ' + t('common.success'))
      // 抽屉正好开着这个插件时同步清掉，否则用户看到的是被清掉的旧日志
      if (logHash.value === hash) {
        logContent.value = ''
      }
    })
    .catch(() => {
      // 取消确认也会 reject，不提示
    })
}

const confirmCleanAllLog = async () => {
  ElMessageBox({
    showCancelButton: true,
    cancelButtonText: t('common.cancel'),
    title: t('common.cleanAllLog'),
    message: t('localization.cleanAllLogsConfirm'),
    draggable: true
  })
    .then(async () => {
      await cleanAllPluginLogApi()
      ElMessage.success(t('common.cleanAllLog') + ' ' + t('common.success'))
      logContent.value = ''
    })
    .catch(() => {})
}

const delLoading = ref(false)
// 未勾选任何插件时，批量删除应禁用（系统插件不可选，此时按钮保持禁用）
const tableSelectedCount = ref(0)
const handleTableSelectionChange = (rows: unknown[]) => {
  tableSelectedCount.value = rows?.length ?? 0
}
const del = async (hash: string, module: string) => {
  delLoading.value = true
  try {
    const res = await deletePluginDataApi([{ hash, module }])
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    delLoading.value = false
    getList()
  }
}
const delSelect = async () => {
  const elTableExpose = await getElTableExpose()
  const selectedRows = elTableExpose?.getSelectionRows() || []
  const deleteData = selectedRows.map((row) => ({
    hash: row.hash,
    module: row.module
  }))

  delLoading.value = true
  try {
    const res = await deletePluginDataApi(deleteData)
    console.log('Data deleted successfully:', res)
    delLoading.value = false
    getList()
  } catch (error) {
    console.error('Error deleting data:', error)
    delLoading.value = false
    getList()
  }
}

const addPlugin = async () => {
  id.value = ''
  dialogVisible.value = true
}

const id = ref('')

const editPlugin = async (data) => {
  id.value = data
  DialogTitle = t('common.edit')
  dialogVisible.value = true
}
const keyDialogVisible = ref(false)
const pluginKey = ref('')

const LoadPluginKey = () => {
  const key = localStorage.getItem(`plugin_key`) as string
  // 进页面不再自动弹「插件 Key」对话框：工具栏里就能直接填，
  // 只有真正需要校验（打开插件市场/安装）时才提示。
  pluginKey.value = key || ''
}

const savePluginKey = async () => {
  if (pluginKey.value) {
    const res = await checkKeyApi(pluginKey.value)
    if (res.code == 200) {
      localStorage.setItem('plugin_key', pluginKey.value)
      keyDialogVisible.value = false
    }
  }
}

const handlePluginKeyChange = () => {
  if (pluginKey.value) {
    localStorage.setItem('plugin_key', pluginKey.value)
  } else {
    localStorage.removeItem('plugin_key')
  }
}

onMounted(() => {
  setMaxHeight()
  window.addEventListener('resize', setMaxHeight)
  LoadPluginKey()
})

const maxHeight = ref(0)

const setMaxHeight = () => {
  const screenHeight = window.innerHeight || document.documentElement.clientHeight
  maxHeight.value = screenHeight * 0.7
}
const logDialogVisible = ref(false)
const closeLogDialogVisible = () => {
  logDialogVisible.value = false
  logSearchText.value = ''
}
const logContent = ref('')
const logSearchText = ref('')
const logScrollbarRef = ref<InstanceType<typeof ElScrollbar>>()
const autoScroll = ref(true)

const logModule = ref('')
const logHash = ref('')
const openLogDialogVisible = async (data) => {
  logModule.value = data.module
  logHash.value = data.hash
  await refreshLog()
  logDialogVisible.value = true
  // 等待DOM更新后滚动到底部
  setTimeout(() => {
    scrollToBottom()
  }, 100)
}

const refreshLog = async () => {
  const res = await getPluginLogApi(logModule.value, logHash.value)
  logContent.value = res.data.data
  if (autoScroll.value) {
    setTimeout(() => {
      scrollToBottom()
    }, 50)
  }
}

const cleanLog = async () => {
  await cleanPluginLogApi(logModule.value, logHash.value)
  logContent.value = ''
  ElMessage.success(t('common.cleanLog') + ' ' + t('common.success'))
}

const scrollToBottom = () => {
  if (logScrollbarRef.value) {
    const scrollbar = logScrollbarRef.value
    const scrollbarEl = scrollbar.$el
    const wrapEl = scrollbarEl?.querySelector('.el-scrollbar__wrap')
    if (wrapEl) {
      wrapEl.scrollTop = wrapEl.scrollHeight
    }
  }
}

const copyLog = async () => {
  if (logContent.value) {
    try {
      await navigator.clipboard.writeText(logContent.value)
      ElMessage.success(t('common.copySuccess'))
    } catch (error) {
      ElMessage.error(t('common.copyFailed'))
    }
  }
}

// 过滤后的日志内容（用于搜索高亮）
const filteredLogContent = computed(() => {
  if (!logSearchText.value || !logContent.value) {
    return logContent.value
  }
  const searchText = logSearchText.value
  const lines = logContent.value.split('\n')
  return lines.filter((line) => line.toLowerCase().includes(searchText.toLowerCase())).join('\n')
})

// 高亮搜索关键词
const highlightLog = (text: string) => {
  if (!logSearchText.value || !text) {
    return text
  }
  const searchText = logSearchText.value
  const regex = new RegExp(`(${searchText})`, 'gi')
  return text.replace(regex, '<mark>$1</mark>')
}
const userStore = useUserStore()
const uploadHeaders = { Authorization: `${userStore.getToken}` }
const upload = ref<UploadInstance>()
const handleExceed: UploadProps['onExceed'] = (files) => {
  upload.value!.clearFiles()
  const file = files[0] as UploadRawFile
  upload.value!.handleStart(file)
}

const handleUploadSuccess = (response) => {
  console.log(response)
  if (response.code === 200) {
    ElMessage.success(t('localization.uploadSuccess'))
  } else {
    ElMessage.error(response.message)
  }
  if (response.code == 505) {
    localStorage.removeItem('plugin_key')
  }
  getList()
  upload.value?.clearFiles()
}
const handleFileChange = (_file, fileList) => {
  if (fileList.length > 0) {
    upload.value!.submit()
  }
}

// 暴露刷新列表方法给父组件
defineExpose({
  refreshList: getList
})
</script>

<template>
  <ContentWrap>
    <ElRow class="locale-search-row">
      <ElCol :span="1">
        <ElText class="mx-1">{{ t('plugin.name') }}:</ElText>
      </ElCol>
      <ElCol :span="5">
        <ElInput v-model="search" :placeholder="t('common.inputText')" style="height: 36px" />
      </ElCol>
      <ElCol :span="5">
        <ElButton type="primary" :icon="searchicon" @click="handleSearch">{{
          t('common.search')
        }}</ElButton>
      </ElCol>
      <ElCol :span="1">
        <ElText class="mx-1">{{ t('plugin.key') }}:</ElText>
      </ElCol>
      <ElCol :span="5">
        <ElInput
          v-model="pluginKey"
          :placeholder="t('plugin.key')"
          style="height: 36px"
          @blur="handlePluginKeyChange"
        >
          <template #append>
            <ElButton @click="keyDialogVisible = true">{{ t('common.config') }}</ElButton>
          </template>
        </ElInput>
      </ElCol>
    </ElRow>
    <ElRow :gutter="16" class="mt-4">
      <ElCol :xs="24" :sm="24" :md="24" :lg="24" :xl="24">
        <div class="flex flex-wrap gap-3 items-center">
          <BaseButton type="primary" @click="addPlugin">
            {{ t('plugin.new') }}
          </BaseButton>

          <BaseButton
            type="danger"
            :loading="delLoading"
            :disabled="tableSelectedCount === 0"
            @click="confirmDeleteSelect"
          >
            {{ t('plugin.delete') }}
          </BaseButton>

          <ElBadge
            :value="pendingPluginsCountValue"
            :hidden="pendingPluginsCountValue === 0"
            :max="99"
          >
            <BaseButton type="success" @click="openMarketDialog">
              {{ t('plugin.market') }}
            </BaseButton>
          </ElBadge>

          <BaseButton type="warning" @click="confirmCleanAllLog">
            {{ t('common.cleanAllLog') }}
          </BaseButton>

          <ElUpload
            ref="upload"
            class="flex items-center"
            :action="'/api/plugin/import?key=' + (pluginKey || '')"
            :headers="uploadHeaders"
            :on-success="handleUploadSuccess"
            :limit="1"
            :on-exceed="handleExceed"
            :auto-upload="false"
            @change="handleFileChange"
          >
            <template #trigger>
              <ElButton class="plugin-import-btn">
                <Icon icon="ep:upload" :size="14" style="margin-right: 4px" />
                {{ t('plugin.import') }}
              </ElButton>
            </template>
          </ElUpload>
        </div>
      </ElCol>
    </ElRow>
    <div style="position: relative; top: 12px">
      <Table
        v-model:pageSize="pageSize"
        v-model:currentPage="currentPage"
        :columns="taskColums"
        :data="dataList"
        stripe
        :border="true"
        :loading="loading"
        :resizable="true"
        scrollbar-always-on
        :pagination="{
          total: total,
          pageSizes: [20, 30, 50, 100, 200, 500, 1000]
        }"
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
  <ElDrawer v-model="dialogVisible" size="50%" direction="rtl">
    <template #title>
      <div style="display: flex; align-items: center; gap: 16px; width: 100%">
        <span style="font-weight: 500; white-space: nowrap">{{ DialogTitle }}</span>
        <span style="color: #f56c6c; font-size: 12px; font-weight: normal; line-height: 1.4">
          {{ t('plugin.parameterConfigTip') }}
        </span>
      </div>
    </template>
    <detail :closeDialog="closeDialog" :getList="getList" :id="id" tp="scan" />
  </ElDrawer>
  <ElDrawer
    v-model="logDialogVisible"
    :title="t('node.log')"
    size="80%"
    direction="rtl"
    :with-header="true"
  >
    <template #header>
      <div style="display: flex; align-items: center; justify-content: space-between; width: 100%">
        <span style="font-weight: 500">{{ t('node.log') }}</span>
        <ElSpace>
          <ElInput
            v-model="logSearchText"
            :placeholder="t('common.search')"
            clearable
            style="width: 200px"
          >
            <template #prefix>
              <ElIcon><Search /></ElIcon>
            </template>
          </ElInput>
          <ElSwitch v-model="autoScroll" :active-text="t('common.autoScroll')" inactive-text="" />
        </ElSpace>
      </div>
    </template>
    <div style="display: flex; flex-direction: column; height: 100%">
      <ElScrollbar ref="logScrollbarRef" style="flex: 1; height: 0">
        <div
          style="
            padding: 16px;
            background: #1e1e1e;
            color: #d4d4d4;
            font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
            font-size: 13px;
            line-height: 1.6;
            min-height: 100%;
            white-space: pre-wrap;
            word-wrap: break-word;
          "
        >
          <div v-if="logSearchText && filteredLogContent">
            <div
              v-for="(line, index) in filteredLogContent.split('\n')"
              :key="index"
              v-html="highlightLog(line)"
            ></div>
          </div>
          <div v-else-if="logContent">
            {{ logContent }}
          </div>
          <div v-else style="color: #888; text-align: center; padding: 40px">
            {{ t('common.noData') }}
          </div>
        </div>
      </ElScrollbar>
      <div
        style="
          padding: 16px;
          border-top: 1px solid var(--el-border-color);
          display: flex;
          justify-content: space-between;
          align-items: center;
        "
      >
        <div style="color: var(--el-text-color-secondary); font-size: 12px">
          <span v-if="logSearchText">
            {{ t('common.searchResult') }}:
            {{ filteredLogContent.split('\n').filter((l) => l).length }}
            {{ t('common.lines') }}
          </span>
          <span v-else-if="logContent">
            {{ logContent.split('\n').filter((l) => l).length }} {{ t('common.lines') }}
          </span>
        </div>
        <ElSpace>
          <BaseButton @click="refreshLog" type="primary">
            {{ t('common.refresh') }}
          </BaseButton>
          <BaseButton @click="copyLog" type="info">
            {{ t('common.copy') }}
          </BaseButton>
          <BaseButton @click="cleanLog" type="danger">{{ t('common.cleanLog') }}</BaseButton>
          <BaseButton @click="closeLogDialogVisible">{{ t('common.off') }}</BaseButton>
        </ElSpace>
      </div>
    </div>
  </ElDrawer>
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
        t('common.save')
      }}</BaseButton>
    </div>
  </Dialog>
</template>

<style scoped lang="less"></style>

<style scoped>
.locale-search-row {
  align-items: center;
  gap: 12px;
}
.locale-search-row > .el-col {
  flex: 0 0 auto;
  width: auto;
  max-width: 100%;
}
.locale-search-row > .el-col:nth-child(2),
.locale-search-row > .el-col:nth-child(5) {
  flex: 0 1 260px;
  min-width: 0;
}
.locale-search-row .el-text {
  white-space: nowrap;
}
/* 搜索按钮与输入框同高，之前 height:100% 在小屏/自动高度下会塌成 16px */
.locale-search-row .el-button {
  height: 38px;
}
</style>
