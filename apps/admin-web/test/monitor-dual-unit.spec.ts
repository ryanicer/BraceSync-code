/**
 * T513 admin 实时监控双单位（N / kPa）—— 页面级判据。
 *
 * 为什么走 mount 而不是只写 Playwright：admin 的 e2e 跑 `USE_MOCK=true` dev server，
 * api 层在浏览器进程内直接 return mock ⇒ 展示态只能靠 mock 播种，而「同一份快照在两个档下
 * 各呈现成什么」这种成对比较，在每秒轮询换新帧的页面上要原子读（T270 假绿注释里那条老坑）。
 * mount 后由本用例掌握数据与切档时机，比较是确定性的；e2e 仍另写一条真实页链路。
 *
 * 依据：PRD §7A.2.1 四.9（②随档切换面 / ③判档恒 N / ⑤表头与数值随档切换且不新增第二列）、
 * 五.3（fail-closed 禁退化 0 / 禁沿用上一帧 / 禁前端补默认面积）、裁定 e（档位记忆 = 本地持久化）。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { AREA_MISSING_HINT } from '@bracesync/shared-utils'
import MonitorPage from '../src/pages/monitor/index.vue'
import { AREA_UNSET_PATIENT_ID, MOCK_AREA_CM2, mockPatientRealtime } from '../src/mock/patients'
import { MONITOR_UNIT_STORAGE_KEY } from '../src/utils/unitPref'
import * as api from '../src/api'

const served = vi.hoisted(() => ({ ids: ['PT-001'] as string[] }))

vi.mock('../src/api', () => ({
  fetchPatients: vi.fn(async () => ({
    list: served.ids.map((pid) => ({ patientId: pid, name: pid, deviceId: 'DEV-T513' })),
    total: served.ids.length,
  })),
  fetchPatientRealtime: vi.fn(async (pid: string) => mockPatientRealtime(pid)),
}))

/** 冻结一份快照后逐次返回同值：轮询只换对象引用、不换数值，成对比较才成立 */
function serveFrozen(pid: string) {
  const frozen = mockPatientRealtime(pid)
  vi.mocked(api.fetchPatientRealtime).mockImplementation(async () =>
    JSON.parse(JSON.stringify(frozen)) as never,
  )
  return frozen
}

const srcOf = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), 'utf8')

let wrapper: VueWrapper | null = null

async function settle(times = 8) {
  for (let i = 0; i < times; i++) await flushPromises()
}

async function mountMonitor(ids: string[] = ['PT-001']) {
  served.ids = ids
  wrapper = mount(MonitorPage, { global: { plugins: [ElementPlus] } })
  await settle()
  return wrapper
}

async function switchTo(w: VueWrapper, unit: 'N' | 'kPa') {
  const btn = w.findAll('.unit-seg-btn').find((b) => b.text() === unit)
  expect(btn, `档位按钮 ${unit} 未渲染`).toBeTruthy()
  await btn!.trigger('click')
  await settle(2)
}

const activeSeg = (w: VueWrapper) => w.find('.unit-seg-btn.unit-seg-active').text()
const cellVals = (w: VueWrapper) => w.findAll('.hm-cell-val').map((n) => n.text())
const tableVals = (w: VueWrapper) =>
  w.findAll('.points-table tbody tr').map((r) => r.findAll('td')[2]?.text() ?? '')
const statusCol = (w: VueWrapper) =>
  w.findAll('.points-table tbody tr').map((r) => r.findAll('td')[3]?.text() ?? '')
const pressureHeader = (w: VueWrapper) => w.findAll('.points-table thead th')[2].text()
/** 患者摘要「当前最大压力」格（.peak-value 唯一） */
const peakNum = (w: VueWrapper) => w.find('.peak-cell.peak-value .peak-num').text()
const cellStyles = (w: VueWrapper) => w.findAll('.hm-cell').map((n) => n.attributes('style') ?? '')
const cellMaxIdx = (w: VueWrapper) =>
  w.findAll('.hm-cell').reduce<number[]>((acc, n, i) => (n.classes('hm-cell-max') ? [...acc, i] : acc), [])
const detailLine = (w: VueWrapper) => w.find('.hm-detail').text()

/** 默认取数实现（每次现取 mock 快照）；每条用例前恢复，防上一轮的 mockImplementation 泄漏 */
function defaultServe() {
  vi.mocked(api.fetchPatientRealtime).mockImplementation(async (pid: string) => mockPatientRealtime(pid))
}

beforeEach(() => {
  vi.clearAllMocks()
  defaultServe()
  localStorage.clear()
  document.body.innerHTML = ''
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('默认档 = N：现状读数逐字不变（PRD 四.2）', () => {
  it('起版落在 N 档，格子/表列/表头/摘要与改动前同形', async () => {
    const w = await mountMonitor()
    expect(activeSeg(w)).toBe('N')
    expect(pressureHeader(w)).toBe('当前压力 (N)')
    // 格子 1 位小数、不带单位字母（本页既有口径；稿面的 2 位属 V3.37 待裁的「两端小数位差异」）
    expect(cellVals(w)).toHaveLength(20)
    for (const t of cellVals(w)) expect(t).toMatch(/^\d+\.\d$/)
    for (const t of tableVals(w)) expect(t).toMatch(/^\d+\.\d$/)
    expect(peakNum(w)).toMatch(/^\d+\.\d N$/)
    expect(w.find('.hm-area-warn').exists()).toBe(false)
  })

  it('N 档文本 = 快照 pressureValue 按本页位数渲染，不掺任何 kPa 值', async () => {
    const frozen = serveFrozen('PT-001')
    const w = await mountMonitor()
    const expected = (frozen.pressureHeatmap ?? []).map((p) => Number(p.pressureValue).toFixed(1))
    expect(tableVals(w)).toEqual(expected)
  })
})

describe('四.9②：随档切换的五个呈现面', () => {
  it('切到 kPa：表头字母、单元格、表列、摘要、详情行、悬浮 title 六处同时换', async () => {
    const frozen = serveFrozen('PT-001')
    const w = await mountMonitor()
    const before = { cells: cellVals(w), table: tableVals(w), header: pressureHeader(w), peak: peakNum(w) }
    await switchTo(w, 'kPa')

    expect(activeSeg(w)).toBe('kPa')
    expect(pressureHeader(w)).toBe('当前压力 (kPa)')

    const kpa = (frozen.pressureHeatmap ?? []).map((p) => String(p.pressureKpa))
    expect(cellVals(w)).toEqual(kpa)
    expect(tableVals(w)).toEqual(kpa)
    // 反证：kPa 档数字确实不同于 N 档（面积 0.64 ⇒ 量级差约 15.6 倍），断言不是恒真
    expect(cellVals(w)).not.toEqual(before.cells)
    expect(tableVals(w)).not.toEqual(before.table)
    expect(pressureHeader(w)).not.toBe(before.header)

    const maxKpa = (frozen.pressureHeatmap ?? []).reduce(
      (m, p) => (p.pressureValue > m.pressureValue ? p : m),
      (frozen.pressureHeatmap ?? [])[0],
    ).pressureKpa
    expect(peakNum(w)).toBe(`${maxKpa} kPa`)
    expect(peakNum(w)).not.toBe(before.peak)

    // 未点选时详情行回显「压力最大点」，随档换数值与字母
    expect(detailLine(w)).toContain(' kPa')
    expect(detailLine(w)).not.toMatch(/·\s*\d+\.\d\d N/)

    const title = w.findAll('.hm-cell')[11].attributes('title') ?? ''
    expect(title).toMatch(/: \d+ kPa$/)
  })

  it('切回 N 档：六个面逐字复原（切档是显示档切换，不是数据变更）', async () => {
    serveFrozen('PT-001')
    const w = await mountMonitor()
    const n = { cells: cellVals(w), table: tableVals(w), header: pressureHeader(w), peak: peakNum(w) }
    await switchTo(w, 'kPa')
    await switchTo(w, 'N')
    expect({ cells: cellVals(w), table: tableVals(w), header: pressureHeader(w), peak: peakNum(w) }).toEqual(n)
  })

  it('四.9⑤：表头随档改字母，同一列不换成本地化的第二列', async () => {
    const w = await mountMonitor()
    const headers = () => w.findAll('.points-table thead th').map((t) => t.text())
    expect(headers()).toEqual(['采集点', '位置', '当前压力 (N)', '状态'])
    await switchTo(w, 'kPa')
    expect(headers()).toEqual(['采集点', '位置', '当前压力 (kPa)', '状态'])
  })
})

describe('四.9③：判档恒为 N，颜色/最大点/状态列不随档变', () => {
  it('反证式对照：同一份快照下数字面变了而判档三面逐格没变', async () => {
    const w = await mountMonitor()
    const before = { color: cellStyles(w), isMax: cellMaxIdx(w), status: statusCol(w), vals: cellVals(w) }
    expect(before.color).toHaveLength(20)
    expect(before.isMax).toHaveLength(1)

    await switchTo(w, 'kPa')
    const after = { color: cellStyles(w), isMax: cellMaxIdx(w), status: statusCol(w), vals: cellVals(w) }
    // 数字面必须真的动了，否则上面三组「相同」可能只是页面没重渲染
    expect(after.vals).not.toEqual(before.vals)
    expect(after.status).toHaveLength(20)
    expect({ color: after.color, isMax: after.isMax, status: after.status }).toEqual({
      color: before.color,
      isMax: before.isMax,
      status: before.status,
    })
    // 状态列仍是四选一（判档词来自 N 阈值，不该冒出 kPa 口径）
    for (const s of after.status) expect(s).toMatch(/^(正常|关注|偏高|无信号)$/)
  })

  it('配色面的判别力实测：夹具 20 格只有 1 种色 ⇒ 逐格等色是恒等式，改由源码级钉死', async () => {
    const w = await mountMonitor()
    const kinds = new Set(cellStyles(w)).size
    // 这条不是行为判据，是「上一条的配色面还有没有牙」的哨兵：hmMaxN=6 而 mock 点在 13.5–55N，
    // 全部落最高档 ⇒ 实测 1 种色。变异「按 kPa 着色」在 vitest(20 passed) 与 e2e(6 passed)
    // 两腿都判不出红（我实测过，见 T513-evidence/01），所以下一条把配色取数钉在源码上。
    // 若夹具改出多种色，这里会红 —— 那是在提醒：逐格等色那一条重新获得判别力，去复跑变异。
    expect(kinds, '夹具配色档数变了：上一条的逐格等色判据恢复判别力，请复跑「按 kPa 着色」变异并撤这条哨兵').toBe(1)
  })

  it('配色取数只喂 pressureValue（源码级：着色不接显示档，也不接 kPa 字段）', () => {
    const src = srcOf('../src/pages/monitor/index.vue')
    expect(src).toMatch(/hmColor\(pt\.pressureValue, hmMaxN\)/)
    expect(src).not.toMatch(/hmColor\([^)]*[Kk]pa/)
    // 上界同理由 N 口径的 hmMaxN 提供，不许换成 heatmapMaxKpa
    expect(src).not.toMatch(/hmColor\([^)]*heatmapMaxKpa/)
  })

  // T513 期这条钉的是「曲线恒 N」（当时裁定纵轴不接显示档）。T643 / PRD V3.44 把实时曲线
  // 纳入随档切换面 ⇒ 钉子换形不换职责：仍禁止页面自己算 kPa（kPa 只许来自快照派生字段）。
  it('曲线纵轴/图例/tooltip 随档切换，N 档读数逐字不变（T643 取代 T513 的「曲线恒 N」）', () => {
    const src = srcOf('../src/pages/monitor/index.vue')
    // kPa 腿（新增）
    expect(src).toContain('`压力 (${axisUnitText(unit.value)})`')
    expect(src).toContain('`压力：${fmtKpa(Number(c.parsed.y))} kPa`')
    expect(src).toContain('`${fmtKpa(Number(v))}${axisUnitText(unit.value)}`')
    // N 腿（改动前原文，逐字仍在 ⇒ 默认档读数不变）
    expect(src).toContain('`压力：${fmtN(Number(c.parsed.y))} N`')
    expect(src).toContain('`${fmtN(Number(v))}N`')
    // 取数面：kPa 序列读的是帧上的后端派生值，不是页面换算结果
    expect(src).toMatch(/d\.kpa/)
    expect(src).not.toMatch(/mockKpaOf|contactAreaCm2|areaCm2/)
  })
})

describe('五.3 fail-closed：面积未配置时 kPa 档只出 --', () => {
  it('PT-003（快照 contactAreaCm2=null、heatmapMaxKpa=null）切到 kPa：数字面全 --，且出提示行', async () => {
    const w = await mountMonitor([AREA_UNSET_PATIENT_ID])
    // N 档照旧有读数，且此刻不许出现提示行（N 不依赖面积）
    expect(w.find('.hm-area-warn').exists()).toBe(false)
    for (const t of cellVals(w)) expect(t).toMatch(/^\d+\.\d$/)

    await switchTo(w, 'kPa')
    expect(cellVals(w)).toEqual(Array.from({ length: 20 }, () => '--'))
    expect(tableVals(w).every((t) => t === '--')).toBe(true)
    expect(pressureHeader(w)).toBe('当前压力 (kPa)')
    // 摘要与详情行：不可换算时只出 --，不再挂单位字母（稿面 217 行同形）
    expect(peakNum(w)).toBe('--')
    expect(detailLine(w)).toContain('· --')
    expect(detailLine(w)).not.toContain('kPa')
    expect(w.find('.hm-area-warn').text()).toBe(AREA_MISSING_HINT)
  })

  it('不许退化成 0：-- 与 0 是两回事（退化 0 会被读成「压力为零」）', async () => {
    const w = await mountMonitor([AREA_UNSET_PATIENT_ID])
    await switchTo(w, 'kPa')
    expect(cellVals(w)).not.toContain('0')
    expect(cellVals(w)).not.toContain('0.0')
    expect(peakNum(w)).not.toMatch(/^0/)
  })

  it('反证：面积在位的患者同档下既有数字也无提示行 ⇒ 提示行判据不是恒真', async () => {
    // 顺序有意为之：页面默认选「第一个有 deviceId 的患者」，PT-001 必须排在第一位才会被选中
    const w = await mountMonitor(['PT-001', AREA_UNSET_PATIENT_ID])
    await switchTo(w, 'kPa')
    expect(w.find('.hm-area-warn').exists()).toBe(false)
    expect(cellVals(w).some((t) => t !== '--')).toBe(true)
    expect(peakNum(w)).toMatch(/ kPa$/)
    expect(MOCK_AREA_CM2).toBeGreaterThan(0)
  })

  it('无帧时不挂提示行（格子本就不渲染，提示会成为唯一残留文本）', async () => {
    // PT-005 未绑定设备 → pressureRecords 为空 → frame.state=none
    const w = await mountMonitor(['PT-005'])
    await switchTo(w, 'kPa')
    expect(w.findAll('.hm-cell')).toHaveLength(0)
    expect(w.find('.hm-area-warn').exists()).toBe(false)
    expect(w.text()).toContain('无实时帧')
  })
})

describe('裁定 e：档位记忆只走本地持久化', () => {
  it('切档写入固定键，重新进页即恢复 kPa', async () => {
    serveFrozen('PT-001')
    const w = await mountMonitor()
    await switchTo(w, 'kPa')
    expect(localStorage.getItem(MONITOR_UNIT_STORAGE_KEY)).toBe('kPa')
    w.unmount()
    wrapper = null

    const w2 = await mountMonitor()
    expect(activeSeg(w2)).toBe('kPa')
    expect(pressureHeader(w2)).toBe('当前压力 (kPa)')
  })

  it('存储里是脏值时回落 N（脏值不许把页面开在无法识别的档上）', async () => {
    localStorage.setItem(MONITOR_UNIT_STORAGE_KEY, 'KG')
    const w = await mountMonitor()
    expect(activeSeg(w)).toBe('N')
    expect(pressureHeader(w)).toBe('当前压力 (N)')
  })

  it('反证：把记忆键删掉后重新进页回 N，证明上一条的 kPa 真来自记忆而非默认值', async () => {
    serveFrozen('PT-001')
    const w = await mountMonitor()
    await switchTo(w, 'kPa')
    expect(localStorage.getItem(MONITOR_UNIT_STORAGE_KEY)).toBe('kPa')
    w.unmount()
    wrapper = null
    localStorage.removeItem(MONITOR_UNIT_STORAGE_KEY)

    const w2 = await mountMonitor()
    expect(activeSeg(w2)).toBe('N')
    expect(pressureHeader(w2)).toBe('当前压力 (N)')
  })

  it('键名固定，且持久化只碰 localStorage（不发请求、不写服务端字段）', () => {
    expect(MONITOR_UNIT_STORAGE_KEY).toBe('admin_monitor_unit')
    const src = srcOf('../src/utils/unitPref.ts')
    expect(src).toMatch(/localStorage\.setItem/)
    expect(src).not.toMatch(/import .*from ['"][^'"]*api['"]/)
    expect(src).not.toMatch(/fetch\(|axios|request\(/)
    // 持久化必须是同步本地写：出现 await / 网络原语就说明往服务端写了（PRD 四.8 禁动 patient_preferences）
    expect(src).not.toMatch(/\bawait\b|XMLHttpRequest|\.post\(|\.put\(/)
  })

  it('切档函数体只改显示档并写本地存储，不重新取数', () => {
    const page = srcOf('../src/pages/monitor/index.vue')
    const body = page.match(/function switchUnit\([\s\S]*?\n\}/)?.[0]
    expect(body, '页面应有 switchUnit').toBeTruthy()
    expect(body).toContain('persistUnit(next)')
    expect(body).not.toMatch(/refreshTick|fetchPatientRealtime|await/)
  })

  it('切档本身不触发取数（运行时对照调用计数）', async () => {
    serveFrozen('PT-001')
    const w = await mountMonitor()
    const calls = vi.mocked(api.fetchPatientRealtime).mock.calls.length
    expect(calls).toBeGreaterThan(0)
    await switchTo(w, 'kPa')
    await switchTo(w, 'N')
    await switchTo(w, 'kPa')
    expect(vi.mocked(api.fetchPatientRealtime).mock.calls.length).toBe(calls)
  })
})

describe('前端零重复换算门禁（派发单 §三 选型 A）', () => {
  const src = srcOf('../src/pages/monitor/index.vue')

  it('页面不出现换算公式，也不读面积字段', () => {
    expect(src).not.toMatch(/contactAreaCm2/)
    expect(src).not.toMatch(/areaCm2/)
    expect(src).not.toMatch(/\*[^\S\n]*10\b/)
    expect(src).not.toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
    // kPa 一律来自快照下发字段
    expect(src).toMatch(/pressureKpa/)
    expect(src).toMatch(/heatmapMaxKpa/)
  })

  it('反面自检：正则对「前端自己换算」的写法真有感', () => {
    const bad = 'const kpa = (p.pressureValue / contactAreaCm2) * 10'
    expect(bad).toMatch(/contactAreaCm2/)
    expect(bad).toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
    expect(bad).toMatch(/\*[^\S\n]*10\b/)
  })

  it('唯一含公式的是 mock 里的服务端替身，页面不引它（口径仍只有一份实现于显示层之外）', () => {
    const mockSrc = srcOf('../src/mock/patients.ts')
    expect(mockSrc).toMatch(/function mockKpaOf/)
    expect(mockSrc).toMatch(/服务端替身/)
    expect(src).not.toMatch(/mockKpaOf/)
  })
})
