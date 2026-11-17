<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElAlert, ElButton, ElForm, ElFormItem, ElInput, ElMessage } from 'element-plus'
import { Dialog } from '@/components/Dialog'
import { useI18n } from '@/hooks/web/useI18n'

const props = defineProps<{
  modelValue: boolean
  count: number
  defaultEnterprise: string
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'confirm', enterprise: string): void
}>()

const { t } = useI18n()

const enterprise = ref('')

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value)
})

// 单条选中时带出已有企业主体，多条选中时留空由用户统一填写
watch(
  () => props.modelValue,
  (opened) => {
    if (opened) enterprise.value = props.defaultEnterprise
  }
)

const submit = () => {
  const value = enterprise.value.trim()
  if (!value) {
    ElMessage.warning(t('assetGroup.enterpriseRequired'))
    return
  }
  emit('confirm', value)
  emit('update:modelValue', false)
}
</script>

<template>
  <Dialog v-model="visible" :title="t('assetGroup.bindEnterpriseTitle')" :max-height="260">
    <ElAlert
      :title="t('assetGroup.bindEnterpriseTip', { count })"
      type="info"
      :closable="false"
      show-icon
      class="mb-16px"
    />
    <ElForm label-width="110px">
      <ElFormItem :label="t('assetGroup.enterprise')" required>
        <ElInput
          v-model="enterprise"
          :placeholder="t('assetGroup.enterprisePlaceholder')"
          maxlength="128"
          show-word-limit
          @keyup.enter="submit"
        />
      </ElFormItem>
    </ElForm>
    <template #footer>
      <div class="dialog-footer">
        <ElButton @click="emit('update:modelValue', false)">{{ t('common.cancel') }}</ElButton>
        <ElButton type="primary" @click="submit">{{ t('common.ok') }}</ElButton>
      </div>
    </template>
  </Dialog>
</template>

<style lang="less" scoped>
.dialog-footer {
  display: flex;
  justify-content: flex-end;
}

.mb-16px {
  margin-bottom: 16px;
}
</style>
