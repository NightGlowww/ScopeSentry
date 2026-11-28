<script setup lang="tsx">
import { computed, reactive, ref } from 'vue'
import {
  ElAlert,
  ElButton,
  ElCol,
  ElInput,
  ElLink,
  ElMessage,
  ElMessageBox,
  ElRow,
  ElTag
} from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { Table, TableColumn } from '@/components/Table'
import { useTable } from '@/hooks/web/useTable'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { deleteFileLeakApi, getFileLeakListApi } from '@/api/fileleak'
import type { FileLeakRow } from '@/api/fileleak/types'
import { clampPageSize, humanBytes, normalizeKeyword, statusCodeParam } from '@/utils/assetList'
import { httpStatusTagType, safeHttpUrl } from '@/utils/httpStatus'
import { timeText } from '@/utils/timeText'

// 文件泄露（只读 + 批量删除）：一行是扫描端「文件泄露检测」命中的一条记录。
// 数据是节点侧模块落库的，所以空态必须说清楚「节点跑完对应模块才会出现」，
// 否则用户会以为是列表坏了。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const keyword = ref('')
// 输入框只收数字：后端按精确响应码过滤，分档（4xx/5xx）只在单元格着色时用。
// 不裁的话用户输入 "4xx" 会原样发给后端并拿到空列表。
const statusCodeInput = ref('')
const statusCode = computed({
  get: () => statusCodeInput.value,
  set: (value: string) => {
    statusCodeInput.value = String(value ?? '')
      .replace(/\D/g, '')
      .slice(0, 3)
  }
})

// formatter 里的 JSX 不带 SFC 的 scoped 属性，所以等宽样式走行内，不依赖 <style scoped>
const monoStyle = {
  fontFamily: 'var(--el-font-family-mono, monospace)'
}

const columns = reactive<TableColumn[]>([
  {
    field: 'selection',
    type: 'selection',
    width: 50
  },
  {
    field: 'url',
    label: t('fileLeak.url'),
    minWidth: 320,
    formatter: (row: FileLeakRow) => {
      // 历史数据可能没有 scheme（只存了 host:port/path），
      // 只有明确的 http(s) 才交给 <a>，否则浏览器会把它拼成当前站点的相对路径。
      const href = safeHttpUrl(row.url)
      if (!href) return <span style={monoStyle}>{row.url || '-'}</span>
      return (
        <ElLink href={href} target="_blank" type="primary">
          {href}
        </ElLink>
      )
    }
  },
  {
    field: 'site',
    label: t('fileLeak.site'),
    minWidth: 180,
    formatter: (row: FileLeakRow) => row.site || '-'
  },
  {
    field: 'title',
    label: t('fileLeak.title'),
    minWidth: 220,
    formatter: (row: FileLeakRow) => row.title || '-'
  },
  {
    field: 'statusCode',
    label: t('fileLeak.statusCode'),
    width: 110,
    // 复用 httpStatus 的分档着色：2xx success / 3xx info / 4xx warning / 5xx danger；
    // 0 表示没拿到响应（超时/连接失败），显示成 "-" 而不是标一个绿色的 0。
    formatter: (row: FileLeakRow) =>
      row.statusCode ? (
        <ElTag type={httpStatusTagType(row.statusCode)}>{row.statusCode}</ElTag>
      ) : (
        '-'
      )
  },
  {
    field: 'contentLength',
    label: t('fileLeak.contentLength'),
    width: 130,
    formatter: (row: FileLeakRow) => humanBytes(row.contentLength)
  },
  {
    field: 'insertTime',
    label: t('fileLeak.insertTime'),
    width: 180,
    formatter: (row: FileLeakRow) => timeText(row.insertTime)
  },
  {
    field: 'updateTime',
    label: t('fileLeak.updateTime'),
    width: 180,
    formatter: (row: FileLeakRow) => timeText(row.updateTime)
  }
])

const { tableRegister, tableState, tableMethods } = useTable({
  fetchDataApi: async () => {
    const { currentPage, pageSize } = tableState
    const res = await getFileLeakListApi({
      keyword: normalizeKeyword(keyword.value),
      statusCode: statusCodeParam(statusCodeInput.value),
      pageIndex: currentPage.value,
      pageSize: clampPageSize(pageSize.value)
    })
    return { list: res?.data?.list ?? [], total: res?.data?.total ?? 0 }
  }
})

const { loading, dataList, total, currentPage, pageSize } = tableState
const { getList, getElTableExpose } = tableMethods

const handleSearch = () => {
  // currentPage 变化时 useTable 会自己拉一次，只有已经在第一页时才手动拉，避免重复请求
  if (currentPage.value !== 1) {
    currentPage.value = 1
    return
  }
  getList()
}

const handleReset = () => {
  keyword.value = ''
  statusCodeInput.value = ''
  handleSearch()
}

const selectedRows = ref<FileLeakRow[]>([])
const selectedIds = computed(() => selectedRows.value.map((row) => row.id))
const deleting = ref(false)

const handleSelectionChange = (rows: FileLeakRow[]) => {
  selectedRows.value = rows ?? []
}

// 删除后清空勾选：表格数据换了但勾选状态还在的话，下一次删除会带上已经删掉的 id
const clearSelection = async () => {
  const elTableExpose = await getElTableExpose()
  elTableExpose?.clearSelection?.()
  selectedRows.value = []
}

const handleDelete = async () => {
  const ids = selectedIds.value
  if (!ids.length) {
    ElMessage.warning(t('fileLeak.selectFirst'))
    return
  }
  // 二次确认：删除是不可恢复的操作，确认框里带上条数，避免误删整页
  try {
    await ElMessageBox.confirm(
      t('fileLeak.deleteConfirm', { count: ids.length }),
      t('common.reminder'),
      {
        confirmButtonText: t('common.ok'),
        cancelButtonText: t('common.cancel'),
        type: 'warning'
      }
    )
  } catch {
    // 用户取消：这是正常选择，不提示、不改列表
    return
  }
  deleting.value = true
  try {
    await deleteFileLeakApi(ids)
    // 整页被删空时回退一页：否则用户停在空白页，会以为数据被清光了
    const emptiedPage = ids.length >= dataList.value.length
    await clearSelection()
    if (emptiedPage && currentPage.value > 1) {
      currentPage.value -= 1 // 分页 watch 会重新拉列表
    } else {
      getList()
    }
  } catch {
    // 失败提示由 axios 拦截器统一弹出，这里只恢复按钮状态
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <ContentWrap>
    <ElAlert
      type="info"
      :closable="false"
      show-icon
      style="margin-bottom: 12px"
      :title="t('fileLeak.noSourceHint')"
    />
    <ElRow :gutter="12" class="filter-row">
      <ElCol :span="5">
        <ElInput
          v-model="keyword"
          :placeholder="t('fileLeak.keywordPlaceholder')"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="3">
        <ElInput
          v-model="statusCode"
          :placeholder="t('fileLeak.statusPlaceholder')"
          maxlength="3"
          clearable
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
      </ElCol>
      <ElCol :span="8">
        <ElButton type="primary" :icon="searchIcon" @click="handleSearch">
          {{ t('common.search') }}
        </ElButton>
        <ElButton @click="handleReset">{{ t('common.reset') }}</ElButton>
      </ElCol>
    </ElRow>
    <div class="toolbar">
      <ElButton
        type="danger"
        :disabled="!selectedIds.length"
        :loading="deleting"
        @click="handleDelete"
      >
        {{ t('common.delete') }}
      </ElButton>
      <span v-if="selectedIds.length" class="selected-hint">
        {{ t('fileLeak.selectedCount', { count: selectedIds.length }) }}
      </span>
    </div>
    <Table
      v-model:pageSize="pageSize"
      v-model:currentPage="currentPage"
      rowKey="id"
      :columns="columns"
      :data="dataList"
      stripe
      :border="true"
      :loading="loading"
      :resizable="true"
      :pagination="{ total, pageSizes: [10, 20, 50, 100, 200] }"
      @register="tableRegister"
      @selection-change="handleSelectionChange"
    >
      <template #empty>
        <div class="empty-state">{{ t('fileLeak.empty') }}</div>
      </template>
    </Table>
  </ContentWrap>
</template>

<style scoped>
.filter-row {
  margin-bottom: 12px;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}

.selected-hint {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.empty-state {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}
</style>
