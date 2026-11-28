import { ElMessageBox } from 'element-plus'
import { i18n } from '@/plugins/vueI18n'

/**
 * 统一的确认弹框：替代 window.confirm。
 * 原生 confirm 在 Electron/浏览器里样式不可控（"127.0.0.1:8082 says ..."），
 * 与整体 UI 完全不一致；这里统一走 Element Plus 的 MessageBox。
 */
export const confirmDialog = async (message: string, title?: string): Promise<boolean> => {
  const tr = (key: string, fallback: string): string => {
    try {
      const resolve = (i18n?.global as unknown as { t?: (k: string) => string })?.t
      const v = typeof resolve === 'function' ? resolve(key) : key
      return typeof v === 'string' && v && v !== key ? v : fallback
    } catch {
      return fallback
    }
  }
  try {
    await ElMessageBox.confirm(message, title ?? tr('common.reminder', '提示'), {
      confirmButtonText: tr('common.ok', '确定'),
      cancelButtonText: tr('common.cancel', '取消'),
      type: 'warning',
      draggable: true,
      closeOnClickModal: false
    })
    return true
  } catch {
    return false
  }
}
