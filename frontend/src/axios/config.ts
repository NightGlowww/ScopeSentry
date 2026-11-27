import { AxiosResponse, AxiosRequestHeaders, InternalAxiosRequestConfig } from './types'
import { ElMessage } from 'element-plus'
import qs from 'qs'
import { translateApiMessage } from './translateMessage'
import { SUCCESS_CODE } from '@/constants'
import { useUserStoreWithOut } from '@/store/modules/user'
import { useLocaleStoreWithOut } from '@/store/modules/locale'

// 把当前界面语言带给后端：后端 response.message 走 go-i18n（zh-CN / en-US 两套），
// 不带 Accept-Language 时永远回中文 —— 英文界面里就会冒出中文提示（2026-11-28 实测）。
// 后端只认 zh / en 两族标签，ko 走英文回退，比中文更接近用户预期。
const backendLanguage = (lang: string): string => {
  if (lang === 'zh-CN') return 'zh-CN'
  return 'en-US'
}

const defaultRequestInterceptors = (config: InternalAxiosRequestConfig) => {
  try {
    const lang = useLocaleStoreWithOut().getCurrentLocale?.lang
    if (lang) {
      const headers = config.headers as AxiosRequestHeaders
      headers['Accept-Language'] = backendLanguage(lang)
    }
  } catch {
    // store 还没初始化（登录页极早期）时忽略：后端会按默认中文处理
  }
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
    // data 里是后端给的具体原因（如「节点内存已用 95%…」），message 只是 i18n 键；
    // 只显示 message 会让所有可预期的拒绝都成一个「api.error」，用户没法排查（2026-11-20）。
    const detail = typeof response?.data?.data === 'string' ? response.data.data.trim() : ''
    ElMessage.error(detail || translateApiMessage(response?.data?.message))
    if (response?.data?.code === 401) {
      const userStore = useUserStoreWithOut()
      userStore.logout()
    }
    return response.data
  }
}

export { defaultResponseInterceptors, defaultRequestInterceptors }
