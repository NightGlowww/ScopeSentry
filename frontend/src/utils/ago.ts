import { useI18n } from '@/hooks/web/useI18n'

// 相对时间（「3 小时前」）。绝对时间在这类提示里帮不上忙：
// 用户看到「11-26 11:57」还得自己算到今天隔了多久（2026-11-28 节点页实测）。
// 单位文案走 i18n，避免把中文硬编码进工具函数。
export const agoText = (value?: string | null): string => {
  if (!value) return '-'
  const then = new Date(value).getTime()
  if (!Number.isFinite(then)) return '-'
  const { t } = useI18n()
  const diff = Date.now() - then
  if (diff <= 0) return t('localization.agoNow')
  const minutes = Math.floor(diff / 60000)
  if (minutes < 1) return t('localization.agoNow')
  if (minutes < 60) return t('localization.agoMinutes', { n: minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return t('localization.agoHours', { n: hours })
  const days = Math.floor(hours / 24)
  return t('localization.agoDays', { n: days })
}
