<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  ElAlert,
  ElButton,
  ElForm,
  ElFormItem,
  ElMessage,
  ElOption,
  ElRadio,
  ElRadioGroup,
  ElSelect
} from 'element-plus'
import { Dialog } from '@/components/Dialog'
import { useI18n } from '@/hooks/web/useI18n'
import { deleteAssetGroupApi } from '@/api/assetGroup'
import { AssetGroup } from '@/api/assetGroup/types'

const props = defineProps<{
  modelValue: boolean
  group: AssetGroup | null
  groups: AssetGroup[]
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'deleted'): void
}>()

const { t } = useI18n()

const action = ref<'dissolve' | 'transfer'>('dissolve')
const targetGroupId = ref('')
const saving = ref(false)

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value)
})

// 目标集团只能是当前集团之外的集团，删自己再划给自己没有意义
const transferTargets = computed(() => props.groups.filter((group) => group.id !== props.group?.id))

watch(
  () => props.modelValue,
  (opened) => {
    if (!opened) return
    action.value = 'dissolve'
    targetGroupId.value = ''
  }
)

const submit = async () => {
  if (!props.group) return
  if (action.value === 'transfer') {
    if (!targetGroupId.value) {
      ElMessage.warning(t('assetGroup.targetGroupRequired'))
      return
    }
    if (targetGroupId.value === props.group.id) {
      ElMessage.warning(t('assetGroup.targetGroupSame'))
      return
    }
  }
  saving.value = true
  try {
    await deleteAssetGroupApi({
      id: props.group.id,
      action: action.value,
      targetGroupId: action.value === 'transfer' ? targetGroupId.value : ''
    })
    emit('deleted')
    emit('update:modelValue', false)
  } catch {
    // 错误提示由 axios 拦截器统一弹出
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Dialog v-model="visible" :title="t('assetGroup.deleteGroup')" :max-height="360">
    <ElAlert
      :title="t('assetGroup.deleteGroupTip')"
      type="warning"
      :closable="false"
      show-icon
      class="mb-16px"
    />
    <ElForm label-width="110px">
      <ElFormItem :label="t('common.name')">
        <span>{{ group?.name }}</span>
      </ElFormItem>
      <ElFormItem :label="t('assetGroup.scopeCount')">
        <span>{{ group?.scopeCount ?? 0 }}</span>
      </ElFormItem>
      <ElFormItem :label="t('assetGroup.groupDeleteAction')">
        <ElRadioGroup v-model="action">
          <ElRadio value="dissolve">{{ t('assetGroup.dissolveAction') }}</ElRadio>
          <ElRadio value="transfer">{{ t('assetGroup.transferAction') }}</ElRadio>
        </ElRadioGroup>
      </ElFormItem>
      <ElFormItem v-if="action === 'transfer'" :label="t('assetGroup.targetGroup')" required>
        <ElSelect
          v-model="targetGroupId"
          :placeholder="t('assetGroup.targetGroupPlaceholder')"
          class="full-width"
        >
          <ElOption
            v-for="item in transferTargets"
            :key="item.id"
            :label="item.name"
            :value="item.id"
          />
        </ElSelect>
      </ElFormItem>
    </ElForm>
    <template #footer>
      <div class="dialog-footer">
        <ElButton @click="emit('update:modelValue', false)">{{ t('common.cancel') }}</ElButton>
        <ElButton type="danger" :loading="saving" @click="submit">{{ t('common.ok') }}</ElButton>
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

.mb-16px {
  margin-bottom: 16px;
}
</style>
