/**
 * T643「单位切换扩展：全部压力图表跟随」· admin 侧判据。
 *
 * 依据：PRD V3.44 §7A.2.1 四.9②（实时曲线与矫形日志压力趋势的纵轴随 N/kPa 切换）、
 * §7D.12（阈值只有 N 口径，不设 kPa 阈值键）、五.3（fail-closed：不可换算只出提示、不退化成 0）、
 * 裁定 e（档位记忆 = 本地持久化，切档不发请求）。
 *
 * 选型 A 的落点：换算真源只有后端那一份，前端只显示 pressureKpa / avgPressureKpa 这类派生字段。
 * 图在 jsdom 里画不出像素，但 Line 组件的 props 读得到 ⇒ 运行时判据钉「喂给图的那列数」。
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus, { ElSelect, ElTabs } from 'element-plus'
import { createPinia, setActivePinia } from 'pinia'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { AREA_MISSING_HINT } from '@bracesync/shared-utils'
import { Line } from 'vue-chartjs'

const srcOf = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), 'utf8')

/**
 * vi.mock 的工厂在被 mock 模块首次被请求时才执行，而那发生在 import 阶段
 * ⇒ 工厂里能引用的东西必须在 vi.hoisted 里（本仓 monitor-dual-unit.spec.ts 同一套路）。
 */
const fx = vi.hoisted(() => {
  const patient = {
    patientId: 'PT-001',
    name: '林小雨',
    gender: 'female',
    age: 13,
    diagnosis: '青少年特发性脊柱侧弯',
    cobbAngle: 28,
    deviceId: 'DEV-A3F312',
    teamId: 'TEAM-001',
    doctorId: 'DOC-001',
    status: 'active',
    createdAt: '2026-03-12T09:00:00+08:00',
    updatedAt: '2026-08-10T18:00:00+08:00',
  }
  // 日聚合的行必须落在页面自己算的那 7 天窗口里（rangeForDays 按东八区取今日）：
  // 写死日期会让 alignWearSeries 把整批行丢掉 ⇒ 空态 ⇒ 判据读不到图。故每次调用现算。
  const cstDate = (offsetDays: number) =>
    new Date(Date.now() - offsetDays * 86400000 + 8 * 3600000).toISOString().slice(0, 10)
  // kPa 列刻意与 N 列数值不同：防「把 N 那列直接画到 kPa 档」蒙过判据
  const rows = (kpa: 'present' | 'absent') =>
    [6, 5, 4, 3, 2, 1, 0].map((offset, i) => {
      const avgPressure = 20 + i
      return {
        date: cstDate(offset),
        wearMinutes: 400 + i * 10,
        avgPressure,
        maxPressure: avgPressure + 8,
        avgPressureKpa: kpa === 'present' ? 300 + i : null,
        maxPressureKpa: kpa === 'present' ? 400 + i : null,
        maxPoint: 'P07',
        frameCount: 800,
        abnormalCount: 0,
      }
    })
  return { patient, rows }
})

vi.mock('../src/api', () => ({
  fetchPatients: vi.fn(async () => ({ list: [fx.patient], total: 1, page: 1, pageSize: 50 })),
  fetchPatientDetail: vi.fn(async () => fx.patient),
  fetchTeams: vi.fn(async () => []),
  fetchAlerts: vi.fn(async () => ({ list: [], total: 0, page: 1, pageSize: 10 })),
  fetchSystemSettings: vi.fn(async () => ({
    dailyWearTargetHours: 8,
    pressureHighThresholdN: 40,
    wearingTargetHours: 8,
  })),
  fetchOrthosisPlans: vi.fn(async () => []),
  saveOrthosisPlanApi: vi.fn(async () => ({})),
  fetchFeelingLogs: vi.fn(async () => []),
  fetchFeelingLogsAdmin: vi.fn(async () => ({ list: [], total: 0, page: 1, pageSize: 20 })),
  fetchPatientDailyWear: vi.fn(async () => fx.rows('present')),
  fetchHealthReports: vi.fn(async () => []),
  replyFeelingLogApi: vi.fn(async () => ({})),
  patientNameOf: (id: string) => id,
  teamNameOf: (id: string) => id,
}))

import OrthosisLogPage from '../src/pages/orthosis-log/index.vue'
import * as api from '../src/api'
import { useAuthStore } from '../src/stores/auth'
import { MONITOR_UNIT_STORAGE_KEY } from '../src/utils/unitPref'

async function settle(times = 8) {
  for (let i = 0; i < times; i++) await flushPromises()
}

/** 走到「患者工作台 → 数据视图」：外层 tab、患者选择、内层 tab 三步都是页内既有交互 */
async function openDataView(w: VueWrapper) {
  await w.findAllComponents(ElTabs)[0].vm.$emit('update:modelValue', 'workspace')
  await settle()
  const sel = w.findAllComponents(ElSelect).find((s) => s.classes().includes('patient-select'))
  expect(sel, '工作台的 el-select.patient-select 未渲染（外层 tab 没切过去）').toBeTruthy()
  await sel!.vm.$emit('update:modelValue', fx.patient.patientId)
  await sel!.vm.$emit('change', fx.patient.patientId)
  await settle()
  const inner = w.findAllComponents(ElTabs)[1]
  expect(inner, '内层 el-tabs 未渲染（patientId 没落上）').toBeTruthy()
  // 两个事件都要手推：实测只推 modelValue 时 Element Plus 不会自己补发 tab-change
  // （页面正是靠 @tab-change 才惰性加载日佩戴数据），代价是一条 Vue 告警。
  await inner!.vm.$emit('update:modelValue', 'data')
  await inner!.vm.$emit('tab-change', 'data')
  await settle()
}

async function mountLogPage() {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore(pinia).user = { name: '验收账号', role: 'admin' }
  const wrapper = mount(OrthosisLogPage, { global: { plugins: [pinia, ElementPlus] } })
  await settle()
  await openDataView(wrapper)
  return wrapper
}

const pressureLine = (w: VueWrapper) => w.findComponent(Line)
const pressureDatasets = (w: VueWrapper) => {
  expect(pressureLine(w).exists(), '压力趋势图未渲染').toBe(true)
  return (pressureLine(w).props('data') as { datasets: Array<{ label: string; data: unknown[] }> }).datasets
}
const yTitleText = (w: VueWrapper) =>
  ((pressureLine(w).props('options') as { scales: { y: { title: { text: string } } } }).scales.y.title.text)
const axisNote = (w: VueWrapper) => w.findAll('.chart-card')[0].find('.axis-note').text()
const pressureEmptyText = (w: VueWrapper) =>
  w.findAll('.chart-card')[0].find('.el-empty__description').text()

async function switchSeg(w: VueWrapper, next: 'N' | 'kPa') {
  const btn = w.findAll('.unit-seg-btn').find((b) => b.text() === next)
  expect(btn, `档位按钮 ${next} 未渲染`).toBeTruthy()
  await btn!.trigger('click')
  await settle(3)
}

const AVG_N = [20, 21, 22, 23, 24, 25, 26]
const AVG_KPA = [300, 301, 302, 303, 304, 305, 306]

describe('矫形日志 · 压力趋势图随 N/kPa 档切换（PRD V3.44 §7A.2.1 四.9②）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    document.body.innerHTML = ''
    vi.mocked(api.fetchPatientDailyWear).mockImplementation(async () => fx.rows('present') as never)
  })

  it('默认 N 档：序列是 N 列、纵轴标题 N、上限线在', async () => {
    const w = await mountLogPage()
    const ds = pressureDatasets(w)
    expect(ds[0].label).toBe('日均压力（N）')
    expect(ds[0].data).toEqual(AVG_N)
    expect(yTitleText(w)).toBe('N')
    expect(ds).toHaveLength(2)
    expect(ds[1].label).toContain('压力上限线')
    expect(axisNote(w)).toContain('纵轴 = 日均压力（N）')
  })

  it('切到 kPa：喂给图的那列换成后端派生列，图例与纵轴标题同步换', async () => {
    const w = await mountLogPage()
    await switchSeg(w, 'kPa')
    const ds = pressureDatasets(w)
    expect(ds[0].label).toBe('日均压力（kPa）')
    expect(ds[0].data).toEqual(AVG_KPA)
    expect(yTitleText(w)).toBe('kPa')
    expect(axisNote(w)).toContain('纵轴 = 日均压力（kPa）')
  })

  it('上限线在 kPa 档整条不画：§7D.12 只有 N 口径阈值，前端不许自算第二套口径', async () => {
    const w = await mountLogPage()
    await switchSeg(w, 'kPa')
    expect(pressureDatasets(w)).toHaveLength(1)
    expect(axisNote(w)).toContain('kPa 档不画这条虚线')
  })

  it('切回 N 档：数据集与轴注逐字复原（切档是显示档切换，不是数据变更）', async () => {
    const w = await mountLogPage()
    await switchSeg(w, 'kPa')
    await switchSeg(w, 'N')
    const ds = pressureDatasets(w)
    expect(ds[0].label).toBe('日均压力（N）')
    expect(ds[0].data).toEqual(AVG_N)
    expect(ds).toHaveLength(2)
    expect(yTitleText(w)).toBe('N')
  })

  it('佩戴时长那条轴注不随压力档变（只有压力图接显示档）', async () => {
    const w = await mountLogPage()
    const noteBefore = w.findAll('.axis-note')[1].text()
    await switchSeg(w, 'kPa')
    expect(w.findAll('.axis-note')[1].text()).toBe(noteBefore)
  })
})

describe('矫形日志 · 五.3 fail-closed（kPa 整列不可换算 ⇒ 不画图也不退化成 0）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    document.body.innerHTML = ''
    vi.mocked(api.fetchPatientDailyWear).mockImplementation(async () => fx.rows('absent') as never)
  })

  it('kPa 档出面积提示而不是空轴假图', async () => {
    const w = await mountLogPage()
    await switchSeg(w, 'kPa')
    expect(pressureLine(w).exists()).toBe(false)
    expect(pressureEmptyText(w)).toBe(AREA_MISSING_HINT)
  })

  it('反证：同一份行数据在 N 档照常画图 ⇒ 上一条的红牌来自档位而不是夹具坏了', async () => {
    const w = await mountLogPage()
    expect(pressureLine(w).exists()).toBe(true)
    expect(pressureDatasets(w)[0].data).toEqual(AVG_N)
  })
})

describe('矫形日志 · 裁定 e：档位记忆与切档零请求', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    document.body.innerHTML = ''
    vi.mocked(api.fetchPatientDailyWear).mockImplementation(async () => fx.rows('present') as never)
  })

  it('切档写 admin_monitor_unit，与实时监控页同一枚键（同一台机器只该有一个偏好）', async () => {
    const w = await mountLogPage()
    await switchSeg(w, 'kPa')
    expect(localStorage.getItem(MONITOR_UNIT_STORAGE_KEY)).toBe('kPa')
  })

  it('进页即恢复 kPa（记忆优先于默认档），首屏喂给图的就是 kPa 列', async () => {
    localStorage.setItem(MONITOR_UNIT_STORAGE_KEY, 'kPa')
    const w = await mountLogPage()
    expect(w.find('.unit-seg-btn.unit-seg-active').text()).toBe('kPa')
    expect(pressureDatasets(w)[0].label).toBe('日均压力（kPa）')
  })

  it('切档不重新取数（运行时对照日佩戴请求计数）', async () => {
    const w = await mountLogPage()
    const calls = vi.mocked(api.fetchPatientDailyWear).mock.calls.length
    expect(calls).toBeGreaterThan(0)
    await switchSeg(w, 'kPa')
    await switchSeg(w, 'N')
    await switchSeg(w, 'kPa')
    expect(vi.mocked(api.fetchPatientDailyWear).mock.calls.length).toBe(calls)
  })
})

// ===== 纯层：日聚合 kPa 列的逐位语义 =====

describe('alignWearSeries 的 kPa 列与 N 列逐日同位（T643）', () => {
  it('缺日是 null；有 N 数而 kPa 为 null 的行照位保留（不可换算 ≠ 无数据）', async () => {
    const { alignWearSeries, rangeForDays } = await import('../src/utils/workbenchData')
    const rows = [
      { date: '2026-09-23', wearMinutes: 480, avgPressure: 30, maxPressure: 40, avgPressureKpa: 470, maxPressureKpa: 620, maxPoint: 'P01', frameCount: 700, abnormalCount: 0 },
      { date: '2026-09-21', wearMinutes: 300, avgPressure: 26, maxPressure: 33, avgPressureKpa: null, maxPressureKpa: null, maxPoint: 'P02', frameCount: 500, abnormalCount: 0 },
    ]
    const s = alignWearSeries(rows as never, rangeForDays(7, new Date('2026-09-23T06:00:00Z')))
    expect(s.dates).toHaveLength(7)
    expect(s.avgPressure).toHaveLength(7)
    expect(s.avgPressureKpa).toHaveLength(7)
    expect(s.avgPressure.map((v, i) => [v, s.avgPressureKpa[i]])).toEqual([
      [null, null], [null, null], [null, null], [null, null], [26, null], [null, null], [30, 470],
    ])
  })

  it('行上根本没有 kPa 字段时整列为 null（后端 A 腿未合前的形态，页面走 fail-closed）', async () => {
    const { alignWearSeries, rangeForDays } = await import('../src/utils/workbenchData')
    const s = alignWearSeries(
      [{ date: '2026-09-23', wearMinutes: 480, avgPressure: 30, maxPressure: 40, maxPoint: 'P01', frameCount: 700, abnormalCount: 0 }] as never,
      rangeForDays(7, new Date('2026-09-23T06:00:00Z')),
    )
    expect(s.avgPressure[6]).toBe(30)
    expect(s.avgPressureKpa).toEqual(Array.from({ length: 7 }, () => null))
  })
})

describe('mockPatientDailyWear 出服务端替身的 kPa 派生（选型 A：口径只有一份实现）', () => {
  it('面积在位的患者：逐日既有 N 也有 kPa，且同一区间两次调用一致', async () => {
    const { mockPatientDailyWear } = await import('../src/mock/patients')
    const rows = mockPatientDailyWear('PT-001', '2026-09-17', '2026-09-23')
    expect(rows).toHaveLength(7)
    for (const r of rows) {
      expect(typeof r.avgPressureKpa).toBe('number')
      expect(typeof r.maxPressureKpa).toBe('number')
    }
    expect(rows).toEqual(mockPatientDailyWear('PT-001', '2026-09-17', '2026-09-23'))
  })

  it('PT-003（面积未配置）：N 列照旧有数、kPa 两列恒 null —— 页面据此才走 fail-closed', async () => {
    const { mockPatientDailyWear } = await import('../src/mock/patients')
    const rows = mockPatientDailyWear('PT-003', '2026-09-17', '2026-09-23')
    expect(rows.length).toBeGreaterThan(0)
    for (const r of rows) {
      expect(r.avgPressure).toBeGreaterThan(0)
      expect(r.avgPressureKpa).toBeNull()
      expect(r.maxPressureKpa).toBeNull()
    }
  })
})

// ===== 零换算门禁（派发单 §三 选型 A）=====

describe('admin 显示层零重复换算门禁（T643 覆盖面）', () => {
  const files: Array<[string, string]> = [
    ['src/pages/monitor/index.vue', srcOf('../src/pages/monitor/index.vue')],
    ['src/pages/orthosis-log/index.vue', srcOf('../src/pages/orthosis-log/index.vue')],
    ['src/utils/workbenchData.ts', srcOf('../src/utils/workbenchData.ts')],
    ['src/utils/unitPref.ts', srcOf('../src/utils/unitPref.ts')],
  ]

  // 分工：`* 10` 这颗针只咬「压力显示层」三个文件。workbenchData.ts 里合法存在一处
  // 「分钟→小时保一位小数」的 `* 10`（与压力单位无关，砍了它等于给换算开路），
  // 所以它只过面积字段与 mockKpaOf 两颗针 —— 针的范围要按被筛物的语义划，不按文件名一刀切。
  const pressureDisplayFiles = files.filter(([name]) => !name.includes('workbenchData'))

  it.each(pressureDisplayFiles)('%s 里既无换算公式也不读面积', (_name, src) => {
    expect(src).not.toMatch(/\*[^\S\n]*10\b/)
    expect(src).not.toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
    expect(src).not.toMatch(/contactAreaCm2|\.\s*areaCm2\b|mockKpaOf/)
  })

  it('workbenchData.ts 只过面积与 mockKpaOf 两颗针（分钟→小时那处 * 10 是合法量纲折算）', () => {
    const src = srcOf('../src/utils/workbenchData.ts')
    expect(src).not.toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
    expect(src).not.toMatch(/contactAreaCm2|mockKpaOf/)
  })

  it('反面自检：三颗针对「前端自己换算」的写法各有感', () => {
    const formula = 'const kpa = (d.avgPressure / contactAreaCm2) * 10'
    const compact = 'const kpa = n/areaCm2*10'
    const viaStandIn = 'const kpa = mockKpaOf(d.avgPressure, areaCm2)'
    expect(formula).toMatch(/\*[^\S\n]*10\b/)
    expect(compact).toMatch(/\*[^\S\n]*10\b/)
    expect(formula).toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
    expect(compact).toMatch(/\/[^\S\n]*[\w$]*[Aa]rea[\w$]*/)
    expect(viaStandIn).toMatch(/mockKpaOf/)
  })

  it('换算只有一处实现，且在显示层之外（mock 里的服务端替身）', () => {
    const mockSrc = srcOf('../src/mock/patients.ts')
    expect(mockSrc).toMatch(/function mockKpaOf/)
    expect(mockSrc).toMatch(/服务端替身/)
    expect(files.map(([, s]) => s).join('\n')).not.toMatch(/mockKpaOf/)
  })

  it('kPa 一律读后端派生字段名，不新建列', () => {
    expect(srcOf('../src/pages/orthosis-log/index.vue')).toMatch(/avgPressureKpa/)
    expect(srcOf('../src/pages/monitor/index.vue')).toMatch(/pressureKpa|heatmapMaxKpa/)
    expect(srcOf('../src/utils/workbenchData.ts')).toMatch(/avgPressureKpa\?: number \| null/)
  })
})
