import { AxiosResponse, AxiosRequestHeaders, InternalAxiosRequestConfig } from './types'
import { ElMessage } from 'element-plus'
import qs from 'qs'
import { translateApiMessage } from './translateMessage'
import { SUCCESS_CODE } from '@/constants'
import { useUserStoreWithOut } from '@/store/modules/user'

const defaultRequestInterceptors = (config: InternalAxiosRequestConfig) => {
  if (
    config.method === 'post' &&
    (config.headers as AxiosRequestHeaders)['Content-Type'] === 'application/x-www-form-urlencoded'
  ) {
    config.data = qs.stringify(config.data)
  }
  if (config.method === 'get' && config.params) {
    let url = config.url as string
    url += '?'
    const keys = Object.keys(config.params)
    for (const key of keys) {
      if (config.params[key] !== void 0 && config.params[key] !== null) {
        url += `${key}=${encodeURIComponent(config.params[key])}&`
      }
    }
    url = url.substring(0, url.length - 1)
    config.params = {}
    config.url = url
  }
  return config
}

const defaultResponseInterceptors = (response: AxiosResponse) => {
  if (response?.headers['content-type'] == 'application/octet-stream') {
    return response
  }
  if (response?.config?.responseType === 'blob') {
    // 如果是文件流，直接过
    return response
  } else if (response.data.code === SUCCESS_CODE) {
    // 读请求不弹成功提示：像"版本信息获取成功"这种每次进页面都弹，属于噪音。
    // 需要时可在请求配置里显式传 showSuccessMessage: true 打开。
    const method = String(response?.config?.method || 'get').toLowerCase()
    const forceSuccessMessage = (response?.config as Recordable)?.showSuccessMessage === true
    // 列表查询在这个项目里走 POST（返回 { list, total }），同样不该弹"操作成功"
    const payload = response?.data?.data
    const looksLikeQuery =
      !!payload && typeof payload === 'object' && ('list' in payload || 'total' in payload)
    const isRead = method === 'get' || looksLikeQuery
    const rawMessage = String(response?.data?.message || '')
    // 通用成功文案（api.success / success / xx.success）不弹：登录、进页面、状态查询都会命中，
    // 属于纯噪音；只有业务自定义文案（如“导入成功”）或显式 showSuccessMessage 才提示。
    const isGenericSuccess =
      rawMessage === '' ||
      rawMessage === 'success' ||
      rawMessage === 'api.success' ||
      rawMessage.endsWith('.success')
    if (rawMessage && (forceSuccessMessage || (!isGenericSuccess && !isRead))) {
      ElMessage.success(translateApiMessage(rawMessage))
    }
    return response.data
  } else {
    ElMessage.error(translateApiMessage(response?.data?.message))
    if (response?.data?.code === 401) {
      const userStore = useUserStoreWithOut()
      userStore.logout()
    }
    return response.data
  }
}

export { defaultResponseInterceptors, defaultRequestInterceptors }
