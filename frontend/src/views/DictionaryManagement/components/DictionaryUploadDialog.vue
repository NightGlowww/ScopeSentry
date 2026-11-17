<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import {
  ElAlert,
  ElButton,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElProgress,
  ElRadioButton,
  ElRadioGroup,
  ElTag,
  ElText,
  ElUpload
} from 'element-plus'
import type { UploadFile, UploadFiles } from 'element-plus'
import { useI18n } from '@/hooks/web/useI18n'
import { uploadDictionaryStatusApi, uploadLargeDictionaryApi } from '@/api/dictionary'
import type { DictionaryUploadTask, DictUploadMode } from '@/api/dictionary/types'

// 大文件上传：选文件 → upload_large 拿 taskId → 轮询 upload_status 显示进度。
// 关掉弹窗只停前端轮询，后台任务继续跑（任务表在后端，重新打开页面仍能在列表里看到结果）。
const props = defineProps<{
  kind: string
  modelValue: boolean
  defaultName?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'done'): void
}>()

const { t } = useI18n()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value)
})

const form = reactive({
  name: '',
  mode: 'create' as DictUploadMode
})
const file = ref<File | null>(null)
const fileList = ref<UploadFiles>([])
const uploading = ref(false)
const task = ref<DictionaryUploadTask | null>(null)

let pollTimer: ReturnType<typeof setTimeout> | null = null

const stopPoll = () => {
  if (pollTimer) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
}

const percent = computed(() => {
  const current = task.value
  if (!current) return 0
  // 行数已知时按 processed/total 算，避免 progress 的浮点误差；
  // 行数还没数完（totalLines=0）时退回后端给的 progress。
  const ratio =
    current.totalLines > 0 ? (current.processedLines * 100) / current.totalLines : current.progress
  return Math.min(100, Math.max(0, Math.round(ratio)))
})

const progressText = computed(() => {
  const current = task.value
  if (!current) return ''
  return t('dictManager.uploadProgress', {
    processed: current.processedLines,
    total: current.totalLines
  })
})

const statusText = computed(() => {
  switch (task.value?.status) {
    case 'pending':
      return t('dictManager.statusPending')
    case 'running':
      return t('dictManager.statusRunning')
    case 'done':
      return t('dictManager.statusDone')
    case 'failed':
      return t('dictManager.statusFailed')
    default:
      return ''
  }
})

const statusTagType = computed(() => {
  switch (task.value?.status) {
    case 'done':
      return 'success'
    case 'failed':
      return 'danger'
    case 'running':
      return 'warning'
    default:
      return 'info'
  }
})

const resetState = () => {
  stopPoll()
  form.name = props.defaultName ?? ''
  // 已经选中的字典默认追加，避免误覆盖同名内容
  form.mode = props.defaultName ? 'append' : 'create'
  file.value = null
  fileList.value = []
  uploading.value = false
  task.value = null
}

watch(
  () => props.modelValue,
  (value) => {
    if (value) {
      resetState()
    } else {
      stopPoll()
    }
  }
)

const onFileChange = (uploadFile: UploadFile, uploadFiles: UploadFiles) => {
  file.value = (uploadFile.raw as File) ?? null
  fileList.value = uploadFiles.slice(-1)
}

const onFileRemove = () => {
  file.value = null
  fileList.value = []
}

const schedulePoll = () => {
  stopPoll()
  pollTimer = setTimeout(poll, 800)
}

const poll = async () => {
  const current = task.value
  if (!current) return
  try {
    const res = await uploadDictionaryStatusApi(current.taskId)
    if (res?.data) {
      task.value = res.data
    }
  } catch {
    // 轮询失败就停下，避免错误提示刷屏；任务在后端仍会继续
    stopPoll()
    return
  }
  if (task.value?.status === 'done') {
    stopPoll()
    ElMessage.success(
      t('dictManager.uploadDone', {
        added: task.value.added,
        duplicated: task.value.duplicated
      })
    )
    emit('done')
    return
  }
  if (task.value?.status === 'failed') {
    stopPoll()
    return
  }
  schedulePoll()
}

const startUpload = async () => {
  const name = form.name.trim()
  if (!name) {
    ElMessage.warning(t('dictManager.uploadNeedName'))
    return
  }
  if (!file.value) {
    ElMessage.warning(t('dictManager.uploadNeedFile'))
    return
  }
  uploading.value = true
  task.value = null
  try {
    const res = await uploadLargeDictionaryApi({
      kind: props.kind,
      name,
      mode: form.mode,
      file: file.value
    })
    if (res?.data) {
      task.value = res.data
      if (task.value.status === 'done') {
        ElMessage.success(
          t('dictManager.uploadDone', {
            added: task.value.added,
            duplicated: task.value.duplicated
          })
        )
        emit('done')
      } else if (task.value.status !== 'failed') {
        schedulePoll()
      }
    }
  } finally {
    uploading.value = false
  }
}

onBeforeUnmount(stopPoll)
</script>

<template>
  <ElDialog
    v-model="visible"
    :title="t('dictManager.uploadTitle')"
    width="560px"
    :close-on-click-modal="false"
  >
    <ElAlert :title="t('dictManager.uploadHint')" type="info" :closable="false" class="mb-4" />
    <ElForm :model="form" label-width="96px">
      <ElFormItem :label="t('dictManager.uploadName')">
        <ElInput
          v-model="form.name"
          :placeholder="t('dictManager.uploadNamePlaceholder')"
          :disabled="uploading"
          clearable
        />
      </ElFormItem>
      <ElFormItem :label="t('dictManager.uploadMode')">
        <ElRadioGroup v-model="form.mode" :disabled="uploading">
          <ElRadioButton value="create">{{ t('dictManager.modeCreate') }}</ElRadioButton>
          <ElRadioButton value="append">{{ t('dictManager.modeAppend') }}</ElRadioButton>
        </ElRadioGroup>
      </ElFormItem>
      <ElFormItem :label="t('dictManager.uploadPick')">
        <ElUpload
          v-model:file-list="fileList"
          drag
          action="#"
          :auto-upload="false"
          :limit="1"
          :on-change="onFileChange"
          :on-remove="onFileRemove"
          :disabled="uploading"
        >
          <div class="upload-hint">{{ t('dictManager.uploadPick') }}</div>
        </ElUpload>
      </ElFormItem>
    </ElForm>
    <div v-if="task" class="upload-progress">
      <ElProgress :percentage="percent" :status="task.status === 'failed' ? 'exception' : ''" />
      <div class="upload-progress__meta">
        <ElTag size="small" :type="statusTagType">{{ statusText }}</ElTag>
        <ElText size="small" type="info">{{ progressText }}</ElText>
      </div>
      <ElAlert
        v-if="task.status === 'failed'"
        :title="t('dictManager.uploadFailed', { error: task.error || '-' })"
        type="error"
        :closable="false"
        class="mt-3"
      />
      <ElAlert
        v-else-if="task.status === 'done'"
        :title="t('dictManager.uploadDone', { added: task.added, duplicated: task.duplicated })"
        type="success"
        :closable="false"
        class="mt-3"
      />
      <ElText v-else-if="task.totalLines === 0" size="small" type="info">
        {{ t('dictManager.uploadPending') }}
      </ElText>
    </div>
    <template #footer>
      <ElButton @click="visible = false">{{ t('common.cancel') }}</ElButton>
      <ElButton type="primary" :loading="uploading" @click="startUpload">
        {{ t('dictManager.uploadStart') }}
      </ElButton>
    </template>
  </ElDialog>
</template>

<style scoped>
.upload-hint {
  padding: 24px 0;
  color: var(--el-text-color-secondary);
}

.upload-progress {
  margin-top: 4px;
}

.upload-progress__meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}
</style>
