<script setup lang="tsx">
import { useI18n } from '@/hooks/web/useI18n'
import { reactive, ref } from 'vue'
import { onMounted } from 'vue'
import { useTable } from '@/hooks/web/useTable'
import {
  ElCard,
  ElRow,
  ElCol,
  ElInput,
  ElMessage,
  ElButton,
  ElDatePicker,
  ElAlert,
  ElText
} from 'element-plus'
import { Table } from '@/components/Table'
import { CrudSchema, useCrudSchemas } from '@/hooks/web/useCrudSchemas'
import { useRoute } from 'vue-router'
import { getProjectServiceDataApi } from '@/api/ProjectAggregation'
const { t } = useI18n()
const { query } = useRoute()
onMounted(() => {
  setMaxHeight()
  window.addEventListener('resize', setMaxHeight)
})

const maxHeight = ref(0)

const setMaxHeight = () => {
  const screenHeight = window.innerHeight || document.documentElement.clientHeight
  maxHeight.value = screenHeight * 0.8
}

// 时间范围（可选）：后端按 asset.time 字符串边界过滤
const timeRange = ref<[string, string] | null>(null)
const loadError = ref(false)

const filter = reactive<{ [key: string]: any }>({})
filter.project = [query.id as string]
const filterChange = async (newFilters: any) => {
  Object.assign(filter, newFilters)
  getList()
}
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
    field: 'service',
    label: t('asset.service'),
    minWidth: 100,
    // 服务是聚合行：count = 该服务下资产数
    formatter: (row, __: any, domainValue: string) => {
      if (!row.count) {
        return <ElText>{domainValue}</ElText>
      }
      return (
        <>
          <ElText>{domainValue}</ElText>
          <ElText type="info">({row.count})</ElText>
        </>
      )
    },
    slots: {
      header: () => {
        return (
          <div>
            <span>{t('asset.service')}</span>
            <ElInput
              v-model={serviceValue.value}
              placeholder={t('common.search')}
              style="width: 80px; margin-left: 10px;"
              size="small"
              onChange={() => filterSearchChange('service_service')}
            />
          </div>
        )
      }
    }
  },
  {
    field: 'host',
    label: t('asset.domain'),
    minWidth: 200,
    slots: {
      header: () => {
        return (
          <div>
            <span>{t('asset.domain')}</span>
            <ElInput
              v-model={domainValue.value}
              placeholder={t('common.search')}
              style="width: 80px; margin-left: 10px;"
              size="small"
              onChange={() => filterSearchChange('service_domain')}
            />
          </div>
        )
      }
    }
  },
  {
    field: 'ip',
    label: t('asset.IP'),
    minWidth: 250,
    slots: {
      header: () => {
        return (
          <div>
            <span>IP</span>
            <ElInput
              v-model={ipValue.value}
              placeholder={t('common.search')}
              style="width: 200px; margin-left: 10px;"
              size="small"
              onChange={() => filterSearchChange('service_ip')}
            />
          </div>
        )
      }
    }
  },
  {
    field: 'port',
    label: t('asset.port'),
    minWidth: 250,
    slots: {
      header: () => {
        return (
          <div>
            <span>{t('asset.port')}</span>
            <ElInput
              v-model={portValue.value}
              placeholder={t('common.search')}
              style="width: 200px; margin-left: 10px;"
              size="small"
              onChange={() => filterSearchChange('service_port')}
            />
          </div>
        )
      }
    }
  },
  {
    field: 'time',
    label: t('asset.time'),
    minWidth: 200
  }
])

const { allSchemas } = useCrudSchemas(crudSchemas)
const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    try {
      const res = await getProjectServiceDataApi('', filter, fq, {
        pageIndex: tableState.currentPage.value,
        pageSize: tableState.pageSize.value,
        startTime: timeRange.value?.[0],
        endTime: timeRange.value?.[1]
      })
      loadError.value = false
      return {
        list: res.data?.list || [],
        total: res.data?.total || 0
      }
    } catch (error) {
      loadError.value = true
      ElMessage.error(t('localization.requestFailed'))
      return { list: [], total: 0 }
    }
  },
  immediate: true
})
const { loading, dataList, currentPage, pageSize, total } = tableState
const { getList } = tableMethods
function tableHeaderColor() {
  return { background: 'var(--app-surface-2)' }
}

const portValue = ref('')
const domainValue = ref('')
const ipValue = ref('')
const serviceValue = ref('')
const fq = reactive<{ [key: string]: any }>({})
const filterSearchChange = async (type: string) => {
  let value = ''
  if (type == 'service_port') {
    value = portValue.value
  }
  if (type == 'service_domain') {
    value = domainValue.value
  }
  if (type == 'service_ip') {
    value = ipValue.value
  }
  if (type == 'service_service') {
    value = serviceValue.value
  }
  fq[type] = value
  getList()
}
const search = () => {
  currentPage.value = 1
  getList()
}
</script>

<template>
  <ElRow>
    <ElCol>
      <ElCard style="height: min-content">
        <div class="aggregation-toolbar">
          <ElDatePicker
            v-model="timeRange"
            type="datetimerange"
            value-format="YYYY-MM-DD HH:mm:ss"
            :start-placeholder="t('export.timeRangeStart')"
            :end-placeholder="t('export.timeRangeEnd')"
            clearable
            @change="search"
          />
          <ElButton size="small" @click="search">{{ t('common.refresh') }}</ElButton>
        </div>
        <ElAlert
          v-if="loadError"
          class="mb-10px"
          type="error"
          :title="t('localization.requestFailed')"
          :closable="false"
          show-icon
        />
        <Table
          v-model:currentPage="currentPage"
          v-model:pageSize="pageSize"
          :columns="allSchemas.tableColumns"
          :data="dataList"
          :max-height="maxHeight"
          :border="true"
          :loading="loading"
          :empty-text="t('common.noData')"
          :pagination="{ total, pageSizes: [20, 50, 100] }"
          rowKey="id"
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
  </ElRow>
</template>

<style lang="less" scoped>
.el-button {
  margin-top: 10px;
}
.aggregation-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.aggregation-toolbar .el-button {
  margin-top: 0;
}
:deep(.el-table .cell.el-tooltip) {
  white-space: pre-line;
}
</style>
