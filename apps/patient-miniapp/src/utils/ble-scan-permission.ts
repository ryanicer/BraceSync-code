/**
 * T740：Android 微信在位置权限缺失时，startBluetoothDevicesDiscovery 的 errMsg 原文为
 * `location permission is denied`。只匹配权限类文案，避免把普通扫描失败误判成权限问题。
 *
 * 独立成模块的原因：utils/ble.ts 含 #ifdef 条件编译段，vitest 直接 import 会触发
 * logger.ts 的转译冲突（先例见 b511-write-retry.spec.ts 的源码结构断言）。
 */
export function isLocationPermissionDenied(errMsg: string): boolean {
  const m = (errMsg || '').toLowerCase()
  return m.includes('location permission') || m.includes('permission is denied') || m.includes('auth deny')
}
