/**
 * T513 热力图双单位 —— 显示层判据（患者端 / admin 端共用的真源）。
 *
 * 这一层的存在理由：换算只在 data-service（T508 `model.KpaFromN`），前端只渲染后端
 * 在同一快照响应里下发的派生值。所以本文件的判据分两类：
 *  ① 「怎么写」（缺值必须 `--`、不许退化成 0；N 档文本必须逐字不动）；
 *  ② 「不能做什么」（本模块不得出现任何换算式）—— 用一条源码级门禁钉住，
 *     防后来人在显示层顺手补一个 `/area*10`，那就是第二条口径线（T203 同型坑）。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import {
  PRESSURE_UNITS,
  DEFAULT_PRESSURE_UNIT,
  AREA_MISSING_HINT,
  normalizePressureUnit,
  pickStartUnit,
  kpaNumberText,
  unitNumberText,
  unitValueText,
  areaHintVisible,
  heroRangeHintVisible,
  axisUnitText,
  kpaObservedMax,
  heroRangeText,
} from '../src/index'

const srcOf = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), 'utf8')

describe('档位词表：只有稿面两个拼写是合法档', () => {
  it('段序与默认档 = 稿面（N 在左、默认 N）', () => {
    expect([...PRESSURE_UNITS]).toEqual(['N', 'kPa'])
    expect(DEFAULT_PRESSURE_UNIT).toBe('N')
  })

  it('normalizePressureUnit 认两个合法拼写', () => {
    expect(normalizePressureUnit('N')).toBe('N')
    expect(normalizePressureUnit('kPa')).toBe('kPa')
  })

  it('大小写/单位别名/脏值一律不认（不能把 ' + "'KN'" + " 当 kPa）", () => {
    for (const raw of ['n', 'kn', 'kpa', 'KG', 'Pa', '', ' ', 'N ', 'NaN', 0, 1, null, undefined, {}, [], true]) {
      expect(normalizePressureUnit(raw), `normalize(${JSON.stringify(raw)}) 应为 null`).toBeNull()
    }
  })

  it('pickStartUnit：合法记忆档生效，脏值/缺值回默认 N', () => {
    expect(pickStartUnit('kPa')).toBe('kPa')
    expect(pickStartUnit('N')).toBe('N')
    expect(pickStartUnit('KG')).toBe(DEFAULT_PRESSURE_UNIT)
    expect(pickStartUnit(null)).toBe(DEFAULT_PRESSURE_UNIT)
    expect(pickStartUnit(undefined)).toBe(DEFAULT_PRESSURE_UNIT)
    expect(pickStartUnit('')).toBe(DEFAULT_PRESSURE_UNIT)
  })
})

describe('kpaNumberText：fail-closed 只出 --，不许退化成 0（PRD 五.3）', () => {
  it('缺失态四种写法都归 --（null / undefined / NaN / Infinity）', () => {
    expect(kpaNumberText(null)).toBe('--')
    expect(kpaNumberText(undefined)).toBe('--')
    expect(kpaNumberText(Number.NaN)).toBe('--')
    expect(kpaNumberText(Number.POSITIVE_INFINITY)).toBe('--')
    expect(kpaNumberText(Number.NEGATIVE_INFINITY)).toBe('--')
  })

  it('真零压力仍是 0 —— 与「配置缺失」是两个语义，不能一起吞成 --', () => {
    expect(kpaNumberText(0)).toBe('0')
    expect(kpaNumberText(1)).toBe('1')
    expect(kpaNumberText(66)).toBe('66')
  })

  it('-- 是两字符占位，不是空串（空串在表格里看不出「有格子但不可换算」）', () => {
    expect(kpaNumberText(null)).not.toBe('')
    expect(kpaNumberText(null)).not.toBe('0')
  })
})

describe('unitNumberText：只改「怎么写」，N 档文本必须逐字不动（PRD 四.2 现状读数不变）', () => {
  it('N 档原样返回调用方格式化好的文本，不重排小数位、不加字母', () => {
    expect(unitNumberText('N', '42.2', 66)).toBe('42.2')
    expect(unitNumberText('N', '0.0', 0)).toBe('0.0')
    expect(unitNumberText('N', '--', null)).toBe('--')
    expect(unitNumberText('N', '42.18 N', 66)).toBe('42.18 N')
  })

  it('kPa 档只读后端派生值，与 N 档文本无关', () => {
    expect(unitNumberText('kPa', '42.2', 66)).toBe('66')
    expect(unitNumberText('kPa', '42.2', null)).toBe('--')
    expect(unitNumberText('kPa', '42.2', undefined)).toBe('--')
  })

  it('反证：kPa 档不得把 N 档文本当兜底（面积缺失时留 N 读数＝双单位假绿）', () => {
    expect(unitNumberText('kPa', '42.2', null)).not.toBe('42.2')
    expect(unitNumberText('kPa', '42.2', null)).not.toContain('42.2')
  })
})

describe('unitValueText：带单位字母的文本（admin 摘要 / 详情行 / 悬浮 title）', () => {
  it('kPa 可换算时挂字母', () => {
    expect(unitValueText('kPa', '4.22 N', 66)).toBe('66 kPa')
    expect(unitValueText('kPa', '0.00 N', 0)).toBe('0 kPa')
  })

  it('不可换算时只出 --，不再挂单位字母（稿面 217 行同形：-- 后不带单位）', () => {
    expect(unitValueText('kPa', '4.22 N', null)).toBe('--')
    expect(unitValueText('kPa', '4.22 N', undefined)).toBe('--')
  })

  it('N 档原样返回', () => {
    expect(unitValueText('N', '4.22 N', 66)).toBe('4.22 N')
  })
})

describe('areaHintVisible：提示行与格子数字必须同源（不许「提示说有、数字说无」）', () => {
  it('kPa 档 + 派生上界缺失 ⇒ 出提示', () => {
    expect(areaHintVisible('kPa', null)).toBe(true)
    expect(areaHintVisible('kPa', undefined)).toBe(true)
  })

  it('kPa 档 + 派生上界在位 ⇒ 不出提示', () => {
    expect(areaHintVisible('kPa', 94)).toBe(false)
    expect(areaHintVisible('kPa', 0)).toBe(false)
  })

  it('N 档永不出提示（N 读数不依赖面积，提示在此档是噪声）', () => {
    expect(areaHintVisible('N', null)).toBe(false)
    expect(areaHintVisible('N', undefined)).toBe(false)
  })
})

describe('heroRangeHintVisible：hero 正常范围副文案只在 N 档（Boss 09-30 裁定 c）', () => {
  it('N 档保留、kPa 档隐藏', () => {
    expect(heroRangeHintVisible('N')).toBe(true)
    expect(heroRangeHintVisible('kPa')).toBe(false)
  })
})

describe('T643 纵轴随档：轴字母与 kPa 档轴上界（PRD V3.44 把趋势纵轴移入随档侧）', () => {
  it('axisUnitText 两档各一个拼写，与档位词表逐字同形', () => {
    expect(axisUnitText('N')).toBe('N')
    expect(axisUnitText('kPa')).toBe('kPa')
  })

  it('kpaObservedMax 只认后端派生的 kPa 数值：逐点与上界取大者', () => {
    expect(kpaObservedMax([47, 63, 12], null)).toBe(63)
    expect(kpaObservedMax([47, 12], 94)).toBe(94)
    expect(kpaObservedMax([null, undefined, NaN, -5, 0], null)).toBe(null)
  })

  it('一个可用值都没有 ⇒ null（调用方出「--」），有 0 也不算可用（0 不是上界）', () => {
    expect(kpaObservedMax([null, null])).toBe(null)
    expect(kpaObservedMax([0, 0], 0)).toBe(null)
    // 反面：真有一个正派生值就必须被拿到，不许因为混进 null 就整窗判 null
    expect(kpaObservedMax([null, 3, null])).toBe(3)
  })

  it('门禁：本模块新增的这条腿同样不许出现换算式', () => {
    const src = srcOf('../src/pressureUnit.ts')
    expect(src).toMatch(/export function kpaObservedMax/)
    expect(src).not.toMatch(/contactAreaCm2/)
    expect(src).not.toMatch(/areaCm2/)
    expect(src).not.toMatch(/\*[^\S\n]*10\b/)
    expect(src).not.toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
  })
})

// T601：文案不再写死 20-60N —— 数值由快照下发的配置边界派生，断言「配置注入值出现在文案里」
describe('heroRangeText：文案由配置边界派生（写死字面量与 sys_configs 漂移即缺陷）', () => {
  it('现网配置 1N 到 5N → 文案逐字出现注入值', () => {
    expect(heroRangeText(1, 5)).toBe('1-5N 正常范围')
  })

  it('边界带小数原样书写，不补零不取整', () => {
    expect(heroRangeText(0.5, 4.5)).toBe('0.5-4.5N 正常范围')
  })

  it('任一边界缺失返回空串（调用方隐藏该行，不猜值不回落旧字面量）', () => {
    expect(heroRangeText(null, 5)).toBe('')
    expect(heroRangeText(1, undefined)).toBe('')
    expect(heroRangeText(null, null)).toBe('')
    expect(heroRangeText(NaN, 5)).toBe('')
    expect(heroRangeText(1, Infinity)).toBe('')
  })

  it('源码不残留写死的 20-60 字面量（本卡缺陷本体不得回潮）', () => {
    expect(srcOf('../src/pressureUnit.ts')).not.toContain('20-60N')
    expect(srcOf('../src/pressureUnit.ts')).not.toMatch(/return `\d+-\d+N/)
  })
})

describe('提示文案逐字取 PRD 与两端稿面', () => {
  it('AREA_MISSING_HINT 原文', () => {
    expect(AREA_MISSING_HINT).toBe('未配置面积，暂无法换算')
  })
})

describe('显示层零换算门禁（防第二口径线）', () => {
  const src = srcOf('../src/pressureUnit.ts')

  it('本模块内不出现换算公式（除以面积 / 乘 10 / 引用面积值）', () => {
    // 口径在 Go 侧 `model.KpaFromN`；这里一旦出现 `/ area`、`* 10` 或读 contactAreaCm2
    // 就意味着前端在重算 —— 两端各算各的，后端改口径时前端不会跟着错，正是最坏的那种假绿。
    expect(src).not.toMatch(/contactAreaCm2/)
    expect(src).not.toMatch(/areaCm2/)
    expect(src).not.toMatch(/\*[^\S\n]*10\b/)
    expect(src).not.toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
  })

  it('反面自检：上面的正则是有效的（真塞一个换算式就会被判红）', () => {
    for (const bad of [
      'const k = (n / areaCm2) * 10',
      'const k = n / effectiveArea * 10',
      'const k = n/effective_area*10',
    ]) {
      expect(bad, bad).toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
      expect(bad, bad).toMatch(/\*[^\S\n]*10\b/)
    }
  })
})
