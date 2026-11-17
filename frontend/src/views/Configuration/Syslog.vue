<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElAlert, ElButton, ElInput, ElOption, ElPagination, ElSelect, ElTag } from 'element-plus'
import { ContentWrap } from '@/components/ContentWrap'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import { getSysLogListApi } from '@/api/syslog'
import type { SysLogEntry, SysLogSource } from '@/api/syslog/types'
import { LOG_LEVELS, logLevelTagType } from '@/utils/logLevel'
import { timeText } from '@/utils/timeText'

// 系统日志（只读）：按级别与关键字过滤进程缓冲/日志文件，时间倒序分页。
// 等宽列表而非表格——日志里空白与缩进有含义，表格会把它们压平。
const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })
const refreshIcon = useIcon({ icon: 'iconoir:refresh' })

const level = ref('')
const keyword = ref('')
const list = ref<SysLogEntry[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(50)
const source = ref<SysLogSource>('none')
const truncated = ref(false)
const loading = ref(false)

const fetchLogs = async () => {
  loading.value = true
  try {
    const res = await getSysLogListApi({
      level: level.value || undefined,
      keyword: keyword.value.trim() || undefined,
      pageIndex: page.value,
      pageSize: Math.min(pageSize.value, 200)
    })
    const data = res?.data
    list.value = data?.list ?? []
    total.value = data?.total ?? 0
    source.value = data?.source ?? 'none'
    truncated.value = data?.truncated === true
    // 后端会按上限/默认值回填分页参数，这里跟着返回值走，避免页码与结果对不上
    if (data?.page) page.value = data.page
    if (data?.pageSize) pageSize.value = data.pageSize
  } catch {
    // 错误提示由 axios 拦截器统一弹出；这里保留上一次成功的结果，避免整页被清空
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  if (page.value !== 1) {
    page.value = 1
  }
  fetchLogs()
}

const onPageChange = (value: number) => {
  page.value = value
  fetchLogs()
}

const onSizeChange = (value: number) => {
  pageSize.value = Math.min(value, 200)
  page.value = 1
  fetchLogs()
}

const levelLabel = (value: string) => {
  switch (value) {
    case 'debug':
      return t('syslog.levelDebug')
    case 'info':
      return t('syslog.levelInfo')
    case 'warn':
      return t('syslog.levelWarn')
    case 'error':
      return t('syslog.levelError')
    default:
      return value
  }
}

const sourceLabel = computed(() => {
  switch (source.value) {
    case 'file':
      return t('syslog.sourceFile')
    case 'memory':
      return t('syslog.sourceMemory')
    default:
      return t('syslog.sourceNone')
  }
})

const sourceTagType = computed<'success' | 'primary' | 'info'>(() => {
  switch (source.value) {
    case 'file':
      return 'success'
    case 'memory':
      return 'primary'
    default:
      return 'info'
  }
})

onMounted(fetchLogs)
</script>

<template>
  <ContentWrap>
    <div class="filter-row">
      <ElSelect v-model="level" class="level-select" @change="handleSearch">
        <ElOption :label="t('syslog.levelAll')" value="" />
        <ElOption v-for="item in LOG_LEVELS" :key="item" :label="levelLabel(item)" :value="item" />
      </ElSelect>
      <ElInput
        v-model="keyword"
        class="keyword-input"
        :placeholder="t('syslog.keywordPlaceholder')"
        maxlength="255"
        clearable
        @keyup.enter="handleSearch"
        @clear="handleSearch"
      />
      <ElButton type="primary" :icon="searchIcon" @click="handleSearch">
        {{ t('common.search') }}
      </ElButton>
      <ElButton :icon="refreshIcon" @click="fetchLogs">{{ t('common.refresh') }}</ElButton>
      <div class="meta">
        <span class="meta-label">{{ t('syslog.sourceLabel') }}</span>
        <ElTag :type="sourceTagType" size="small">{{ sourceLabel }}</ElTag>
        <span class="meta-total">{{ t('syslog.total', { total }) }}</span>
      </div>
    </div>

    <ElAlert
      v-if="truncated"
      type="warning"
      :closable="false"
      show-icon
      class="truncated-alert"
      :title="t('syslog.truncated')"
    />

    <div v-loading="loading" class="log-list">
      <div v-for="(item, index) in list" :key="`${item.time}-${index}`" class="log-row">
        <span class="log-time">{{ timeText(item.time) }}</span>
        <ElTag :type="logLevelTagType(item.level)" size="small" class="log-level">
          {{ (item.level || '-').toUpperCase() }}
        </ElTag>
        <span class="log-caller" :title="item.caller || ''">{{ item.caller || '-' }}</span>
        <span class="log-message">{{ item.message }}</span>
      </div>
      <div v-if="!loading && !list.length" class="empty-state">{{ t('syslog.empty') }}</div>
    </div>

    <ElPagination
      class="log-pagination"
      layout="sizes, prev, pager, next"
      :page-sizes="[20, 50, 100, 200]"
      :total="total"
      :page-size="pageSize"
      :current-page="page"
      @current-change="onPageChange"
      @size-change="onSizeChange"
    />
  </ContentWrap>
</template>

<style scoped>
.filter-row {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  margin-bottom: 15px;
}

.level-select {
  width: 150px;
}

.keyword-input {
  width: 260px;
}

.meta {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-left: auto;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.meta-label {
  color: var(--el-text-color-secondary);
}

.meta-total {
  margin-left: 4px;
}

.truncated-alert {
  margin-bottom: 12px;
}

.log-list {
  min-height: 200px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.log-row {
  display: grid;
  grid-template-columns: 168px 78px minmax(120px, 220px) minmax(0, 1fr);
  gap: 12px;
  align-items: start;
  padding: 6px 8px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  font-family: 'Fira Code', 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 12.5px;
  line-height: 1.6;
}

.log-row:hover {
  background: var(--el-fill-color-light);
}

.log-time {
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

.log-level {
  justify-self: start;
}

.log-caller {
  overflow: hidden;
  color: var(--el-text-color-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-message {
  word-break: break-word;
  white-space: pre-wrap;
}

.empty-state {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}

.log-pagination {
  justify-content: flex-end;
  margin-top: 12px;
}

@media (max-width: 768px) {
  .log-row {
    grid-template-columns: 1fr;
    gap: 4px;
  }

  .meta {
    margin-left: 0;
  }
}
</style>
