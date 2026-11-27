<script setup lang="tsx">
import { computed, reactive, ref } from 'vue'
import {
  ElButton,
  ElCol,
  ElRow,
  ElInput,
  ElSelect,
  ElOption,
  ElDatePicker,
  ElAlert,
  ElMessage
} from 'element-plus'
import { Table, TableColumn } from '@/components/Table'
import { useI18n } from '@/hooks/web/useI18n'
import { previewBatchExportApi, batchExportCsvApi, type BatchPreviewResult } from '@/api/export'

const { t } = useI18n('export')

// 类型项只存 key：label 在模板里取 t()，切语言才会跟着变
// （写成固定字符串会在 setup 期定死，换语言时下拉仍是旧语言）。
const typeOptions = [
  { value: 'site', key: 'batchTypeSite' },
  { value: 'domain', key: 'batchTypeDomain' },
  { value: 'ip', key: 'batchTypeIp' },
  { value: 'url', key: 'batchTypeUrl' },
  { value: 'ip_port', key: 'batchTypeIpPort' },
  { value: 'cip', key: 'batchTypeCip' },
  { value: 'wih', key: 'batchTypeWih' },
  { value: 'asset_ip', key: 'batchTypeAssetIp' },
  { value: 'asset_domain', key: 'batchTypeAssetDomain' },
  { value: 'asset_site', key: 'batchTypeAssetSite' },
  { value: 'asset_wih', key: 'batchTypeAssetWih' },
  { value: 'vulnerability', key: 'batchTypeVulnerability' },
  { value: 'sensitive', key: 'batchTypeSensitive' },
  { value: 'fileleak', key: 'batchTypeFileleak' },
  { value: 'npoc_service', key: 'batchTypeNpocService' }
]

const form = reactive({
  type: 'url',
  project: '',
  taskName: '',
  range: [] as string[],
  fields: [] as string[]
})

const preview = ref<BatchPreviewResult | null>(null)
const loading = ref(false)
const exporting = ref(false)

const buildPayload = () => ({
  type: form.type,
  project: form.project || undefined,
  taskName: form.taskName || undefined,
  startTime: form.range?.[0] || undefined,
  endTime: form.range?.[1] || undefined,
  fields: form.fields.length ? form.fields : undefined
})

const doPreview = async () => {
  loading.value = true
  try {
    const res = await previewBatchExportApi(buildPayload())
    preview.value = res.data
    if (!form.fields.length && res.data.availableFields?.length) {
      form.fields = res.data.availableFields.slice(0, 12)
      // 用默认字段再取一次，让预览直接有列
      const again = await previewBatchExportApi(buildPayload())
      preview.value = again.data
    }
  } catch (e: any) {
    ElMessage.error(t('batchPreviewFailed', { msg: e?.message || t('batchUnknownError') }))
  } finally {
    loading.value = false
  }
}

const doExport = async () => {
  exporting.value = true
  try {
    const blob = await batchExportCsvApi(buildPayload())
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = form.type + '-' + Date.now() + '.csv'
    link.click()
    URL.revokeObjectURL(link.href)
    ElMessage.success(t('batchDownloadStarted'))
  } catch (e: any) {
    ElMessage.error(t('batchExportFailed', { msg: e?.message || t('batchUnknownError') }))
  } finally {
    exporting.value = false
  }
}

const previewColumns = computed<TableColumn[]>(() => {
  const p = preview.value
  if (!p) return []
  return (p.fields || []).map((f, i) => ({
    field: 'c' + i,
    label: f,
    minWidth: 160
  }))
})

const previewRows = computed(() =>
  (preview.value?.rows || []).map((row) => {
    const obj: Record<string, string> = {}
    row.forEach((v, i) => (obj['c' + i] = v))
    return obj
  })
)
</script>

<template>
  <div>
    <ElRow :gutter="12" class="mb-10px">
      <ElCol :span="4">
        <ElSelect v-model="form.type" style="width: 100%" @change="doPreview">
          <ElOption
            v-for="opt in typeOptions"
            :key="opt.value"
            :label="t(opt.key)"
            :value="opt.value"
          />
        </ElSelect>
      </ElCol>
      <ElCol :span="4">
        <ElInput v-model="form.project" :placeholder="t('batchProjectPlaceholder')" clearable />
      </ElCol>
      <ElCol :span="4">
        <ElInput v-model="form.taskName" :placeholder="t('batchTaskPlaceholder')" clearable />
      </ElCol>
      <ElCol :span="6">
        <ElDatePicker
          v-model="form.range"
          type="datetimerange"
          value-format="YYYY-MM-DD HH:mm:ss"
          :start-placeholder="t('batchStartPlaceholder')"
          :end-placeholder="t('batchEndPlaceholder')"
          style="width: 100%"
        />
      </ElCol>
      <ElCol :span="6">
        <ElButton type="primary" :loading="loading" @click="doPreview">{{
          t('batchPreview')
        }}</ElButton>
        <ElButton type="success" :loading="exporting" @click="doExport">{{
          t('batchExportCsv')
        }}</ElButton>
      </ElCol>
    </ElRow>

    <ElRow :gutter="12" class="mb-10px" v-if="preview?.availableFields?.length">
      <ElCol :span="24">
        <span class="label">{{ t('batchFields') }}</span>
        <ElSelect
          v-model="form.fields"
          multiple
          filterable
          collapse-tags
          :placeholder="t('batchFieldsPlaceholder')"
          style="width: 70%"
        >
          <ElOption v-for="f in preview.availableFields" :key="f" :label="f" :value="f" />
        </ElSelect>
      </ElCol>
    </ElRow>

    <ElAlert
      v-if="preview"
      class="mb-10px"
      type="info"
      :closable="false"
      show-icon
      :title="
        t('batchPreviewSummary', {
          type: preview.type,
          collection: preview.collection,
          total: preview.total,
          limit: preview.limit
        })
      "
    />

    <Table
      v-if="previewColumns.length"
      :columns="previewColumns"
      :data="previewRows"
      stripe
      border
      max-height="420"
    />
  </div>
</template>

<style scoped>
.mb-10px {
  margin-bottom: 10px;
}
.label {
  margin-right: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
