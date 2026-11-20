import { computed, type Ref } from 'vue'
import { useI18n } from '@/hooks/web/useI18n'

/**
 * 把资产页的搜索表达式翻译成人话，用于空状态提示。
 * 例：task=="x" → 当前筛选（任务：x）没有结果
 *      domain=="x" → 当前筛选（主机：x）没有结果
 * 无筛选时回退到默认文案（"暂无数据"）。
 */
export const useEmptyHint = (searchParams: Ref<string>) => {
  const { t } = useI18n()
  return computed(() => {
    const s = (searchParams.value || '').trim()
    const task = /task\s*==\s*"([^"]+)"/.exec(s)
    const domain = /domain\s*==\s*"([^"]+)"/.exec(s)
    if (task) return t('smartEmpty.task', { v: task[1] })
    if (domain) return t('smartEmpty.domain', { v: domain[1] })
    if (s) return t('smartEmpty.filtered')
    return t('common.noData')
  })
}
