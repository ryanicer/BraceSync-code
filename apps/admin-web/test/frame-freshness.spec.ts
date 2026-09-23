// T322 实时监控「假实时」收口 —— 帧新鲜度纯层单测
// 判据要能被判别：正常 / 恰好到期 / 超期 / 无帧 / 时刻缺失 / 时刻非法 / 时钟超前 各出一条断言。
import { describe, it, expect } from 'vitest'
import {
  FRAME_TTL_MS,
  FRAME_TAG_TEXT,
  frameFreshness,
  formatClock,
  formatFrameAge,
  latestFrameTime,
} from '../src/utils/frameFreshness'

const NOW = new Date('2026-09-22T15:00:00Z').getTime()
const iso = (ms: number) => new Date(ms).toISOString()

describe('frameFreshness 三态判定', () => {
  it('records 为空 / null / undefined → none（页面无帧，不得把 seed 兜底当实时）', () => {
    for (const recs of [[], null, undefined]) {
      expect(frameFreshness(recs as never, NOW).state).toBe('none')
    }
  })

  it('帧龄在 TTL 内 → fresh；恰好等于 TTL 仍算 fresh（后端 online 判据是 ≤2h）', () => {
    expect(frameFreshness([{ timestamp: iso(NOW - 1000) }], NOW).state).toBe('fresh')
    expect(frameFreshness([{ timestamp: iso(NOW - FRAME_TTL_MS) }], NOW).state).toBe('fresh')
  })

  it('帧龄超 TTL 1ms 即 expired（staging 冻结种子帧就落在这条上）', () => {
    expect(frameFreshness([{ timestamp: iso(NOW - FRAME_TTL_MS - 1) }], NOW).state).toBe('expired')
    expect(frameFreshness([{ timestamp: iso(NOW - 12 * 60 * 60 * 1000) }], NOW).state).toBe('expired')
  })

  it('有帧但时刻非法/缺失 → expired 而非 fresh：无采集时刻就没有「实时」凭据', () => {
    expect(frameFreshness([{ timestamp: 'not-a-date' }], NOW).state).toBe('expired')
    expect(frameFreshness([{}], NOW).state).toBe('expired')
    expect(frameFreshness([{ timestamp: '' }], NOW).collectedAt).toBeNull()
  })

  it('timestamp 缺失时退回 uploadTime；两者都缺才算无时刻', () => {
    const r = frameFreshness([{ uploadTime: iso(NOW - 5000) }], NOW)
    expect(r.state).toBe('fresh')
    expect(r.collectedAt).toBe(NOW - 5000)
  })

  it('多帧取最大时刻（不依赖后端返回顺序）', () => {
    const recs = [{ timestamp: iso(NOW - 3000) }, { timestamp: iso(NOW - 900_000) }, { timestamp: iso(NOW - 60_000) }]
    expect(latestFrameTime(recs)).toBe(NOW - 3000)
    expect(frameFreshness(recs, NOW).collectedAt).toBe(NOW - 3000)
  })

  it('时钟超前（设备/服务端时钟跳校）→ 帧龄按 0 计，不误判过期', () => {
    const r = frameFreshness([{ timestamp: iso(NOW + 5 * 60 * 1000) }], NOW)
    expect(r.state).toBe('fresh')
    expect(r.ageMs).toBe(0)
  })

  it('三态文案各自唯一，禁止把 expired/none 仍写成「实时同步中」', () => {
    expect(FRAME_TAG_TEXT.fresh).toBe('实时同步中')
    expect(FRAME_TAG_TEXT.expired).toBe('数据已过期')
    expect(FRAME_TAG_TEXT.none).toBe('无实时数据')
    expect(new Set(Object.values(FRAME_TAG_TEXT)).size).toBe(3)
  })
})

describe('时刻与帧龄格式化', () => {
  it('formatClock 输出本地 HH:MM:SS，null 给空串占位', () => {
    expect(formatClock(new Date('2026-09-22T10:30:00').getTime())).toBe('10:30:00')
    expect(formatClock(null)).toBe('')
  })

  it('formatFrameAge 按秒/分/小时升档', () => {
    expect(formatFrameAge(8_000)).toBe('距今 8s')
    expect(formatFrameAge(59_400)).toBe('距今 59s')
    expect(formatFrameAge(3 * 60_000)).toBe('距今 3 分')
    expect(formatFrameAge(2 * 3_600_000 + 24 * 60_000)).toBe('距今 2 小时 24 分')
    expect(formatFrameAge(5 * 3_600_000)).toBe('距今 5 小时')
  })
})
