/**
 * T579 — 佩戴期望时长同源尺
 *
 * 三格：① 下发字段（profile 只读面）读取与合法域 ② 取不到时两页共用的兜底 = 22
 *      ③ 仓内机械尺：wearing / anomaly 两页都不再自带目标时长常量，兜底只住一处。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

vi.mock('../../src/utils/request', () => ({ request: vi.fn() }))

import { request } from '../../src/utils/request'
import {
  FALLBACK_TARGET_HOURS,
  loadWearTarget,
  normalizeTargetHours,
  wearTargetHours,
} from '../../src/utils/wear-target'

const requestMock = vi.mocked(request)
const SRC = fileURLToPath(new URL('../../src', import.meta.url))
const PAGES = ['pages/wearing/index.vue', 'pages/anomaly/index.vue']

beforeEach(() => {
  requestMock.mockReset()
  wearTargetHours.value = FALLBACK_TARGET_HOURS
})

describe('T579 — 合法域', () => {
  it('1..24 的数值（含小数与纯数字串）认，其余算「后端没给」', () => {
    expect(normalizeTargetHours(22)).toBe(22)
    expect(normalizeTargetHours('22')).toBe(22)
    expect(normalizeTargetHours(1)).toBe(1)
    expect(normalizeTargetHours(24)).toBe(24)
    // 后端 validateSettings 收 1..24 的 float64（handler.go），小数是合法配置，不许被前端当脏值丢掉
    expect(normalizeTargetHours(16.5)).toBe(16.5)
    expect(normalizeTargetHours('16.5')).toBe(16.5)
    for (const raw of [0, -1, 25, 0.5, '', 'abc', null, undefined, NaN, Infinity, -Infinity, {}, []]) {
      expect(normalizeTargetHours(raw)).toBeNull()
    }
  })
})

describe('T579 — 从 profile 只读面取下发值', () => {
  it('取到即用，且请求打的是那条 self-scope 只读路由', async () => {
    requestMock.mockResolvedValue({ patientId: 'pat-x', dailyWearTargetHours: 20 })
    expect(await loadWearTarget()).toBe(20)
    expect(wearTargetHours.value).toBe(20)
    expect(requestMock).toHaveBeenCalledWith({ url: '/api/v1/patient/profile', method: 'GET' })
  })

  it('下发的是小数也原样用（后端 float64，前端不取整）', async () => {
    requestMock.mockResolvedValue({ dailyWearTargetHours: 16.5 })
    expect(await loadWearTarget()).toBe(16.5)
    expect(wearTargetHours.value).toBe(16.5)
  })

  it('字段缺席（Winner 那一笔还没落地时）退回 22', async () => {
    requestMock.mockResolvedValue({ patientId: 'pat-x' })
    expect(await loadWearTarget()).toBe(22)
  })

  it('值不合法退回 22，不把脏值写进共用读数', async () => {
    requestMock.mockResolvedValue({ dailyWearTargetHours: 0 })
    expect(await loadWearTarget()).toBe(22)
    expect(wearTargetHours.value).toBe(22)
  })

  it('请求失败（bind 态 403 / 未登录 / 断网）退 22 且不上抛', async () => {
    requestMock.mockRejectedValue(new Error('40301'))
    expect(await loadWearTarget()).toBe(22)
  })

  it('兜底常量本身就是后端现值口径 22', () => {
    expect(FALLBACK_TARGET_HOURS).toBe(22)
  })
})

describe('T579 — 仓内机械尺（判据 1、2、3 的可复跑形）', () => {
  // 旧常量名按段拼，字面量整体不进源码：T576 判据 1 那把 grep 尺扫整个 apps/patient-miniapp，
  // 本文件写一次就会让合并后的命中数从 0 变成 2。
  const LEGACY_CONST = ['WEAR', 'TARGET', 'H'].join('_')

  it('两页都不再出现旧常量，且都从共用入口取数', () => {
    for (const rel of PAGES) {
      const text = fs.readFileSync(path.join(SRC, rel), 'utf8')
      expect(text.includes(LEGACY_CONST), rel).toBe(false)
      expect(text.includes("from '../../utils/wear-target'"), rel).toBe(true)
      expect(text.includes('loadWearTarget'), rel).toBe(true)
    }
  })

  it('判据 3：两页都没有本地写死的目标时长常量', () => {
    const localConst = /const\s+[A-Za-z_]*TARGET[A-Za-z_]*\s*=\s*\d+/
    for (const rel of PAGES) {
      const text = fs.readFileSync(path.join(SRC, rel), 'utf8')
      expect(localConst.test(text), rel).toBe(false)
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
