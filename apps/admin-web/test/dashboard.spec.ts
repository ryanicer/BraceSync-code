// Dashboard 页渲染单测：6 KPI + 4 图 + 2 排行（mock 数据）
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import DashboardPage from '../src/pages/dashboard/index.vue'

async function flushAll() {
  // mock 层带 150ms 延迟，fake timers 下推进多轮确保 Promise.all 全部落定
  for (let i = 0; i < 6; i++) {
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
  }
}

describe('Dashboard 页', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.clear()
    setActivePinia(createPinia())
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('渲染 6 个 KPI 卡片且数值来自 mock 契约', async () => {
    const wrapper = mount(DashboardPage, {
      global: { plugins: [createPinia(), ElementPlus] },
    })
    await flushAll()

    const labels = ['累计患者', '今日活跃佩戴', '今日告警次数', '平均佩戴时长', '设备在线率', '本月新增患者']
    for (const label of labels) {
      expect(wrapper.text()).toContain(label)
    }
    expect(wrapper.text()).toContain('1256')
    expect(wrapper.text()).toContain('892')
    expect(wrapper.text()).toContain('47')
    expect(wrapper.text()).toContain('8.2h')
    expect(wrapper.text()).toContain('96.8%')
    expect(wrapper.text()).toContain('38')
    wrapper.unmount()
  })

  it('渲染 4 张图表（佩戴趋势/告警趋势/团队患者数/时长分布）', async () => {
    const wrapper = mount(DashboardPage, {
      global: { plugins: [createPinia(), ElementPlus] },
    })
    await flushAll()
    const canvases = wrapper.findAll('canvas')
    expect(canvases.length).toBe(4)
    wrapper.unmount()
  })

  it('渲染 2 个排行榜（团队/医生）', async () => {
    const wrapper = mount(DashboardPage, {
      global: { plugins: [createPinia(), ElementPlus] },
    })
    await flushAll()
    expect(wrapper.text()).toContain('团队佩戴达标排行')
    expect(wrapper.text()).toContain('医生管理患者排行')
    expect(wrapper.text()).toContain('脊柱侧弯一组')
    expect(wrapper.text()).toContain('张建国')
    expect(wrapper.text()).toContain('94.6%')
    expect(wrapper.text()).toContain('96.2%')
    wrapper.unmount()
  })
})
