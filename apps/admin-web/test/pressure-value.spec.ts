// T322 问题二 —— 压力读数负值归零纯层单测
// 现场依据：staging 卡内所列患者的末次帧 20 点里实测 5 个负值（P05 -0.0426 / P06 -0.0898 /
// P07 -0.0216 / P08 -0.1072 / P17 -0.0266），来自后端逐点减校准基线。夹具照这个形态造。
import { describe, it, expect } from 'vitest'
import { nonNegative, normalizeFramePressure } from '../src/utils/pressureValue'

interface Pt {
  pointId: string
  pressureValue: number
  isMax: boolean
}

const frame = (values: number[]): Pt[] =>
  values.map((v, i) => ({
    pointId: `P${String(i + 1).padStart(2, '0')}`,
    pressureValue: v,
    isMax: v === Math.max(...values),
  }))

describe('nonNegative', () => {
  it('负值归零，正值与 0 原样', () => {
    expect(nonNegative(-0.1072)).toBe(0)
    expect(nonNegative(-42.3)).toBe(0)
    expect(nonNegative(0)).toBe(0)
    expect(nonNegative(4.0)).toBe(4)
  })

  it('NaN 不在这个口径里被洗成 0：数据缺失要能被看见', () => {
    expect(Number.isNaN(nonNegative(NaN))).toBe(true)
  })
})

describe('normalizeFramePressure', () => {
  it('整帧负值全部归零，且读数归零不挪动点位与最大点标记', () => {
    const src = frame([-0.0426, -0.0898, -0.0216, 0.014, -0.1072])
    src[3].isMax = true
    const out = normalizeFramePressure({ pressureHeatmap: src })
    expect(out.pressureHeatmap!.map((p) => p.pressureValue)).toEqual([0, 0, 0, 0.014, 0])
    expect(out.pressureHeatmap!.map((p) => p.pointId)).toEqual(['P01', 'P02', 'P03', 'P04', 'P05'])
    expect(out.pressureHeatmap!.find((p) => p.isMax)!.pointId).toBe('P04')
  })

  it('不就地改入参：拉到的原始快照要保持原样可对账', () => {
    const src = frame([-0.1, 5])
    normalizeFramePressure({ pressureHeatmap: src })
    expect(src[0].pressureValue).toBe(-0.1)
  })

  it('本帧无负值时原样返回同一对象（每秒轮询不重建 20 点数组）', () => {
    const snap = { pressureHeatmap: frame([0, 4.0, 0.014]) }
    expect(normalizeFramePressure(snap)).toBe(snap)
  })

  it('热力图缺失或为空数组时不报错，且不动其它字段', () => {
    expect(normalizeFramePressure({})).toEqual({})
    const empty = { pressureHeatmap: [] as Pt[], todayHours: 6.5 }
    expect(normalizeFramePressure(empty)).toBe(empty)
    expect(normalizeFramePressure(empty).todayHours).toBe(6.5)
  })

  it('归零后按页面口径取最大值：帧内只剩负值时曲线取到 0 而不是负数', () => {
    const out = normalizeFramePressure({ pressureHeatmap: frame([-0.02, -0.2, -0.005]) })
    const max = out.pressureHeatmap!.reduce((m, p) => Math.max(m, p.pressureValue), 0)
    expect(max).toBe(0)
  })
})
