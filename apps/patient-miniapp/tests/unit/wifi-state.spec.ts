/**
 * T094a KNOWN_RED — WiFi 配网状态机单测（PRD §7A.9）。
 *
 * 预期红态：stub（src/utils/wifi-state.ts）固定返回错误占位值，
 * 以下断言在 Iris 实现真实状态映射前全部 FAIL。
 *
 * 覆盖：
 *   - 扫描过滤（BSYNC- 前缀 + 2.4G 前置）
 *   - 进度推进 0→1→2→3
 *   - 成功态 9
 *   - 失败态 -1/-2/-3/-4
 *   - 15s 超时
 */
import { describe, it, expect } from 'vitest'
import {
  filterBleDevices,
  nextProvisionStep,
  resolveProvisionStatus,
  isProvisionTimeout,
  PROVISION_TIMEOUT_MS,
  type ScanDevice,
  type ProvisionStep,
} from '../../src/utils/wifi-state'

describe('WiFi 配网 — 扫描过滤（PRD §7A.9）', () => {
  const devices: ScanDevice[] = [
    { name: 'BSYNC-001', is24G: true },
    { name: 'BSYNC-002', is24G: false }, // 非 2.4G，过滤
    { name: 'OtherDevice', is24G: true }, // 非 BSYNC- 前缀，过滤
    { name: 'BSYNC-003', is24G: true },
  ]

  it('仅保留 BSYNC- 前缀且 2.4G 的设备', () => {
    const result = filterBleDevices(devices)
    expect(result).toHaveLength(2)
    expect(result.map((d) => d.name)).toEqual(['BSYNC-001', 'BSYNC-003'])
  })

  it('空输入返回空数组', () => {
    expect(filterBleDevices([])).toEqual([])
  })

  it('全部不符合时返回空数组', () => {
    const allBad: ScanDevice[] = [
      { name: 'Foo', is24G: true },
      { name: 'BSYNC-X', is24G: false },
    ]
    expect(filterBleDevices(allBad)).toEqual([])
  })
})

describe('WiFi 配网 — 进度推进 0→1→2→3（PRD §7A.9）', () => {
  it('step 0 → 1', () => {
    expect(nextProvisionStep(0 as ProvisionStep)).toBe(1)
  })
  it('step 1 → 2', () => {
    expect(nextProvisionStep(1 as ProvisionStep)).toBe(2)
  })
  it('step 2 → 3', () => {
    expect(nextProvisionStep(2 as ProvisionStep)).toBe(3)
  })
  it('step 3 已到终点，保持 3', () => {
    expect(nextProvisionStep(3 as ProvisionStep)).toBe(3)
  })
})

describe('WiFi 配网 — 状态码解析（PRD §7A.9）', () => {
  it('code 0 → progress step 0', () => {
    const r = resolveProvisionStatus(0)
    expect(r.kind).toBe('progress')
    if (r.kind === 'progress') expect(r.step).toBe(0)
  })
  it('code 1 → progress step 1', () => {
    const r = resolveProvisionStatus(1)
    expect(r.kind).toBe('progress')
    if (r.kind === 'progress') expect(r.step).toBe(1)
  })
  it('code 2 → progress step 2', () => {
    const r = resolveProvisionStatus(2)
    expect(r.kind).toBe('progress')
    if (r.kind === 'progress') expect(r.step).toBe(2)
  })
  it('code 3 → progress step 3', () => {
    const r = resolveProvisionStatus(3)
    expect(r.kind).toBe('progress')
    if (r.kind === 'progress') expect(r.step).toBe(3)
  })
  it('code 9 → success', () => {
    expect(resolveProvisionStatus(9).kind).toBe('success')
  })
  it('code -1 → 密码错误', () => {
    const r = resolveProvisionStatus(-1)
    expect(r.kind).toBe('failure')
    if (r.kind === 'failure') {
      expect(r.code).toBe(-1)
      expect(r.message).toContain('密码')
    }
  })
  it('code -2 → 找不到网络', () => {
    const r = resolveProvisionStatus(-2)
    expect(r.kind).toBe('failure')
    if (r.kind === 'failure') {
      expect(r.code).toBe(-2)
      expect(r.message).toContain('网络')
    }
  })
  it('code -3 → 网络地址失败', () => {
    const r = resolveProvisionStatus(-3)
    expect(r.kind).toBe('failure')
    if (r.kind === 'failure') {
      expect(r.code).toBe(-3)
    }
  })
  it('code -4 → 服务器不可达', () => {
    const r = resolveProvisionStatus(-4)
    expect(r.kind).toBe('failure')
    if (r.kind === 'failure') {
      expect(r.code).toBe(-4)
    }
  })
})

describe('WiFi 配网 — 15s 超时（PRD §7A.9）', () => {
  it('超时阈值常量为 15000ms', () => {
    expect(PROVISION_TIMEOUT_MS).toBe(15000)
  })
  it('耗时 14999ms → 未超时', () => {
    expect(isProvisionTimeout(14999)).toBe(false)
  })
  it('耗时恰好 15000ms → 未超时（边界）', () => {
    expect(isProvisionTimeout(15000)).toBe(false)
  })
  it('耗时 15001ms → 超时', () => {
    expect(isProvisionTimeout(15001)).toBe(true)
  })
  it('耗时 30000ms → 超时', () => {
    expect(isProvisionTimeout(30000)).toBe(true)
  })
})
