// 设备域 mock 数据（对齐 api-contracts.ts getDevices）
// T508：contactAreaCm2 = 压力点有效受压面积（cm²）。000034 不回填 ⇒ 现网未配置的设备该列 NULL，
// mock 保留同一形态（只有 DEV-A3F312 当「已配置」样本），前端 kPa 档两态都能跑到。
import type { Device } from '@bracesync/shared-types'

const DEVICES: Device[] = [
  { deviceId: 'DEV-A3F312', model: 'PRS-ML05-RC', firmwareVersion: 'v1.4.2', patientId: 'PT-001', wifiSsid: 'Hospital-WiFi', contactAreaCm2: 0.64, bindTime: '2026-03-12T10:00:00+08:00', status: 'online', lastReportAt: '2026-08-11T14:30:00+08:00' },
  { deviceId: 'DEV-B7E456', model: 'PRS-ML05-RC', firmwareVersion: 'v1.4.2', patientId: 'PT-002', wifiSsid: 'Home-5G', contactAreaCm2: null, bindTime: '2026-04-02T14:00:00+08:00', status: 'online', lastReportAt: '2026-08-11T14:28:00+08:00' },
  { deviceId: 'DEV-C9D789', model: 'PRS-ML05-RC', firmwareVersion: 'v1.3.9', patientId: 'PT-003', wifiSsid: 'Home-WiFi', contactAreaCm2: null, bindTime: '2026-05-18T15:30:00+08:00', status: 'abnormal', lastReportAt: '2026-08-11T09:10:00+08:00' },
  { deviceId: 'DEV-D2A012', model: 'PRS-ML05-RC', firmwareVersion: 'v1.4.2', patientId: 'PT-004', wifiSsid: 'Home-WiFi-2', contactAreaCm2: null, bindTime: '2026-06-01T10:30:00+08:00', status: 'offline', lastReportAt: '2026-08-10T22:15:00+08:00' },
  { deviceId: 'DEV-E5B347', model: 'PRS-ML05-RC', firmwareVersion: 'v1.4.0', patientId: 'PT-006', wifiSsid: 'School-Net', contactAreaCm2: null, bindTime: '2026-07-15T16:00:00+08:00', status: 'online', lastReportAt: '2026-08-11T14:25:00+08:00' },
  { deviceId: 'DEV-F8C590', model: 'PRS-ML05-RC', firmwareVersion: 'v1.4.2', patientId: null, wifiSsid: null, contactAreaCm2: null, bindTime: null, status: 'unbound', lastReportAt: null },
]

export function mockDevices(params: { keyword?: string }): { list: Device[]; total: number; page: number; pageSize: number } {
  let list = DEVICES.map((d) => ({ ...d }))
  if (params.keyword) {
    const kw = params.keyword.toLowerCase()
    list = list.filter((d) => d.deviceId.toLowerCase().includes(kw) || (d.patientId ?? '').toLowerCase().includes(kw))
  }
  return { list, total: list.length, page: 1, pageSize: list.length }
}

export function mockPatientName(patientId: string | null): string {
  if (!patientId) return '-'
  const names: Record<string, string> = {
    'PT-001': '林小雨', 'PT-002': '陈子航', 'PT-003': '王梓萌',
    'PT-004': '刘俊熙', 'PT-005': '赵欣然', 'PT-006': '孙浩然',
  }
  return names[patientId] ?? patientId
}

// T268: 设备详情 / 绑定历史 / 注册（语义对齐 device-service：register 幂等、
// deviceId 4-48 位 alnum/-/_；bindings 含历史解绑记录）
export interface DeviceBindingRecord {
  bindingId: string
  deviceId: string
  patientId: string
  bindAt: string
  unbindAt: string | null
  reason: string | null
  operatorId: string | null
}

const BINDINGS: DeviceBindingRecord[] = [
  { bindingId: 'B-001', deviceId: 'DEV-A3F312', patientId: 'PT-001', bindAt: '2026-03-12T10:00:00+08:00', unbindAt: null, reason: null, operatorId: 'A0001' },
  { bindingId: 'B-002', deviceId: 'DEV-C9D789', patientId: 'PT-003', bindAt: '2026-05-18T15:30:00+08:00', unbindAt: '2026-06-01T09:00:00+08:00', reason: 'rebind', operatorId: 'A0001' },
  { bindingId: 'B-003', deviceId: 'DEV-C9D789', patientId: 'PT-003', bindAt: '2026-06-01T09:30:00+08:00', unbindAt: null, reason: null, operatorId: 'A0001' },
]

export function mockDeviceDetail(deviceId: string): Device {
  const dev = DEVICES.find((d) => d.deviceId === deviceId)
  if (!dev) throw new Error(`设备不存在: ${deviceId}`)
  return { ...dev }
}

/** 安装详情「设备型号」行用：设备行缺失返回 null（对齐真实库里 LEFT JOIN devices 未命中） */
export function mockDeviceModel(deviceId: string): string | null {
  return DEVICES.find((d) => d.deviceId === deviceId)?.model ?? null
}

export function mockDeviceBindings(deviceId: string): DeviceBindingRecord[] {
  return BINDINGS.filter((b) => b.deviceId === deviceId).map((b) => ({ ...b }))
}

const DEVICE_ID_RE = /^[A-Za-z0-9_-]{4,48}$/

// ===== T728 设备验签密钥（mock 态）=====
//
// 真实链路里明文只存在于「本次新建」那一发，库里此后只有 AES-GCM 密文；mock 没有加密列，
// 用一个进程内 Map 存「这一世还拿得到的明文」，重生成即覆盖旧值 —— 让「旧 secret 即时失效」
// 与「关掉回显窗就再也取不到」两件事在本地开发路径上也能跑到。
// 未登记的设备（初始 6 台 seed 行）刻意不给密钥：镜像 T573 现网那批「密文不成立、只能重生成」
// 的设备，前端的降级分支才有样本可走。
const SECRET_BY_DEVICE = new Map<string, string>()
const SECRET_VERSION_BY_DEVICE = new Map<string, number>()

/** 64 位 hex，形态对齐 device-service crypto.RandomSecret(32)（固件 HKDF ikm 约定） */
function genDeviceSecret(): string {
  let out = ''
  while (out.length < 64) out += Math.random().toString(16).slice(2)
  return out.slice(0, 64)
}

/** .env 文件体逐字照 service.SecretEnvBody：两行 + LF 行尾（CRLF 会让固件侧 sha256 对不上） */
export function mockDeviceSecretEnv(deviceId: string): string {
  const secret = SECRET_BY_DEVICE.get(deviceId)
  if (!secret) {
    throw new Error(`设备 ${deviceId} 没有可导出的验签密钥（历史数据或从未注册），请重新生成后再下载`)
  }
  return `DEVICE_ID=${deviceId}\nDEVICE_SECRET=${secret}\n`
}

/** 批量：逐台降级，拿不出来的进 skipped（真实链路里它们进 zip 的 _skipped.txt 与留痕） */
export function mockDeviceSecretEnvs(deviceIds: string[]): { envs: string[]; skipped: string[] } {
  const envs: string[] = []
  const skipped: string[] = []
  for (const id of deviceIds) {
    try {
      envs.push(mockDeviceSecretEnv(id))
    } catch {
      skipped.push(id)
    }
  }
  return { envs, skipped }
}

export interface DeviceRegisterResult extends Device {
  deviceSecret?: string
}

export function mockRegisterDevice(input: { deviceId: string; model?: string }): DeviceRegisterResult {
  const deviceId = input.deviceId.trim()
  if (!DEVICE_ID_RE.test(deviceId)) {
    throw new Error(`无效设备ID: ${deviceId}（4-48 位，字母/数字/-/_）`)
  }
  const existing = DEVICES.find((d) => d.deviceId === deviceId)
  if (existing) {
    // 幂等命中：本次生成的随机数没有落库 ⇒ 这一发不回明文（后端同判据，omitempty）
    return { ...existing }
  }
  const dev: Device = {
    deviceId,
    model: 'PRS-ML05-RC',
    firmwareVersion: '',
    patientId: null,
    wifiSsid: null,
    // T508：刚注册的设备面积未配置（后台还没填）⇒ null，与真实写路径同形
    contactAreaCm2: null,
    bindTime: null,
    status: 'unbound',
    lastReportAt: null,
  }
  DEVICES.push(dev)
  const secret = genDeviceSecret()
  SECRET_BY_DEVICE.set(deviceId, secret)
  SECRET_VERSION_BY_DEVICE.set(deviceId, 1)
  return { ...dev, deviceSecret: secret }
}

/** 重生成：新值覆盖旧值（一次性回显），版本号自增 —— 旧明文此后取不到也签不过。 */
export function mockRegenerateDeviceSecret(deviceId: string): { deviceId: string; secret: string; secretVersion: number } {
  if (!DEVICES.some((d) => d.deviceId === deviceId)) throw new Error(`设备不存在: ${deviceId}`)
  const secret = genDeviceSecret()
  const next = (SECRET_VERSION_BY_DEVICE.get(deviceId) ?? 0) + 1
  SECRET_BY_DEVICE.set(deviceId, secret)
  SECRET_VERSION_BY_DEVICE.set(deviceId, next)
  return { deviceId, secret, secretVersion: next }
}

