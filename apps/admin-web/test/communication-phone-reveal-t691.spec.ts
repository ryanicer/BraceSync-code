/**
 * T691 r134（方案2a）—— 沟通页「联系电话」行：脱敏展示 + 按需 reveal + 角色裁剪。
 *
 * 判据（把 api 层换成可控桩，直连真接线，不手工改内部态）：
 *  - fetchFeedbacks 返回带 phoneMasked 的行（复现 ROLE_ADMIN 侧服务端装配）；
 *  - revealPatientPhoneApi 桩返回明文（复现独立按需解密端点）。
 * 判别面：
 *  ① admin：详情含「联系电话」行、默认显脱敏号、无明文；点「查看号码」→ 调 reveal(该 patientId) → 出明文。
 *  ② cs：整行不渲染（v-if admin），页面上找不到「联系电话」标签，也不显示脱敏号。
 *  ③ reveal 只按当前选中行的 patientId 触发一次。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import CommunicationPage from '../src/pages/communication/index.vue'
import { useAuthStore } from '../src/stores/auth'
import type { Feedback } from '@bracesync/shared-types'

const ROW: Feedback = {
  feedbackId: 'FB-001', patientId: 'PT-001', type: '佩戴不适',
  content: '左肩压痛。', submitTime: '2026-08-11T09:20:00+08:00',
  handler: null, replyContent: null, replyTime: null, status: 'pending',
  phoneMasked: '138****6688',
}

const revealCalls = vi.hoisted(() => [] as string[])

vi.mock('../src/api', () => ({
  fetchFeedbacks: vi.fn(async () => [{ ...ROW }]),
  processFeedbackApi: vi.fn(async () => undefined),
  patientNameOf: (id: string | null) => (id === 'PT-001' ? '林小雨' : id ?? ''),
  revealPatientPhoneApi: vi.fn(async (patientId: string) => {
    revealCalls.push(patientId)
    return { patientId, phone: '138 6688 6688' }
  }),
}))

let wrapper: VueWrapper | null = null
async function settle(rounds = 12) { for (let i = 0; i < rounds; i++) await flushPromises() }

async function mountAs(role: 'admin' | 'cs') {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().login(role)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { render: () => null } },
      { path: '/communication', component: { render: () => null } },
    ],
  })
  await router.push('/communication')
  await router.isReady()
  wrapper = mount(CommunicationPage, { global: { plugins: [router, pinia, ElementPlus] } })
  await settle()
  return wrapper!
}

function buttonsByText(w: VueWrapper, text: string) {
  return w.findAll('button').filter((b) => b.text().includes(text))
}

describe('T691 r134 联系电话行（方案2a）', () => {
  beforeEach(() => { revealCalls.length = 0 })
  afterEach(() => { wrapper?.unmount(); wrapper = null })

  it('admin：默认展示脱敏号、含「联系电话」行，且未点前不出现明文', async () => {
    const w = await mountAs('admin')
    const text = w.text()
    expect(text).toContain('联系电话')
    expect(text).toContain('138****6688')
    // 明文「138 6688 6688」在点查看前绝不出现
    expect(text).not.toContain('138 6688 6688')
    expect(revealCalls).toEqual([])
  })

  it('admin：点「查看号码」→ 按选中行 patientId 调 reveal，成功后渲染明文 + 已记录回显', async () => {
    const w = await mountAs('admin')
    const btn = buttonsByText(w, '查看号码')[0]
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await settle()
    expect(revealCalls).toEqual(['PT-001'])
    const text = w.text()
    expect(text).toContain('138 6688 6688')
    expect(text).toContain('已记录本次查看')
    // 明文出现后脱敏态按钮已被复制态替换
    expect(buttonsByText(w, '查看号码').length).toBe(0)
  })

  it('cs：整行摘除——找不到「联系电话」标签，也不显示脱敏号，且无任何 reveal 入口', async () => {
    const w = await mountAs('cs')
    const text = w.text()
    expect(text).not.toContain('联系电话')
    expect(text).not.toContain('138****6688')
    expect(buttonsByText(w, '查看号码').length).toBe(0)
    expect(revealCalls).toEqual([])
  })
})
