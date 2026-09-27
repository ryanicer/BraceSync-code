// T419 G-6/S-6 —— 「系统配置」页可见层的防回潮门禁。
//
// 为什么写在这一层：admin-web 的 Playwright 用例跑的是 USE_MOCK dev server，
// 保存请求在 api 层就被 mock 分支拦下（进程内返回，不发 HTTP），所以「表单项下线后
// 键还回不回传」这件事在 e2e 里 observable 不到；而它恰恰是这次改动最容易踩的坑 ——
// 后端 validateSettings 对 pressureFluctuationPct 做 [1,100] 区间校验
// （services/user-service/internal/handler/handler.go:2040），字段一旦从载荷里消失就是 0 ⇒
// 整个「保存配置」按钮必 400。S-6 收口只摘 UI、不动 form，需要一条断言把这个不对称钉住。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import type { SystemSettings } from '../src/mock/system'

/**
 * 后端 GET /admin/settings 的现值。
 * 🔴 三个被 S-6/G-6 触及的字段刻意取**与页面初始默认值不同**的数（默认见 settings/index.vue:260-264：
 * pressureFluctuationPct 30 / wearInterruptMinutes 60 / sensorDriftN 2.8）——
 * 若沿用默认值，「载荷里还有这个键」会用例绿、但分不清是 GET 回显链路通的、还是 form 初始值漏出来的。
 */
const SETTINGS: SystemSettings = {
  collectIntervalSeconds: 1800,
  retentionDays: 365,
  maxPatients: 10000,
  dailyWearTargetHours: 22,
  pressureHighThresholdN: 5,
  pressureLowThresholdN: 1,
  pressureFluctuationPct: 27,
  wearInterruptMinutes: 45,
  sensorDriftN: 0.3,
  wifiPresets: [],
}

vi.mock('../src/api', () => ({
  fetchSystemSettings: vi.fn(async (): Promise<SystemSettings> => ({ ...SETTINGS })),
  saveSystemSettingsApi: vi.fn(async () => undefined),
  fetchNotifyRules: vi.fn(async () => []),
  updateNotifyRuleApi: vi.fn(async () => undefined),
  fetchNotificationLogs: vi.fn(async () => ({ list: [], total: 0 })),
  patientNameOf: vi.fn((id: string) => id),
  fetchAuditLogsApi: vi.fn(async () => ({ list: [], total: 0 })),
}))

import * as api from '../src/api'
import SettingsPage from '../src/pages/settings/index.vue'

async function settle() {
  for (let i = 0; i < 5; i++) await flushPromises()
}

async function mountSettings() {
  const wrapper = mount(SettingsPage, {
    attachTo: document.body,
    global: { plugins: [ElementPlus], stubs: { RouterLink: { template: '<a><slot /></a>' } } },
  })
  await settle()
  return wrapper
}

/** 页面上渲染出来的表单标签（含两张卡的 form-item，不含注释/代码） */
function labels(wrapper: Awaited<ReturnType<typeof mountSettings>>): string[] {
  return wrapper.findAll('.el-form-item__label').map((n) => n.text().trim())
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('T419 G-6 —— 系统配置页告警阈值字段用新术语', () => {
  it('两个改名项的标签 = PRD §7D.12 V3.20 收口后的新词', async () => {
    const ls = labels(await mountSettings())
    expect(ls).toContain('设备离线判定时间（分钟）')
    expect(ls).toContain('传感器标定异常告警阈值（N）')
  })

  it('旧词整页零命中（键名与码值不动，只锁显示文案）', async () => {
    const wrapper = await mountSettings()
    const text = wrapper.text()
    for (const dead of ['佩戴中断', '传感器漂移', '矫智通']) {
      expect(text, `可见文案里不得出现「${dead}」`).not.toContain(dead)
    }
  })
})

describe('T419 S-6 —— 「压力波动幅度阈值」只摘 UI，不动载荷', () => {
  it('表单项不再渲染', async () => {
    const wrapper = await mountSettings()
    expect(labels(wrapper)).not.toContain('压力波动幅度阈值（%）')
    expect(wrapper.text()).not.toContain('压力波动幅度阈值')
  })

  it('点「保存配置」：载荷仍带 pressureFluctuationPct（缺键 ⇒ 后端按 0 校验 ⇒ 400）', async () => {
    const wrapper = await mountSettings()
    const btn = wrapper.findAll('button').find((b) => b.text().trim() === '保存配置')
    expect(btn, '找不到「保存配置」按钮').toBeTruthy()
    await btn!.trigger('click')
    await settle()

    const calls = vi.mocked(api.saveSystemSettingsApi).mock.calls
    expect(calls.length, '没有发出保存请求').toBe(1)
    const payload = calls[0][0] as unknown as Record<string, unknown>
    expect(payload).toHaveProperty('pressureFluctuationPct', SETTINGS.pressureFluctuationPct)
    // 回显链路同样不能少：GET 的现值进 form，才会被原样带回去（值刻意不等于页面初始默认，见 SETTINGS 注释）
    expect(vi.mocked(api.fetchSystemSettings)).toHaveBeenCalledTimes(1)
    expect(payload.wearInterruptMinutes).toBe(SETTINGS.wearInterruptMinutes)
    expect(payload.sensorDriftN).toBe(SETTINGS.sensorDriftN)
  })
})
