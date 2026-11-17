<script setup lang="ts">
import { ElButton, ElCard, ElScrollbar, ElTag, ElTooltip } from 'element-plus'
import { Icon } from '@/components/Icon'
import { useI18n } from '@/hooks/web/useI18n'
import { UNASSIGNED_GROUP_ID } from '@/api/assetGroup'
import { AssetGroup } from '@/api/assetGroup/types'

const { t } = useI18n()

const props = defineProps<{
  groups: AssetGroup[]
  activeId: string
  unassignedCount: number
}>()

const emit = defineEmits<{
  (event: 'select', id: string): void
  (event: 'create'): void
  (event: 'edit', group: AssetGroup): void
  (event: 'delete', group: AssetGroup): void
  (event: 'reorder', orderIds: string[]): void
}>()

// 上移/下移只交换相邻两项，然后把完整顺序交给后端 reorder 接口，
// 避免只提交两个 id 让后端猜测意图
const move = (index: number, offset: number) => {
  const target = index + offset
  if (target < 0 || target >= props.groups.length) return
  const orderIds = props.groups.map((group) => group.id)
  const current = orderIds[index]
  orderIds[index] = orderIds[target]
  orderIds[target] = current
  emit('reorder', orderIds)
}
</script>

<template>
  <ElCard shadow="never" class="group-sidebar">
    <template #header>
      <div class="group-sidebar__header">
        <span class="group-sidebar__title">{{ t('assetGroup.groupTitle') }}</span>
        <ElButton type="primary" link @click="emit('create')">
          {{ t('assetGroup.createGroup') }}
        </ElButton>
      </div>
    </template>
    <ElScrollbar class="group-sidebar__scroll">
      <div
        class="group-item group-item--fixed"
        :class="{ 'group-item--active': activeId === UNASSIGNED_GROUP_ID }"
        @click="emit('select', UNASSIGNED_GROUP_ID)"
      >
        <span class="group-item__name">{{ t('assetGroup.unassigned') }}</span>
        <ElTag size="small" type="info" effect="plain">{{ unassignedCount }}</ElTag>
      </div>
      <div
        v-for="(group, index) in groups"
        :key="group.id"
        class="group-item"
        :class="{ 'group-item--active': activeId === group.id }"
        @click="emit('select', group.id)"
      >
        <div class="group-item__main">
          <span class="group-item__name" :title="group.description || group.name">
            {{ group.name }}
          </span>
          <ElTag size="small" effect="plain">{{ group.scopeCount }}</ElTag>
        </div>
        <div class="group-item__actions" @click.stop>
          <ElTooltip :content="t('assetGroup.moveUp')" placement="top" :show-after="400">
            <Icon
              class="group-item__action"
              icon="material-symbols:arrow-upward"
              :size="14"
              @click="move(index, -1)"
            />
          </ElTooltip>
          <ElTooltip :content="t('assetGroup.moveDown')" placement="top" :show-after="400">
            <Icon
              class="group-item__action"
              icon="material-symbols:arrow-downward"
              :size="14"
              @click="move(index, 1)"
            />
          </ElTooltip>
          <ElTooltip :content="t('assetGroup.editGroup')" placement="top" :show-after="400">
            <Icon
              class="group-item__action"
              icon="uil:edit"
              :size="14"
              @click="emit('edit', group)"
            />
          </ElTooltip>
          <ElTooltip :content="t('assetGroup.deleteGroup')" placement="top" :show-after="400">
            <Icon
              class="group-item__action group-item__action--danger"
              icon="material-symbols:delete-outline"
              :size="14"
              @click="emit('delete', group)"
            />
          </ElTooltip>
        </div>
      </div>
      <div v-if="!groups.length" class="group-sidebar__empty">{{ t('assetGroup.noGroup') }}</div>
    </ElScrollbar>
  </ElCard>
</template>

<style lang="less" scoped>
.group-sidebar {
  height: 100%;
}

.group-sidebar__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.group-sidebar__title {
  font-size: 14px;
  font-weight: 600;
}

.group-sidebar__scroll {
  max-height: calc(100vh - 260px);
}

.group-sidebar__empty {
  padding: 12px 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  text-align: center;
}

.group-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  padding: 8px 10px;
  margin-bottom: 4px;
  color: var(--el-text-color-regular);
  cursor: pointer;
  border-radius: 8px;
  transition:
    background-color 0.16s ease,
    color 0.16s ease;
}

.group-item:hover {
  background-color: var(--el-fill-color-light);
}

.group-item--active {
  font-weight: 600;
  color: var(--app-primary);
  background-color: var(--app-primary-soft);
}

.group-item--active:hover {
  background-color: var(--app-primary-soft);
}

.group-item__main {
  display: flex;
  flex: 1;
  gap: 6px;
  align-items: center;
  min-width: 0;
}

.group-item__name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-item__actions {
  display: flex;
  gap: 4px;
  align-items: center;
  opacity: 0;
  transition: opacity 0.16s ease;
}

.group-item:hover .group-item__actions {
  opacity: 1;
}

.group-item__action {
  color: var(--el-text-color-secondary);
  cursor: pointer;
}

.group-item__action:hover {
  color: var(--app-primary);
}

.group-item__action--danger:hover {
  color: var(--el-color-danger);
}
</style>
