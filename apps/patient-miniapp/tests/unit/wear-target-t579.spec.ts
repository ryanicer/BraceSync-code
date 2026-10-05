/**
 * T579 — 佩戴期望时长同源尺
 *
 * 三格：① 下发字段读取与合法域 ② 字段未到齐时两页共用的兜底 = 22
 *      ③ 仓内机械尺：wearing / anomaly 两页都不再自带目标时长常量，兜底只住一处。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  FALLBACK_TARGET_HOURS,
  applyTargetHours,
  readTargetHours,
  normalizeTargetHours,
  wearTargetHours,
} from '../../src/utils/wear-target'

const SRC = fileURLToPath(new URL('../../src', import.meta.url))
const PAGES = ['pages/wearing/index.vue', 'pages/anomaly/index.vue']

describe('T579 — 下发字段读取', () => {
  it('对象响应面取顶层字段', () => {
    expect(normalizeTargetHours(22)).toBe(22)
    expect(readTargetHours({ dailyWearTargetHours: 20 })).toBe(20)
  })

  it('数组响应面（daily-wear 的 data）自最近一天往回取第一个有效值', () => {
    expect(readTargetHours([{ dailyWearTargetHours: 18 }, { dailyWearTargetHours: 21 }])).toBe(21)
    expect(readTargetHours([{ dailyWearTargetHours: 19 }, {}])).toBe(19)
  })

  it('合法域外的值一律算「后端没给」', () => {
    for (const raw of [0, -1, 25, 2.5, '', 'abc', null, undefined, NaN]) {
      expect(normalizeTargetHours(raw)).toBeNull()
    }
    expect(readTargetHours([])).toBeNull()
    expect(readTargetHours(null)).toBeNull()
  })

  it('数字字符串按后端 sys_configs 的 KV 原样（串）也认', () => {
    expect(normalizeTargetHours('22')).toBe(22)
  })
})

describe('T579 — 兜底与共用读数', () => {
  it('兜底值是 22（后端现值口径）', () => {
    expect(FALLBACK_TARGET_HOURS).toBe(22)
  })

  it('字段未到齐 → 退回 22；到齐 → 用下发值；再次未到齐 → 回到 22', () => {
    expect(applyTargetHours([])).toBe(22)
    expect(wearTargetHours.value).toBe(22)
    expect(applyTargetHours([{ dailyWearTargetHours: 20 }])).toBe(20)
    expect(applyTargetHours(null)).toBe(22)
  })
})

describe('T579 — 仓内机械尺（判据 1、2 的可复跑形）', () => {
  // 旧常量名按段拼，字面量整体不进源码：T576 判据 1 那把 grep 尺扫整个 apps/patient-miniapp，
  // 本文件写一次就会让合并后的命中数从 0 变成 2。
  const LEGACY_CONST = ['WEAR', 'TARGET', 'H'].join('_')

  it('两页都不再自带目标时长常量，改为读共用入口', () => {
    for (const rel of PAGES) {
      const text = fs.readFileSync(path.join(SRC, rel), 'utf8')
      expect(text.includes(LEGACY_CONST), rel).toBe(false)
      expect(text.includes("from '../../utils/wear-target'"), rel).toBe(true)
    }
  })

  it('兜底常量在 src 下只定义一处', () => {
    const hits: string[] = []
    const walk = (dir: string) => {
      for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
        const full = path.join(dir, e.name)
        if (e.isDirectory()) walk(full)
        else if (/\.(ts|vue)$/.test(e.name) && /FALLBACK_TARGET_HOURS\s*=/.test(fs.readFileSync(full, 'utf8'))) {
          hits.push(path.relative(SRC, full).replace(/\\/g, '/'))
        }
      }
    }
    walk(SRC)
    expect(hits).toEqual(['utils/wear-target.ts'])
  })
})
