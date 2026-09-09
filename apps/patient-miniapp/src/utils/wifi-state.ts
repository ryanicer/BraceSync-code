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
