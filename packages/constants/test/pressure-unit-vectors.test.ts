/**
 * T755：mN → N 跨语言契约向量。
 *
 * 同一份 packages/constants/testdata/pressure-unit-vectors.json 由
 * Go services/units 与 TS packages/constants 共读；任一侧改公式或向量，另一端必红。
 */
import { describe, it, expect } from 'vitest'
import { nFromMn } from '../src/index'
import vectors from '../testdata/pressure-unit-vectors.json'

type MnToNVector = { mn: number; n: number }

describe('mN → N 契约向量（与 Go services/units 共读）', () => {
  const cases = vectors.mn_to_n as MnToNVector[]

  it('向量非空', () => {
    expect(cases.length).toBeGreaterThan(0)
  })

  for (const c of cases) {
    it(`${c.mn} mN -> ${c.n} N`, () => {
      expect(nFromMn(c.mn)).toBeCloseTo(c.n, 12)
    })
  }
})
