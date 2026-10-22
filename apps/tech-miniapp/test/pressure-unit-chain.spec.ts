/**
 * T733（方案A·单位链收口）— mN 采集 → mN 存储 → N/kPa 只在展示层换算一次，端到端钉死。
 *
 * 卡面基准值：设备一帧 P01 = 346（int16 mN）。
 *   采集层（utils/b513-frame.ts 解码）必须交付 346 原值；
 *   存储层（POST /baselines 的 offsetValues）必须仍是 346；
 *   展示层（utils/pressureDisplay.ts）必须渲染 0.346 N / 5 kPa，而不是 346 N / 5406 kPa。
 *
 * utils/ble.ts 在 vitest 里挂不起来（logger #ifdef 双声明，见 test/t218-ble-robustness.spec.ts 头注），
 * 故解码腿走新下沉的纯函数 b513-frame.ts，接线用源码结构断言兜（同 install-status.spec.ts 写法）。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'
import { nFromMn, MN_PER_N } from '@bracesync/constants'
import { kpaFromN, DEFAULT_SENSOR_AREA_CM2 } from '@bracesync/shared-utils'
import { B513_FRAME_BYTES, decodeB513Frame, toInt16 } from '../src/utils/b513-frame'
import {
  CALIBRATION_SATURATED_MN,
  computeCalibrationOffsets,
  detectCalibrationSentinels,
} from '../src/utils/calibration'
import { calibCellText, calibPressureLevel, calibStatText, mnToNText } from '../src/utils/pressureDisplay'

vi.mock('../src/utils/request', () => {
  const request = vi.fn()
  return { request, USE_MOCK: false }
})
import { request } from '../src/utils/request'
import { saveBaseline } from '../src/api/baseline'

function srcOf(rel: string): string {
  return fs.readFileSync(fileURLToPath(new URL(rel, import.meta.url)), 'utf8')
}

/** 20 点 int16（mN）→ 40 字节小端线序 */
function encodeMnFrame(vals: number[]): number[] {
  const bytes: number[] = []
  for (const v of vals) {
    const u = v < 0 ? v + 65536 : v
    bytes.push(u & 0xff, (u >> 8) & 0xff)
  }
  return bytes
}

const MN_346_FRAME = Array.from({ length: 20 }, (_, i) => (i === 0 ? 346 : 0))

describe('① 采集层零换算：B513 解码交付 raw mN', () => {
  it('346 mN 上板 → 解码出 346（不是 0.346，不再 ÷1000）', () => {
    const frame = decodeB513Frame(encodeMnFrame(MN_346_FRAME))
    expect(frame).not.toBeNull()
    expect(frame).toHaveLength(20)
    expect(frame![0]).toBe(346)
  })

  it('负偏置（int16 回绕）原样还原，不被夹零也不被换算', () => {
    const frame = decodeB513Frame(encodeMnFrame(Array.from({ length: 20 }, (_, i) => (i === 0 ? -346 : 0))))
    expect(frame![0]).toBe(-346)
    expect(toInt16(0xffff)).toBe(-1)
  })

  it('不足 40 字节回 null（拿残包凑一帧＝假数）', () => {
    expect(decodeB513Frame([0x5a, 0x01])).toBeNull()
    expect(B513_FRAME_BYTES).toBe(40)
  })

  it('接线铁证：ble.ts 采集腿不再出现 ÷MN_PER_N，解码改走 b513-frame', () => {
    const ble = srcOf('../src/utils/ble.ts')
    expect(ble).toContain('decodeB513Frame(frameBytes)')
    expect(ble).not.toMatch(/signed\s*\/\s*MN_PER_N/)
    expect(ble).not.toContain("frame.push(signed / MN_PER_N)")
    expect(ble).not.toContain('@bracesync/constants')
  })
})

describe('② 存储层零换算：5 帧均值与 POST /baselines 都是 raw mN', () => {
  it('5 帧 × P01=346 mN ⇒ 偏移量 346（mN 原值，均值不跨空间）', () => {
    const frames = Array.from({ length: 5 }, () => [...MN_346_FRAME])
    const offsets = computeCalibrationOffsets(frames)
    expect(offsets[0]).toBe(346)
  })

  it('POST /baselines 的 offsetValues 落的是 raw mN（字段名是契约，值是 mN）', async () => {
    request.mockReset().mockResolvedValue({ baselineId: 'BSL-1' })
    const frames = Array.from({ length: 5 }, () => [...MN_346_FRAME])
    const offsetsMn = computeCalibrationOffsets(frames)
    await saveBaseline('INS-001', offsetsMn, 'DEV-001')

    expect(request).toHaveBeenCalledTimes(1)
    const arg = request.mock.calls[0][0] as { url: string; method?: string; data: Record<string, unknown> }
    expect(arg.url).toBe('/api/v1/baselines')
    expect(arg.method).toBe('POST')
    expect(arg.data.offsetValues).toEqual(offsetsMn)
    expect((arg.data.offsetValues as number[])[0]).toBe(346)
    // 反证：客户端若先 ÷1000，库里就会落进 N 空间（T733 要修的错位）
    expect((arg.data.offsetValues as number[])[0]).not.toBe(346 / MN_PER_N)
  })

  it('故障哨兵阈值同在 mN 空间：12500 mN 触发、346 mN 不触发', () => {
    expect(CALIBRATION_SATURATED_MN).toBe(10_000) // ＝ 10 N
    const ok = Array.from({ length: 5 }, () => [...MN_346_FRAME])
    expect(detectCalibrationSentinels(ok).some((s) => s.reason === 'saturated' && s.channel === 1)).toBe(false)
    const hot = Array.from({ length: 5 }, () => Array(20).fill(1000))
    hot[1][0] = 12500
    expect(detectCalibrationSentinels(hot)).toContainEqual({ channel: 1, reason: 'saturated' })
  })
})

describe('③ 展示层唯一换算：346 mN ⇒ 0.346 N ⇒ 5 kPa（不是 346 N / 5406 kPa）', () => {
  it('nFromMn 是唯一分母出口', () => {
    expect(nFromMn(346)).toBeCloseTo(0.346, 12)
    expect(MN_PER_N).toBe(1000)
  })

  it('矩阵单元格 N 档：0.35（两位小数），kPa 档：5', () => {
    const A = DEFAULT_SENSOR_AREA_CM2 // 0.64
    expect(mnToNText(346)).toBe('0.35')
    expect(calibCellText(346, 'N', A)).toBe('0.35')
    expect(calibCellText(346, 'kPa', A)).toBe('5')
    expect(kpaFromN(nFromMn(346), A)).toBe(5)
    // 反证：未换算直接喂 kpaFromN 就是卡面说的「5401 kPa」事故形
    expect(kpaFromN(346, A)).toBeGreaterThan(5000)
  })

  it('统计行带单位字母：0.35N / 5kPa', () => {
    expect(calibStatText(346, 'N', DEFAULT_SENSOR_AREA_CM2)).toBe('0.35N')
    expect(calibStatText(346, 'kPa', DEFAULT_SENSOR_AREA_CM2)).toBe('5kPa')
  })

  it('判色恒按 N（阈值不随档、也不随 mN 量级漂）', () => {
    expect(calibPressureLevel(346)).toBe(0) // 0.346 N → 最低档（<0.5N）
    expect(calibPressureLevel(1500)).toBe(2) // 1.5 N → <2N 档
    expect(calibPressureLevel(6000)).toBe(4) // 6 N → <10N 档
    expect(calibPressureLevel(-6000)).toBe(4) // 负偏置按绝对值判色
    expect(calibPressureLevel(25000)).toBe(6) // 25 N → 顶档
  })

  it('脏值（非有限 mN）出「--」，不退化成 0.00 N', () => {
    expect(mnToNText(NaN)).toBe('--')
    expect(calibCellText(NaN, 'N', DEFAULT_SENSOR_AREA_CM2)).toBe('--')
    expect(calibStatText(Infinity, 'N', DEFAULT_SENSOR_AREA_CM2)).toBe('--')
  })

  it('接线铁证：÷1000 只出现在 utils/pressureDisplay.ts（各页禁自写 /1000）', () => {
    const dir = fileURLToPath(new URL('../src', import.meta.url))
    /** 只看会执行的行：注释里提「nFromMn」是解释口径，不算第二条换算腿 */
    const codeLinesUsing = (text: string): number =>
      text
        .split(/\r?\n/)
        .map((l) => l.trim())
        .filter((l) => l !== '' && !l.startsWith('//') && !l.startsWith('*') && !l.startsWith('/*') && !l.startsWith('<!--'))
        .filter((l) => /nFromMn|\/\s*MN_PER_N/.test(l)).length
    const hits: string[] = []
    const walk = (d: string) => {
      for (const e of fs.readdirSync(d, { withFileTypes: true })) {
        const p = `${d}/${e.name}`
        if (e.isDirectory()) walk(p)
        else if (/\.(ts|vue)$/.test(e.name) && codeLinesUsing(srcOf(p)) > 0) hits.push(p.replace(dir, ''))
      }
    }
    walk(dir)
    expect(hits.sort()).toEqual(['/utils/pressureDisplay.ts'])
    // 安装页渲染腿全部经 pressureDisplay（模板不再自己 toFixed 原始 mN）
    const page = srcOf('../src/pages/install/index.vue')
    expect(page).toContain("from '../../utils/pressureDisplay'")
    expect(page).toContain('{{ mnToNText(offsetSummary.maxAbs) }}N')
    expect(page).not.toContain('offsetSummary.maxAbs.toFixed')
    expect(page).toContain('offsetValues: offsetsMn')
  })
})
