<script setup lang="ts">
import { useI18n } from '@/hooks/web/useI18n'
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Dialog } from '@/components/Dialog'
import { Form } from '@/components/Form'
import { useForm } from '@/hooks/web/useForm'
import { reactive, computed } from 'vue'
import { useValidator } from '@/hooks/web/useValidator'
import { FormSchema } from '@/components/Form'
import { useDesign } from '@/hooks/web/useDesign'
import { changePasswordApi } from '@/api/login'
import { useUserStore } from '@/store/modules/user'

const userStore = useUserStore()
const { getPrefixCls } = useDesign()
const prefixCls = getPrefixCls('lock-dialog')
const { required } = useValidator()

const { t } = useI18n()

const props = defineProps({
  modelValue: {
    type: Boolean
  }
})

const emit = defineEmits(['update:modelValue'])

const dialogVisible = computed({
  get: () => props.modelValue,
  set: (val) => {
    emit('update:modelValue', val)
  }
})

const dialogTitle = ref(t('common.changePassword'))

const rules = reactive({
  newPassword: [required()]
})

const schema: FormSchema[] = reactive([
  {
    label: t('common.newPassword'),
    field: 'newPassword',
    component: 'Input',
    componentProps: {
      type: 'password'
    }
  }
])

const { formRegister, formMethods } = useForm()

const { getFormData, getElFormExpose } = formMethods

export interface changePassword {
  newPassword: string
}
const changePassword = async () => {
  const formExpose = await getElFormExpose()
  formExpose?.validate(async (valid) => {
    if (valid) {
      dialogVisible.value = false
      const formData = await getFormData()
      // 不要打印 formData：里面有明文新密码，会留在浏览器控制台/录屏里
      const changePasswordData: changePassword = {
        newPassword: formData.newPassword
      }
      const res = await changePasswordApi(changePasswordData)
      if (res.code == 200) {
        // 后端这个接口回的是 data: null，读 res.data.message 会直接抛 TypeError，
        // 于是「改成功了但没有任何提示」。改成本地文案，不再依赖响应体形状。
        ElMessage.success(t('common.passwordChanged'))
      }
    }
  })
}
const userInfo = userStore.getUserInfo
const username = userInfo!.username
</script>

<template>
  <Dialog
    v-model="dialogVisible"
    width="500px"
    max-height="170px"
    :class="prefixCls"
    :title="dialogTitle"
  >
    <div class="flex flex-col items-center">
      <img src="@/assets/imgs/avatar.jpg" alt="" class="w-70px h-70px rounded-[50%]" />
      <span class="text-14px my-10px text-[var(--top-header-text-color)]"> {{ username }} </span>
    </div>
    <Form :is-col="false" :schema="schema" :rules="rules" @register="formRegister" />
    <template #footer>
      <BaseButton type="primary" @click="changePassword">{{ t('common.submit') }}</BaseButton>
    </template>
  </Dialog>
</template>

<style lang="less" scoped>
:global(.v-lock-dialog) {
  @media (width <= 767px) {
    max-width: calc(100vw - 16px);
  }
}
</style>
