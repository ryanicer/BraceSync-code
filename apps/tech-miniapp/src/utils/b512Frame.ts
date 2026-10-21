/**
 * T726: B512 notify 帧解析（纯函数）。
 * 新固件回两字节帧 [0x0A,0x00]（10 再 0），旧实现只取 byte0 ⇒ 把 10 当状态码、真值 0 被丢弃，
 * 清除完成/配网初始态判定失败。此处按长度分流：len==1 沿用「byte0 即状态码」的旧语义；
 * len>=2 取最后一个字节为状态码、前置字节作帧头记日志（协议未定义多字节状态字段）。
 * 与 ble.ts 拆开是为了能在 vitest 里直接挂载行为验证（ble.ts 含 #ifdef，挂不起来）。
 */

export interface B512Frame {
  /** 解析出的状态码（有符号字节）；空帧或坏输入为 null */
  code: number | null
  /** 字节数；输入不是 ArrayBuffer/TypedArray 时为 -1 */
  len: number
  /** 原始字节 hex，逗号分隔小写，如 `0a,00` */
  hex: string
  /** 前置字节（帧头/版本）hex；len==1 时为空串 */
  headerHex: string
}

function asInt8(value: unknown): Int8Array | null {
  if (value == null) return null
  if (value instanceof ArrayBuffer) return new Int8Array(value)
  if (ArrayBuffer.isView(value)) {
    const view = value as ArrayBufferView
    return new Int8Array(view.buffer, view.byteOffset, view.byteLength)
  }
  return null
}

function hexByte(byte: number): string {
  const u = byte & 0xff
  return (u < 16 ? '0' : '') + u.toString(16)
}

export function parseB512Frame(value: unknown): B512Frame {
  const bytes = asInt8(value)
  if (!bytes) return { code: null, len: -1, hex: '', headerHex: '' }
  if (bytes.length === 0) return { code: null, len: 0, hex: '', headerHex: '' }

  const parts: string[] = []
  for (let i = 0; i < bytes.length; i++) parts.push(hexByte(bytes[i]))

  return {
    code: bytes[bytes.length - 1],
    len: bytes.length,
    hex: parts.join(','),
    headerHex: parts.slice(0, parts.length - 1).join(','),
  }
}
