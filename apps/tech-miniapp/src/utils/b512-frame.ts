/**
 * T726 — B512 notify 帧判读（技师端）
 *
 * 为什么单独成模块：src/utils/ble.ts 在 vitest 里挂不起来（logger 的 #ifdef 双声明会让 esbuild
 * transform 失败，见 test/b512-resubscribe.spec.ts 头注），帧形判读必须能在纯函数层可测。
 * 患者端有一份同逻辑的 apps/patient-miniapp/src/utils/b512-frame.ts——两端各自成包、不跨 app 引用，
 * 改动必须同步两处（新固件两字节帧对两端同时成立，只修一端等于给另一端出一个已知带病的包）。
 *
 * 现象（卡面）：新固件 B512 回两字节帧 `0x0A 0x00`，旧实现 `new Int8Array(res.value)[0]` 只取首字节
 * ⇒ 把 10 当状态码、真值 0 被丢 ⇒ 「清除失败」假象 + 状态机不推进（T240 的 clear 回执判定就死在这）。
 */

/** 协议已定义的 int8 状态码（docs/hardware/给小顾-2026-09-16/ble_provision.h:22-31） */
export const B512_KNOWN_CODES = [0, 1, 2, 3, 9, -1, -2, -3, -4]

/** 单帧字节上限：超过就不是状态帧，宁可不判读也不拿猜出来的字节驱动状态机 */
export const B512_FRAME_MAX_LEN = 8

export interface B512Frame {
  /** 判读出的状态码；null＝这一帧判不出来，调用方不得派发任何状态 */
  code: number | null
  /** 原始字节 hex（'0a,00'），与固件侧对日志用 */
  rawHex: string
  len: number
  /** len>=2 时的前置字节（帧头/版本）；协议未定义其含义，只进日志不参与判读 */
  headerHex: string
  /** 非空即异常/可疑帧：调用方必须记 warn，并把原始 hex 一起留住 */
  warn: string
}

/** 字节序列 → 逗号分隔 hex（空序列返回空串） */
export function toHex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join(',')
}

/** 单字节按 int8 解读：0x0A→10、0xFF→-1、0x80→-128 */
function asInt8(b: number): number {
  return b > 127 ? b - 256 : b
}

/**
 * 判读一帧 B512。
 * - len==1：沿用现语义，byte0 即状态码（向后兼容旧固件）；
 * - len>=2：协议没定义多字节帧的状态字段位置（合同 §7.2 只写「设备回报状态机」，
 *   固件 09-16 头文件是 `setValue(&code, 1)` 单字节），所以按 T726 卡面兜底——
 *   取**最后一个字节**为状态码，前置字节作为帧头/版本进日志；🔴 禁止再回到「首字节即状态码」；
 * - 空帧/超长：code=null，只留日志。
 */
export function parseB512Frame(value: ArrayBuffer | Uint8Array): B512Frame {
  const bytes = value instanceof Uint8Array ? value : new Uint8Array(value)
  const rawHex = toHex(bytes)
  const len = bytes.length

  if (len === 0) return { code: null, rawHex, len, headerHex: '', warn: '空帧（0 字节），不派发状态' }
  if (len > B512_FRAME_MAX_LEN) {
    return { code: null, rawHex, len, headerHex: '', warn: `异常长度 ${len}B > ${B512_FRAME_MAX_LEN}B，不按状态帧判读` }
  }

  const code = asInt8(bytes[len - 1])
  const headerHex = len >= 2 ? toHex(bytes.subarray(0, len - 1)) : ''
  const warn = !B512_KNOWN_CODES.includes(code) ? `未知状态码 ${code}` : ''
  return { code, rawHex, len, headerHex, warn }
}

/**
 * 收帧日志（与固件侧对表用，判据要求 raw=[0a,00] 与 code=0 同条）。
 * 尾部「第 N 帧 订阅轮次=M」是 T216 的可观测性契约，被两端 b512-resubscribe.spec.ts 钉着，不得删。
 */
export function formatB512FrameLog(frame: B512Frame, frameNo: number, subscribeRound: number): string {
  const head = `B512 raw=[${frame.rawHex}] len=${frame.len} → code=${frame.code}`
  const header = frame.headerHex ? ` 帧头=[${frame.headerHex}]` : ''
  return `${head}${header} 第${frameNo}帧 订阅轮次=${subscribeRound}`
}
