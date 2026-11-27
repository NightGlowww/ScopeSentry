import { describe, expect, it } from 'vitest'
import { SCAN_MODULES, moduleStageLabel } from './scanModules'

const fakeT = (k: string) => 'L:' + k

describe('moduleStageLabel', () => {
  it('按当前语言取模块名并带上阶段序号', () => {
    expect(moduleStageLabel('PortScan', fakeT)).toBe('L:task.module.PortScan · 5/14')
  })

  it('未知模块原样返回，不猜也不显示裸 key', () => {
    expect(moduleStageLabel('SomeFutureModule', fakeT)).toBe('SomeFutureModule')
  })

  it('空值返回空串', () => {
    expect(moduleStageLabel(undefined, fakeT)).toBe('')
    expect(moduleStageLabel('', fakeT)).toBe('')
  })

  it('模块清单与节点流水线一致（顺序即阶段号）', () => {
    expect(SCAN_MODULES).toHaveLength(14)
    expect(SCAN_MODULES[0].key).toBe('TargetHandler')
    expect(SCAN_MODULES[13].key).toBe('PageMonitoring')
  })
})
