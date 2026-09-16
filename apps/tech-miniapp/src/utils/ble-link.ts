// T218 A-3: 原地重连的纯逻辑（患者端 utils/wifi-state.ts pickReconnectTarget 平移）。
// 🔴 本文件保持零 import：ble.ts 在 vitest 里挂不起来（#ifdef 条件编译不生效），
// 只有零依赖文件才能做真单测（test/t218-ble-robustness.spec.ts 直接 import 本文件）。

/** 原地重连的一次性重扫窗口：实测设备断开后 ≥6s 不广播，故比普通扫描（6s）长（患者端同值） */
export const RECONNECT_SCAN_MS = 15000

const BSYNC_PREFIX = 'BSYNC-'

export function isBsyncDevice(name: string): boolean {
  return (name || '').startsWith(BSYNC_PREFIX)
}

/**
 * 从重扫结果里挑重连目标：优先匹配已知广播名（防同房间多台设备），
 * 没有已知名时回退到第一台 BSYNC 设备；都没有则返回 null（判重连失败）。
 */
export function pickReconnectTarget<T extends { name: string }>(found: T[], expectedName: string): T | null {
  const list = found || []
  if (expectedName) {
    const same = list.find((d) => (d.name || '') === expectedName)
    if (same) return same
  }
  return list.find((d) => isBsyncDevice(d.name)) ?? null
}
