<script setup lang="tsx">
import { ContentWrap } from '@/components/ContentWrap'
import {
  ElButton,
  ElTabPane,
  ElTabs,
  ElInput,
  ElSwitch,
  ElDropdown,
  ElDropdownMenu,
  ElDropdownItem,
  ElIcon,
  ElMessage,
  ElMessageBox
} from 'element-plus'
import { ArrowDown, Delete, Search } from '@element-plus/icons-vue'
import ProjectList from './components/ProjectList.vue'
import AddProject from './components/AddProject.vue'
import { useI18n } from '@/hooks/web/useI18n'
import { h, reactive, ref } from 'vue'
import { Dialog } from '@/components/Dialog'
import { deleteProjectApi, getProjectDataApi } from '@/api/project'
const { t } = useI18n()
let allProjectData = reactive({})
let tabNames = ref<string[]>([])
let tagNum = reactive({})
const projectListLoading = ref(false)
const getProjectTag = async (pageIndex: number, pageSize: number) => {
  if (pageIndex === 0) {
    pageIndex = currentPage.value
    pageSize = currentpageSize.value
  } else {
    currentPage.value = pageIndex
    currentpageSize.value = pageSize
  }
  try {
    const res = await getProjectDataApi(search.value, pageIndex, pageSize)
    // 更新响应式对象
    Object.assign(allProjectData, res.data.result)
    tabNames.value = Object.keys(res.data.tag)
    Object.assign(tagNum, res.data.tag)
    const index = tabNames.value.indexOf('All')
    if (index !== -1) {
      tabNames.value.splice(index, 1)
    }
  } catch (error) {
    console.error('An error occurred:', error)
  }
}
const dialogVisible = ref(false)
const addProject = async () => {
  dialogVisible.value = true
}
const closeDialog = () => {
  dialogVisible.value = false
}
const search = ref('')
const currentPage = ref(1)
const currentpageSize = ref(50)

const loading = ref(false)
const handleSearch = async () => {
  // 取标签失败时原来两个 loading 都停在 true：整页遮罩不消失，也没法再搜
  loading.value = true
  projectListLoading.value = true
  try {
    await getProjectTag(currentPage.value, currentpageSize.value)
  } finally {
    loading.value = false
    projectListLoading.value = false
  }
}
handleSearch()
const multipleSelection = ref(false)
const delSelect = async () => {
  const deleteAsset = ref<boolean>(false)
  ElMessageBox({
    showCancelButton: true,
    cancelButtonText: t('common.cancel'),
    title: t('common.delete'),
    draggable: true,
    // Should pass a function if VNode contains dynamic props
    message: () =>
      h('div', { style: { display: 'flex', alignItems: 'center' } }, [
        h('p', { style: { margin: '0 10px 0 0' } }, t('task.delAsset')),
        h(ElSwitch, {
          modelValue: deleteAsset.value,
          'onUpdate:modelValue': (val: boolean) => {
            deleteAsset.value = val
          }
        })
      ])
  })
    .then(async () => {
      await deleteProjectApi(selectedRowIds.value, deleteAsset.value)
      ElMessage.success(t('common.success'))
      getProjectTag(currentPage.value, currentpageSize.value)
    })
    .catch(() => {
      // 取消删除也会走到这里（ElMessageBox 以 reject 表示取消），不需要提示
    })
}
const selectedRowIds = ref([])
</script>

<template>
  <ContentWrap>
    <!-- 2026-11-18 布局事故：搜索按钮只有图标、还带 size="large" + height:100%，
         渲染成一个和输入框不等高的蓝色药丸；"多选"裹在 ElFormItem 里带来表单默认
         间隙；标签列用了非法 span=0.5。这里统一成一个工具栏：左搜索、右操作。 -->
    <div class="project-toolbar">
      <div class="project-toolbar__search">
        <span class="project-toolbar__label">{{ t('common.search') }}:</span>
        <ElInput
          v-model="search"
          class="project-toolbar__input"
          :placeholder="t('common.inputText')"
          clearable
          @keyup.enter="handleSearch"
        />
        <ElButton :icon="Search" :loading="loading" @click="handleSearch">
          {{ t('common.search') }}
        </ElButton>
      </div>
      <div class="project-toolbar__actions">
        <ElButton type="primary" @click="addProject">{{ t('project.addProject') }}</ElButton>
        <span class="project-toolbar__switch">
          <span>{{ t('common.multipleSelection') }}</span>
          <ElSwitch v-model="multipleSelection" />
        </span>
        <ElDropdown v-if="multipleSelection" trigger="click">
          <ElButton plain>
            {{ t('common.operation') }}
            <ElIcon class="el-icon--right"><ArrowDown /></ElIcon>
          </ElButton>
          <template #dropdown>
            <ElDropdownMenu>
              <ElDropdownItem :icon="Delete" @click="delSelect">{{
                t('common.delete')
              }}</ElDropdownItem>
            </ElDropdownMenu>
          </template>
        </ElDropdown>
      </div>
    </div>
    <ElTabs v-loading="projectListLoading">
      <ElTabPane :label="`${t('common.all')} (${tagNum['All']})`"
        ><ProjectList
          :tableDataList="allProjectData['All']"
          :getProjectTag="getProjectTag"
          :total="tagNum['All']"
          :multipleSelection="multipleSelection"
          v-model:selectedRows="selectedRowIds"
      /></ElTabPane>
      <ElTabPane
        v-for="tagName in tabNames"
        :label="`${tagName} (${tagNum[tagName]})`"
        :key="tagName"
        ><ProjectList
          :tableDataList="allProjectData[tagName]"
          :getProjectTag="getProjectTag"
          :total="tagNum[tagName]"
          :multipleSelection="multipleSelection"
          v-model:selectedRows="selectedRowIds"
      /></ElTabPane>
    </ElTabs>
  </ContentWrap>
  <Dialog
    v-model="dialogVisible"
    :title="t('project.addProject')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
  >
    <AddProject
      :closeDialog="closeDialog"
      projectid=""
      :getProjectData="getProjectTag"
      :schedule="false"
    />
  </Dialog>
</template>

<style scoped>
.project-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
}
.project-toolbar__search {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.project-toolbar__label {
  color: var(--el-text-color-regular);
  font-size: 14px;
  white-space: nowrap;
}
.project-toolbar__input {
  width: 260px;
}
.project-toolbar__actions {
  display: flex;
  align-items: center;
  gap: 12px;
}
.project-toolbar__switch {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--el-text-color-regular);
  font-size: 13px;
}
</style>
