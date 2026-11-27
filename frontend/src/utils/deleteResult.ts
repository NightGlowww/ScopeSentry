import { useI18n } from '@/hooks/web/useI18n'

// 删除类接口现在回 { deleted, requested }（2026-11-28 之后的后端契约）。
// 统一在这里生成提示：删了几条、有几条是别人已经删掉的，
// 避免每个页面各写一套、也避免「删除成功」这种什么都说明不了的话。
export function deleteResultMessage(res: any, fallbackCount?: number): string {
  const { t } = useI18n()
  const data = res?.data ?? res ?? {}
  const deleted = Number(data.deleted ?? fallbackCount ?? 0)
  const requested = Number(data.requested ?? deleted)
  const missing = Math.max(0, requested - deleted)
  if (missing > 0 && deleted >= 0) {
    return t('localization.deletePartial', { n: deleted, missing })
  }
  return t('localization.deleteDone', { n: deleted })
}
