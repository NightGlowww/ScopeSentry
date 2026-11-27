<script lang="tsx">
import { ElBreadcrumb, ElBreadcrumbItem } from 'element-plus'
import { ref, watch, computed, unref, defineComponent } from 'vue'
import { useRouter } from 'vue-router'
import type { RouteLocationNormalizedLoaded } from 'vue-router'
import { useI18n } from '@/hooks/web/useI18n'
import { Icon } from '@/components/Icon'
import { useAppStore } from '@/store/modules/app'
import { useDesign } from '@/hooks/web/useDesign'

const { getPrefixCls } = useDesign()

const prefixCls = getPrefixCls('breadcrumb')

const appStore = useAppStore()

// 面包屑图标
const breadcrumbIcon = computed(() => appStore.getBreadcrumbIcon)

export default defineComponent({
  name: 'Breadcrumb',
  setup() {
    const { currentRoute } = useRouter()

    const { t } = useI18n()

    const levelList = ref<AppRouteRecordRaw[]>([])

    // 面包屑直接取路由的匹配链。
    // 旧实现是在菜单树里 filter(path === 当前叶子) 再 treeToList 拍平：客户端跳转时
    // 上一页的叶子会留在列表里（从「备份恢复」跳到「MCP 调用日志」会显示
    // 配置 / 备份恢复 / MCP 调用日志；整页刷新才正常）——2026-11-28 实测。
    const getBreadcrumb = () => {
      levelList.value = currentRoute.value.matched
        .filter((record) => !!record.meta?.title)
        .map(
          (record) =>
            ({
              path: record.path,
              name: record.name,
              meta: record.meta,
              redirect: (record as unknown as AppRouteRecordRaw).redirect
            }) as AppRouteRecordRaw
        )
    }

    const renderBreadcrumb = () => {
      // 路由链里有一些只用于布局/重定向、没有 title 的节点，
      // 不过滤会在面包屑里留下"配置 / / / 系统日志"这种空条目和多余分隔符
      // getBreadcrumb 已经把匹配链拍平成一维，这里只补一道空标题兜底
      const breadcrumbList = unref(levelList).filter((v) => !!v?.meta?.title)
      return breadcrumbList.map((v) => {
        const disabled = !v.redirect || v.redirect === 'noredirect'
        const meta = v.meta
        return (
          <ElBreadcrumbItem to={{ path: disabled ? '' : v.path }} key={v.name}>
            {meta?.icon && breadcrumbIcon.value ? (
              <>
                <Icon icon={meta.icon} class="mr-[5px]"></Icon> {t(v?.meta?.title || '')}
              </>
            ) : (
              t(v?.meta?.title || '')
            )}
          </ElBreadcrumbItem>
        )
      })
    }

    watch(
      () => currentRoute.value,
      (route: RouteLocationNormalizedLoaded) => {
        if (route.path.startsWith('/redirect/')) {
          return
        }
        getBreadcrumb()
      },
      {
        immediate: true
      }
    )

    return () => (
      <ElBreadcrumb separator="/" class={`${prefixCls} flex items-center h-full ml-[10px]`}>
        {/*
          这里原本套了 TransitionGroup + animate__fadeInRight。变长列表里离场节点会卡住：
          DOM 里残留上一页的叶子，只剩下 v-leave-active 这个类而没有任何动画结束事件，
          于是「配置/备份恢复 → 配置/MCP 调用日志」会显示三段（2026-11-28 实测，5 秒后仍在）。
          面包屑是导航指示器，不需要入场动画，直接平铺。
        */}
        {renderBreadcrumb()}
      </ElBreadcrumb>
    )
  }
})
</script>

<style lang="less" scoped>
@prefix-cls: ~'@{elNamespace}-breadcrumb';

.@{prefix-cls} {
  :deep(&__item) {
    display: flex;
    .@{prefix-cls}__inner {
      display: flex;
      align-items: center;
      color: var(--top-header-text-color);

      &:hover {
        color: var(--el-color-primary);
      }
    }
  }

  :deep(&__item):not(:last-child) {
    .@{prefix-cls}__inner {
      color: var(--top-header-text-color);

      &:hover {
        color: var(--el-color-primary);
      }
    }
  }

  :deep(&__item):last-child {
    .@{prefix-cls}__inner {
      color: var(--el-text-color-placeholder);

      &:hover {
        color: var(--el-text-color-placeholder);
      }
    }
  }
}
</style>
