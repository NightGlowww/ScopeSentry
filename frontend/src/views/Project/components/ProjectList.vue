<script setup lang="ts">
import { Table } from '@/components/Table'
import { h, ref, watch } from 'vue'
import {
  ElAvatar,
  ElDropdown,
  ElDropdownMenu,
  ElDropdownItem,
  ElPagination,
  ElCheckbox,
  ElSwitch,
  ElCheckboxGroup
} from 'element-plus'
import { useI18n } from '@/hooks/web/useI18n'
import { Dialog } from '@/components/Dialog'
import { defineProps } from 'vue'
import { ElMessageBox } from 'element-plus'
import { deleteProjectApi } from '@/api/project'
import AddProject from './AddProject.vue'
import { useIcon } from '@/hooks/web/useIcon'
import { useRouter } from 'vue-router'
const { t } = useI18n()
const { push } = useRouter()
interface Recordable {
  id: string
  name: string
  logo: string
  AssetCount: number
  tag: string
}
const props = defineProps({
  tableDataList: {
    type: Array as () => Recordable[],
    default: () => []
  },
  getProjectTag: {
    type: Function as unknown as () => (pageIndex: number, pageSize: number) => Promise<void>,
    required: true
  },
  total: {
    type: Number,
    default: 0
  },
  multipleSelection: {
    type: Boolean
  },
  selectedRows: {
    type: Array as () => (string | number)[],
    default: () => []
  }
})
const loading = ref(false)
let ProjectId = ''
const dialogVisible = ref(false)
const closeDialog = () => {
  dialogVisible.value = false
}
const edit = async (id: string) => {
  ProjectId = id
  dialogVisible.value = true
}
const del = (id: string) => {
  // ElMessageBox.alert('Are you sure you want to delete the selected data?', '', {
  //   confirmButtonText: 'YES',
  //   callback: async () => {
  //     await deleteProjectApi([id])
  //     props.getProjectTag(currentPage.value, pageSize.value)
  //   }
  // })
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
  }).then(async () => {
    await deleteProjectApi([id], deleteAsset.value)
  })
}
const editIcon = useIcon({ icon: 'uil:edit' })
const delIcon = useIcon({ icon: 'material-symbols:delete-outline' })
const data = useIcon({ icon: 'carbon:data-vis-1' })
const handleCommand = (command: string | number | object) => {
  if (command['type'] == 'edit') {
    edit(command['id'])
  } else if (command['type'] == 'del') {
    del(command['id'])
  } else {
    action(command['id'])
  }
}
const handlePageChange = () => {
  props.getProjectTag(currentPage.value, pageSize.value)
}
const currentPage = ref(1)
const pageSize = ref(50)
const small = ref(false)
const background = ref(false)
const disabled = ref(false)
const emit = defineEmits<{
  (event: 'update:selectedRows', value: (string | number)[]): void
}>()

const localSelectedRows = ref([...props.selectedRows])

watch(localSelectedRows, (newVal) => {
  if (JSON.stringify(newVal) !== JSON.stringify(props.selectedRows)) {
    emit('update:selectedRows', newVal)
  }
  isAllSelected.value = newVal.length === props.tableDataList.length
})

watch(
  () => props.selectedRows,
  (newVal) => {
    localSelectedRows.value = [...newVal]
  }
)
watch(
  () => props.tableDataList,
  (newVal) => {
    isAllSelected.value = localSelectedRows.value.length === newVal.length
  }
)
const isAllSelected = ref(false)
const toggleAllSelection = () => {
  if (isAllSelected.value) {
    localSelectedRows.value = props.tableDataList.map((item) => item.id)
  } else {
    localSelectedRows.value = []
  }
}
// 卡片上「最近一次扫描」的状态标签：和后端 task.status 编码对齐
// 0 待开始 1 运行中 2 暂停 3 完成 4 失败
const statusText = (s: number) => {
  const map: Record<number, string> = {
    0: t('task.waitStart'),
    1: t('task.running'),
    2: t('task.stop'),
    3: t('task.finish'),
    4: t('task.failed')
  }
  return map[Number(s)] ?? '-'
}
const statusType = (s: number): any => {
  const map: Record<number, string> = {
    0: 'primary',
    1: 'info',
    2: 'warning',
    3: 'success',
    4: 'danger'
  }
  return map[Number(s)] ?? 'default'
}

const action = (id: string) => {
  push(`/project-management/project-detail?id=${id}`)
}
</script>

<template>
  <ElCheckbox v-if="multipleSelection" v-model="isAllSelected" @change="toggleAllSelection">
    {{ t('common.selectAll') }}
  </ElCheckbox>
  <ElCheckboxGroup v-model="localSelectedRows">
    <Table
      :columns="[]"
      :data="tableDataList"
      :loading="loading"
      custom-content
      :card-wrap-style="{
        // 210px 装不下「名称 + 总资产 + 任务数 + 最近扫描 + 状态标签」：
        // 名称会折行、标签被挤到单独一行（2026-11-27 截图复现）
        width: '260px',
        marginBottom: '20px',
        marginRight: '20px'
      }"
    >
      <template #content="row">
        <ElDropdown trigger="contextmenu" @command="handleCommand">
          <!-- 卡片一直是 cursor-pointer 却没有 @click：左键点下去什么都不发生，
               只有右键才出菜单，用户视角就是「项目管理点不进去、没有功能」（2026-11-27 反馈） -->
          <div class="flex cursor-pointer" @click="action(row.id)">
            <ElCheckbox :value="row.id" class="pr-16px" v-if="multipleSelection" @click.stop />
            <div class="pr-16px">
              <template v-if="row.logo != ''">
                <ElAvatar :src="row.logo" class="avatar" fit="cover" />
              </template>
              <template v-else>
                <ElAvatar class="avatar avatar-placeholder"> {{ row.name.charAt(0) }} </ElAvatar>
              </template>
            </div>
            <div class="card-text">
              <div class="name">{{ row.name }}</div>
              <div class="assets-info">{{ t('project.totalAssets') }} : {{ row.AssetCount }}</div>
              <!-- 任务维度：只给「资产数」看不出这个项目最近有没有在干活（2026-11-27 反馈） -->
              <div class="assets-info">
                {{ t('project.taskCount') }} : {{ row.taskCount ?? 0 }}
                <span v-if="row.runningTasks > 0" class="running-dot">
                  · {{ t('task.running') }} {{ row.runningTasks }}
                </span>
              </div>
              <div v-if="row.lastTaskAt" class="assets-info last-task">
                {{ t('project.lastScan') }} : {{ String(row.lastTaskAt).slice(5, 16) }}
                <ElTag size="small" :type="statusType(row.lastTaskStatus)" effect="plain">
                  {{ statusText(row.lastTaskStatus) }}
                </ElTag>
              </div>
            </div>
          </div>
          <template #dropdown>
            <ElDropdownMenu>
              <ElDropdownItem :icon="editIcon" :command="{ type: 'edit', id: row.id }">{{
                t('common.edit')
              }}</ElDropdownItem>
              <ElDropdownItem :icon="delIcon" :command="{ type: 'del', id: row.id }">{{
                t('common.delete')
              }}</ElDropdownItem>
              <ElDropdownItem :icon="data" :command="{ type: 'aggregation', id: row.id }">{{
                t('project.aggregation')
              }}</ElDropdownItem>
              <ElDropdownItem :command="{ type: 'open', id: row.id }">{{
                t('common.view')
              }}</ElDropdownItem>
            </ElDropdownMenu>
          </template>
        </ElDropdown>
      </template>
    </Table>
  </ElCheckboxGroup>
  <ElPagination
    v-model:current-page="currentPage"
    v-model:page-size="pageSize"
    :page-sizes="[50, 70, 100, 200, 400]"
    :small="small"
    :disabled="disabled"
    :background="background"
    layout="total, sizes, prev, pager, next, jumper"
    :total="total"
    @size-change="handlePageChange"
    @current-change="handlePageChange"
  />
  <Dialog
    v-model="dialogVisible"
    :title="t('common.edit')"
    center
    style="border-radius: 15px; box-shadow: 5px 5px 10px rgba(0, 0, 0, 0.3)"
  >
    <AddProject
      :closeDialog="closeDialog"
      :projectid="ProjectId"
      :getProjectData="$props.getProjectTag"
      :schedule="false"
    />
  </Dialog>
</template>
<style>
.avatar {
  width: 45px;
  height: 45px;
  line-height: 45px;
  font-size: 24px;
}

.avatar-placeholder {
  background-color: cornflowerblue;
}
.demo-tabs > .el-tabs__content {
  padding: 32px;
  color: #6b778c;
  font-size: 32px;
  font-weight: 600;
}
/* 文本块要能收缩：flex 子项默认 min-width:auto，长项目名会把整行撑出去 */
.card-text {
  min-width: 0;
  flex: 1 1 auto;
}

.name {
  margin-bottom: 6px;
  font-weight: 700;
  font-size: 16px;
  /* artex-own-infra 这种长名字在 210px 里会折成两行，改成省略号 */
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.assets-info {
  color: #b1b3b8;
  font-size: 11px;
  line-height: 1.6;
}

/* 最近扫描：时间与状态标签必须同一行，否则标签会掉到下一行显得像两条信息 */
.last-task {
  display: flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}
</style>
