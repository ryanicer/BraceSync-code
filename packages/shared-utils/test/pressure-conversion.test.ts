/**
 * T662（T643 二期）：编辑输入/配置表单场景的 N ↔ kPa 换算。
 *
 * 公式必须与后端 data-service `model.KpaFromN` 逐字对齐：
 *   kPa = round( max(0, N) / area_cm2 * 10 )
 *
 * 🔴 本模块只许编辑/配置表单调用；展示层一律读后端派生值，见 pressureConversion.ts
 * 顶部使用约束。
 */
import { describe, it, expect } from 'vitest'
import { kpaFromN, nFromKpa, DEFAULT_SENSOR_AREA_CM2, editingValueText } from '../src/index'

describe('kpaFromN：与后端 KpaFromN 同公式（归零 → 除面积乘 10 → 四舍五入取整）', () => {
  it('默认面积 0.64 cm² 下的参考换算表（PRD §7A.2.1 三）', () => {
    const A = DEFAULT_SENSOR_AREA_CM2
    // 同 PRD 表格：1N→16, 2N→31, 4N→63, 6N→94
    expect(kpaFromN(1, A)).toBe(16)
    expect(kpaFromN(2, A)).toBe(31)
    expect(kpaFromN(4, A)).toBe(63)
    expect(kpaFromN(6, A)).toBe(94)
  })

  it('归零：负值按 0 算（零点漂移不折算成负压力）', () => {
    expect(kpaFromN(-1, 0.64)).toBe(0)
    expect(kpaFromN(-0.5, 0.64)).toBe(0)
    expect(kpaFromN(0, 0.64)).toBe(0)
  })

  it('面积缺失/非法 → null（fail-closed，不许退化成 0）', () => {
    expect(kpaFromN(10, null)).toBeNull()
    expect(kpaFromN(10, undefined)).toBeNull()
    expect(kpaFromN(10, 0)).toBeNull()
    expect(kpaFromN(10, -0.5)).toBeNull()
    expect(kpaFromN(10, NaN)).toBeNull()
    expect(kpaFromN(10, Infinity)).toBeNull()
  })

  it('N 非有限 → null', () => {
    expect(kpaFromN(NaN, 0.64)).toBeNull()
    expect(kpaFromN(Infinity, 0.64)).toBeNull()
  })

  it('1 cm² 面积：10N = 100 kPa（量纲手算验证）', () => {
    expect(kpaFromN(10, 1)).toBe(100)
    expect(kpaFromN(1, 1)).toBe(10)
  })

  it('四舍五入：round half up', () => {
    // 0.64 面积下，3.0N = 46.875 → 47
    expect(kpaFromN(3, 0.64)).toBe(47)
    // 0.64 面积下，0.5N = 7.8125 → 8
    expect(kpaFromN(0.5, 0.64)).toBe(8)
  })
})

describe('nFromKpa：反向换算（kPa → N，保留精度，不归零）', () => {
  it('与 kpaFromN 大致可逆（16 kPa → N → kPa 仍 16）', () => {
    const A = DEFAULT_SENSOR_AREA_CM2
    const n = nFromKpa(16, A)
    expect(n).not.toBeNull()
    expect(kpaFromN(n!, A)).toBe(16)
  })

  it('面积缺失/非法 → null', () => {
    expect(nFromKpa(100, null)).toBeNull()
    expect(nFromKpa(100, 0)).toBeNull()
    expect(nFromKpa(100, NaN)).toBeNull()
  })

  it('负值 kPa → null（物理上非负）', () => {
    expect(nFromKpa(-1, 0.64)).toBeNull()
  })

  it('1 cm² 面积：100 kPa = 10 N', () => {
    expect(nFromKpa(100, 1)).toBeCloseTo(10, 6)
  })

  it('0 kPa → 0 N（真零还是零）', () => {
    expect(nFromKpa(0, 0.64)).toBe(0)
  })
})

describe('editingValueText：编辑场景的数字+单位文本', () => {
  it('N 档原样格式化（保留指定小数位）', () => {
    expect(editingValueText('N', 45.0, 0.64, 1)).toBe('45.0 N')
    expect(editingValueText('N', 45, 0.64, 0)).toBe('45 N')
  })

  it('kPa 档按面积换算后显示整数 kPa', () => {
    expect(editingValueText('kPa', 45, 0.64)).toBe('703 kPa')
  })

  it('面积缺失时 kPa 档显示 --（fail-closed）', () => {
    expect(editingValueText('kPa', 45, null)).toBe('--')
  })
})
