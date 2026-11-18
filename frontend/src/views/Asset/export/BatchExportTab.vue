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
import { previewBatchExportApi, batchExportCsvApi, type BatchPreviewResult } from '@/api/export'

const typeOptions = [
  { value: 'site', label: '站点 site' },
  { value: 'domain', label: '域名 domain' },
  { value: 'ip', label: 'IP ip' },
  { value: 'url', label: 'URL url' },
  { value: 'ip_port', label: 'IP 端口 ip_port' },
  { value: 'cip', label: '企业 ICP cip' },
  { value: 'wih', label: 'WIH wih' },
  { value: 'asset_ip', label: '资产 IP asset_ip' },
  { value: 'asset_domain', label: '资产域名 asset_domain' },
  { value: 'asset_site', label: '资产站点 asset_site' },
  { value: 'asset_wih', label: '资产 WIH asset_wih' },
  { value: 'vulnerability', label: '漏洞 vulnerability' },
  { value: 'sensitive', label: '敏感信息 sensitive' },
  { value: 'fileleak', label: '文件泄露 fileleak' },
  { value: 'npoc_service', label: 'NPoc 服务 npoc_service' }
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
    ElMessage.error('预览失败：' + (e?.message || '未知错误'))
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
    ElMessage.success('已开始下载')
  } catch (e: any) {
    ElMessage.error('导出失败：' + (e?.message || '未知错误'))
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
          <ElOption v-for="t in typeOptions" :key="t.value" :label="t.label" :value="t.value" />
        </ElSelect>
      </ElCol>
      <ElCol :span="4">
        <ElInput v-model="form.project" placeholder="项目（可选）" clearable />
      </ElCol>
      <ElCol :span="4">
        <ElInput v-model="form.taskName" placeholder="任务名（可选）" clearable />
      </ElCol>
      <ElCol :span="6">
        <ElDatePicker
          v-model="form.range"
          type="datetimerange"
          value-format="YYYY-MM-DD HH:mm:ss"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          style="width: 100%"
        />
      </ElCol>
      <ElCol :span="6">
        <ElButton type="primary" :loading="loading" @click="doPreview">预览</ElButton>
        <ElButton type="success" :loading="exporting" @click="doExport">导出 CSV</ElButton>
      </ElCol>
    </ElRow>

    <ElRow :gutter="12" class="mb-10px" v-if="preview?.availableFields?.length">
      <ElCol :span="24">
        <span class="label">导出字段</span>
        <ElSelect
          v-model="form.fields"
          multiple
          filterable
          collapse-tags
          placeholder="默认取前 12 个字段，可自行调整"
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
      :title="'类型 ' + preview.type + ' → 集合 ' + preview.collection + '：命中 ' + preview.total + ' 行，预览前 ' + preview.limit + ' 行'"
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
