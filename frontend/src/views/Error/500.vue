<script setup lang="ts">
import { Error } from '@/components/Error'
import { usePermissionStore } from '@/store/modules/permission'
import { useRoute, useRouter } from 'vue-router'

const { push, back } = useRouter()
const route = useRoute()

const permissionStore = usePermissionStore()

const errorClick = () => {
  push(permissionStore.addRouters[0]?.path as string)
}
</script>

<template>
  <Error :type="'500'" :path="route.fullPath" @error-click="errorClick" @back-click="back" />
</template>