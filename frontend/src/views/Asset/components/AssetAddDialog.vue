<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { ElButton, ElDialog, ElForm, ElFormItem, ElInput, ElMessage, ElSelect } from 'element-plus'
import { useI18n } from '@/hooks/web/useI18n'
import { addAssetApi } from '@/api/asset'

const props = defineProps<{
  modelValue: boolean
  projectList: { value: string; label: string }[]
}>()
const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved'): void
}>()

const { t } = useI18n()
const formRef = ref()
const saving = ref(false)

const form = reactive({
  host: '',
  ip: '',
  port: '',
  url: '',
  service: '',
  title: '',
  project: '',
  tags: [] as string[]
})

const rules = {
  host: [{ required: true, message: t('asset.addAssetHostRequired'), trigger: 'blur' }]
}

const reset = () => {
  form.host = ''
  form.ip = ''
  form.port = ''
  form.url = ''
  form.service = ''
  form.title = ''
  form.project = ''
  form.tags = []
}

watch(
  () => props.modelValue,
  (visible) => {
    if (visible) reset()
  }
)

const submit = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid: boolean) => {
    if (!valid) return
    saving.value = true
    try {
      await addAssetApi({
        host: form.host.trim(),
        ip: form.ip.trim(),
        port: form.port.trim(),
        url: form.url.trim(),
        service: form.service.trim(),
        title: form.title.trim(),
        project: form.project,
        tags: form.tags
      })
      ElMessage.success(t('asset.addAssetSaved'))
      emit('saved')
      emit('update:modelValue', false)
    } catch {
      // 错误提示由 axios 拦截器统一弹出（409 会显示后端返回的“已存在”）
    } finally {
      saving.value = false
    }
  })
}
</script>

<template>
  <ElDialog
    :model-value="modelValue"
    @update:model-value="emit('update:modelValue', $event)"
    :title="t('asset.addAsset')"
    width="560px"
  >
    <ElForm ref="formRef" :model="form" :rules="rules" label-width="90px">
      <ElFormItem :label="t('asset.addAssetHost')" prop="host">
        <ElInput v-model="form.host" placeholder="app.example.com" />
      </ElFormItem>
      <ElFormItem :label="t('asset.addAssetIp')">
        <ElInput v-model="form.ip" placeholder="10.0.0.9" />
      </ElFormItem>
      <ElFormItem :label="t('asset.addAssetPort')">
        <ElInput v-model="form.port" placeholder="443" />
      </ElFormItem>
      <ElFormItem :label="t('asset.addAssetUrl')">
        <ElInput v-model="form.url" placeholder="https://app.example.com/" />
      </ElFormItem>
      <ElFormItem :label="t('asset.addAssetService')">
        <ElInput v-model="form.service" placeholder="https" />
      </ElFormItem>
      <ElFormItem :label="t('asset.addAssetTitle')">
        <ElInput v-model="form.title" />
      </ElFormItem>
      <ElFormItem :label="t('asset.addAssetProject')">
        <ElSelect v-model="form.project" clearable filterable style="width: 100%">
          <ElOption
            v-for="item in projectList"
            :key="item.value"
            :label="item.label"
            :value="item.value"
          />
        </ElSelect>
      </ElFormItem>
      <ElFormItem :label="t('asset.addAssetTags')">
        <ElSelect
          v-model="form.tags"
          multiple
          filterable
          allow-create
          default-first-option
          :reserve-keyword="false"
          style="width: 100%"
        />
      </ElFormItem>
    </ElForm>
    <template #footer>
      <ElButton @click="emit('update:modelValue', false)">{{ t('common.cancel') }}</ElButton>
      <ElButton type="primary" :loading="saving" @click="submit">
        {{ t('asset.addAssetSubmit') }}
      </ElButton>
    </template>
  </ElDialog>
</template>
