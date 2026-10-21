/**
 * T726 — B512 notify 帧判读单测（技师端）
 *
 * 卡面判据 1 要求的五种帧形：[0] / [10,0] / [0,10] / [] / [10]，外加负码向后兼容、超长帧、
 * ArrayBuffer 入口（真机 res.value 就是 ArrayBuffer，不是 Uint8Array）与日志格式（判据 3）。
 *
 * 为什么测纯函数而不是测 ble.ts：ble.ts 在 vitest 里挂不起来（见 test/b512-resubscribe.spec.ts 头注），
 * 所以帧判读抽到 src/utils/b512-frame.ts，这里直接测它；ble.ts 的接线用文件级结构断言兜（最后一条）。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  B512_FRAME_MAX_LEN,
  B512_KNOWN_CODES,
  formatB512FrameLog,
  parseB512Frame,
} from '../src/utils/b512-frame'

const BLE = fileURLToPath(new URL('../src/utils/ble.ts', import.meta.url))

const frame = (...bytes: number[]) => new Uint8Array(bytes)

describe('T726 — 卡面判据 1：五种帧形', () => {
  it('[0] 单字节 0 → code 0，无帧头、无 warn（旧固件向后兼容）', () => {
    const r = parseB512Frame(frame(0x00))
    expect(r).toMatchObject({ code: 0, rawHex: '00', len: 1, headerHex: '', warn: '' })
  })

  it('[10,0] 新固件两字节帧 → code 0（真值在末字节），帧头 0a 只进日志', () => {
    const r = parseB512Frame(frame(0x0a, 0x00))
    expect(r.code).toBe(0)
    expect(r.rawHex).toBe('0a,00')
    expect(r.len).toBe(2)
    expect(r.headerHex).toBe('0a')
    expect(r.warn).toBe('')
  })

  it('[0,10] 反序帧 → 按末字节判读得 10，标未知（不崩、原始 hex 留住）', () => {
    const r = parseB512Frame(frame(0x00, 0x0a))
    expect(r.code).toBe(10)
    expect(r.rawHex).toBe('00,0a')
    expect(r.headerHex).toBe('00')
    expect(r.warn).toContain('未知状态码 10')
  })

  it('[] 空帧 → code null + warn，绝不派发状态', () => {
    const r = parseB512Frame(frame())
    expect(r.code).toBeNull()
    expect(r.len).toBe(0)
    expect(r.rawHex).toBe('')
    expect(r.warn).toContain('空帧')
  })

  it('[10] 单字节 10 → 判为未知，不得被当成 clear 完成（只有 code 0 是）', () => {
    const r = parseB512Frame(frame(0x0a))
    expect(r.code).toBe(10)
    expect(r.len).toBe(1)
    expect(r.warn).toContain('未知状态码 10')
    expect(B512_KNOWN_CODES).not.toContain(10)
    // clear 判定的唯一真值就是 0：这一条若有人改成「收到帧就算完成」，上面两行会先红
    expect(parseB512Frame(frame(0x0a, 0x00)).code).toBe(0)
  })
})

describe('T726 — 其余帧形与入口', () => {
  it('负码按 int8 解读：[ff]→-1、[fc]→-4、[0a,ff]→-1（多字节同样只判末字节）', () => {
    expect(parseB512Frame(frame(0xff)).code).toBe(-1)
    expect(parseB512Frame(frame(0xfc)).code).toBe(-4)
    expect(parseB512Frame(frame(0x0a, 0xff)).code).toBe(-1)
  })

  it('三字节帧同样取末字节为码，前两个字节都算帧头', () => {
    const r = parseB512Frame(frame(0x01, 0x02, 0x09))
    expect(r.code).toBe(9)
    expect(r.headerHex).toBe('01,02')
    expect(r.rawHex).toBe('01,02,09')
  })

  it(`超过 ${B512_FRAME_MAX_LEN} 字节：判不出来就返回 null 并 warn，不拿猜的字节驱动状态机`, () => {
    const long = new Uint8Array(B512_FRAME_MAX_LEN + 1).fill(0x00)
    const r = parseB512Frame(long)
    expect(r.code).toBeNull()
    expect(r.warn).toContain('异常长度')
    expect(r.rawHex.split(',')).toHaveLength(B512_FRAME_MAX_LEN + 1)
  })

  it('真机入口是 ArrayBuffer：同一帧两种入口结果一致', () => {
    const buf = new Uint8Array([0x0a, 0x00]).buffer
    expect(parseB512Frame(buf)).toMatchObject({ code: 0, rawHex: '0a,00', len: 2 })
  })
})

describe('T726 — 卡面判据 2/3：日志与接线', () => {
  it('日志同条含 raw=[0a,00] 与 code=0，并保留 T216 的「帧 订阅轮次=」可观测性契约', () => {
    const line = formatB512FrameLog(parseB512Frame(frame(0x0a, 0x00)), 3, 2)
    expect(line).toContain('raw=[0a,00]')
    expect(line).toContain('code=0')
    expect(line).toContain('帧头=[0a]')
    expect(line).toContain('帧 订阅轮次=')
  })

  it('单字节帧的日志不出帧头字段（不给固件侧留假线索）', () => {
    expect(formatB512FrameLog(parseB512Frame(frame(0x09)), 1, 1)).not.toContain('帧头=')
  })

  it('ble.ts 不再存在「首字节即状态码」的单一路径，且两个回调都派发判读后的 code', () => {
    const src = fs.readFileSync(BLE, 'utf8')
    expect(src).not.toContain('new Int8Array(res.value)[0]')
    expect(src).toContain('parseB512Frame(res.value)')
    expect(src).toContain('if (frame.code === null) return')
    expect(src).toContain('wifiStatusCallback?.(frame.code)')
    expect(src).toContain('wifiClearCallback?.(frame.code)')
    expect(path.basename(BLE)).toBe('ble.ts')
  })
})
