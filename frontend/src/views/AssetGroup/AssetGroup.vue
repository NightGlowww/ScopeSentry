<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElCol, ElRow } from 'element-plus'
import { useI18n } from '@/hooks/web/useI18n'
import {
  UNASSIGNED_GROUP_ID,
  getAssetGroupListApi,
  getAssetScopeListApi,
  reorderAssetGroupApi
} from '@/api/assetGroup'
import { AssetGroup } from '@/api/assetGroup/types'
import GroupSidebar from './components/GroupSidebar.vue'
import GroupFormDialog from './components/GroupFormDialog.vue'
import GroupDeleteDialog from './components/GroupDeleteDialog.vue'
import ScopeTable from './components/ScopeTable.vue'

const { t } = useI18n()

const groups = ref<AssetGroup[]>([])
const activeGroupId = ref(UNASSIGNED_GROUP_ID)
const unassignedCount = ref(0)

const groupFormVisible = ref(false)
const editingGroup = ref<AssetGroup | null>(null)
const deleteDialogVisible = ref(false)
const deletingGroup = ref<AssetGroup | null>(null)

const activeGroupName = computed(() => {
  if (activeGroupId.value === UNASSIGNED_GROUP_ID) return t('assetGroup.unassigned')
  return groups.value.find((group) => group.id === activeGroupId.value)?.name ?? ''
})

const loadGroups = async () => {
  try {
    const res = await getAssetGroupListApi()
    groups.value = res?.data?.items ?? []
  } catch {
    groups.value = []
  }
  // 当前选中的集团可能刚被删除，回落到「未分组」而不是停在空白视图
  const stillExists = groups.value.some((group) => group.id === activeGroupId.value)
  if (activeGroupId.value !== UNASSIGNED_GROUP_ID && !stillExists) {
    activeGroupId.value = UNASSIGNED_GROUP_ID
  }
}

// 未分组不在集团列表里，单独取一次总数给左侧固定项显示角标
const loadUnassignedCount = async () => {
  try {
    const res = await getAssetScopeListApi({
      groupId: UNASSIGNED_GROUP_ID,
      pageIndex: 1,
      pageSize: 1
    })
    unassignedCount.value = res?.data?.total ?? 0
  } catch {
    unassignedCount.value = 0
  }
}

const handleSelect = (id: string) => {
  activeGroupId.value = id
}

const handleCreate = () => {
  editingGroup.value = null
  groupFormVisible.value = true
}

const handleEdit = (group: AssetGroup) => {
  editingGroup.value = group
  groupFormVisible.value = true
}

const handleDelete = (group: AssetGroup) => {
  deletingGroup.value = group
  deleteDialogVisible.value = true
}

// 上移/下移先本地换位再提交接口，失败时重新拉取列表把顺序纠正回来
const handleReorder = async (orderIds: string[]) => {
  const ordered = orderIds
    .map((id) => groups.value.find((group) => group.id === id))
    .filter((group): group is AssetGroup => Boolean(group))
  if (ordered.length === groups.value.length) {
    groups.value = ordered
  }
  try {
    await reorderAssetGroupApi(orderIds)
  } catch {
    await loadGroups()
  }
}

const handleGroupSaved = async (id?: string) => {
  await loadGroups()
  if (id) activeGroupId.value = id
}

const handleGroupDeleted = async () => {
  deletingGroup.value = null
  await loadGroups()
  await loadUnassignedCount()
}

// 地契增删改或移动都会影响两侧的 scopeCount，统一在这里重算
const handleScopeChanged = async () => {
  await loadGroups()
  await loadUnassignedCount()
}

onMounted(() => {
  loadGroups()
  loadUnassignedCount()
})
</script>

<template>
  <ElRow :gutter="16">
    <ElCol :span="5">
      <GroupSidebar
        :groups="groups"
        :active-id="activeGroupId"
        :unassigned-count="unassignedCount"
        @select="handleSelect"
        @create="handleCreate"
        @edit="handleEdit"
        @delete="handleDelete"
        @reorder="handleReorder"
      />
    </ElCol>
    <ElCol :span="19">
      <ScopeTable
        :group-id="activeGroupId"
        :group-name="activeGroupName"
        :groups="groups"
        @changed="handleScopeChanged"
      />
    </ElCol>
  </ElRow>
  <GroupFormDialog v-model="groupFormVisible" :group="editingGroup" @saved="handleGroupSaved" />
  <GroupDeleteDialog
    v-model="deleteDialogVisible"
    :group="deletingGroup"
    :groups="groups"
    @deleted="handleGroupDeleted"
  />
</template>
