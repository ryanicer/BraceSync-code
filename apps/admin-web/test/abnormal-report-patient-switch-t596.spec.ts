/**
 * T596 —— 异常报告页换患者必须重新取数：表头换人后 KPI/汇总不得仍是上一名患者的响应。
 *
 * 缺陷：取数只绑「查询」按钮与挂载两处，患者下拉是纯前端状态变更 ⇒ 换人 0 新请求，
 * 表头已是新患者、四格 KPI 与汇总行还是上一名患者的响应（staging 实测 0 条挂错人）。
 *
 * 为什么走 mount：admin 的 e2e 跑 USE_MOCK dev server，api 层在浏览器进程内直接 return mock，
 * 网络面板数不到请求 ⇒ 请求次数只能把 api 模块换成计数器来读（同 T533 monitor 用例的取舍）。
 * 走真实下拉交互，由 Element Plus 自己发 change 事件（不手工 emit，免得测的是我假设的接线）。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createPinia } from 'pinia'
import AbnormalReportPage from '../src/pages/abnormal-report/index.vue'
import type { AbnormalReport } from '../src/mock/alerts'

const served = vi.hoisted(() => ({ ids: ['PT-001', 'PT-002'] as string[] }))
const reportCalls = vi.hoisted(() => [] as string[])
const gates = vi.hoisted(() => ({}) as Record<string, (rep: AbnormalReport) => void>)

function makeReport(patientId: string): AbnormalReport {
  // PT-001 空区间（复现步骤里的 0 条响应）、PT-002 total 8（复现步骤里的查询结果）
  const total = patientId === 'PT-002' ? 8 : 0
  return {
    patientId,
    start: '2026-09-30',
    end: '2026-10-06',
    total,
    byStatus: [{ key: 'pending', count: total }],
    byType: total
      ? [{ key: 'wear_duration_short', count: 5 }, { key: 'sensor_drift', count: 2 }, { key: 'wear_interrupt', count: 1 }]
      : [],
    byDay: total ? [{ key: '2026-10-01', count: total }] : [],
  }
}

vi.mock('../src/api', () => ({
  adminLogin: vi.fn(),
  fetchPatients: vi.fn(async () => ({
    list: served.ids.map((pid) => ({
      patientId: pid, name: `患者${pid}`, age: 30, gender: 'male',
      diagnosis: '脊柱侧弯', deviceId: `DEV-${pid}`, teamId: 'team-1', doctorId: 'doc-1',
    })),
    total: served.ids.length,
  })),
  fetchAbnormalReport: vi.fn(({ patientId }: { patientId: string }) => {
    reportCalls.push(patientId)
    if (gates[patientId]) return new Promise<AbnormalReport>((res) => { gates[patientId] = res })
    return Promise.resolve(makeReport(patientId))
  }),
  exportAbnormalReportApi: vi.fn(async () => undefined),
  doctorNameOf: (id?: string | null) => id || '-',
  teamNameOf: (id?: string | null) => id || '-',
}))

let wrapper: VueWrapper | null = null

/** 只推进微任务与毫秒级定时器：下拉面板的显隐靠过渡定时器，断言前推几轮让它落位 */
async function settle(rounds = 8) {
  for (let i = 0; i < rounds; i++) {
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
  }
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { render: () => null } },
      { path: '/abnormal-report', component: { render: () => null } },
    ],
  })
  await router.push('/abnormal-report')
  await router.isReady()
  wrapper = mount(AbnormalReportPage, { global: { plugins: [router, createPinia(), ElementPlus] } })
  await settle()
  return wrapper
}

/** 真实交互：点开患者下拉 → 点目标选项（同 T533 monitor 用例的 pickPatient） */
async function pickPatient(pid: string) {
  const sel = wrapper!.find('.patient-select')
  expect(sel.exists(), '页面上没有患者下拉').toBe(true)
  const trigger = sel.element.querySelector<HTMLElement>('.el-select__wrapper') ?? (sel.element as HTMLElement)
  trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await settle(4)
  const pops = [...document.querySelectorAll<HTMLElement>('.el-popper[aria-hidden="false"]')]
  expect(pops.length, '展开中的下拉面板应恰有 1 个').toBe(1)
  const items = [...pops[0].querySelectorAll<HTMLElement>('.el-select-dropdown__item')]
  const label = `${pid} · 患者${pid}`
  const opt = items.find((li) => (li.textContent ?? '').trim() === label)
  expect(opt, `下拉里没有「${label}」，实到：${items.map((li) => (li.textContent ?? '').trim()).join(' / ')}`).toBeTruthy()
  opt!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await settle()
}

describe('T596 换患者 = 重新取数', () => {
  beforeEach(() => {
    // 保留真实 rAF（vue-chartjs 首帧要用），只冻住定时器与钟
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
    reportCalls.length = 0
    for (const k of Object.keys(gates)) delete gates[k]
    served.ids = ['PT-001', 'PT-002']
    localStorage.clear()
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.useRealTimers()
  })

  it('挂载自动选首位患者并取数一次', async () => {
    await mountPage()
    expect(reportCalls).toEqual(['PT-001'])
    // 空区间提示属于 PT-001 的响应（复现步骤现象 1）
    expect(wrapper!.text()).toContain('所选区间内该患者无异常记录')
  })

  it('换选 PT-002：立即重新取数且恰一次，KPI 随表头换人（修前 0 新请求 = 复现）', async () => {
    await mountPage()
    reportCalls.length = 0
    await pickPatient('PT-002')
    expect(reportCalls, '换患者后应带新患者重新取数；双接线会多一次').toEqual(['PT-002'])
    // 表头与数据同人：汇总抬头是新患者、KPI 是新患者响应的 8（不再是上一名的 0）
    expect(wrapper!.text()).toContain('区间异常汇总 · 患者PT-002（PT-002）')
    expect(wrapper!.text()).not.toContain('所选区间内该患者无异常记录')
    const kpis = wrapper!.findAll('.kpi-value').map((n) => n.text())
    expect(kpis[0]).toBe('8')
  })

  it('来回连切两次，每次切换各重查一次', async () => {
    await mountPage()
    reportCalls.length = 0
    await pickPatient('PT-002')
    await pickPatient('PT-001')
    expect(reportCalls).toEqual(['PT-002', 'PT-001'])
  })

  it('在途的旧患者响应不许压回当前表头（连切时后到的过期响应被丢弃）', async () => {
    await mountPage()
    reportCalls.length = 0
    // 占位让下一次 PT-002 请求被扣住：mock 命中 gates 后会把占位换成真正的 resolve
    gates['PT-002'] = () => undefined
    await pickPatient('PT-002')
    await pickPatient('PT-001')
    // 此刻表头已回到 PT-001，其响应已落地（total 0）
    expect(wrapper!.findAll('.kpi-value').map((n) => n.text())[0]).toBe('0')
    // 迟到的 PT-002 total 8 响应此刻才回来
    gates['PT-002'](makeReport('PT-002'))
    await settle()
    expect(wrapper!.findAll('.kpi-value').map((n) => n.text())[0], '过期响应不得覆盖当前患者的数据').toBe('0')
  })
})
