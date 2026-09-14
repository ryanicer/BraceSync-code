/**
 * STUB: T094a KNOWN_RED scaffold — implementation by Iris Stage 3b.
 *
 * WiFi 配网状态机（PRD §7A.9）。
 * 本文件仅提供函数签名与错误占位返回值，供 Ella 的 KNOWN_RED 单测断言失败（红）。
 * Iris 转绿时替换函数体为真实状态映射逻辑，无需改动 import 路径。
 *
 * 纯 TS，不依赖 uni.* / DOM / Vue / BLE。
 */

/** BLE 扫描到的设备（仅状态机需要的字段） */
export interface ScanDevice {
  /** 设备名称（需匹配 BSYNC- 前缀） */
  name: string
  /** 是否为 2.4G 频段设备 */
  is24G: boolean
}

/** 配网进度阶段 */
export type ProvisionStep = 0 | 1 | 2 | 3

/** 配网最终状态（进度 0-3 / 成功 9 / 失败 -1~-4） */
export type ProvisionStatus =
  | { kind: 'progress'; step: ProvisionStep }
  | { kind: 'success' }
  | { kind: 'failure'; code: -1 | -2 | -3 | -4; message: string }

/** 配网超时阈值（毫秒）。PRD §7A.9：患者端 15s。 */
export const PROVISION_TIMEOUT_MS = 15000

/**
 * 过滤 BLE 扫描结果：仅保留 BSYNC- 前缀且为 2.4G 的设备。
 * PRD §7A.9：扫描阶段仅显示 BSYNC- 前缀设备，2.4G 前置提示。
 */
export function filterBleDevices(devices: ScanDevice[]): ScanDevice[] {
  return devices.filter((d) => d.name.startsWith('BSYNC-') && d.is24G)
}

/**
 * 推进配网进度步骤：0→1→2→3。
 * PRD §7A.9：写入步骤推进 0→1→2→3。
 * @param current 当前步骤（0-3）
 * @returns 下一步骤；若已到 3 则返回 3（不再推进）
 */
export function nextProvisionStep(current: ProvisionStep): ProvisionStep {
  return current < 3 ? ((current + 1) as ProvisionStep) : 3
}

/**
 * 解析配网状态码 → 进度/成功/失败。
 * PRD §7A.9：0-3 进度，9 成功，-1 密码错，-2 无网络，-3 地址失败，-4 服务器不可达。
 */
export function resolveProvisionStatus(code: number): ProvisionStatus {
  if (code >= 0 && code <= 3) {
    return { kind: 'progress', step: code as ProvisionStep }
  }
  if (code === 9) {
    return { kind: 'success' }
  }
  switch (code) {
    case -1:
      return { kind: 'failure', code: -1, message: 'WiFi 密码错误，请检查密码后重试' }
    case -2:
      return { kind: 'failure', code: -2, message: '找不到 WiFi 网络，请检查网络名称' }
    case -3:
      return { kind: 'failure', code: -3, message: '网络地址获取失败，请检查路由器' }
    case -4:
      return { kind: 'failure', code: -4, message: '服务器不可达，请稍后重试' }
    default:
      return { kind: 'failure', code: -1, message: '配网失败，请重试' }
  }
}

/**
 * 判断配网是否超时。
 * PRD §7A.9：患者端 15s 响应超时。
 * @param elapsedMs 已耗时（毫秒）
 */
export function isProvisionTimeout(elapsedMs: number): boolean {
  return elapsedMs > PROVISION_TIMEOUT_MS
}

// ===== T192：BLE 设备识别与患者端生活化文案（PRD §7A.9 患技差异表） =====

/** 广播名前缀（协议定稿 §1：广播名 = BSYNC-{device_id 后 6 位}） */
export const BSYNC_PREFIX = 'BSYNC-'

/** 是否为本网关设备的广播名（02-scan 列表唯一过滤条件，不涉频段） */
export function isBsyncDevice(name: string): boolean {
  return (name || '').startsWith(BSYNC_PREFIX)
}

/** 由云端 device_id 推导期望广播名；不足 6 位则原样拼接 */
export function broadcastNameOf(deviceId: string): string {
  const id = deviceId || ''
  return BSYNC_PREFIX + id.slice(-6)
}

/**
 * RSSI → 生活化信号描述。患者端不出现数值（PRD §7A.9 约束）。
 * 阈值 -60dBm：室内 BLE 近场（设计稿要求 1 米以内）通常优于该值。
 */
export function signalLabel(rssi: number, good = '信号良好', weak = '信号弱'): string {
  return typeof rssi === 'number' && rssi >= -60 ? good : weak
}

/** 网络名输入容错：前后空格自动清理（PRD §7A.9.1 ③-2） */
export function normalizeWifiName(raw: string): string {
  return (raw || '').trim()
}

/**
 * 03 表单两个字段进 B511 载荷前的统一清洗，并回报是否去过空格。
 * 网络名 trim ＝ PRD §7A.9.1 ③-2 明文要求；**密码 trim 是该条的口径外扩，待 PM/Peter 回写 PRD**：
 * 真机 09-14 实证「密码末尾多一个空格 → 设备拿到的是错密码 → 合法回 -1」，
 * 而 PRD 同一条自己就写着"-1 是最主要失败原因"。风险：PSK 以真空格结尾者将不再可用（WPA2 PSK 实际不出现）。
 */
export function normalizeWifiCreds(rawSsid: string, rawPwd: string): {
  ssid: string
  pwd: string
  hadWhitespace: boolean
} {
  const ssid = normalizeWifiName(rawSsid)
  const pwd = (rawPwd || '').trim()
  return { ssid, pwd, hadWhitespace: ssid !== (rawSsid || '') || pwd !== (rawPwd || '') }
}

/**
 * 03 原地重连时从重扫结果里挑目标：优先当初连接的那台（广播名相同），
 * 其次窗口内任一 BSYNC 广播（设备可能只在 localName 里带名字），都没有返回 null。
 * name 匹配只做优先、不做硬条件——真机断连后重扫到的顺序并不稳定。
 */
export function pickReconnectTarget<T extends { name: string }>(found: T[], expectedName: string): T | null {
  const list = found || []
  if (expectedName) {
    const same = list.find((d) => (d.name || '') === expectedName)
    if (same) return same
  }
  return list.find((d) => isBsyncDevice(d.name)) ?? null
}
