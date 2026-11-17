<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElButton, ElForm, ElFormItem, ElInput, ElInputNumber, ElMessage } from 'element-plus'
import { Dialog } from '@/components/Dialog'
import { useI18n } from '@/hooks/web/useI18n'
import { addAssetGroupApi, editAssetGroupApi } from '@/api/assetGroup'
import { AssetGroup } from '@/api/assetGroup/types'

const props = defineProps<{
  modelValue: boolean
  group: AssetGroup | null
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved', id?: string): void
}>()

const { t } = useI18n()

const formRef = ref()
const saving = ref(false)

const form = reactive({
  name: '',
  description: '',
  sortOrder: 0
})

const rules = {
  name: [
    { required: true, message: t('common.required'), trigger: 'blur' },
    { max: 64, message: t('assetGroup.groupNameMax'), trigger: 'blur' }
  ]
}

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value)
})

// 弹窗每次打开都按当前模式重置，避免上一次编辑的内容残留
watch(
  () => props.modelValue,
  (opened) => {
    if (!opened) return
    form.name = props.group?.name ?? ''
    form.description = props.group?.description ?? ''
    form.sortOrder = props.group?.sortOrder ?? 0
  }
)

const submit = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid: boolean) => {
    if (!valid) return
    const name = form.name.trim()
    if (!name) {
      ElMessage.warning(t('common.required'))
      return
    }
    saving.value = true
    try {
      if (props.group) {
        await editAssetGroupApi({
          id: props.group.id,
          name,
          description: form.description.trim(),
          sortOrder: form.sortOrder
        })
        emit('saved')
      } else {
        const res = await addAssetGroupApi({
          name,
          description: form.description.trim(),
          sortOrder: form.sortOrder
        })
        emit('saved', res.data?.id)
      }
      emit('update:modelValue', false)
    } catch {
      // 错误提示由 axios 拦截器统一弹出（集团重名会显示后端返回的 400）
    } finally {
      saving.value = false
    }
  })
}
</script>

<template>
  <Dialog
    v-model="visible"
    :title="group ? t('assetGroup.editGroup') : t('assetGroup.createGroup')"
    :max-height="380"
  >
    <ElForm ref="formRef" :model="form" :rules="rules" label-width="100px" @submit.prevent>
      <ElFormItem :label="t('assetGroup.groupName')" prop="name">
        <ElInput
          v-model="form.name"
          :placeholder="t('assetGroup.namePlaceholder')"
          maxlength="64"
          show-word-limit
        />
      </ElFormItem>
      <ElFormItem :label="t('assetGroup.groupDescription')">
        <ElInput
          v-model="form.description"
          type="textarea"
          :rows="3"
          :placeholder="t('assetGroup.descriptionPlaceholder')"
          maxlength="255"
          show-word-limit
        />
      </ElFormItem>
      <ElFormItem :label="t('assetGroup.sortOrder')">
        <ElInputNumber v-model="form.sortOrder" :min="0" :max="9999" controls-position="right" />
      </ElFormItem>
    </ElForm>
    <template #footer>
      <div class="dialog-footer">
        <ElButton @click="emit('update:modelValue', false)">{{ t('common.cancel') }}</ElButton>
        <ElButton type="primary" :loading="saving" @click="submit">{{ t('common.ok') }}</ElButton>
      </div>
    </template>
  </Dialog>
</template>

<style lang="less" scoped>
.dialog-footer {
  display: flex;
  justify-content: flex-end;
}
</style>
