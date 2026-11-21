<script setup lang="tsx">
import { useI18n } from '@/hooks/web/useI18n'
import { h, nextTick, reactive, Ref, ref, computed } from 'vue'
import { onMounted } from 'vue'
import { useTable } from '@/hooks/web/useTable'
import {
  ElCard,
  ElPagination,
  ElRow,
  ElCol,
  InputInstance,
  ElTag,
  ElInput,
  ElButton,
  ElTooltip,
  ElRadioGroup,
  ElRadioButton
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { CrudSchema, useCrudSchemas } from '@/hooks/web/useCrudSchemas'
import { addTagApi, deleteTagApi, getSubdomainApi, totalDataApi } from '@/api/asset'
import Csearch from '../search/Csearch.vue'
import TimeRangeFilter from './TimeRangeFilter.vue'
import { RowState } from '@/api/asset/types'
import { Icon } from '@/components/Icon'
import { HostValueCell } from '@/components/HostValueCell'
import { copyText } from '@/utils/clipboard'
import { resolveSubdomainHref, splitHostValues } from '@/utils/hostLink'
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
    keyword: 'ip',
    example: 'ip="192.168.2.1"',
    explain: t('searchHelp.ip')
  },
  {
    keyword: 'domain',
    example: 'domain="example.com"',
    explain: t('searchHelp.domain')
  },
  {
    keyword: 'type',
    example: 'type="CNAME"',
    explain: t('searchHelp.subdomainType')
  },
  {
    keyword: 'value',
    example: 'value="allcdn.example.com"',
    explain: t('searchHelp.subdoaminValue')
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

const maxHeight = ref(0)
const setMaxHeight = () => {
  const screenHeight = window.innerHeight || document.documentElement.clientHeight
  maxHeight.value = screenHeight * 0.7
}

// 空状态说人话：把当前搜索表达式翻译成"为什么这里是空的"
const emptyHint = computed(() => {
  const s = (searchParams.value || '').trim()
  const task = /task\s*==\s*"([^"]+)"/.exec(s)
  const domain = /domain\s*==\s*"([^"]+)"/.exec(s)
  if (task) return t('smartEmpty.task', { v: task[1] })
  if (domain) return t('smartEmpty.domain', { v: domain[1] })
  if (s) return t('smartEmpty.filtered')
  return t('common.noData')
})

// 发现时间范围：对齐 ARL-Next v1.5.1 的时间筛选（2026-11-22）
// 只做"最近 N 天"这类粗粒度，够回答「这两天新出现了什么」；精确区间留给搜索表达式
const timeRange = ref<string>('all')
const timeSort = ref<string>('desc')
const timeParams = computed(() => {
  const v = timeRange.value
  if (v === 'all') return { from: '', to: '' }
  const days = Number(v)
  if (!days) return { from: '', to: '' }
  // 「今天」= 今天 00:00（days=1 时不能减满 24 小时，否则变成昨天）
  const d = new Date(Date.now() - (days - 1) * 24 * 3600 * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  const from = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} 00:00:00`
  return { from, to: '' }
})

const SEARCH_KEY = 'asset:subdomain:search'
const searchParams = ref(sessionStorage.getItem(SEARCH_KEY) || '')
const handleSearch = (data: any) => {
  searchParams.value = data
  sessionStorage.setItem(SEARCH_KEY, data || '')
  getList()
}

// 时间筛选变化：保留当前搜索表达式，只重新取数
const onTimeFilterChange = (v?: string) => {
  // 直接用事件带过来的值，避免 v-model 尚未落定时读到旧值
  if (v) timeRange.value = v
  handleSearch(searchParams.value || '')
}
const rowStateMap = reactive<Record<string, RowState>>({})
const crudSchemas = reactive<CrudSchema[]>([
  {
    field: 'selection',
    type: 'selection',
    minWidth: 55
  },
  {
    field: 'index',
    label: t('tableDemo.index'),
    type: 'index',
    minWidth: 30
  },
  {
    field: 'host',
    label: t('subdomain.subdomainName'),
    minWidth: 200,
    formatter: (row: Recordable, __: TableColumn, host: string | string[]) => {
      // 只有 A/AAAA/CNAME 指向真实主机时才按 https 优先补链接；
      // TXT/NS/MX/PTR 与非法值一律纯文本。多值走「首值 + N」，超宽由 CSS 截断。
      const values = splitHostValues(host)
      return (
        <HostValueCell
          value={values}
          href={resolveSubdomainHref(row, values[0])}
          policy="assume-https"
          copyLabel="复制子域名"
        />
      )
    }
  },
  {
    field: 'type',
    label: t('subdomain.recordType'),
    minWidth: 200,
    columnKey: 'type',
    filters: [
      { text: 'A', value: 'A' },
      { text: 'NS', value: 'NS' },
      { text: 'CNAME', value: 'CNAME' },
      { text: 'PTR', value: 'PTR' },
      { text: 'TXT', value: 'TXT' }
    ]
  },
  {
    field: 'value',
    label: t('subdomain.recordValue'),
    minWidth: 250,
    formatter: (_: Recordable, __: TableColumn, RecordValue: string[]) => {
      if (RecordValue == null) return '-'
      const text = RecordValue.join('\n')
      return (
        <div class="flex items-center">
          <span style="white-space: pre-line">{text}</span>
          <ElTooltip content="复制记录值" placement="top">
            <Icon aria-label="复制记录值" role="button" tabindex="0"
              icon="ep:copy-document"
              size={13}
              color="var(--el-text-color-placeholder)"
              class="ml-4px cursor-pointer"
              onClick={() => copyText(text)}
            />
          </ElTooltip>
        </div>
      )
    }
  },
  {
    field: 'ip',
    label: t('asset.IP'),
    minWidth: 150,
    formatter: (_: Recordable, __: TableColumn, IPValue: string[]) => {
      if (IPValue == null) return '-'
      const text = IPValue.join('\n')
      return (
        <div class="flex items-center">
          <span style="white-space: pre-line">{text}</span>
          <ElTooltip content="复制 IP" placement="top">
            <Icon aria-label="复制 IP" role="button" tabindex="0"
              icon="ep:copy-document"
              size={13}
              color="var(--el-text-color-placeholder)"
              class="ml-4px cursor-pointer"
              onClick={() => copyText(text)}
            />
          </ElTooltip>
        </div>
      )
    }
  },
  {
    field: 'tags',
    label: t('localization.tag'),
    fit: 'true',
    showOverflowTooltip: false,
    formatter: (row: Recordable, __: TableColumn, tags: string[]) => {
      if (tags == null) {
        tags = []
      }
      // 初始化状态
      if (!rowStateMap[row.id]) {
        rowStateMap[row.id] = {
          inputVisible: false,
          inputValue: '',
          inputRef: ref(null) as Ref<InputInstance | null>
        }
      }
      const rowState = rowStateMap[row.id]
      const handleInputConfirm = async () => {
        if (rowState.inputValue) {
          tags.push(rowState.inputValue) // 将输入值添加到 tags 中
          addTagApi(row.id, index, rowState.inputValue)
        }
        rowState.inputVisible = false // 隐藏输入框
        rowState.inputValue = '' // 清空输入框的值
      }
      const deleteTag = async (row: Recordable, tag: string) => {
        if (!row.tags) {
          row.tags = []
        }
        const indexT = row.tags.indexOf(tag)
        if (indexT > -1) {
          row.tags.splice(indexT, 1) // 从数组中移除指定的元素
        }
        await deleteTagApi(row.id, index, tag)
        // 如需强制刷新表格数据，可取消下行注释
        // await getList()
      }
      const showInput = () => {
        rowState.inputVisible = true
        nextTick(() => {
          // console.log('inputRef:', rowState.inputRef)
          // if (rowState.inputRef.value?.input) {
          //   rowState.inputRef.value.input.focus()
          // }
        })
      }
      // 标签点击处理函数
      const handleTagClick = (event: MouseEvent, tag: string) => {
        if ((event.target as HTMLElement).classList.contains('el-tag__close')) {
          // 点击关闭按钮时不处理
          return
        }
        // 这里可以添加处理点击事件的逻辑
        console.log('Tag clicked:', tag)
        changeTags('tags', tag)
      }

      return h(ElRow, {}, () => [
        // 渲染标签
        ...tags.map((tag) =>
          h(ElCol, { span: 24, key: tag }, () => [
            h('div', { onClick: (event: MouseEvent) => handleTagClick(event, tag) }, [
              h(ElTag, { closable: true, onClose: () => deleteTag(row, tag) }, () => tag)
            ])
          ])
        ),

        // 输入框或按钮
        h(
          ElCol,
          { span: 24 },
          rowState.inputVisible
            ? () =>
                h(ElInput, {
                  ref: rowState.inputRef,
                  modelValue: rowState.inputValue, // 双向绑定输入框值
                  'onUpdate:modelValue': (value: string) => (rowState.inputValue = value),
                  class: 'w-20',
                  size: 'small',
                  onKeyup: (event: KeyboardEvent) => {
                    if (event.key === 'Enter') {
                      handleInputConfirm() // 只在回车键被按下时触发
                    }
                  },
                  onBlur: handleInputConfirm // 失去焦点时调用 handleInputConfirm
                })
            : () =>
                h(
                  ElButton,
                  { class: 'button-new-tag', size: 'small', onClick: () => showInput() },
                  () => t('common.newTag')
                )
        )
      ])
    },
    minWidth: 130
  },
  {
    field: 'time',
    label: t('asset.time'),
    minWidth: 200
  }
])
let index = 'subdomain'
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

const lastSearchParams = ref('')
let lastFilter = reactive<{ [key: string]: any }>({})

const { allSchemas } = useCrudSchemas(crudSchemas)
const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const searchParamsChanged = searchParams.value !== lastSearchParams.value
    const filterChanged = JSON.stringify(filter) !== JSON.stringify(lastFilter)

    const { currentPage, pageSize } = tableState

    if (searchParamsChanged || filterChanged) {
      currentPage.value = 1
      getTotal(searchParams.value, currentPage.value, pageSize.value, filter)
      lastSearchParams.value = searchParams.value
      lastFilter = { ...filter }
    }
    const res = await getSubdomainApi(
      searchParams.value,
      currentPage.value,
      pageSize.value,
      filter,
      timeParams.value.from,
      timeParams.value.to,
      timeSort.value
    )
    return {
      list: res.data.list,
      flag: true
    }
  },
  immediate: false
})
const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList, getElTableExpose } = tableMethods

const getTotal = async (
  search: string,
  pageIndex: number,
  pageSize: number,
  filter: Record<string, any>
) => {
  // 带时间范围：否则「共 N 条」永远是全量，和筛选后的列表对不上
  let res = await totalDataApi(search, pageIndex, pageSize, filter, index, timeFrom.value)
  total.value = res.data.total
}

function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}
// 筛选条件记忆：切页返回后下拉框选择不丢
const FILTER_KEY = 'asset:subdomain:filter'
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
const changeTags = (type, value) => {
  const key = `${type}=${value}`
  console.log(key)
  dynamicTags.value = [...dynamicTags.value, key]
}
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
</script>

<template>
  <Csearch
    :getList="getList"
    :handleSearch="handleSearch"
    :initialSearch="searchParams"
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
          v-model:pageSize="pageSize"
          v-model:currentPage="currentPage"
          :columns="allSchemas.tableColumns"
          :data="dataList"
          :empty-text="emptyHint"
          stripe
          :border="true"
          :loading="loading"
          :resizable="true"
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
        <ElPagination
          v-model:pageSize="pageSize"
          v-model:currentPage="currentPage"
          :page-sizes="[10, 20, 50, 100, 200, 500, 1000]"
          layout="total, sizes, prev, pager, next, jumper"
          :total="total"
        />
      </ElCard>
    </ElCol>
  </ElRow>
</template>

<style lang="less" scoped>
.time-range-row {
  margin-bottom: 8px;
  align-items: center;
}
.time-range-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-right: 10px;
}

.el-button {
  margin-top: 10px;
}
:deep(.el-table .cell.el-tooltip) {
  white-space: pre-line;
}
</style>
