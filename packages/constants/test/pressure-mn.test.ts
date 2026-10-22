/**
 * T733 方案A（单位链收口）：mN → N 的全仓唯一分母与唯一展示层换算出口。
 *
 * 口径：采集层（BLE 解析）与存储层（DB/API 落库）零换算，raw mN 原样走；
 * ÷MN_PER_N 只允许出现在展示层，且只能经 `nFromMn`（禁各页自写 /1000）。
 * T719 事故根因＝删了入口换算却没补出口换算（346 mN 显示成 346 N）。
 */
import { describe, it, expect } from 'vitest'
import { MN_PER_N, nFromMn } from '../src/index'

describe('MN_PER_N：全仓唯一分母', () => {
  it('= 1000（1 N = 1000 mN，T173 权威口径 mN）', () => {
    expect(MN_PER_N).toBe(1000)
  })
})

describe('nFromMn：展示层唯一换算出口（mN → N）', () => {
  it('T733 卡面基准：设备一帧 346 mN ⇒ 展示 0.346 N（不是 346 N）', () => {
    expect(nFromMn(346)).toBeCloseTo(0.346, 12)
  })

  it('零与负值（零点偏置可为负）原样按比例换算，不夹零', () => {
    expect(nFromMn(0)).toBe(0)
    expect(nFromMn(-200)).toBeCloseTo(-0.2, 12)
  })

  it('常见量级：8000 mN（支具高压力）→ 8 N；5401 mN → 5.401 N', () => {
    expect(nFromMn(8000)).toBeCloseTo(8, 12)
    expect(nFromMn(5401)).toBeCloseTo(5.401, 12)
  })

  it('非有限入参 → NaN（脏值不许退化成 0 N 渲染出去）', () => {
    expect(Number.isNaN(nFromMn(NaN))).toBe(true)
    expect(Number.isNaN(nFromMn(Infinity))).toBe(true)
    expect(Number.isNaN(nFromMn(-Infinity))).toBe(true)
  })

  it('换算只此一处：结果恒等于 mn / MN_PER_N（禁第二条口径线）', () => {
    for (const mn of [1, 123, 999, 1000, 12345]) {
      expect(nFromMn(mn)).toBe(mn / MN_PER_N)
    }
  })
})
