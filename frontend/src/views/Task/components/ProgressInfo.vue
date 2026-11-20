<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useI18n } from '@/hooks/web/useI18n'
import { reactive, h, ref, computed, onMounted, onUnmounted } from 'vue'
import { Table, TableColumn } from '@/components/Table'
import { getTaskProgressApi, pauseTaskTargetApi, resumeTaskTargetApi } from '@/api/task'
import { useTable } from '@/hooks/web/useTable'
import {
  ElButton,
  ElCard,
  ElMessage,
  ElMessageBox,
  ElPagination,
  ElTag,
  ElTooltip,
  ElAlert,
  ElCheckbox
} from 'element-plus'
import { Icon } from '@/components/Icon'
const { t } = useI18n()

const props = defineProps<{
  closeDialog: () => void
  getProgressInfoID: string
  getProgressInfotype: string
  getProgressInforunnerid: string
  // 该任务期间外部工具失败次数（0 表示无异常产出）
  cmdFail?: number
  // 当前任务名：产出一栏点击时带过去筛选
  taskName?: string
}>()
// 模块列：直接显示耗时，而不是"完成/运行中"标签 + 悬停才看得到的时间戳。
// 用时：<60s → 「12s」；>=60s → 「1m23s」；>=1h → 「1h05m」。
const fmtDuration = (start: string, end: string): string => {
  if (!start) return ''
  const s = new Date(start.replace(/-/g, '/')).getTime()
  const e = end ? new Date(end.replace(/-/g, '/')).getTime() : Date.now()
  if (!isFinite(s) || !isFinite(e) || e < s) return ''
  const sec = Math.round((e - s) / 1000)
  if (sec < 60) return sec + 's'
  const m = Math.floor(sec / 60)
  if (m < 60) return m + 'm' + String(sec % 60).padStart(2, '0') + 's'
  const hh = Math.floor(m / 60)
  return hh + 'h' + String(m % 60).padStart(2, '0') + 'm'
}

const moduleCell = (_: Recordable, __: TableColumn, cellValue: string[]) => {
  if (!Array.isArray(cellValue)) return '-'
  if (cellValue.length == 3) {
    return h(Icon, { icon: 'ph:prohibit' })
  }
  if (!cellValue[0]) return '-'
  const dur = fmtDuration(cellValue[0], cellValue[1])
  const done = !!cellValue[1]
  const tip = h('div', {}, [h('div', {}, ['Start: ' + cellValue[0]]), h('div', {}, ['End: ' + (cellValue[1] || '-')])])
  return h(ElTooltip, { placement: 'top' }, {
    content: () => tip,
    default: () =>
      h(ElTag, { type: done ? 'success' : 'primary', size: 'small' }, () =>
        (done ? dur : t('task.running') + ' ' + dur)
      )
  })
}

const progressColums = reactive<TableColumn[]>([
  {
    field: 'target',
    label: t('task.taskTarget'),
    minWidth: 40,
    formatter: (row: Recordable, __: TableColumn, cellValue: string) => {
      const tooltipContent = row.node && row.node !== '' ? row.node : null
      const base = tooltipContent
        ? h(
            ElTooltip,
            { content: tooltipContent, placement: 'top', rawContent: true },
            {
              default: () => cellValue // 用 cellValue 作为原始数据显示
            }
          )
        : cellValue // 如果 row.node 是空字符串，则直接显示 cellValue
      if (row.paused === true) {
        return h('span', { class: 'paused-target' }, [
          base,
          h(ElTag, { type: 'info', size: 'small', class: 'ml-2' }, () => t('task.targetPaused'))
        ])
      }
      return base
    }
  },
  {
    field: 'TargetHandler',
    label: t('scanTemplate.TargetHandler'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'SubdomainScan',
    label: t('scanTemplate.SubdomainScan'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'SubdomainSecurity',
    label: t('scanTemplate.SubdomainSecurity'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'PortScanPreparation',
    label: t('scanTemplate.PortScanPreparation'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'PortScan',
    label: t('scanTemplate.PortScan'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'PortFingerprint',
    label: t('scanTemplate.PortFingerprint'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'AssetMapping',
    label: t('scanTemplate.AssetMapping'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'AssetHandle',
    label: t('scanTemplate.AssetHandle'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'URLScan',
    label: t('scanTemplate.URLScan'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'WebCrawler',
    label: t('scanTemplate.WebCrawler'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'URLSecurity',
    label: t('scanTemplate.URLSecurity'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'DirScan',
    label: t('scanTemplate.DirScan'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'VulnerabilityScan',
    label: t('scanTemplate.VulnerabilityScan'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'All',
    label: t('common.all'),
    minWidth: 30,
    formatter: moduleCell
  },
  {
    field: 'pausedAction',
    label: t('common.operation'),
    width: 100,
    formatter: (row: Recordable) => {
      const paused = row.paused === true
      return h(
        ElButton,
        {
          link: true,
          type: paused ? 'primary' : 'warning',
          onClick: () => toggleTarget(row)
        },
        () => (paused ? t('task.resumeTarget') : t('task.pauseTarget'))
      )
    }
  }
])
const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const res = await getTaskProgressApi(props.getProgressInfoID, currentPage.value, pageSize.value)
    // 顺带取任务级工具失败数（接口已返回），不再依赖父组件传参
    cmdFail.value = (res.data as any).cmdFail || 0
    outcome.value = (res.data as any).outcome || {}
    return {
      total: res.data.total,
      list: res.data.list
    }
  },
  immediate: false
})
const { loading, dataList, total, currentPage, pageSize } = tableState

// 模块列默认只显示「真的有数据」的那些：任务没跑到的模块留空列只会拉长横向滚动。
// 想看全部就打开「显示全部模块」。
const showAllModules = ref(false)
const visibleColumns = computed(() => {
  // 还没拿到数据时不能折叠：否则列数组变空，表格渲染不出来（实测首屏空白）
  if (showAllModules.value || !(dataList.value || []).length) return progressColums
  return progressColums.filter((c: any) => {
    const f = c.field
    if (!f || f === 'target' || f === 'node' || f === 'action') return true
    return (dataList.value || []).some((row: any) => {
      const v = row[f]
      if (Array.isArray(v)) return v.some((x: string) => x !== '' && x !== undefined && x !== null)
      return !!v
    })
  })
})
const { getList } = tableMethods

// 暂停/恢复单个目标（issue #277）：暂停前确认一次，恢复直接执行
const toggleTarget = async (row: Recordable) => {
  const paused = row.paused === true
  try {
    if (paused) {
      await resumeTaskTargetApi(props.getProgressInfoID, row.target)
    } else {
      await ElMessageBox.confirm(
        t('task.pauseTargetConfirm', { target: row.target }),
        t('common.delWarning'),
        { confirmButtonText: 'OK', cancelButtonText: t('common.cancel'), type: 'warning' }
      )
      await pauseTaskTargetApi(props.getProgressInfoID, row.target)
    }
    ElMessage.success(t('common.success'))
    getList()
  } catch {
    // 取消确认或请求失败：失败提示由 axios 拦截器统一弹出
  }
}

// 自动刷新：任务进度是动态的，弹窗打开期间每 5 秒静默刷新一次，
// 用户不用手动点、也看不到整表闪白（Table 的 loading 只在新查询时置位）。
const outcomeLabels: Record<string, string> = { subdomain: t("task.outSubdomain"), url: t("task.outUrl"), asset: t("task.outAsset"), vulnerability: t("task.outVuln") }
// 产出标签跳转：漏洞去结果页，子域/资产去资产页对应 Tab（带 task=="名字" 过滤）
const router = useRouter()
const clickableTabs: Record<string, string> = { vulnerability: '', subdomain: 'subdomain', asset: 'asset' }
const onOutcomeClick = (key: string) => {
  const tab = clickableTabs[key]
  if (tab === undefined || !props.taskName) return
  if (key === 'vulnerability') {
    router.push({ path: '/asset-information/nuclei-result', query: { taskName: props.taskName } })
  } else {
    router.push({
      path: '/asset-information',
      query: { tab, search: 'task=="' + props.taskName + '"' }
    })
  }
  props.closeDialog && props.closeDialog()
}
const cmdFail = ref(0)
// 任务产出统计（子域/URL/资产/漏洞）
const outcome = ref<Record<string, number>>({})
const lastRefresh = ref('')
const autoRefresh = ref(true)
let timer: ReturnType<typeof setInterval> | null = null

const refreshOnce = async () => {
  try {
    await getList()
    const d = new Date()
    const pad = (n: number) => String(n).padStart(2, '0')
    lastRefresh.value = `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  } catch {
    // 静默失败：不打扰用户，下一轮继续
  }
}

const startAuto = () => {
  stopAuto()
  if (!autoRefresh.value) return
  timer = setInterval(refreshOnce, 5000)
}
const stopAuto = () => {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}
const toggleAuto = () => {
  if (autoRefresh.value) {
    startAuto()
    refreshOnce()
  } else {
    stopAuto()
  }
}

onMounted(async () => {
  await refreshOnce()
  startAuto()
})
onUnmounted(stopAuto)
</script>
<template>
  <ElAlert
    v-if="cmdFail > 0"
    type="error"
    :closable="false"
    show-icon
    style="margin-bottom: 8px"
    :title="t('task.cmdFailBanner', { n: cmdFail })"
  />
  <div class="module-toggle">
    <ElCheckbox v-model="showAllModules" size="small">{{ t('task.showAllModules') }}</ElCheckbox>
    <span class="module-toggle__hint">{{ t('task.moduleColumnsHint', { n: visibleColumns.length - 2 }) }}</span>
  </div>

  <div v-if="Object.values(outcome).some(v => v > 0)" class="outcome-bar">
    <span class="outcome-title">{{ t("task.outcome") }}</span>
    <ElTag
      v-for="(label, key) in outcomeLabels"
      :key="key"
      v-show="(outcome as any)[key]"
      size="small"
      type="info"
      effect="plain"
      :class="{ 'outcome-clickable': clickableTabs[key] !== undefined }"
      @click="onOutcomeClick(key)"
    >
      {{ label }} {{ (outcome as any)[key] }}
    </ElTag>
  </div>
  <div class="progress-hint">
    <span class="hint-text">
      {{ t('task.autoRefreshHint') }}
      <span v-if="lastRefresh">· {{ t('task.lastRefresh') }} {{ lastRefresh }}</span>
    </span>
    <ElButton size="small" :type="autoRefresh ? 'primary' : 'default'" @click="toggleAuto">
      {{ autoRefresh ? t('task.autoRefreshOn') : t('task.autoRefreshOff') }}
    </ElButton>
  </div>
  <Table
    v-model:pageSize="pageSize"
    v-model:currentPage="currentPage"
    @register="tableRegister"
    :columns="visibleColumns"
    :data="dataList"
    rowKey="_id"
    :loading="loading"
    :resizable="true"
    max-height="600"
    :tooltip-options="{
      offset: 1,
      showArrow: false,
      effect: 'dark',
      enterable: true,
      showAfter: 0,
      popperOptions: {},
      popperClass: 'test',
      placement: 'top',
      hideAfter: 0,
      disabled: true
    }"
    :style="{
      fontFamily:
        '-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica Neue,Arial,Noto Sans,sans-serif,Apple Color Emoji,Segoe UI Emoji,Segoe UI Symbol,Noto Color Emoji'
    }"
  />
  <ElCard>
    <ElPagination
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      :page-sizes="[10, 20, 50, 100, 200, 500, 1000]"
      layout="total, sizes, prev, pager, next, jumper"
      :total="total"
    />
  </ElCard>
</template>

<style scoped>
.module-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.module-toggle__hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.outcome-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 13px;
}
.outcome-title {
  color: var(--el-text-color-secondary);
}
.outcome-clickable {
  cursor: pointer;
}
.outcome-clickable:hover {
  border-color: var(--el-color-primary);
}
</style>
