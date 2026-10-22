/**
 * T675 — 空载校准纯函数单测
 * 纯函数下沉到 apps/tech-miniapp/src/utils/calibration.ts
 * 覆盖：帧完整性、零点偏移计算、故障哨兵（dead/saturated）、偏移摘要、哨兵文案。
 *
 * T733（方案A·单位链）：帧与偏移量全程 **raw mN**（采集/存储零换算），
 * 故本文件的数字全部改写成 mN 量级（1500 mN = 1.5 N），饱和阈值常量改名 CALIBRATION_SATURATED_MN。
 */
import { describe, it, expect } from 'vitest'
import {
  CALIBRATION_FRAME_COUNT,
  CALIBRATION_POINT_COUNT,
  CALIBRATION_FRAME_TIMEOUT_MS,
  CALIBRATION_SATURATED_MN,
  isValidCalibrationFrame,
  isCalibrationComplete,
  computeCalibrationOffsets,
  detectCalibrationSentinels,
  summarizeCalibrationOffsets,
  calibrationSentinelText,
} from '../src/utils/calibration'

// 辅助：生成指定通道全零帧（20 点全为 0
const zeroFrame = (): number[] => Array(CALIBRATION_POINT_COUNT).fill(0)
// 辅助：指定通道设值，其余默认 value
const frameWith = (channel: number, value: number, rest: number = 0): number[] => {
  const f = Array(CALIBRATION_POINT_COUNT).fill(rest)
  f[channel - 1] = value
  return f
}

describe('CALIBRATION_* 常量', () => {
  it('帧数 帧数 = 5（B513 1Hz 采集 5 秒稳态', () => {
    expect(CALIBRATION_FRAME_COUNT).toBe(5)
  })
  it('点数 = 20（P01..P20）', () => {
    expect(CALIBRATION_POINT_COUNT).toBe(20)
  })
  it('帧到达超时 = 10000ms（T675 B513 零帧 10s 看门狗）', () => {
    expect(CALIBRATION_FRAME_TIMEOUT_MS).toBe(10_000)
  })
  it('哨兵饱和阈值 = 10000 mN（＝10 N，只提示不拦截；T733 起帧为 mN 空间）', () => {
    expect(CALIBRATION_SATURATED_MN).toBe(10_000)
  })
})

describe('isValidCalibrationFrame', () => {
  it('20 个有限数 = 合法', () => {
    expect(isValidCalibrationFrame(Array(20).fill(1.5))).toBe(true)
  })
  it('长度 19 = 不合法', () => {
    expect(isValidCalibrationFrame(Array(19).fill(0))).toBe(false)
  })
  it('长度 21 = 不合法', () => {
    expect(isValidCalibrationFrame(Array(21).fill(0))).toBe(false)
  })
  it('含 NaN = 不合法（不静默补 0', () => {
    const f = Array(20).fill(1)
    f[3] = NaN
    expect(isValidCalibrationFrame(f)).toBe(false)
  })
  it('含 Infinity = 不合法', () => {
    const f = Array(20).fill(1)
    f[3] = Infinity
    expect(isValidCalibrationFrame(f)).toBe(false)
  })
  it('非数组 = 不合法', () => {
    expect(isValidCalibrationFrame(null)).toBe(false)
    expect(isValidCalibrationFrame(undefined)).toBe(false)
    expect(isValidCalibrationFrame({ length: 20 } as unknown)).toBe(false)
  })
})

describe('isCalibrationComplete', () => {
  it('5 帧 × 20 点全合法 = 完整', () => {
    const frames = Array(5).fill(null).map(() => Array(20).fill(1))
    expect(isCalibrationComplete(frames)).toBe(true)
  })
  it('4 帧 = 不完整', () => {
    const frames = Array(4).fill(null).map(() => Array(20).fill(1))
    expect(isCalibrationComplete(frames)).toBe(false)
  })
  it('5 帧里有 1 帧残缺 = 不完整', () => {
    const frames = Array(5).fill(null).map(() => Array(20).fill(1))
    frames[2][3] = NaN
    expect(isCalibrationComplete(frames)).toBe(false)
  })
})

describe('computeCalibrationOffsets', () => {
  it('5 帧全 0 = 偏移全 0', () => {
    const frames = Array(5).fill(null).map(() => Array(20).fill(0))
    expect(computeCalibrationOffsets(frames)).toEqual(Array(20).fill(0))
  })
  it('各通道跨帧平均', () => {
    // 第 1 通道 1,2,3,4,5 → 平均 3
    const frames: number[][] = []
    for (let i = 0; i < 5; i++) {
      const f = Array(20).fill(0)
      f[0] = i + 1
      frames.push(f)
    }
    const offsets = computeCalibrationOffsets(frames)
    expect(offsets[0]).toBeCloseTo(3, 5)
    expect(offsets[1]).toBeCloseTo(0, 5)
  })
  it('帧不完整时抛错不许拿残缺帧算假偏移', () => {
    const frames = Array(3).fill(null).map(() => Array(20).fill(1))
    expect(() => computeCalibrationOffsets(frames)).toThrow(/校准帧不完整/)
  })
  it('任何稳定偏置都如实记录不设偏移量门禁已废除 T675', () => {
    // 治具应力下空载 P01 约 1500 mN（=1.5N）也属正常稳定偏置——以前会被门禁挡掉，现在必须算成功
    const frames = Array(5).fill(null).map(() => {
      const f = Array(20).fill(200) // 200 mN = 0.2 N
      f[0] = 1500 // 1500 mN = 1.5 N
      return f
    })
    const offsets = computeCalibrationOffsets(frames)
    expect(offsets[0]).toBeCloseTo(1500, 5)
    expect(offsets[1]).toBeCloseTo(200, 5)
  })
})

describe('detectCalibrationSentinels', () => {
  it('无异常无哨兵', () => {
    const frames = Array(5).fill(null).map(() => Array(20).fill(1))
    expect(detectCalibrationSentinels(frames)).toEqual([])
  })
  it('整轮恒 0 = dead', () => {
    const frames = Array(5).fill(null).map(() => {
      const f = Array(20).fill(1)
      f[3] = 0
      return f
    })
    const result = detectCalibrationSentinels(frames)
    expect(result).toContainEqual({ channel: 4, reason: 'dead' })
    expect(result.length).toBe(1)
  })
  it('饱和超量程 = saturated（帧为 mN 空间：12500 mN ＝ 12.5 N > 10000 mN）', () => {
    const frames = Array(5).fill(null).map(() => Array(20).fill(1000))
    frames[2][7] = 12500  // 第 3 帧 P08 = 12500 mN（12.5N）> 10000 mN（10N）
    const result = detectCalibrationSentinels(frames)
    expect(result).toContainEqual({ channel: 8, reason: 'saturated' })
    expect(result.length).toBe(1)
  })
  it('恒 0 与超量程互斥 0 不超量程', () => {
    // 整轮 0 只归 dead，不归 saturated
    const frames = Array(5).fill(null).map(() => Array(20).fill(0))
    const result = detectCalibrationSentinels(frames)
    expect(result.every((s) => s.reason === 'dead')).toBe(true)
    expect(result.length).toBe(20)
  })
  it('自定义饱和阈值（mN）', () => {
    // 其他通道给 1000 mN（1N）避免被 dead 捕获
    const frames = [frameWith(1, 5000, 1000)]
    // 默认 10000 mN（10N）阈值下 5000 mN 不触发
    expect(detectCalibrationSentinels(frames)).toEqual([])
    // 阈值改成 4000 mN（4N）→ 触发 saturated
    expect(detectCalibrationSentinels(frames, 4000)).toContainEqual({ channel: 1, reason: 'saturated' })
  })
})

describe('summarizeCalibrationOffsets', () => {
  it('全 0 = 全 0', () => {
    const r = summarizeCalibrationOffsets(Array(20).fill(0))
    expect(r.maxAbs).toBe(0)
    expect(r.meanAbs).toBe(0)
  })
  it('最大偏移定位正确', () => {
    const offsets = Array(20).fill(0.3)
    offsets[4] = -2.5  // P05 = -2.5
    const r = summarizeCalibrationOffsets(offsets)
    expect(r.maxAbs).toBeCloseTo(2.5, 5)
    expect(r.maxChannel).toBe(5)
  })
  it('均值正确', () => {
    const offsets = Array(20).fill(0)
    offsets[0] = 1
    offsets[1] = 3
    const r = summarizeCalibrationOffsets(offsets)
    expect(r.meanAbs).toBeCloseTo(4 / 20, 5)
  })
  it('长度不对抛错', () => {
    expect(() => summarizeCalibrationOffsets([1, 2, 3])).toThrow()
  })
})

describe('calibrationSentinelText', () => {
  it('空 = 空串（调用方隐藏警示条）', () => {
    expect(calibrationSentinelText([])).toBe('')
  })
  it('单通道文案（逐字 = 卡面口径）', () => {
    expect(calibrationSentinelText([{ channel: 3, reason: 'dead' }])).toBe('通道 03 读数异常，请检查传感器')
  })
  it('多通道用顿号连接', () => {
    expect(calibrationSentinelText([
      { channel: 1, reason: 'dead' },
      { channel: 5, reason: 'saturated' },
      { channel: 20, reason: 'dead' },
    ])).toBe('通道 01、05、20 读数异常，请检查传感器')
  })
})
