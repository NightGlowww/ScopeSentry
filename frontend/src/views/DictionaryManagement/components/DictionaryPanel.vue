<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  ElButton,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElMessageBox,
  ElPagination,
  ElTag,
  ElText
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { BaseButton } from '@/components/Button'
import { useI18n } from '@/hooks/web/useI18n'
import { useIcon } from '@/hooks/web/useIcon'
import {
  appendDictionaryApi,
  deleteDictionaryEntriesApi,
  deleteDictionaryFileApi,
  DICT_MAX_APPEND_LINES,
  DICT_PREVIEW_LIMIT,
  DICT_SEARCH_LIMIT,
  downloadDictionaryApi,
  listDictionariesByKindApi,
  previewDictionaryApi,
  saveDictionaryByKindApi,
  searchDictionaryApi
} from '@/api/dictionary'
import type { DictionaryItem, DictionaryRow } from '@/api/dictionary/types'
import { splitDictLines } from '@/utils/dictLines'
import DictionaryUploadDialog from './DictionaryUploadDialog.vue'

// 一个 kind 一套面板：左侧字典列表，右侧预览/搜索/追加/删除/下载。
// brute / cdn 只是 kind 固定，接口由 api/dictionary 内部路由到 /dict_brute、/dict_cdn。
const props = defineProps<{ kind: string }>()

const { t } = useI18n()
const searchIcon = useIcon({ icon: 'iconoir:search' })

const KIND_LABEL_KEYS: Record<string, string> = {
  dir: 'dictManager.kindDir',
  subdomain: 'dictManager.kindSubdomain',
  brute: 'dictManager.kindBrute',
  cdn: 'dictManager.kindCdn',
  other: 'dictManager.kindOther'
}

const kindLabel = computed(() => t(KIND_LABEL_KEYS[props.kind] ?? 'dictManager.kindOther'))

// brute / cdn 的保存接口是「整份覆盖」，其余 kind 走 create（同名直接报错），
// 新建弹窗里把差异说清楚，避免用户以为两种 tab 行为一样。
const isFixedKind = computed(() => props.kind === 'brute' || props.kind === 'cdn')

// ---- 左侧字典列表 ----
const listLoading = ref(false)
const dictionaries = ref<DictionaryItem[]>([])
const total = ref(0)
const pageIndex = ref(1)
const pageSize = ref(20)
const listSearch = ref('')
const selected = ref<DictionaryItem | null>(null)

const loadList = async () => {
  listLoading.value = true
  try {
    const res = await listDictionariesByKindApi(props.kind, {
      search: listSearch.value.trim(),
      pageIndex: pageIndex.value,
      pageSize: pageSize.value
    })
    dictionaries.value = res?.data?.list ?? []
    total.value = res?.data?.total ?? 0
    // 列表刷新后同步选中项的行数/大小；不在当前页时保持原选中对象不动
    const current = selected.value
    if (current) {
      const fresh = dictionaries.value.find((item) => item.name === current.name)
      if (fresh) selected.value = fresh
    }
  } finally {
    listLoading.value = false
  }
}

const applyListSearch = () => {
  pageIndex.value = 1
  loadList()
}

const onPageChange = (page: number) => {
  pageIndex.value = page
  loadList()
}

// ---- 右侧内容 ----
const rowsLoading = ref(false)
const rows = ref<DictionaryRow[]>([])
const previewTotal = ref(0)
const previewTruncated = ref(false)
const mode = ref<'preview' | 'search'>('preview')
const keyword = ref('')
const searchReturned = ref(0)
const searchTruncated = ref(false)
const selectedValues = ref<string[]>([])

const rowColumns = reactive<TableColumn[]>([
  { field: 'selection', type: 'selection', width: '55' },
  { field: 'seq', label: t('dictManager.seq'), width: 90 },
  { field: 'value', label: t('dictManager.value'), minWidth: 200 }
])

const currentLineCount = computed(() => previewTotal.value || selected.value?.lineCount || 0)

const clearRows = () => {
  rows.value = []
  selectedValues.value = []
}

const clearSelection = () => {
  selected.value = null
  previewTotal.value = 0
  previewTruncated.value = false
  mode.value = 'preview'
  keyword.value = ''
  clearRows()
}

const loadPreview = async () => {
  const current = selected.value
  if (!current) return
  rowsLoading.value = true
  try {
    const res = await previewDictionaryApi(props.kind, current.name, DICT_PREVIEW_LIMIT)
    const data = res?.data
    rows.value = (data?.lines ?? []).map((value, index) => ({ value, seq: index + 1 }))
    previewTotal.value = data?.total ?? current.lineCount
    previewTruncated.value = !!data?.truncated
    mode.value = 'preview'
    selectedValues.value = []
  } catch {
    clearRows()
  } finally {
    rowsLoading.value = false
  }
}

const selectDictionary = async (item: DictionaryItem) => {
  if (selected.value?.name === item.name) return
  selected.value = item
  previewTotal.value = item.lineCount
  previewTruncated.value = false
  keyword.value = ''
  mode.value = 'preview'
  await loadPreview()
}

const runSearch = async () => {
  const current = selected.value
  if (!current) return
  const value = keyword.value.trim()
  if (!value) {
    await loadPreview()
    return
  }
  rowsLoading.value = true
  try {
    const res = await searchDictionaryApi(props.kind, current.name, value, DICT_SEARCH_LIMIT)
    const data = res?.data
    rows.value = (data?.matches ?? []).map((item) => ({ value: item.value, seq: item.seq }))
    searchReturned.value = data?.returned ?? rows.value.length
    searchTruncated.value = !!data?.truncated
    mode.value = 'search'
    selectedValues.value = []
  } catch {
    clearRows()
  } finally {
    rowsLoading.value = false
  }
}

const onSelectionChange = (selection: DictionaryRow[]) => {
  selectedValues.value = selection.map((row) => row.value)
}

const refreshCurrent = async () => {
  await loadList()
  if (selected.value) {
    if (mode.value === 'search' && keyword.value.trim()) {
      await runSearch()
    } else {
      await loadPreview()
    }
  }
}

// ---- 追加行 ----
const appendDialog = reactive({ visible: false, text: '' })

const openAppend = () => {
  if (!selected.value) return
  appendDialog.text = ''
  appendDialog.visible = true
}

const submitAppend = async () => {
  const current = selected.value
  if (!current) return
  const lines = splitDictLines(appendDialog.text)
  if (!lines.length) {
    ElMessage.warning(t('dictManager.appendEmpty'))
    return
  }
  if (lines.length > DICT_MAX_APPEND_LINES) {
    ElMessage.warning(t('dictManager.appendTooMany', { max: DICT_MAX_APPEND_LINES }))
    return
  }
  const res = await appendDictionaryApi(props.kind, current.name, lines)
  const data = res?.data
  ElMessage.success(
    t('dictManager.appendDone', {
      added: data?.added ?? 0,
      duplicated: data?.duplicated ?? 0,
      total: data?.total ?? 0
    })
  )
  appendDialog.visible = false
  await loadList()
  if (selected.value) previewTotal.value = data?.total ?? previewTotal.value
  await loadPreview()
}

// ---- 删除选中行 ----
const deleteSelectedRows = async () => {
  const current = selected.value
  if (!current) return
  if (!selectedValues.value.length) {
    ElMessage.warning(t('dictManager.deleteRowsEmpty'))
    return
  }
  const confirmed = await ElMessageBox.confirm(
    t('dictManager.deleteRowsConfirm', { count: selectedValues.value.length }),
    t('common.delete'),
    { type: 'warning' }
  ).catch(() => false)
  if (!confirmed) return
  const res = await deleteDictionaryEntriesApi(props.kind, current.name, selectedValues.value)
  const data = res?.data
  ElMessage.success(
    t('dictManager.deleteRowsDone', {
      deleted: data?.deleted ?? 0,
      total: data?.total ?? 0
    })
  )
  await loadList()
  if (selected.value) previewTotal.value = data?.total ?? previewTotal.value
  await loadPreview()
}

// ---- 下载 / 新建 / 删除字典 ----
const download = async () => {
  const current = selected.value
  if (!current) return
  const res = await downloadDictionaryApi(props.kind, current.name)
  const blob = res?.data as unknown as Blob
  if (!blob) return
  const url = window.URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `${current.name}.txt`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  window.URL.revokeObjectURL(url)
}

const createDialog = reactive({ visible: false, name: '', content: '' })

const openCreate = () => {
  createDialog.name = ''
  createDialog.content = ''
  createDialog.visible = true
}

const submitCreate = async () => {
  const name = createDialog.name.trim()
  if (!name) {
    ElMessage.warning(t('dictManager.nameRequired'))
    return
  }
  const res = await saveDictionaryByKindApi(props.kind, name, createDialog.content)
  const data = res?.data
  ElMessage.success(
    t('dictManager.createDone', { name: data?.name ?? name, count: data?.lineCount ?? 0 })
  )
  createDialog.visible = false
  await loadList()
  // 新建的字典可能不在当前页：直接用返回结果构造选中项，右侧立刻可用
  selected.value = {
    id: data?.id ?? '',
    name: data?.name ?? name,
    kind: data?.kind ?? props.kind,
    category: data?.kind ?? props.kind,
    size: data?.size ?? '',
    lineCount: data?.lineCount ?? 0,
    updatedAt: new Date().toISOString()
  }
  previewTotal.value = selected.value.lineCount
  previewTruncated.value = false
  keyword.value = ''
  await loadPreview()
}

const removeDictionary = async () => {
  const current = selected.value
  if (!current) return
  const confirmed = await ElMessageBox.confirm(
    t('dictManager.deleteFileConfirm', { name: current.name }),
    t('common.delete'),
    { type: 'warning' }
  ).catch(() => false)
  if (!confirmed) return
  await deleteDictionaryFileApi(props.kind, current.name)
  ElMessage.success(t('dictManager.deleteFileDone', { name: current.name }))
  clearSelection()
  await loadList()
}

// ---- 大文件上传 ----
const uploadVisible = ref(false)

const onUploadDone = async () => {
  await refreshCurrent()
}

onMounted(loadList)
</script>

<template>
  <div class="dict-panel">
    <aside class="dict-panel__side">
      <ElInput
        v-model="listSearch"
        :placeholder="t('dictManager.listSearchPlaceholder')"
        clearable
        @keyup.enter="applyListSearch"
        @clear="applyListSearch"
      >
        <template #append>
          <ElButton :icon="searchIcon" @click="applyListSearch" />
        </template>
      </ElInput>
      <div v-loading="listLoading" class="dict-panel__list">
        <div
          v-for="item in dictionaries"
          :key="item.id || item.name"
          class="dict-item"
          :class="{ 'is-active': item.name === selected?.name }"
          role="button"
          tabindex="0"
          @click="selectDictionary(item)"
          @keydown.enter="selectDictionary(item)"
        >
          <div class="dict-item__name">{{ item.name }}</div>
          <div class="dict-item__meta">
            <ElText size="small" type="info">
              {{ t('dictManager.lineCount', { count: item.lineCount }) }}
            </ElText>
            <ElText size="small" type="info">{{ item.size }}</ElText>
          </div>
        </div>
        <div v-if="!listLoading && !dictionaries.length" class="dict-panel__empty">
          {{ t('dictManager.emptyList') }}
        </div>
      </div>
      <ElPagination
        v-if="total > pageSize"
        small
        layout="prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="pageIndex"
        @current-change="onPageChange"
      />
    </aside>

    <section class="dict-panel__main">
      <div class="dict-panel__header">
        <div class="dict-panel__title">
          <ElText tag="b">{{ selected ? selected.name : t('dictManager.listTitle') }}</ElText>
          <ElTag size="small" type="info">{{ kindLabel }}</ElTag>
          <ElText v-if="selected" size="small" type="info">
            {{ t('dictManager.lineCount', { count: currentLineCount }) }}
          </ElText>
        </div>
        <div class="dict-panel__actions">
          <BaseButton type="primary" :disabled="!selected" @click="openAppend">
            {{ t('dictManager.append') }}
          </BaseButton>
          <BaseButton type="danger" :disabled="!selected" @click="deleteSelectedRows">
            {{ t('dictManager.deleteRows') }}
          </BaseButton>
          <BaseButton type="primary" :disabled="!selected" @click="download">
            {{ t('dictManager.download') }}
          </BaseButton>
          <BaseButton type="danger" :disabled="!selected" @click="removeDictionary">
            {{ t('dictManager.deleteFile') }}
          </BaseButton>
          <ElButton @click="openCreate">{{ t('dictManager.create') }}</ElButton>
          <ElButton type="warning" @click="uploadVisible = true">
            {{ t('dictManager.upload') }}
          </ElButton>
          <ElButton :disabled="!selected" @click="refreshCurrent">
            {{ t('common.refresh') }}
          </ElButton>
        </div>
      </div>

      <div v-if="!selected" class="dict-panel__placeholder">
        {{ t('dictManager.emptySelection', { limit: DICT_PREVIEW_LIMIT }) }}
      </div>
      <template v-else>
        <div class="dict-panel__search">
          <ElInput
            v-model="keyword"
            :placeholder="t('dictManager.searchPlaceholder')"
            clearable
            @keyup.enter="runSearch"
            @clear="loadPreview"
          />
          <ElButton type="primary" :icon="searchIcon" @click="runSearch">
            {{ t('common.search') }}
          </ElButton>
          <ElButton v-if="mode === 'search'" link type="primary" @click="loadPreview">
            {{ t('dictManager.backToPreview') }}
          </ElButton>
          <ElText v-if="mode === 'search'" size="small" type="info">
            {{ t('dictManager.searchSummary', { returned: searchReturned }) }}
            <template v-if="searchTruncated">
              {{ t('dictManager.searchTruncated', { limit: DICT_SEARCH_LIMIT }) }}
            </template>
          </ElText>
          <ElText v-else-if="previewTruncated" size="small" type="info">
            {{
              t('dictManager.previewTruncated', {
                limit: DICT_PREVIEW_LIMIT,
                total: currentLineCount
              })
            }}
          </ElText>
        </div>

        <Table
          :columns="rowColumns"
          :data="rows"
          stripe
          :border="true"
          :loading="rowsLoading"
          :resizable="true"
          max-height="520"
          @selection-change="onSelectionChange"
        >
          <template #empty>
            <div class="dict-panel__empty">
              {{ mode === 'search' ? t('dictManager.searchEmpty') : t('dictManager.emptyPreview') }}
            </div>
          </template>
        </Table>
      </template>
    </section>

    <ElDialog
      v-model="appendDialog.visible"
      :title="t('dictManager.appendTitle', { name: selected?.name ?? '' })"
      width="560px"
      :close-on-click-modal="false"
    >
      <ElInput
        v-model="appendDialog.text"
        type="textarea"
        :rows="10"
        :placeholder="t('dictManager.appendPlaceholder')"
      />
      <ElText size="small" type="info" class="dict-dialog__hint">
        {{ t('dictManager.appendHint') }}
      </ElText>
      <template #footer>
        <ElButton @click="appendDialog.visible = false">{{ t('common.cancel') }}</ElButton>
        <ElButton type="primary" @click="submitAppend">{{ t('common.submit') }}</ElButton>
      </template>
    </ElDialog>

    <ElDialog
      v-model="createDialog.visible"
      :title="t('dictManager.createTitle')"
      width="560px"
      :close-on-click-modal="false"
    >
      <ElForm label-width="96px">
        <ElFormItem :label="t('dictManager.nameLabel')">
          <ElInput v-model="createDialog.name" :placeholder="t('dictManager.namePlaceholder')" />
        </ElFormItem>
        <ElFormItem :label="t('dictManager.contentLabel')">
          <ElInput
            v-model="createDialog.content"
            type="textarea"
            :rows="8"
            :placeholder="t('dictManager.contentPlaceholder')"
          />
        </ElFormItem>
      </ElForm>
      <ElText size="small" type="info" class="dict-dialog__hint">
        {{ isFixedKind ? t('dictManager.createOverwriteHint') : t('dictManager.createRejectHint') }}
      </ElText>
      <template #footer>
        <ElButton @click="createDialog.visible = false">{{ t('common.cancel') }}</ElButton>
        <ElButton type="primary" @click="submitCreate">{{ t('common.save') }}</ElButton>
      </template>
    </ElDialog>

    <DictionaryUploadDialog
      v-model="uploadVisible"
      :kind="kind"
      :default-name="selected?.name ?? ''"
      @done="onUploadDone"
    />
  </div>
</template>

<style scoped>
.dict-panel {
  display: flex;
  gap: 16px;
  min-height: 560px;
}

.dict-panel__side {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 300px;
  flex: 0 0 300px;
}

.dict-panel__list {
  flex: 1;
  overflow-y: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
  padding: 4px;
  max-height: 520px;
}

.dict-item {
  padding: 8px 10px;
  border-radius: 4px;
  cursor: pointer;
  outline: none;
}

.dict-item:hover {
  background: var(--el-fill-color-light);
}

.dict-item.is-active {
  background: var(--el-color-primary-light-9);
}

.dict-item__name {
  font-weight: 600;
  word-break: break-all;
}

.dict-item__meta {
  display: flex;
  gap: 12px;
  margin-top: 4px;
}

.dict-panel__main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dict-panel__header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.dict-panel__title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.dict-panel__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.dict-panel__search {
  display: flex;
  align-items: center;
  gap: 8px;
}

.dict-panel__placeholder,
.dict-panel__empty {
  padding: 24px 8px;
  color: var(--el-text-color-secondary);
  text-align: center;
}

.dict-dialog__hint {
  display: block;
  margin-top: 8px;
}
</style>
