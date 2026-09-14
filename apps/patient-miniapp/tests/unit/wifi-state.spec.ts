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
  isBsyncDevice,
  broadcastNameOf,
  signalLabel,
  normalizeWifiName,
  normalizeWifiCreds,
  pickReconnectTarget,
  type ScanDevice,
  type ProvisionStep,
} from '../../src/utils/wifi-state'
import { encryptWifiPayload } from '../../src/utils/aes-ctr'
import { SIGNAL_GOOD, SIGNAL_WEAK } from '../../src/utils/wifi-copy'

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

/**
 * T192：BLE 设备识别与患者端生活化口径（PRD §7A.9 患技差异表 / §7A.9.1 ③-2）
 */
describe('T192 — 广播名识别（协议定稿：BSYNC-{device_id 后 6 位}）', () => {
  it('仅 BSYNC- 前缀视为本网关设备', () => {
    expect(isBsyncDevice('BSYNC-701001')).toBe(true)
    expect(isBsyncDevice('MyPhone_A123')).toBe(false)
    expect(isBsyncDevice('')).toBe(false)
  })

  it('由云端 device_id 推导期望广播名', () => {
    expect(broadcastNameOf('PRS-ML05-RC-20260701001')).toBe('BSYNC-701001')
  })

  it('device_id 不足 6 位时原样拼接，不截出错误名称', () => {
    expect(broadcastNameOf('701')).toBe('BSYNC-701')
    expect(broadcastNameOf('')).toBe('BSYNC-')
  })
})

describe('T192 — 患者端信号与生活化文案', () => {
  it('近场信号给「信号良好」，不出现 RSSI 数值', () => {
    expect(signalLabel(-50, SIGNAL_GOOD, SIGNAL_WEAK)).toBe('信号良好')
    expect(signalLabel(-60, SIGNAL_GOOD, SIGNAL_WEAK)).toBe('信号良好')
  })
  it('远端信号给「信号弱」', () => {
    expect(signalLabel(-75, SIGNAL_GOOD, SIGNAL_WEAK)).toBe('信号弱')
  })

  it('网络名前后空格自动清理（§7A.9.1 ③-2）', () => {
    expect(normalizeWifiName('  Home_WiFi_2.4G ')).toBe('Home_WiFi_2.4G')
    expect(normalizeWifiName('')).toBe('')
  })
})

describe('T192 — 03 表单清洗 normalizeWifiCreds', () => {
  const KEY = '2b7e151628aed2a6abf7158809cf4f3c'

  it('网络名与密码两端空格都去掉，并回报发生过清洗', () => {
    expect(normalizeWifiCreds('  Home_WiFi', 'abc123 ')).toEqual({
      ssid: 'Home_WiFi',
      pwd: 'abc123',
      hadWhitespace: true,
    })
  })

  it('本来干净时不误报清洗', () => {
    expect(normalizeWifiCreds('Home_WiFi', 'abc123')).toEqual({
      ssid: 'Home_WiFi',
      pwd: 'abc123',
      hadWhitespace: false,
    })
  })

  it('纯空格密码清洗后为空串（页面据此走"密码为空"拦截，而不是下发一个空格）', () => {
    expect(normalizeWifiCreds('Home_WiFi', '   ')).toEqual({
      ssid: 'Home_WiFi',
      pwd: '',
      hadWhitespace: true,
    })
  })

  it('清洗后的凭据比带尾随空格的少 1 字节 —— 对应真机 48B→47B', () => {
    const { pwd } = normalizeWifiCreds('Home_WiFi', 'abc123 ')
    const dirty = encryptWifiPayload('Home_WiFi', 'abc123 ', KEY, 1).length / 2
    const clean = encryptWifiPayload('Home_WiFi', pwd, KEY, 1).length / 2
    expect(clean).toBe(dirty - 1)
  })
})

describe('T192 — 03 原地重连的目标挑选', () => {
  it('优先挑当初连接的那台（广播名相同）', () => {
    const found = [
      { deviceId: 'AA', name: 'BSYNC-701002' },
      { deviceId: 'BB', name: 'BSYNC-701001' },
    ]
    expect(pickReconnectTarget(found, 'BSYNC-701001')?.deviceId).toBe('BB')
  })

  it('同名设备不在结果里时，退而挑窗口内任一 BSYNC 广播', () => {
    const found = [
      { deviceId: 'CC', name: 'MyPhone_A123' },
      { deviceId: 'DD', name: 'BSYNC-701009' },
    ]
    expect(pickReconnectTarget(found, 'BSYNC-701001')?.deviceId).toBe('DD')
  })

  it('非 BSYNC 广播一律不选，重扫无果返回 null（页面留在 03，不弹回 02）', () => {
    expect(pickReconnectTarget([{ deviceId: 'EE', name: 'MyPhone_A123' }], 'BSYNC-701001')).toBeNull()
    expect(pickReconnectTarget([], 'BSYNC-701001')).toBeNull()
  })

  it('无期望名（异常路径）时仍能挑出 BSYNC 设备', () => {
    expect(pickReconnectTarget([{ deviceId: 'FF', name: 'BSYNC-701001' }], '')?.deviceId).toBe('FF')
  })
})
