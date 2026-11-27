<script setup lang="tsx">
import { useI18n } from '@/hooks/web/useI18n'
import { useEmptyHint } from '@/hooks/web/useEmptyHint'
import { computed, reactive, ref } from 'vue'
import { onMounted, onUnmounted } from 'vue'
import { useTable } from '@/hooks/web/useTable'
import TimeRangeFilter from './TimeRangeFilter.vue'
import { ElCard, ElPagination, ElRow, ElCol, ElTag, ElTooltip } from 'element-plus'
import { Icon } from '@/components/Icon'
import { copyText } from '@/utils/clipboard'
import { Table, TableColumn } from '@/components/Table'
import { CrudSchema, useCrudSchemas } from '@/hooks/web/useCrudSchemas'
import { getIPAssetApi } from '@/api/asset'
import Csearch from '../search/Csearch.vue'
const { t } = useI18n()
interface Project {
  value: string
  label: string
  children?: Project[]
}
const props = defineProps<{
  projectList: Project[]
  taskList: { id: string; name: string }[]
}>()
const searchKeywordsData = [
  {
    keyword: 'domain',
    example: 'domain="baidu.com"',
    explain: t('searchHelp.name')
  },
  {
    keyword: 'ip',
    example: 'ip="192.168.2.1"',
    explain: t('searchHelp.ip')
  },
  {
    keyword: 'port',
    example: 'port="3306"',
    explain: t('searchHelp.port')
  },
  {
    keyword: 'app',
    example: 'app="Nginx"',
    explain: t('searchHelp.app')
  },
  {
    keyword: 'service',
    example: 'service="ssh"',
    explain: t('searchHelp.protocol')
  },
  {
    keyword: 'webServer',
    example: 'webServer="Flask"',
    explain: 'Search Web Server'
  },
  {
    keyword: 'project',
    example: 'project="Hackerone"',
    explain: t('searchHelp.project')
  }
]
onMounted(() => {
  setMaxHeight()
  window.addEventListener('resize', setMaxHeight)
})

onUnmounted(() => {
  window.removeEventListener('resize', setMaxHeight)
})

const maxHeight = ref(0)
const setMaxHeight = () => {
  const screenHeight = window.innerHeight || document.documentElement.clientHeight
  maxHeight.value = screenHeight * 0.7
}

const SEARCH_KEY = 'asset:ip:search'
const searchParams = ref(sessionStorage.getItem(SEARCH_KEY) || '')
const emptyHint = useEmptyHint(searchParams)
const handleSearch = (data: any) => {
  searchParams.value = data
  sessionStorage.setItem(SEARCH_KEY, data || '')
  // 搜索等于换结果集：不回第 1 页的话，停在第 N 页搜一个小结果会看到空表（2026-11-27）
  if (currentPage.value !== 1) {
    currentPage.value = 1
    return
  }
  getList()
}
const crudSchemas = reactive<CrudSchema[]>([
  {
    field: 'selection',
    type: 'selection',
    minWidth: 35
  },
  {
    field: 'ip',
    label: t('asset.IP'),
    minWidth: 140,
    formatter: (_: Recordable, __: TableColumn, ip: string) => (
      <div class="flex items-center">
        <span>{ip}</span>
        <ElTooltip content="复制 IP" placement="top">
          <Icon aria-label="复制 IP" role="button" tabindex="0"
            icon="ep:copy-document"
            size={13}
            color="var(--el-text-color-placeholder)"
            class="ml-4px cursor-pointer"
            onClick={() => copyText(ip)}
          />
        </ElTooltip>
      </div>
    )
  },
  {
    field: 'port',
    label: t('asset.port'),
    minWidth: 100
  },
  {
    field: 'domain',
    label: t('asset.domain'),
    minWidth: 100
  },
  {
    field: 'service',
    label: t('asset.service'),
    minWidth: 100
  },
  {
    field: 'products',
    label: t('asset.products'),
    minWidth: 180,
    formatter: (_: Recordable, __: TableColumn, ProductsValue: string[] | null) => {
      if (!ProductsValue || ProductsValue.length === 0) return
      if (ProductsValue.length != 0) {
        return (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
            {ProductsValue.map((product) => (
              <div key={product} style={{ cursor: 'pointer' }}>
                <ElTag type={'success'}>{product}</ElTag>
              </div>
            ))}
          </div>
        )
      }
    }
  },
  {
    field: 'time',
    label: t('asset.time'),
    minWidth: 180
  }
])
let index = 'IPAsset'
crudSchemas.forEach((schema) => {
  schema.hidden = schema.hidden ?? false // 如果没有 hidden 属性，添加并设置为 false
})
let statisticsHidden = ref(false)
// 从localStorage读取配置并更新列的显示状态
const loadColumnConfig = () => {
  const savedConfig = JSON.parse(localStorage.getItem(`columnConfig_${index}`) || '{}')
  console.log(savedConfig)
  crudSchemas.forEach((col) => {
    if (savedConfig[col.field] !== undefined && col.field != 'select') {
      col.hidden = savedConfig[col.field] // 恢复列的显示状态
    }
  })
  statisticsHidden.value = savedConfig['statisticsHidden']
}
// 保存配置到localStorage
const saveColumnConfig = () => {
  const config = crudSchemas.reduce((acc, col) => {
    acc[col.field] = col.hidden // 保存每列的显示状态
    return acc
  }, {})
  config['statisticsHidden'] = statisticsHidden.value
  localStorage.setItem(`columnConfig_${index}`, JSON.stringify(config)) // 按index保存配置
}

// 处理列显示状态变化
const handleColumnVisibilityChange = ({ field, hidden }) => {
  console.log(field, hidden)
  const columnIndex = crudSchemas.findIndex((col) => col.field === field)
  if (columnIndex !== -1) {
    // 使用对象的展开运算符来创建一个新的对象，并更新隐藏属性
    crudSchemas[columnIndex].hidden = hidden
  }
  saveColumnConfig()
}
loadColumnConfig()
const { allSchemas } = useCrudSchemas(crudSchemas)
const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getIPAssetApi(
      searchParams.value,
      currentPage.value,
      pageSize.value,
      filter,
      timeFrom.value,
      timeSort.value
    )
    return {
      list: res.data.list,
      total: res.data.total
    }
  },
  immediate: false
})

// 发现时间筛选（对齐 ARL-Next v1.5.1）
const timeRange = ref<string>('all')
const timeSort = ref<string>('desc')
const timeFrom = computed(() => {
  if (timeRange.value === 'all') return ''
  const n = Number(timeRange.value)
  if (!n) return ''
  const d = new Date(Date.now() - (n - 1) * 24 * 3600 * 1000) // 「今天」= 今天 00:00，不是昨天
  const pad = (x: number) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} 00:00:00`
})

// 时间筛选变化：保留当前搜索表达式，只重新取数（handleSearch 需要参数，直接绑会清空搜索）
const onTimeFilterChange = (v?: string) => {
  // 直接用事件带过来的值，避免 v-model 尚未落定时读到旧值
  if (v) timeRange.value = v
  handleSearch(searchParams.value || '')
  // 「共 N 条」由独立的 getTotal 维护，时间变化时也要刷新（否则总数停在旧值）
  if (typeof getTotal === 'function') {
    getTotal(searchParams.value || '', currentPage.value, pageSize.value, filter)
  }
}

const { loading, dataList, total, currentPage, pageSize } = tableState
// 单独设置 IP 组件的 pageSize 默认值
pageSize.value = 10 // 可以根据需要修改为你想要的默认值
const { getList, getElTableExpose } = tableMethods
function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}
// 筛选条件记忆：切页返回后下拉框选择不丢
const FILTER_KEY = 'asset:ip:filter'
const savedFilter = (() => {
  try {
    return JSON.parse(sessionStorage.getItem(FILTER_KEY) || '{}')
  } catch {
    return {}
  }
})()
const filter = reactive<{ [key: string]: any }>(savedFilter)
const persistFilter = () => {
  try {
    sessionStorage.setItem(FILTER_KEY, JSON.stringify({ ...filter }))
  } catch {
    // 忽略
  }
}
const filterChange = async (newFilters: any) => {
  Object.assign(filter, newFilters)
  persistFilter()
  getList()
}
const handleFilterSearch = (data: any, newFilters: any) => {
  Object.assign(filter, newFilters)
  persistFilter()
  searchParams.value = data
  sessionStorage.setItem(SEARCH_KEY, data || '')
  getList()
}
const dynamicTags = ref<string[]>([])

const handleClose = (tag: string) => {
  if (dynamicTags.value) {
    const [key, value] = tag.split('=')
    if (key in filter && Array.isArray(filter[key])) {
      filter[key] = filter[key].filter((item: string) => item !== value)
      if (filter[key].length === 0) {
        delete filter[key]
      }
    }
    dynamicTags.value = dynamicTags.value.filter((item) => item !== tag)
  }
}
const getFilter = () => {
  return filter
}
const spanMethod = ({ row, column, rowIndex, columnIndex }) => {
  // columnIndex:
  // 0 = selection (复选框列)
  // 1 = IP
  // 2 = Port

  // 📌 Selection 列合并（与 IP 列同步）
  if (columnIndex === 0) {
    if (row.ipRowSpan > 0) {
      return [row.ipRowSpan, 1] // 显示并合并
    } else {
      return [0, 0] // 隐藏单元格
    }
  }

  // 📌 IP 合并
  if (columnIndex === 1) {
    if (row.ipRowSpan > 0) {
      return [row.ipRowSpan, 1] // 显示并合并
    } else {
      return [0, 0] // 隐藏单元格
    }
  }

  // 📌 Port 合并
  if (columnIndex === 2) {
    if (row.portRowSpan > 0) {
      return [row.portRowSpan, 1]
    } else {
      return [0, 0]
    }
  }

  // 📌 默认正常显示
  return [1, 1]
}
</script>

<template>
  <Csearch
    :timeFrom="timeFrom"
    :initialSearch="searchParams"
    :getList="getList"
    :handleSearch="handleSearch"
    :searchKeywordsData="searchKeywordsData"
    :index="index"
    :getElTableExpose="getElTableExpose"
    :projectList="$props.projectList"
    :taskList="$props.taskList"
    :handleFilterSearch="handleFilterSearch"
    :crudSchemas="crudSchemas"
    :dynamicTags="dynamicTags"
    :handleClose="handleClose"
    @update-column-visibility="handleColumnVisibilityChange"
    :searchResultCount="total"
    :getFilter="getFilter"
  />
  <TimeRangeFilter v-model="timeRange" v-model:sort="timeSort" @change="onTimeFilterChange" />
  <ElRow>
    <ElCol>
      <ElCard style="height: min-content">
        <Table
          :empty-text="emptyHint"
          v-model:pageSize="pageSize"
          v-model:currentPage="currentPage"
          :columns="allSchemas.tableColumns"
          :data="dataList"
          rowKey="datakey"
          stripe
          :border="true"
          :loading="loading"
          :span-method="spanMethod"
          :resizable="true"
          :max-height="maxHeight"
          @register="tableRegister"
          @filter-change="filterChange"
          :headerCellStyle="tableHeaderColor"
          :style="{
            fontFamily:
              '-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica Neue,Arial,Noto Sans,sans-serif,Apple Color Emoji,Segoe UI Emoji,Segoe UI Symbol,Noto Color Emoji'
          }"
        />
      </ElCard>
    </ElCol>
    <ElCol ::span="24">
      <ElCard>
        <!-- 这一页分页数的是「IP」，不是表格行：一个 IP 的多端口/多服务会合并成多行，
             所以「每页 3 个 IP」可能显示 50 行。不写清楚会被当成翻页坏了（2026-11-28）。 -->
        <div class="ip-page-bar">
          <span class="ip-page-hint">
            按 IP 分页 · 共 {{ total }} 个 IP（端口/服务按 IP 合并展示，行数会多于每页 IP 数）
          </span>
          <ElPagination
            v-model:pageSize="pageSize"
            v-model:currentPage="currentPage"
            :page-sizes="[10, 20, 50, 100, 200, 500, 1000]"
            layout="sizes, prev, pager, next, jumper"
            :total="total"
            class="ip-pagination"
          />
        </div>
      </ElCard>
    </ElCol>
  </ElRow>
</template>

<style lang="less" scoped>
.el-button {
  margin-top: 10px;
}
:deep(.el-table .cell.el-tooltip) {
  white-space: pre-line;
}
</style>

<style lang="less" scoped>
.ip-page-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.ip-page-hint {
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
</style>

