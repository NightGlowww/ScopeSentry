import { ElMessage } from 'element-plus'

/**
 * 统一的复制到剪贴板：成功/失败都给 toast，避免各页面各写一遍。
 */
export const copyText = async (
  text?: string,
  okMsg = '已复制',
  failMsg = '复制失败'
): Promise<void> => {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(okMsg)
  } catch {
    ElMessage.error(failMsg)
  }
}
