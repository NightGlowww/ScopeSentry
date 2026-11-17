<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElButton, ElForm, ElFormItem, ElInput, ElMessage, ElOption, ElSelect } from 'element-plus'
import { Dialog } from '@/components/Dialog'
import { useI18n } from '@/hooks/web/useI18n'
import { addAssetScopeApi, updateAssetScopeApi } from '@/api/assetGroup'
import { AssetScope, AssetScopeType } from '@/api/assetGroup/types'

const props = defineProps<{
  modelValue: boolean
  groupId: string
  scope: AssetScope | null
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved'): void
}>()

const { t } = useI18n()

const formRef = ref()
const saving = ref(false)

const form = reactive({
  name: '',
  scope: '',
  scopeType: '' as AssetScopeType | ''
})

const rules = {
  name: [{ required: true, message: t('common.required'), trigger: 'blur' }],
  scope: [{ required: true, message: t('common.required'), trigger: 'blur' }]
}

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value)
})

const scopeTypeOptions = computed(() => [
  { value: '', label: t('assetGroup.scopeTypeAuto') },
  { value: 'domain', label: t('assetGroup.scopeTypeDomain') },
  { value: 'ip', label: t('assetGroup.scopeTypeIp') },
  { value: 'mixed', label: t('assetGroup.scopeTypeMixed') }
])

// 编辑时回填原始文本而不是解析后的数组：用户要看到自己输入的换行格式再改
watch(
  () => props.modelValue,
  (opened) => {
    if (!opened) return
    form.name = props.scope?.name ?? ''
    form.scope = props.scope?.scope ?? ''
    form.scopeType = props.scope?.scopeType ?? ''
  }
)

const submit = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid: boolean) => {
    if (!valid) return
    const name = form.name.trim()
    const scope = form.scope.trim()
    if (!name || !scope) {
      ElMessage.warning(t('common.required'))
      return
    }
    saving.value = true
    try {
      if (props.scope) {
        await updateAssetScopeApi({
          id: props.scope.id,
          name,
          scope,
          scopeType: form.scopeType,
          groupId: props.groupId
        })
      } else {
        await addAssetScopeApi({
          name,
          scope,
          scopeType: form.scopeType,
          groupId: props.groupId
        })
      }
      emit('saved')
      emit('update:modelValue', false)
    } catch {
      // 错误提示由 axios 拦截器统一弹出（非法域名/IP 会显示后端返回的 400）
    } finally {
      saving.value = false
    }
  })
}
</script>

<template>
  <Dialog
    v-model="visible"
    :title="scope ? t('assetGroup.editScope') : t('assetGroup.addScope')"
    :max-height="480"
  >
    <ElForm ref="formRef" :model="form" :rules="rules" label-width="100px" @submit.prevent>
      <ElFormItem :label="t('assetGroup.scopeName')" prop="name">
        <ElInput
          v-model="form.name"
          :placeholder="t('assetGroup.scopeNamePlaceholder')"
          maxlength="128"
          show-word-limit
        />
      </ElFormItem>
      <ElFormItem :label="t('assetGroup.scopeType')">
        <ElSelect v-model="form.scopeType" class="full-width">
          <ElOption
            v-for="item in scopeTypeOptions"
            :key="item.value || 'auto'"
            :label="item.label"
            :value="item.value"
          />
        </ElSelect>
      </ElFormItem>
      <ElFormItem :label="t('assetGroup.scopeContent')" prop="scope">
        <ElInput
          v-model="form.scope"
          type="textarea"
          :rows="10"
          resize="vertical"
          :placeholder="t('assetGroup.scopePlaceholder')"
        />
      </ElFormItem>
      <ElFormItem>
        <div class="scope-hint">{{ t('assetGroup.scopeHint') }}</div>
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

.full-width {
  width: 100%;
}

.scope-hint {
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
</style>
