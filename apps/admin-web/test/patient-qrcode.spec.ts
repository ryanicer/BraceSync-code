// T327 患者详情抽屉「患者ID 二维码」（依据：设计稿 docs/design/admin/患者管理.html:144-152 规格 + :186-199 markup）
// 断言里的中文串逐条抄自设计稿，改文案必须同时改设计稿，否则本卡先变红。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import QRCode from 'qrcode'
import { createRouter, createMemoryHistory } from 'vue-router'
import PatientsPage from '../src/pages/patients/index.vue'

// T372 后抽屉里有「异常报告」跳转按钮 ⇒ 页面 setup 用到 useRouter()，本用例必须装一个 router
const noop = { render: () => null }

async function flushAll() {
  for (let i = 0; i < 8; i++) {
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
  }
}

let wrapper: VueWrapper | null = null

/** el-drawer 可能 teleport 到 body，也可能就地渲染 ⇒ 两处都找 */
function drawerEl(): HTMLElement {
  const el = document.querySelector('.el-drawer') ?? wrapper!.element.querySelector('.el-drawer')
  if (!el) throw new Error('患者详情抽屉未渲染')
  return el as HTMLElement
}

/** 标题「姓名（患者ID）」里括号内那段 —— 页面显示与码内载荷必须同源于它 */
function displayedId(drawer: HTMLElement): string {
  const title = drawer.querySelector('.el-drawer__title')?.textContent ?? ''
  const m = title.match(/[（(]([^）)]+)[）)]/)
  if (!m) throw new Error(`抽屉标题里没有患者ID：${title}`)
  return m[1].trim()
}

/** 从 SVG path 的「M x y h 段长 v1 h-段长 z」序列里还原暗模块总数（只数正向段，回程是负数） */
function darkModulesOf(d: string): number {
  return [...d.matchAll(/h(\d+)/g)].reduce((n, s) => n + Number(s[1]), 0)
}

async function openRow(index: number) {
  await flushAll()
  const rows = wrapper!.findAll('.el-table__body tr')
  if (!rows[index]) throw new Error(`列表只有 ${rows.length} 行，取不到第 ${index + 1} 行`)
  await rows[index].trigger('click')
  await flushAll()
}

beforeEach(() => {
  vi.useFakeTimers()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: noop },
      { path: '/patients', component: noop },
      { path: '/abnormal-report', component: noop },
    ],
  })
  wrapper = mount(PatientsPage, { global: { plugins: [router, ElementPlus] } })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.useRealTimers()
})

describe('患者详情抽屉 · 患者ID 二维码', () => {
  it('首位是二维码卡片，文案照设计稿', async () => {
    await openRow(0)
    const drawer = drawerEl()
    const card = drawer.querySelector<HTMLElement>('.pid-card')
    expect(card, '抽屉正文首位要有 .pid-card').not.toBeNull()
    // 卡片必须在「基本信息」那张描述表之前（设计稿：抽屉正文首位）
    expect(
      card!.compareDocumentPosition(drawer.querySelector('.el-descriptions') as Node) &
        Node.DOCUMENT_POSITION_FOLLOWING,
      '二维码卡片须在基本信息分组之前',
    ).toBeTruthy()

    expect(drawer.querySelector('.pid-value')!.textContent!.trim(), '左列ID要等于标题里的ID')
      .toBe(displayedId(drawer))
    expect(drawer.querySelector('.pid-meta .label')!.textContent!.trim()).toBe('患者ID')
    expect(drawer.querySelector('.pid-note')!.textContent!.trim())
      .toBe('技师端「绑定」页扫码即自动填入患者ID，避免手输长ID出错。')
    expect(drawer.querySelector('.qr-cap')!.textContent!.trim()).toBe('扫描二维码录入患者ID')
    expect(drawer.querySelector('.qr-frame svg')!.getAttribute('width'), '图形按设计稿 144×144')
      .toBe('144')
  })

  it('码内载荷 = 患者ID 明文本体，不是写死的图', async () => {
    await openRow(0)
    const drawer = drawerEl()
    const id = displayedId(drawer)
    const svg = drawer.querySelector('.qr-frame svg')!
    const path = svg.querySelector('path')!
    const expectQr = QRCode.create(id, { errorCorrectionLevel: 'M' })

    expect(svg.getAttribute('viewBox'), '模块数要和该ID编出来的矩阵一致')
      .toBe(`0 0 ${expectQr.modules.size} ${expectQr.modules.size}`)
    expect(darkModulesOf(path.getAttribute('d')!), '暗模块数须等于该ID矩阵的暗模块数')
      .toBe(Array.from(expectQr.modules.data).filter(Boolean).length)
    expect(path.getAttribute('fill'), '深色按设计稿 #333').toBe('#333333')
  })

  it('换一个患者，图形跟着换', async () => {
    await openRow(0)
    const firstId = displayedId(drawerEl())
    const firstD = drawerEl().querySelector('.qr-frame path')!.getAttribute('d')

    await drawerEl().querySelector<HTMLElement>('.el-drawer__close-btn')!.click()
    await flushAll()
    await openRow(1)
    const secondId = displayedId(drawerEl())
    const secondD = drawerEl().querySelector('.qr-frame path')!.getAttribute('d')

    expect(secondId, '两行须是不同患者').not.toBe(firstId)
    expect(secondD).not.toBe(firstD)
    expect(darkModulesOf(secondD!), '第二个患者的图形须按它自己的ID编出来')
      .toBe(Array.from(QRCode.create(secondId, { errorCorrectionLevel: 'M' }).modules.data)
        .filter(Boolean).length)
  })
})
