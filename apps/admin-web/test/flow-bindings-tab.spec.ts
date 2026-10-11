// T666：流程绑定独立 Tab 后，组件的空态分支在 e2e(USE_MOCK dev server) 里造不出来
// （mock 基线自带「默认告警流程」且其有 2 个种子实例不可删），故空态用组件级单测钉住。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import type { FlowTemplate, FlowTypeBinding } from '../src/api/flow'

const BINDINGS: FlowTypeBinding[] = [
  { alertType: 'pressure_high', alertTypeName: '压力偏高', templateId: null, templateName: null, updatedAt: null, updatedBy: null, updatedByName: null },
  { alertType: 'wear_interrupt', alertTypeName: '设备离线', templateId: null, templateName: null, updatedAt: null, updatedBy: null, updatedByName: null },
  { alertType: 'wear_duration_short', alertTypeName: '佩戴时长不足', templateId: null, templateName: null, updatedAt: null, updatedBy: null, updatedByName: null },
  { alertType: 'sensor_drift', alertTypeName: '传感器标定异常', templateId: null, templateName: null, updatedAt: null, updatedBy: null, updatedByName: null },
]

const TEMPLATE: FlowTemplate = {
  templateId: 'FLOW_T0001', name: '默认告警流程', nodes: [], edges: [],
  creator: 'ops_admin', creatorName: '运营管理员', version: 1,
  createdAt: '2026-10-11T10:00:00+08:00', updatedAt: '2026-10-11T10:00:00+08:00', instanceCount: 0,
}

vi.mock('../src/api/flow', () => ({
  fetchFlowTypeBindings: vi.fn(async () => structuredClone(BINDINGS)),
  fetchFlowTemplates: vi.fn(async () => [] as FlowTemplate[]),
  saveFlowTypeBindingsApi: vi.fn(async (items: unknown) => items),
}))

import * as flowApi from '../src/api/flow'
import FlowTypeBindings from '../src/pages/alerts/flow/FlowTypeBindings.vue'

async function settle() {
  for (let i = 0; i < 5; i++) await flushPromises()
}

async function mountCard() {
  const wrapper = mount(FlowTypeBindings, {
    attachTo: document.body,
    global: { plugins: [ElementPlus] },
  })
  await settle()
  return wrapper
}

describe('T666 FlowTypeBindings 空态与基础渲染', () => {
  beforeEach(() => {
    vi.mocked(flowApi.fetchFlowTemplates).mockResolvedValue([])
  })

  it('无模板时空态可见：指引到流程配置 Tab，且不再出现旧文案「下方设计器」', async () => {
    const wrapper = await mountCard()
    expect(wrapper.findAll('.ftb-row')).toHaveLength(4)
    const empty = wrapper.find('.ftb-no-tpl')
    expect(empty.exists()).toBe(true)
    expect(empty.text()).toContain('流程配置')
    expect(empty.text()).not.toContain('下方设计器')
    expect(empty.text()).toContain('重新拉取')
    wrapper.unmount()
  })

  it('有模板时不出空态，下拉候选包含模板名', async () => {
    vi.mocked(flowApi.fetchFlowTemplates).mockResolvedValue([structuredClone(TEMPLATE)])
    const wrapper = await mountCard()
    expect(wrapper.find('.ftb-no-tpl').exists()).toBe(false)
    // el-option 内容 teleport 到 body，DOM 文本取不到；按 Vue 组件实例的 label 属性断言候选
    const labels = wrapper.findAllComponents({ name: 'ElOption' }).map((o) => String(o.props('label') ?? ''))
    expect(labels.some((l) => l.includes('默认告警流程'))).toBe(true)
    wrapper.unmount()
  })
})
