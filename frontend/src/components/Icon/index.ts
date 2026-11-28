import IconSfc from './src/Icon.vue'

export type { IconTypes } from './src/types'

/**
 * Icon 组件：class / onClick / role / tabindex / aria-label 这些属性由组件内部
 * 用 v-bind="$attrs" 显式透传到真正的 <i> 上，运行期一直有效；但 SFC 的 props
 * 类型不包含它们，在 TSX 里直接写 <Icon onClick=... /> 会被判"属性不存在"。
 * 这里放宽成"任意 props 的组件"，只影响图标这一个组件，保持透传可用。
 */
export const Icon = IconSfc as unknown as (props: Record<string, any>) => any

