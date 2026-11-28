import type { Composer } from 'vue-i18n'
import { i18n } from '@/plugins/vueI18n'

// 后端返回的 message 是 i18n key（api.error / api.asset.list.success 之类），
// 这里负责把它翻成当前语言。两条底线：
//   1) 认识的整 key 用词条；
//   2) 不认识的按后缀归类到通用文案，绝不把 api.xxx 这种裸 key 弹给用户。
//      （2026-11-29 实测：后端 114 个文案 key 里只有 2 个有词条，其余全靠这里兜底）
const KEY_LIKE = /^[a-z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+$/

const SUFFIX_FALLBACK: [string, string][] = [
  ['success', 'api.success'],
  ['failed', 'api.error'],
  ['error', 'api.error'],
  ['not_found', 'api.not_found'],
  ['unauthorized', 'api.unauthorized'],
  ['forbidden', 'api.forbidden'],
  ['bad_request', 'api.bad_request'],
  ['required', 'api.required_fields'],
  ['required_fields', 'api.required_fields'],
  // 参数类：后端有一堆 xxx_invalid / invalid_xxx / id_missing 的一次性 key，
  // 逐条翻译收益极低（文案都是「参数不对」），统一归到通用提示；要精确文案再单独加词条。
  ['invalid', 'api.bad_request'],
  ['invalid_params', 'api.bad_request'],
  ['invalid_type', 'api.bad_request'],
  ['invalid_action', 'api.bad_request'],
  ['invalid_fields', 'api.bad_request'],
  ['invalid_filename', 'api.bad_request'],
  ['invalid_file_type', 'api.bad_request'],
  ['id_missing', 'api.required_fields'],
  ['blank', 'api.bad_request'],
  ['negative', 'api.bad_request'],
  ['no_files_selected', 'api.bad_request'],
  ['invalidParams', 'api.bad_request'],
  ['invalid_flag', 'api.bad_request'],
  ['name_exists', 'api.name_exists']
]

// 前缀规则：整族共用一个通用文案（api.required.id / api.required.ids / api.required.name …）
const PREFIX_FALLBACK: [string, string][] = [
  ['api.required.', 'api.required_fields'],
  ['api.bad_request.', 'api.bad_request']
]

export const translateApiMessage = (message: unknown): string => {
  if (typeof message !== 'string' || !i18n) return typeof message === 'string' ? message : ''
  const { te, t, locale } = i18n.global as unknown as Composer
  const lang = locale.value
  if (te(message, lang) || te(message, 'en')) return String(t(message))
  // 不是 key（后端直接回的人话）就原样透出
  if (!KEY_LIKE.test(message)) return message
  for (const [prefix, generic] of PREFIX_FALLBACK) {
    if (message.startsWith(prefix)) return String(t(generic))
  }
  for (const [suffix, generic] of SUFFIX_FALLBACK) {
    if (message.endsWith('.' + suffix) || message.endsWith('_' + suffix)) return String(t(generic))
  }
  // 认识的只是 key 形状但没有词条也没有后缀可归：用通用失败文案，不露 key
  return String(t('api.error'))
}
