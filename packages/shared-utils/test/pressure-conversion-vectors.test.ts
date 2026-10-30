/**
 * T755：N → kPa 跨语言契约向量。
 *
 * 同一份 packages/constants/testdata/pressure-unit-vectors.json 由
 * Go data-service model.KpaFromN 与 TS shared-utils kpaFromN 共读；
 * 任一侧改公式或向量，另一端必红。
 */
import { describe, it, expect } from 'vitest'
import { kpaFromN } from '../src/index'
import vectors from '../../constants/testdata/pressure-unit-vectors.json'

type NToKpaVector = { n: number; area_cm2: number; kpa: number }

describe('N → kPa 契约向量（与 Go model.KpaFromN 共读）', () => {
  const cases = vectors.n_to_kpa as NToKpaVector[]

  it('向量非空', () => {
    expect(cases.length).toBeGreaterThan(0)
  })

  for (const c of cases) {
    it(`${c.n} N @ ${c.area_cm2} cm2 -> ${c.kpa} kPa`, () => {
      expect(kpaFromN(c.n, c.area_cm2)).toBe(c.kpa)
    })
  }
})
