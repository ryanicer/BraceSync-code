// T600「当前最大压力」与「最大压力采集点」不同层 —— PM 裁定方案一的展示面判据。
//
// 缺陷结构：第三格原由前端在页面会话内逐帧比较生成（刷新/换患者/跨日即清零），
// 服务端同一响应里的今日峰值 maxPressure/maxPoint 被注释明写弃用、数值从不显示。
// 方案一：第三格点位与值同取服务端今日峰值（stat:today 全天口径），前端不再会话内累计。
//
// 判据的牙：
//   - 本帧 heatmap 最大点刻意与服务端今日峰值点不同（P07/0.3 vs P14/1.8），
//     旧实现首拍就会把格点画成本帧最大点 P07（会话累计首拍=本帧 max）。
//   - 「刷新一致」用两枚独立挂载表达（新 wrapper = 一次新加载的页面）：本帧换一个最大点，
//     服务端字段不变 ⇒ 第三格两次逐字一致；服务端字段真变 ⇒ 格才跟着变（证明只听服务端）。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { readFileSync } from 'node:fs'
import MonitorPage from '../src/pages/monitor/index.vue'
import type { PressureHeatmapPoint, RealtimeSnapshot } from '../src/mock/patients'

vi.mock('../src/api', () => ({
  fetchPatients: vi.fn(async () => ({
    list: [{ patientId: 'PT-001', name: '林小雨', deviceId: 'DEV-A3F312' }],
    total: 1,
  })),
  fetchPatientRealtime: vi.fn(async () => ({})),
}))

import * as api from '../src/api'

async function settle(times = 8) {
  for (let i = 0; i < times; i++) await flushPromises()
}

/** 20 点热力图：把本帧最大点放在 maxPointId（isMax 同源），其余点给小值 0.1 */
function heatmap(maxPointId: string, maxV: number, kpaAtMax: number): PressureHeatmapPoint[] {
  return Array.from({ length: 20 }, (_, i) => {
    const pointId = `P${String(i + 1).padStart(2, '0')}`
    const isMax = pointId === maxPointId
    return {
      pointId,
      row: Math.floor(i / 5) + 1,
      col: (i % 5) + 1,
      label: `R${Math.floor(i / 5) + 1}C${(i % 5) + 1}`,
      pressureValue: isMax ? maxV : 0.1,
      isMax,
      pressureKpa: isMax ? kpaAtMax : 2,
    }
  })
}

interface SnapOver {
  frameMaxId?: string
  frameMaxV?: number
  frameMaxKpa?: number
  maxPressure?: number
  maxPoint?: string
  withFrame?: boolean
  p14Kpa?: number
}

function snapshot(over: SnapOver = {}): RealtimeSnapshot {
  const {
    frameMaxId = 'P07',
    frameMaxV = 0.346,
    frameMaxKpa = 5,
    maxPressure = 1.779,
    maxPoint = 'P14',
    withFrame = true,
    p14Kpa = 28,
  } = over
  const pts = heatmap(frameMaxId, frameMaxV, frameMaxKpa).map((p) =>
    p.pointId === 'P14' ? { ...p, pressureKpa: p14Kpa } : p,
  )
  return {
    status: 'online',
    todayHours: 1.5,
    maxPressure,
    maxPoint,
    events: 0,
    pressureRecords: withFrame ? [{ timestamp: new Date().toISOString() }] : [],
    alerts: [],
    pressureHeatmap: withFrame ? pts : [],
  } as unknown as RealtimeSnapshot
}

async function mountMonitor(): Promise<VueWrapper> {
  const wrapper = mount(MonitorPage, { global: { plugins: [ElementPlus] } })
  await settle()
  return wrapper
}

const peakCells = (w: VueWrapper) => w.findAll('.peak-card .peak-cell')
const currentMaxCell = (w: VueWrapper) => peakCells(w)[1].find('.peak-num').text()
const todayPeakCell = (w: VueWrapper) => peakCells(w)[2].find('.peak-text').text()

async function switchSeg(w: VueWrapper, next: 'N' | 'kPa') {
  const btn = w.findAll('.unit-seg-btn').find((b) => b.text() === next)
  expect(btn, `档位按钮 ${next} 未渲染`).toBeTruthy()
  await btn!.trigger('click')
  await settle(3)
}

describe('T600 实时监控 · 今日峰值格只取服务端 maxPoint/maxPressure（方案一）', () => {
  let wrapper: VueWrapper | null = null

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    document.body.innerHTML = ''
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.clearAllTimers()
  })

  it('两格分层：第二格显本帧最大 0.3 N（P07），第三格显服务端今日峰值 P14 (R3C4) · 1.8 N', async () => {
    vi.mocked(api.fetchPatientRealtime).mockResolvedValue(snapshot() as never)
    wrapper = await mountMonitor()
    expect(currentMaxCell(wrapper)).toContain('0.3 N')
    expect(todayPeakCell(wrapper)).toBe('P14 (R3C4) · 1.8 N')
    // 本帧最大点的点位号不得出现在今日峰值格里（不同时间窗的两个量不许混成一格）
    expect(todayPeakCell(wrapper)).not.toContain('P07')
  })

  it('刷新一致（核心）：两次页面加载本帧最大点不同、服务端字段相同 ⇒ 第三格逐字一致；服务端真变才跟着变', async () => {
    // 第一次加载：本帧最大在 P07
    vi.mocked(api.fetchPatientRealtime).mockResolvedValue(snapshot({ frameMaxId: 'P07', frameMaxV: 0.346 }) as never)
    wrapper = await mountMonitor()
    const first = todayPeakCell(wrapper)
    expect(first).toBe('P14 (R3C4) · 1.8 N')
    // 第二格确实随本帧换了，证明两次喂的帧不同（不是「帧没变所以格没动」的假绿）
    expect(currentMaxCell(wrapper)).toContain('0.3 N')
    wrapper.unmount()

    // 第二次加载（＝浏览器刷新后的新页面会话）：本帧最大换到 P20/0.9，服务端今日峰值原样
    vi.mocked(api.fetchPatientRealtime).mockResolvedValue(snapshot({ frameMaxId: 'P20', frameMaxV: 0.9, frameMaxKpa: 14 }) as never)
    wrapper = await mountMonitor()
    expect(currentMaxCell(wrapper), '第二格应跟随本帧变成 0.9 N').toContain('0.9 N')
    expect(todayPeakCell(wrapper), '第三格是今日口径：刷新后必须与上一轮逐字一致，不被本帧 0.9 顶掉').toBe(first)
    wrapper.unmount()

    // 反证它真的听服务端：服务端今日峰值推进到 P03/2.1，格必须跟着变（不是冻结的死文本）
    vi.mocked(api.fetchPatientRealtime).mockResolvedValue(
      snapshot({ frameMaxId: 'P20', frameMaxV: 0.9, frameMaxKpa: 14, maxPoint: 'P03', maxPressure: 2.1, p14Kpa: 28 }) as never,
    )
    // P03 的本帧派生 kpa 由 frameMaxKpa 给不到（本帧最大是 P20），单独补一枚
    const withP03Kpa = snapshot({ frameMaxId: 'P20', frameMaxV: 0.9, frameMaxKpa: 14, maxPoint: 'P03', maxPressure: 2.1 })
    const pts = (withP03Kpa.pressureHeatmap as PressureHeatmapPoint[]).map((p) =>
      p.pointId === 'P03' ? { ...p, pressureKpa: 33 } : p,
    )
    withP03Kpa.pressureHeatmap = pts
    vi.mocked(api.fetchPatientRealtime).mockResolvedValue(withP03Kpa as never)
    wrapper = await mountMonitor()
    expect(todayPeakCell(wrapper)).toBe('P03 (R1C3) · 2.1 N')
  })

  it('kPa 档：值取同点位本帧派生 pressureKpa（28 kPa），切回 N 复原；不做前端换算', async () => {
    vi.mocked(api.fetchPatientRealtime).mockResolvedValue(snapshot() as never)
    wrapper = await mountMonitor()
    await switchSeg(wrapper, 'kPa')
    expect(todayPeakCell(wrapper)).toBe('P14 (R3C4) · 28 kPa')
    await switchSeg(wrapper, 'N')
    expect(todayPeakCell(wrapper)).toBe('P14 (R3C4) · 1.8 N')
  })

  it('当前无帧但今日已有峰值（摘设备后）：格仍显今日峰值，label 按点位编号拼，不随无帧清空', async () => {
    vi.mocked(api.fetchPatientRealtime).mockResolvedValue(snapshot({ withFrame: false }) as never)
    wrapper = await mountMonitor()
    // heatmap 为空 ⇒ 第二格出占位；第三格是今日累计量，照样显示
    expect(currentMaxCell(wrapper)).toBe('--')
    expect(todayPeakCell(wrapper)).toBe('P14 (R3C4) · 1.8 N')
    // 无帧即无同点位 kPa 派生 ⇒ 值部分 fail-closed 显 --，不用 N 值自己算；
    // 但点位是今日真实峰值点（服务端给的），不随值缺失一起抹掉
    await switchSeg(wrapper, 'kPa')
    expect(todayPeakCell(wrapper)).toBe('P14 (R3C4) · --')
  })

  it('今日无峰值（maxPoint 空串 / maxPressure 非正）：格给占位，不显示 0.0 N 或示例点位', async () => {
    vi.mocked(api.fetchPatientRealtime).mockResolvedValue(
      snapshot({ withFrame: false, maxPoint: '', maxPressure: 0 }) as never,
    )
    wrapper = await mountMonitor()
    expect(todayPeakCell(wrapper)).toBe('--')
  })

  it('防复活：页面源码不再有会话内逐帧累计的状态与跨日清零逻辑', () => {
    // vitest 转换后 import.meta.url 不保证 file scheme，按工作目录拼（测试在 apps/admin-web 下运行）
    const src = readFileSync(`${process.cwd()}/src/pages/monitor/index.vue`, 'utf8')
    expect(src, 'T600 后不得再出现会话峰值引用').not.toContain('todayPeak')
    expect(src, '前端跨日清零逻辑随会话累计一并删除（跨日是服务端 stat:today 的事）').not.toContain('dateKey')
    // 新口径锚点必须在场，防止「整个格被删了所以查不到旧词」的空面假绿
    expect(src).toContain('snapshot.maxPoint')
    expect(src).toContain('todayMax')
  })
})
