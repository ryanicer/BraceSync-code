/**
 * T533 —— 后台把患者从 A 切到 B，实时快照请求必须只出现一次。
 *
 * 缺陷：本页同时挂了 el-select 的 @change 与 watch(selectedPatientId)，两条都调 handlePatientChange
 * ⇒ 用户一次选择跑两遍 refreshTick。双接线同出 bec08ae4（2026-08-27），T513 base 15edbbff 上即存在。
 *
 * 为什么走 mount 而不是 Playwright：admin 的 e2e 跑 USE_MOCK dev server，api 层在浏览器进程内直接
 * return mock，网络面板里没有这条请求可数 ⇒ 请求次数只能把 api 模块换成计数器来读。
 * staging 网络面板/har 那一格需要部署与 admin 凭据，另在 T533 卡内登记，不由本用例冒充。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import MonitorPage from '../src/pages/monitor/index.vue'
import { mockPatientRealtime } from '../src/mock/patients'

const served = vi.hoisted(() => ({ ids: ['PT-001', 'PT-002'] as string[] }))
const realtimeCalls = vi.hoisted(() => [] as string[])

vi.mock('../src/api', () => ({
  fetchPatients: vi.fn(async () => ({
    list: served.ids.map((pid) => ({ patientId: pid, name: pid, deviceId: `DEV-${pid}` })),
    total: served.ids.length,
  })),
  fetchPatientRealtime: vi.fn(async (pid: string) => {
    realtimeCalls.push(pid)
    return mockPatientRealtime(pid)
  }),
}))

let wrapper: VueWrapper | null = null

/** 只推进微任务与毫秒级定时器：页面轮询是 1000ms 一档，断言前绝不允许它自己打一次请求 */
async function settle(rounds = 10) {
  for (let i = 0; i < rounds; i++) {
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
  }
}

async function mountMonitor() {
  wrapper = mount(MonitorPage, { global: { plugins: [ElementPlus] } })
  await settle()
  return wrapper
}

/** 走真实交互：展开下拉 → 点选项，由 Element Plus 自己决定发哪几个事件（不手工 emit，免得测的是我假设的接线） */
async function pickPatient(pid: string) {
  const sel = wrapper!.find('.el-select')
  expect(sel.exists(), '页面上没有患者下拉').toBe(true)
  const trigger = sel.element.querySelector<HTMLElement>('.el-select__wrapper') ?? (sel.element as HTMLElement)
  trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await settle(4)
  const pops = [...document.querySelectorAll<HTMLElement>('.el-popper[aria-hidden="false"]')]
  expect(pops.length, '展开中的下拉面板应恰有 1 个').toBe(1)
  const items = [...pops[0].querySelectorAll<HTMLElement>('.el-select-dropdown__item')]
  const label = `${pid} · ${pid}`
  const opt = items.find((li) => (li.textContent ?? '').trim() === label)
  expect(opt, `下拉里没有「${label}」，实到：${items.map((li) => (li.textContent ?? '').trim()).join(' / ')}`).toBeTruthy()
  opt!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await settle()
}

describe('T533 切患者 = 一次实时请求', () => {
  beforeEach(() => {
    // 保留真实 rAF（vue-chartjs 首帧要用），只冻住定时器与钟
    vi.useFakeTimers({
      toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'],
    })
    realtimeCalls.length = 0
    served.ids = ['PT-001', 'PT-002']
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.useRealTimers()
  })

  it('挂载后首帧只请求一次（双接线时这里就是两次）', async () => {
    await mountMonitor()
    expect(realtimeCalls).toEqual(['PT-001'])
  })

  it('从 PT-001 选到 PT-002：fetchPatientRealtime 恰好被调一次，且带的是新患者', async () => {
    await mountMonitor()
    realtimeCalls.length = 0
    await pickPatient('PT-002')
    expect(realtimeCalls).toEqual(['PT-002'])
  })

  it('来回连切两次，每次切换各发一次（多出来的那次会在这里现形）', async () => {
    await mountMonitor()
    realtimeCalls.length = 0
    await pickPatient('PT-002')
    await pickPatient('PT-001')
    expect(realtimeCalls).toEqual(['PT-002', 'PT-001'])
  })
})
