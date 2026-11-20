// 扫描模块的中文名与阶段序号。
// 进度里直接显示 PortFingerprint / AssetMapping 这类内部模块名，不熟悉流程的人看不懂，
// 也判断不出"跑到哪一步了"（2026-11-20）。
export interface ScanModuleMeta {
  key: string
  label: string
}

// 顺序即扫描流水线顺序（与节点 pool.go 的 modules 列表一致）
export const SCAN_MODULES: ScanModuleMeta[] = [
  { key: 'TargetHandler', label: '目标解析' },
  { key: 'SubdomainScan', label: '子域名扫描' },
  { key: 'SubdomainSecurity', label: '子域安全检测' },
  { key: 'PortScanPreparation', label: '端口扫描预处理' },
  { key: 'PortScan', label: '端口扫描' },
  { key: 'PortFingerprint', label: '端口指纹识别' },
  { key: 'AssetMapping', label: '资产映射' },
  { key: 'AssetHandle', label: '资产处理' },
  { key: 'URLScan', label: 'URL 扫描' },
  { key: 'WebCrawler', label: '爬虫' },
  { key: 'URLSecurity', label: 'URL 安全检测' },
  { key: 'DirScan', label: '目录扫描' },
  { key: 'VulnerabilityScan', label: '漏洞扫描' },
  { key: 'PageMonitoring', label: '页面监控' }
]

const TOTAL = SCAN_MODULES.length

/** 返回形如「端口扫描 · 6/14」；未知模块原样返回，不猜。 */
export const moduleStageLabel = (mod?: string): string => {
  if (!mod) return ''
  const idx = SCAN_MODULES.findIndex((m) => m.key === mod)
  if (idx < 0) return mod
  return `${SCAN_MODULES[idx].label} · ${idx + 1}/${TOTAL}`
}
