import { createRouter, createWebHashHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import type { App } from 'vue'
import { Layout } from '@/utils/routerHelper'
import { useI18n } from '@/hooks/web/useI18n'
import { NO_RESET_WHITE_LIST } from '@/constants'

const { t } = useI18n()

export const constantRouterMap: AppRouteRecordRaw[] = [
  {
    path: '/',
    component: Layout,
    redirect: '/dashboard',
    name: 'Root',
    meta: {
      hidden: true
    }
  },
  {
    path: '/redirect',
    component: Layout,
    name: 'Redirect',
    children: [
      {
        path: '/redirect/:path(.*)',
        name: 'RedirectParent',
        component: () => import('@/views/Redirect/Redirect.vue'),
        meta: {}
      }
    ],
    meta: {
      hidden: true,
      noTagsView: true
    }
  },
  {
    path: '/login',
    component: () => import('@/views/Login/Login.vue'),
    name: 'Login',
    meta: {
      hidden: true,
      title: t('router.login'),
      noTagsView: true
    }
  },
  // 常见首页别名：直接输 /index、/home 时不应该看到 404（2026-11-20 实测踩到）
  {
    path: '/index',
    redirect: '/dashboard/index',
    meta: { hidden: true }
  },
  {
    path: '/home',
    redirect: '/dashboard/index',
    meta: { hidden: true }
  },
  {
    path: '/404',
    component: () => import('@/views/Error/404.vue'),
    name: 'NoFind',
    meta: {
      hidden: true,
      title: '404',
      noTagsView: true
    }
  }
]

export const asyncRouterMap: AppRouteRecordRaw[] = [
  {
    path: '/dashboard',
    component: Layout,
    redirect: '/dashboard/index',
    name: 'Dashboard',
    meta: {},
    children: [
      {
        path: 'index',
        component: () => import('@/views/Dashboard/Analysis.vue'),
        name: 'index',
        meta: {
          title: t('router.dashboard'),
          icon: 'ant-design:dashboard-filled',
          // 首页固定在标签栏，不允许关闭（affix 会在 TagsView 初始化时自动注入）
          affix: true,
          noCache: true
        }
      }
    ]
  },
  {
    path: '/asset-information',
    component: Layout,
    name: 'AssetInformation',
    redirect: '/asset-information/index',
    meta: {
      title: t('router.assetManagement'),
      icon: 'carbon:view'
    },
    children: [
      {
        path: 'index',
        component: () => import('@/views/Asset/Asset.vue'),
        name: 'Asset-Information',
        meta: {
          title: t('router.assetinfo'),
          // 与父项「资产管理」用同一个 carbon:view，展开后看起来像重复（2026-11-27 反馈）
          icon: 'mdi:view-list-outline',
          noCache: true
        }
      },
      {
        path: 'asset-group',
        component: () => import('@/views/AssetGroup/AssetGroup.vue'),
        name: 'Asset-Group',
        meta: {
          title: t('router.assetGroup'),
          icon: 'material-symbols:corporate-fare',
          noCache: true
        }
      },
      {
        path: 'stat-finger',
        component: () => import('@/views/AssetInsight/StatFinger.vue'),
        name: 'Asset-StatFinger',
        meta: {
          title: t('router.statFinger'),
          icon: 'mdi:fingerprint',
          noCache: true
        }
      },
      {
        path: 'nuclei-result',
        component: () => import('@/views/AssetInsight/NucleiResult.vue'),
        name: 'Asset-NucleiResult',
        meta: {
          title: t('router.nucleiResult'),
          icon: 'mdi:shield-search',
          noCache: true
        }
      },
      {
        path: 'wih',
        component: () => import('@/views/AssetInsight/Wih.vue'),
        name: 'Asset-WIH',
        meta: {
          title: t('router.wih'),
          icon: 'mdi:key-alert',
          noCache: true
        }
      },
      {
        path: 'cert',
        component: () => import('@/views/AssetInsight/Cert.vue'),
        name: 'Asset-Cert',
        meta: {
          title: t('router.cert'),
          icon: 'mdi:certificate-outline',
          noCache: true
        }
      },
      {
        path: 'cip',
        component: () => import('@/views/AssetInsight/Cip.vue'),
        name: 'Asset-CIP',
        meta: {
          title: t('router.cip'),
          icon: 'mdi:domain',
          noCache: true
        }
      },
      {
        path: 'icp-task',
        component: () => import('@/views/AssetInsight/IcpTask.vue'),
        name: 'Asset-IcpTask',
        meta: {
          title: t('router.icpTask'),
          icon: 'mdi:account-search-outline',
          noCache: true
        }
      },
      {
        path: 'fofa',
        component: () => import('@/views/AssetInsight/Fofa.vue'),
        name: 'Asset-Fofa',
        meta: {
          title: t('router.fofa'),
          icon: 'mdi:radar',
          noCache: true
        }
      },
      {
        path: 'policy',
        component: () => import('@/views/AssetInsight/ScanPolicy.vue'),
        name: 'Asset-Policy',
        meta: {
          title: t('router.scanPolicy'),
          icon: 'mdi:file-cog-outline',
          noCache: true
        }
      },
      {
        path: 'http-status',
        component: () => import('@/views/AssetInsight/HttpStatus.vue'),
        name: 'Asset-HttpStatus',
        meta: {
          title: t('router.httpStatus'),
          icon: 'mdi:web-check',
          noCache: true
        }
      },
      {
        path: 'service',
        component: () => import('@/views/AssetInsight/Service.vue'),
        name: 'Asset-Service',
        meta: {
          title: t('router.assetService'),
          icon: 'mdi:server-network',
          noCache: true
        }
      },
      {
        path: 'fileleak',
        component: () => import('@/views/AssetInsight/FileLeak.vue'),
        name: 'Asset-FileLeak',
        meta: {
          title: t('router.fileLeak'),
          icon: 'mdi:file-alert-outline',
          noCache: true
        }
      },
      {
        path: 'npoc-service',
        component: () => import('@/views/AssetInsight/NpocService.vue'),
        name: 'Asset-NpocService',
        meta: {
          title: t('router.npocService'),
          icon: 'mdi:server-security',
          noCache: true
        }
      },
      {
        // 资产详情页：组件一直存在（views/Asset/detail/AssetDetail.vue），
        // 但路由从来没注册过 —— 资产表里任何跳到 /asset-information/asset-detail 的入口
        // 都落在「页面不存在」（2026-11-27 实测）。
        path: 'asset-detail',
        component: () => import('@/views/Asset/detail/AssetDetail.vue'),
        name: 'Asset-Detail',
        meta: {
          title: t('asset.assetDetail'),
          icon: 'mdi:file-document-outline',
          hidden: true,
          noCache: true
        }
      }
    ]
  },
  {
    path: '/task-management',
    redirect: '/task-management/ScanTask',
    component: Layout,
    name: 'TaskManagement',
    meta: {
      title: t('router.taskManagement'),
      icon: 'bi:list-task'
    },
    children: [
      {
        path: 'ScanTask',
        component: () => import('@/views/Task/Task.vue'),
        name: 'ScanTask',
        meta: {
          title: t('router.scanTask'),
          icon: 'emojione:eye',
          noCache: true
        }
      },
      {
        path: 'ScheduledTask',
        component: () => import('@/views/Task/ScheduledTask.vue'),
        name: 'ScheduledTask',
        meta: {
          title: t('router.scheduledTask'),
          icon: 'mdi:invoice-scheduled-outline',
          noCache: true
        }
      },
      {
        path: 'ScanTemplate',
        component: () => import('@/views/Task/ScanTemplate.vue'),
        name: 'ScanTemplate',
        meta: {
          title: t('router.scanTemplate'),
          icon: 'icon-park:page-template',
          noCache: true
        }
      }
    ]
  },
  {
    path: '/plugin-management',
    component: Layout,
    name: 'Plugin Management',
    redirect: '/plugin-management/index',
    meta: {},
    children: [
      {
        path: 'index',
        component: () => import('@/views/Plugins/plugin.vue'),
        name: 'PluginManagement',
        meta: {
          title: t('router.pluginsManager'),
          icon: 'mingcute:plugin-2-fill',
          noCache: true
        }
      }
    ]
  },
  {
    path: '/node-management',
    component: Layout,
    name: 'NodeManagement',
    redirect: '/node-management/index',
    meta: {},
    children: [
      {
        path: 'index',
        component: () => import('@/views/Node/Node.vue'),
        name: 'Node-Management',
        meta: {
          title: t('router.nodeManagement'),
          icon: 'material-symbols:network-node',
          noCache: true
        }
      }
    ]
  },
  {
    path: '/project-management',
    component: Layout,
    name: 'ProjectManagement',
    redirect: '/project-management/index',
    meta: {},
    children: [
      {
        path: 'index',
        component: () => import('@/views/Project/Project.vue'),
        name: 'Project-Management',
        meta: {
          title: t('router.projectManagement'),
          icon: 'eos-icons:project-outlined',
          noCache: true
        }
      },
      {
        path: 'project-detail',
        component: () => import('@/views/Project/detail/ProjectDetail.vue'),
        name: 'ProjectDetail',
        meta: {
          title: t('asset.assetDetail'),
          noTagsView: true,
          noCache: true,
          hidden: true,
          canTo: true,
          activeMenu: '/project-management'
        }
      }
    ]
  },
  {
    path: '/poc-management',
    component: Layout,
    name: 'POCManagement',
    redirect: '/poc-management/index',
    meta: {},
    children: [
      {
        path: 'index',
        component: () => import('@/views/Poc/Poc.vue'),
        name: 'POC-Management',
        meta: {
          title: t('router.pocManagement'),
          icon: 'ant-design:bug-filled',
          noCache: true
        }
      }
    ]
  },
  {
    path: '/fingerprint-management',
    component: Layout,
    name: 'FingerprintManagement',
    redirect: '/fingerprint-management/index',
    meta: {},
    children: [
      {
        path: 'index',
        component: () => import('@/views/Fingerprint/Fingerprint.vue'),
        name: 'Fingerprint-Management',
        meta: {
          title: t('router.fingerprintManagement'),
          icon: 'material-symbols:fingerprint',
          noCache: true
        }
      }
    ]
  },
  {
    path: '/sensitive-information-rules',
    component: Layout,
    name: 'Sensitive information rules',
    redirect: '/sensitive-information-rules/index',
    meta: {},
    children: [
      {
        path: 'index',
        component: () => import('@/views/Sensitive/Sensitive.vue'),
        name: 'Sensitive-information-rules',
        meta: {
          title: t('router.sensitiveInformationRules'),
          icon: 'carbon:deploy-rules',
          noCache: true
        }
      }
    ]
  },
  {
    path: '/dictionary-management',
    redirect: '/dictionary-management/manage',
    component: Layout,
    name: 'Dictionary management',
    meta: {
      title: t('router.dictionaryManagement'),
      icon: 'material-symbols:dictionary'
    },
    children: [
      {
        path: 'manage',
        component: () => import('@/views/DictionaryManagement/manage.vue'),
        name: 'Dictionary-management',
        meta: {
          title: t('router.dictionaryManagement'),
          icon: 'arcticons:dictionaryformids',
          noCache: true
        }
      },
      // {
      //   path: 'subdomain',
      //   component: () => import('@/views/DictionaryManagement/SubdomainDictionary.vue'),
      //   name: 'Subdomain management',
      //   meta: {
      //     title: t('router.subdomainDictionary'),
      //     icon: 'iconoir:dns',
      //     noCache: true
      //   }
      // },
      // {
      //   path: 'dir',
      //   component: () => import('@/views/DictionaryManagement/DirDictionary.vue'),
      //   name: 'Dir management',
      //   meta: {
      //     title: t('router.dirDictionary'),
      //     icon: 'octicon:file-directory-24',
      //     noCache: true
      //   }
      // },
      {
        path: 'port',
        component: () => import('@/views/DictionaryManagement/PortDictionary.vue'),
        name: 'PortDictionary management',
        meta: {
          title: t('router.portDictionary'),
          icon: 'jam:beatport',
          noCache: true
        }
      }
    ]
  },
  {
    path: '/configuration',
    redirect: '/configuration/system',
    component: Layout,
    name: 'configuration',
    meta: {
      title: t('router.configuration'),
      icon: 'tdesign:system-setting'
    },
    children: [
      {
        path: 'system',
        component: () => import('@/views/Configuration/system.vue'),
        name: 'system configuration',
        meta: {
          title: t('router.system'),
          icon: 'uil:setting',
          noCache: true
        }
      },
      {
        path: 'subfinder',
        component: () => import('@/views/Configuration/Subfinder.vue'),
        name: 'subfinder configuration',
        meta: {
          title: t('router.subfinder'),
          icon: 'ri:tools-line',
          noCache: true
        }
      },
      {
        path: 'intel',
        component: () => import('@/views/Configuration/IntelFeeds.vue'),
        name: 'intel feeds',
        meta: {
          title: t('router.intelFeeds'),
          icon: 'mdi:rss',
          noCache: true
        }
      },
      {
        path: 'rad',
        component: () => import('@/views/Configuration/rad.vue'),
        name: 'rad configuration',
        meta: {
          title: t('router.rad'),
          icon: 'game-icons:web-spit',
          noCache: true
        }
      },
      {
        path: 'apikey',
        component: () => import('@/views/Configuration/ApiKey.vue'),
        name: 'apikey management',
        meta: {
          title: t('router.apiKeyManagement'),
          icon: 'mdi:key-variant',
          noCache: true
        }
      },
      {
        path: 'mcp-audit',
        component: () => import('@/views/Configuration/McpAudit.vue'),
        name: 'mcp audit',
        meta: {
          title: t('router.mcpAudit'),
          icon: 'mdi:clipboard-text-clock-outline',
          noCache: true
        }
      },
      {
        path: 'audit',
        component: () => import('@/views/Configuration/AuditLog.vue'),
        name: 'audit log',
        meta: {
          title: t('router.auditLog'),
          icon: 'mdi:clipboard-list-outline',
          noCache: true
        }
      },
      {
        path: 'status',
        component: () => import('@/views/Configuration/SystemStatus.vue'),
        name: 'system status',
        meta: {
          title: t('router.systemStatus'),
          // 原为 mdi:server-network，与「资产管理 → 服务」撞车
          icon: 'mdi:chart-box-outline',
          noCache: true
        }
      },
      {
        path: 'backup',
        component: () => import('@/views/Configuration/Backup.vue'),
        name: 'backup configuration',
        meta: {
          title: t('router.backup'),
          icon: 'mdi:backup-restore',
          noCache: true
        }
      },
      {
        path: 'syslog',
        component: () => import('@/views/Configuration/Syslog.vue'),
        name: 'syslog configuration',
        meta: {
          title: t('router.syslog'),
          icon: 'mdi:math-log',
          noCache: true
        }
      }
    ]
  },
  {
    path: '/github',
    redirect: '/github/index',
    component: Layout,
    name: 'github',
    meta: {
      title: t('router.githubMonitor'),
      icon: 'mdi:github'
    },
    children: [
      {
        path: 'index',
        component: () => import('@/views/GitHub/index.vue'),
        name: 'github monitor',
        meta: {
          // 父项已经是「GitHub 泄露监控」，子项用同一个标题+图标，菜单展开后完全分不清
          title: t('router.githubMonitorList'),
          icon: 'mdi:shield-search',
          noCache: true
        }
      }
    ]
  },
  {
    path: '/about',
    redirect: '/about/index',
    component: Layout,
    name: 'About',
    meta: {},
    children: [
      {
        path: 'index',
        component: () => import('@/views/about.vue'),
        name: 'About ScopeSentry',
        meta: {
          title: t('common.about'),
          // 原为 carbon:deploy-rules，与「敏感信息规则」撞车
          icon: 'mdi:information-outline',
          noCache: true
        }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHashHistory(),
  strict: true,
  routes: constantRouterMap as RouteRecordRaw[],
  scrollBehavior: () => ({ left: 0, top: 0 })
})

// 部署会替换 assets 里的分块文件；浏览器若还持有旧的 index.html，
// 点进懒加载页面时会去取已经不存在的 chunk → 404 → 整个页面白屏（2026-11-27 实测：
// /node-management 打开后 #app 是空的，失败资源是 useVisibleInterval-*.js）。
// 这里在路由层兜住：chunk 加载失败就自动整页刷新一次（30 秒内只做一次，避免死循环）。
router.onError((err: unknown) => {
  const msg = String((err as Error)?.message || err || '')
  const isChunkError = /Failed to fetch dynamically imported module|Importing a module script failed|Loading chunk|dynamically imported module/i.test(msg)
  if (!isChunkError) return
  const KEY = 'chunk-reload-at'
  const last = Number(sessionStorage.getItem(KEY) || 0)
  if (Date.now() - last < 30000) return
  sessionStorage.setItem(KEY, String(Date.now()))
  location.reload()
})

export const resetRouter = (): void => {
  router.getRoutes().forEach((route) => {
    const { name } = route
    if (name && !NO_RESET_WHITE_LIST.includes(name as string)) {
      router.hasRoute(name) && router.removeRoute(name)
    }
  })
}

export const setupRouter = (app: App<Element>) => {
  app.use(router)
}

export default router
