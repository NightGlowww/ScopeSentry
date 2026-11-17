<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElAlert, ElButton, ElForm, ElFormItem, ElMessage, ElOption, ElSelect } from 'element-plus'
import { Dialog } from '@/components/Dialog'
import { useI18n } from '@/hooks/web/useI18n'
import { UNASSIGNED_GROUP_ID } from '@/api/assetGroup'
import { AssetGroup } from '@/api/assetGroup/types'

const props = defineProps<{
  modelValue: boolean
  groups: AssetGroup[]
  count: number
  currentGroupId: string
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'confirm', groupId: string): void
}>()

const { t } = useI18n()

const targetGroupId = ref('')

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value)
})

// 目标是当前集团时移动没有意义，直接从下拉里去掉；
// 「未分组」用哨兵值表示，落库前由 API 层换成空串
const options = computed(() => [
  ...(props.currentGroupId === UNASSIGNED_GROUP_ID
    ? []
    : [{ value: UNASSIGNED_GROUP_ID, label: t('assetGroup.unassigned') }]),
  ...props.groups
    .filter((group) => group.id !== props.currentGroupId)
    .map((group) => ({ value: group.id, label: group.name }))
])

watch(
  () => props.modelValue,
  (opened) => {
    if (opened) targetGroupId.value = ''
  }
)

const submit = () => {
  if (!targetGroupId.value) {
    ElMessage.warning(t('assetGroup.targetGroupRequired'))
    return
  }
  emit('confirm', targetGroupId.value)
  emit('update:modelValue', false)
}
</script>

<template>
  <Dialog v-model="visible" :title="t('assetGroup.batchMoveTitle')" :max-height="260">
    <ElAlert
      :title="t('assetGroup.batchMoveTip', { count })"
      type="info"
      :closable="false"
      show-icon
      class="mb-16px"
    />
    <ElForm label-width="110px">
      <ElFormItem :label="t('assetGroup.batchMoveTarget')" required>
        <ElSelect
          v-model="targetGroupId"
          :placeholder="t('assetGroup.batchMoveTargetPlaceholder')"
          class="full-width"
        >
          <ElOption
            v-for="item in options"
            :key="item.value"
            :label="item.label"
            :value="item.value"
          />
        </ElSelect>
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

.full-width {
  width: 100%;
}

.mb-16px {
  margin-bottom: 16px;
}
</style>
