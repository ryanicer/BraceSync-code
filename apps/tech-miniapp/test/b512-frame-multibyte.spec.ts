/**
 * T726 — 技师端 B512「10 再 0」多字节帧兼容
 *
 * 卡面要求 1/4：按长度分流 + 单测覆盖 [0]/[10,0]/[0,10]/[]/[10] 五种帧形。
 * 行为验证挂在纯模块 utils/b512Frame.ts 上（utils/ble.ts 含 #ifdef，vitest 里挂不起来，
 * 见 b512-resubscribe.spec.ts 同族说明）；ble.ts 侧只做结构断言，钉住「不再首字节即状态码」
 * 与「raw 与 code 同条日志」。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'
import { parseB512Frame } from '../src/utils/b512Frame'

const buf = (bytes: number[]) => new Uint8Array(bytes).buffer

describe('T726 — 五种帧形的解析行为', () => {
  it('[0] 单字节：沿用旧语义，byte0 即状态码', () => {
    const f = parseB512Frame(buf([0x00]))
    expect(f.code).toBe(0)
    expect(f.len).toBe(1)
    expect(f.hex).toBe('00')
    expect(f.headerHex).toBe('')
  })

  it('[10,0] 新固件两字节帧：真值 0 落在末字节，10 记为帧头', () => {
    const f = parseB512Frame(buf([0x0a, 0x00]))
    expect(f.code).toBe(0)
    expect(f.len).toBe(2)
    expect(f.hex).toBe('0a,00')
    expect(f.headerHex).toBe('0a')
  })

  it('[0,10] 反序帧：末字节 10 为码（不是 clear 完成）', () => {
    const f = parseB512Frame(buf([0x00, 0x0a]))
    expect(f.code).toBe(10)
    expect(f.hex).toBe('00,0a')
    expect(f.headerHex).toBe('00')
  })

  it('[] 空帧：不可解析，不抛异常', () => {
    const f = parseB512Frame(buf([]))
    expect(f.code).toBeNull()
    expect(f.len).toBe(0)
  })

  it('[10] 单字节 10：视为状态码 10，不得被判成 clear 完成（clear 只认 0）', () => {
    const f = parseB512Frame(buf([0x0a]))
    expect(f.code).toBe(10)
    expect(f.code === 0).toBe(false)
  })

  it('三字节及以上：末字节为码，其余全记帧头', () => {
    const f = parseB512Frame(buf([0x0a, 0x01, 0xff]))
    expect(f.code).toBe(-1)
    expect(f.hex).toBe('0a,01,ff')
    expect(f.headerHex).toBe('0a,01')
  })
})

describe('T726 — 向后兼容：负状态码与坏输入', () => {
  it('配网失败码 -1/-2/-3/-4 单字节仍是同号（有符号解析，不被当成 255/254…）', () => {
    expect(parseB512Frame(buf([0xff])).code).toBe(-1)
    expect(parseB512Frame(buf([0xfe])).code).toBe(-2)
    expect(parseB512Frame(buf([0xfd])).code).toBe(-3)
    expect(parseB512Frame(buf([0xfc])).code).toBe(-4)
  })

  it('两字节帧里的负码：末字节有符号取值', () => {
    expect(parseB512Frame(buf([0x0a, 0xff])).code).toBe(-1)
  })

  it('坏输入（null/undefined/普通对象/数字）不崩溃，一律 code=null', () => {
    for (const bad of [null, undefined, {}, 0, '00', []]) {
      const f = parseB512Frame(bad)
      expect(f.code, `bad input: ${String(bad)}`).toBeNull()
    }
  })

  it('带偏移的 TypedArray 视图按视图窗口取值，不误读整块 buffer', () => {
    const whole = new Uint8Array([0x7f, 0x7f, 0x0a, 0x00])
    const view = whole.subarray(2)
    const f = parseB512Frame(view)
    expect(f.hex).toBe('0a,00')
    expect(f.code).toBe(0)
  })
})

describe('T726 — ble.ts 收帧处的结构与日志口径', () => {
  const src = fs.readFileSync(fileURLToPath(new URL('../src/utils/ble.ts', import.meta.url)), 'utf8')

  it('验收 2：不再有「只取 byte0 当状态码」的单一路径', () => {
    expect(src).not.toContain('new Int8Array(res.value)[0]')
    expect(src).toContain('parseB512Frame(res.value)')
  })

  it('验收 3：raw 与 code 同条日志，且保留帧序/轮次', () => {
    expect(src).toContain('B512 raw=[${frame.hex}] len=${frame.len} → code=${code}')
    expect(src).toContain('帧 订阅轮次=')
  })

  it('验收 要求 3：两个回调的触发条件与时机不变（仍同帧同码分发，clear 仍只认 0）', () => {
    expect(src).toContain('wifiStatusCallback?.(code)')
    expect(src).toContain('wifiClearCallback?.(code)')
    expect(src).toContain('if (code === 0) {')
  })

  it('空帧/坏输入走 warn 分支且不分发回调', () => {
    const at = src.indexOf('if (frame.code === null) {')
    expect(at).toBeGreaterThan(-1)
    const branch = src.slice(at, src.indexOf('const code = frame.code', at))
    expect(branch.length).toBeGreaterThan(0)
    expect(branch).toContain('bleLog.warn(')
    expect(branch).toContain('return')
    expect(branch).not.toContain('Callback?.')
  })
})
