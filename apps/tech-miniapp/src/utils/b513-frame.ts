/**
 * T733（方案A·单位链收口）— B513 实时压力帧解码
 *
 * 为什么单独成模块：与 b512-frame.ts 同理——src/utils/ble.ts 在 vitest 里挂不起来
 * （ble-log → logger 的 #ifdef 双声明会让 esbuild transform 失败，见
 * test/t218-ble-robustness.spec.ts 头注），帧解码必须能在纯函数层可测。
 *
 * 线协议（T173 权威口径，固件 1.9）：一帧 40 字节 = 20 × int16 小端有符号，**值即 mN**；
 * 默认 MTU 下被拆成 2×20 字节 Notify，分包拼接仍由 ble.ts 负责（T109）。
 *
 * 🔴 采集层零换算：本模块只把 int16 原值还原出来，绝不 ÷1000。
 * ÷MN_PER_N 只允许出现在展示层（`nFromMn`，@bracesync/constants）——
 * 旧实现在这里除了 ÷1000，是 T733 要拆掉的采集层换算（它让校准基线落在 N 空间、
 * 设备帧落在 mN 空间，两半对不齐）。
 */

/** 每帧点数（P01..P20） */
export const B513_POINT_COUNT = 20

/** 每帧字节数：20 × int16 = 40 字节 */
export const B513_FRAME_BYTES = B513_POINT_COUNT * 2

/** 无符号 16 位 → 有符号 int16（>32767 回绕，零点偏置可为负） */
export function toInt16(raw: number): number {
  return raw > 32767 ? raw - 65536 : raw
}

/**
 * 40 字节 → 20 点 **raw int16 mN**（不做任何单位换算）。
 * 不足 40 字节回 null：调用方继续拼包，不许拿残缺字节凑一帧出假数。
 */
export function decodeB513Frame(bytes: readonly number[]): number[] | null {
  if (!Array.isArray(bytes) || bytes.length < B513_FRAME_BYTES) return null
  const frame: number[] = []
  for (let i = 0; i < B513_POINT_COUNT; i++) {
    const raw = (bytes[i * 2] ?? 0) | ((bytes[i * 2 + 1] ?? 0) << 8)
    frame.push(toInt16(raw))
  }
  return frame
}
