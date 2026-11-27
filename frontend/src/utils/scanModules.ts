// 扫描模块的内部名与阶段序号。
// 进度里直接显示 PortFingerprint / AssetMapping 这类内部模块名，不熟悉流程的人看不懂，
// 也判断不出「跑到哪一步了」（2026-11-20）。
//
// 文案走 i18n：key 是内部模块名，显示名由调用方按当前语言取（task.module.<key>）。
// 之前这里存的是中文字面量，英文界面里任务进度会冒出「漏洞扫描 · 13/14」。
export interface ScanModuleMeta {
  key: string
}

// 顺序即扫描流水线顺序（与节点 pool.go 的 modules 列表一致）
export const SCAN_MODULES: ScanModuleMeta[] = [
  { key: 'TargetHandler' },
  { key: 'SubdomainScan' },
  { key: 'SubdomainSecurity' },
  { key: 'PortScanPreparation' },
  { key: 'PortScan' },
  { key: 'PortFingerprint' },
  { key: 'AssetMapping' },
  { key: 'AssetHandle' },
  { key: 'URLScan' },
  { key: 'WebCrawler' },
  { key: 'URLSecurity' },
  { key: 'DirScan' },
  { key: 'VulnerabilityScan' },
  { key: 'PageMonitoring' }
]

const TOTAL = SCAN_MODULES.length

type Translate = (key: string) => string

/** 返回形如「端口扫描 · 6/14」；未知模块原样返回，不猜。 */
export const moduleStageLabel = (mod: string | undefined, t: Translate): string => {
  if (!mod) return ''
  const idx = SCAN_MODULES.findIndex((m) => m.key === mod)
  if (idx < 0) return mod
  return `${t('task.module.' + SCAN_MODULES[idx].key)} · ${idx + 1}/${TOTAL}`
}
