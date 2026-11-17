<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElOption, ElOptionGroup, ElSelect } from 'element-plus'
import { getProjectAllApi } from '@/api/project'
import { useI18n } from '@/hooks/web/useI18n'

// 项目筛选器：三个读模型都按「项目名」精确过滤（后端 filter 是 {project: name}），
// 而 /api/project/all 的 children.value 是项目 ID，所以这里把 label（项目名）当值用。
interface ProjectOption {
  label: string
  value: string
}

interface ProjectGroup {
  label: string
  value: string
  children?: ProjectOption[]
}

const props = withDefaults(defineProps<{ modelValue: string; placeholder?: string }>(), {
  placeholder: ''
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const { t } = useI18n()
const groups = ref<ProjectGroup[]>([])
const loading = ref(false)

const selected = computed({
  get: () => props.modelValue,
  set: (value: string) => emit('update:modelValue', value)
})

const load = async () => {
  loading.value = true
  try {
    const res = await getProjectAllApi()
    groups.value = (res?.data?.list ?? []) as ProjectGroup[]
  } catch {
    groups.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <ElSelect
    v-model="selected"
    :placeholder="placeholder || t('common.selectText')"
    :loading="loading"
    clearable
    filterable
    class="project-select"
  >
    <ElOptionGroup v-for="group in groups" :key="group.label" :label="group.label">
      <ElOption
        v-for="item in group.children ?? []"
        :key="item.value"
        :label="item.label"
        :value="item.label"
      />
    </ElOptionGroup>
  </ElSelect>
</template>

<style scoped>
.project-select {
  width: 180px;
}
</style>
