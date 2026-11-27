<script setup lang="ts">
import { confirmDialog } from '@/utils/confirm'
import { ElRow, ElCol, ElButton, ElMessage } from 'element-plus'
import { useI18n } from '@/hooks/web/useI18n'
import { ElCard } from 'element-plus'
import { ref, onBeforeMount } from 'vue'
import { Codemirror } from 'vue-codemirror'
import { javascript } from '@codemirror/lang-javascript'
import { oneDark } from '@codemirror/theme-one-dark'
import { getRadConfigurationApi, saveRadConfigurationApi } from '@/api/Configuration'
const { t } = useI18n()
const content = ref(``)
const extensions = [javascript(), oneDark]
onBeforeMount(async () => {
  try {
    const res = await getRadConfigurationApi()

    if (res.code === 200) {
      content.value = res.data.content
    } else {
      console.error(`API request failed with status code ${res.code}`)
    }
  } catch (error) {
    console.error('An error occurred while fetching the subfinder config:', error)
  }
})
const confirmAdd = async () => {
  const confirmed = await confirmDialog(t('localization.saveConfirm'))
  if (confirmed) {
    await save()
  }
}
const save = async () => {
  // 保存结果要有反馈；失败也复位 loading，否则按钮会一直转
  saveLoading.value = true
  try {
    const res = await saveRadConfigurationApi(content.value)
    if (res.code == 200) {
      ElMessage.success(t('common.success'))
    } else {
      ElMessage.error(res.message || t('common.operateFailed'))
    }
  } catch (e: any) {
    ElMessage.error(e?.message || t('common.operateFailed'))
  } finally {
    saveLoading.value = false
  }
}
const saveLoading = ref(false)
</script>

<template>
  <ElCard shadow="never" class="mb-20px">
    <template #header>
      <ElRow>
        <ElCol :span="3" style="height: 100%">
          <span>{{ t('rad.title') }}</span>
        </ElCol>
        <ElCol :span="2" :offset="19">
          <ElButton type="primary" @click="confirmAdd" :loading="saveLoading">{{
            t('common.save')
          }}</ElButton>
        </ElCol>
      </ElRow>
    </template>
    <codemirror
      v-model="content"
      :style="{ height: '800px' }"
      :autofocus="true"
      :indent-with-tab="true"
      :tab-size="2"
      :extensions="extensions"
    />
  </ElCard>
</template>

<style scoped>
.header-container {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100%;
}
</style>
