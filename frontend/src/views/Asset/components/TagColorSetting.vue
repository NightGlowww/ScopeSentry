<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElButton, ElColorPicker, ElDialog, ElInput, ElMessage, ElTag } from 'element-plus'
import { useI18n } from '@/hooks/web/useI18n'
import { getTagColorsApi, setTagColorApi } from '@/api/asset'
import { tagColorProps } from '@/utils/tagColor'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved'): void
}>()

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const overrides = ref<Record<string, string>>({})
const newTag = ref('')
const newColor = ref('primary')
const customColor = ref('')

const colorOptions = [
  { value: 'primary', labelKey: 'asset.colorPrimary' },
  { value: 'success', labelKey: 'asset.colorSuccess' },
  { value: 'info', labelKey: 'asset.colorInfo' },
  { value: 'warning', labelKey: 'asset.colorWarning' },
  { value: 'danger', labelKey: 'asset.colorDanger' }
]

const load = async () => {
  loading.value = true
  try {
    const res = await getTagColorsApi()
    overrides.value = res.data?.colors ?? {}
  } catch {
    overrides.value = {}
  } finally {
    loading.value = false
  }
}

// 每次打开重新拉一次，避免别处改过之后看到旧数据
watch(
  () => props.modelValue,
  (visible) => {
    if (visible) {
      newTag.value = ''
      customColor.value = ''
      newColor.value = 'primary'
      load()
    }
  }
)

const save = async () => {
  const tag = newTag.value.trim()
  if (!tag) {
    ElMessage.warning(t('asset.tagColorAddTag'))
    return
  }
  saving.value = true
  try {
    await setTagColorApi(tag, customColor.value || newColor.value)
    ElMessage.success(t('asset.tagColorSaved'))
    newTag.value = ''
    customColor.value = ''
    await load()
    emit('saved')
  } catch {
    ElMessage.error(t('asset.tagColorSaveFailed'))
  } finally {
    saving.value = false
  }
}

const selectColor = (value: string) => {
  newColor.value = value
  customColor.value = ''
}

const clear = async (tag: string) => {
  try {
    await setTagColorApi(tag, '')
    await load()
    emit('saved')
  } catch {
    ElMessage.error(t('asset.tagColorSaveFailed'))
  }
}
</script>

<template>
  <ElDialog
    :model-value="modelValue"
    @update:model-value="emit('update:modelValue', $event)"
    :title="t('asset.tagColorTitle')"
    width="560px"
  >
    <div v-loading="loading">
      <div v-if="Object.keys(overrides).length === 0" class="tag-color-empty">
        {{ t('asset.tagColorEmpty') }}
      </div>
      <div v-for="(color, tag) in overrides" :key="tag" class="tag-color-row">
        <div class="tag-color-row__main">
          <ElTag v-bind="tagColorProps(tag, overrides)" effect="light">{{ tag }}</ElTag>
          <span class="tag-color-row__value">{{ color }}</span>
        </div>
        <ElButton link type="danger" @click="clear(tag)">
          {{ t('asset.tagColorClear') }}
        </ElButton>
      </div>

      <div class="tag-color-editor">
        <div class="tag-color-editor__row">
          <ElInput
            v-model="newTag"
            :placeholder="t('asset.tagColorAddTag')"
            maxlength="64"
            style="width: 200px"
          />
          <ElTag
            v-for="option in colorOptions"
            :key="option.value"
            :type="option.value"
            effect="light"
            :class="{ 'is-active': newColor === option.value && !customColor }"
            @click="selectColor(option.value)"
          >
            {{ t(option.labelKey) }}
          </ElTag>
        </div>
        <div class="tag-color-editor__row">
          <span class="tag-color-editor__label">{{ t('asset.tagColorCustom') }}</span>
          <ElColorPicker v-model="customColor" />
          <ElButton type="primary" :loading="saving" @click="save">
            {{ t('asset.tagColorSave') }}
          </ElButton>
        </div>
      </div>
    </div>
  </ElDialog>
</template>

<style scoped>
.tag-color-empty {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin-bottom: 12px;
}
.tag-color-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 0;
}
.tag-color-row__main {
  display: flex;
  align-items: center;
  gap: 8px;
}
.tag-color-row__value {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.tag-color-editor {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.tag-color-editor__row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 10px;
}
.tag-color-editor__label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.is-active {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 1px;
}
</style>
