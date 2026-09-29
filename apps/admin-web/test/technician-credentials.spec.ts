// T486 admin-web 腿：技师凭据弹窗要把「怎么登录」说出口（依据：派发单 T486 交付要求 3）
//
// 卡片要求创建成功弹窗明示三行：登录账号（手机号明文）／初始密码（一次性）／技师使用手机号＋密码登录。
// 前两行 T480 已在（code PR 见 showCredentials 注释），本卡补第三行 —— 技师端登录页只收
// 「手机号 + 密码」，弹窗里出现「技师编号」却不说明它不能用于登录，管理员就会拿编号去试。
//
// 🔴 段落**下标**逐条钉死，不是「页面文本含某句」：e2e/tests/admin-tech.spec.ts 的
//   A-TECH-03 / A-TECH-09 按 nth(0/1/2) 断前三个字段，谁把新句子插到中间，前端单测仍绿
//   而那条 Playwright 会在 CI 上判红。这里提前用同一套下标，两侧同向。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import TechniciansPage from '../src/pages/technicians/index.vue'
import { __resetOrgForTest, mockCreateTechnician, mockTechnicians } from '../src/mock/org'

/** 页面里的那句提示词（technicians/index.vue showCredentials）；逐字对平，改措辞必须同时改这里 */
const LOGIN_HINT = '技师使用手机号 + 密码登录（技师编号不能用于登录）。'

/** 弹窗正文逐段文本（p 元素顺序＝页面渲染顺序） */
const CRED_PARAGRAPHS = 5

async function flushAll() {
  // mock 层带 150ms 延迟；MessageBox 有过渡，推进多轮确保落定
  for (let i = 0; i < 8; i++) {
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
  }
}

function mountPage(): VueWrapper {
  return mount(TechniciansPage, { global: { plugins: [ElementPlus] } })
}

function buttonsIn(root: ParentNode): HTMLButtonElement[] {
  return [...root.querySelectorAll<HTMLButtonElement>('button')]
}

function clickByText(root: ParentNode, text: string): void {
  const btn = buttonsIn(root).find((b) => (b.textContent ?? '').includes(text))
  if (!btn) throw new Error(`找不到文案为「${text}」的按钮`)
  btn.click()
}

function dialogIn(root: ParentNode): HTMLElement {
  const dlg = [...root.querySelectorAll<HTMLElement>('.el-dialog')].pop()
  if (!dlg) throw new Error('模态框未渲染')
  return dlg
}

function formItem(root: ParentNode, label: string): HTMLElement {
  const item = [...root.querySelectorAll<HTMLElement>('.el-form-item')]
    .find((i) => (i.querySelector('.el-form-item__label')?.textContent ?? '').includes(label))
  if (!item) throw new Error(`找不到表单项「${label}」`)
  return item
}

function typeInto(root: ParentNode, label: string, value: string): void {
  const input = formItem(root, label).querySelector('input')!
  input.value = value
  input.dispatchEvent(new Event('input'))
}

function lastMessageBox(): HTMLElement {
  const boxes = document.querySelectorAll<HTMLElement>('.el-message-box')
  const box = boxes[boxes.length - 1]
  if (!box) throw new Error('凭据弹窗未渲染')
  return box
}

function credParagraphs(box: HTMLElement): string[] {
  return [...box.querySelectorAll<HTMLElement>('p')].map((p) => (p.textContent ?? '').trim())
}

/** 走完「新建技师 → 填三项 → 选团队 → 确认创建」，返回凭据弹窗 */
async function createTechnician(wrapper: VueWrapper, name: string, phone: string): Promise<HTMLElement> {
  clickByText(wrapper.element, '新建技师')
  await flushAll()
  const dlg = dialogIn(wrapper.element)
  typeInto(dlg, '姓名', name)
  typeInto(dlg, '手机号', phone)
  const select = formItem(dlg, '所属团队').querySelector<HTMLElement>('.el-select__wrapper')
  if (!select) throw new Error('所属团队下拉未渲染')
  select.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushAll()
  const pops = [...document.querySelectorAll<HTMLElement>('.el-popper[aria-hidden="false"]')]
  const opt = pops
    .flatMap((p) => [...p.querySelectorAll<HTMLElement>('.el-select-dropdown__item')])
    .find((li) => (li.textContent ?? '').trim() === '脊柱侧弯一组')
  if (!opt) throw new Error(`团队下拉里没有「脊柱侧弯一组」，展开面板 ${pops.length} 个`)
  opt.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushAll()
  clickByText(dialogIn(wrapper.element), '确认创建')
  await flushAll()
  return lastMessageBox()
}

function cellText(wrapper: VueWrapper, header: string, rowName: string): string {
  const ths = wrapper.findAll('th')
  const col = ths.findIndex((th) => th.text().trim() === header)
  if (col < 0) throw new Error(`表头里没有「${header}」列`)
  const row = wrapper.findAll('tbody tr').find((r) => r.text().includes(rowName))
  if (!row) throw new Error(`列表里没有「${rowName}」那一行`)
  return row.findAll('td')[col].text().trim()
}

describe('T486 创建技师成功弹窗：三行凭据 + 登录方式说明', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    __resetOrgForTest()
    document.body.innerHTML = ''
  })

  afterEach(() => {
    __resetOrgForTest()
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('段落数与下标：编号／登录手机号／初始密码／登录方式／一次性说明', async () => {
    const wrapper = mountPage()
    await flushAll()
    const cred = await createTechnician(wrapper, 'T486新建技师', '13800000270')
    const ps = credParagraphs(cred)
    expect(ps).toHaveLength(CRED_PARAGRAPHS)
    // 0/1/2 三格与 e2e A-TECH-03 的下标断言同形
    expect(ps[0]).toMatch(/^技师编号：TECH-\d+$/)
    expect(ps[1]).toBe('登录手机号：13800000270')
    expect(ps[2]).toMatch(/^初始密码：Br[a-z0-9]{8}#7$/)
    expect(ps[3]).toBe(LOGIN_HINT)
    expect(ps[4]).toContain('仅此一次展示，关闭后不可再看')
    expect(buttonsIn(cred).some((b) => b.textContent?.includes('我已转交本人'))).toBe(true)
    wrapper.unmount()
  })

  it('登录方式那一句是独立段落，没被并进一次性说明（并句会让 e2e 的下标断言假绿）', async () => {
    const wrapper = mountPage()
    await flushAll()
    const cred = await createTechnician(wrapper, 'T486独立段技师', '13800000271')
    const ps = credParagraphs(cred)
    expect(ps.filter((t) => t.includes('手机号 + 密码登录'))).toHaveLength(1)
    expect(ps[4]).not.toContain('手机号 + 密码登录')
    wrapper.unmount()
  })

  it('明文手机号只在弹窗那一格：新行回列表仍是 §9.2 脱敏串（掩码口径不回退）', async () => {
    const wrapper = mountPage()
    await flushAll()
    const cred = await createTechnician(wrapper, 'T486掩码技师', '13800000272')
    expect(cred.textContent).toContain('13800000272')
    // 页面主体（不含 portal 到 body 的弹窗）不得出现明文
    expect(wrapper.text()).not.toContain('13800000272')
    expect(cellText(wrapper, '手机号', 'T486掩码技师')).toBe('138****0272')
    const row = mockTechnicians({ page: 1, pageSize: 100 }).list.find((t) => t.name === 'T486掩码技师')
    expect(row?.phoneMasked).toBe('138****0272')
    expect(row?.phoneState).toBe('masked')
    wrapper.unmount()
  })
})

describe('T486 重置密码弹窗：与创建共用同一形状', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    __resetOrgForTest()
    document.body.innerHTML = ''
  })

  afterEach(() => {
    __resetOrgForTest()
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('重置后同样带登录方式说明，账号行读的是脱敏号（读侧没有明文）', async () => {
    const wrapper = mountPage()
    await flushAll()
    const row = wrapper.findAll('tbody tr').find((r) => r.text().includes('周师傅'))
    if (!row) throw new Error('列表里没有周师傅那一行')
    clickByText(row.element, '重置密码')
    await flushAll()
    clickByText(lastMessageBox(), '确认')
    await flushAll()
    const ps = credParagraphs(lastMessageBox())
    expect(ps).toHaveLength(CRED_PARAGRAPHS)
    expect(ps[1]).toContain('登录手机号：138****5678')
    expect(ps[2]).toMatch(/^初始密码：Br[a-z0-9]{8}#7$/)
    expect(ps[3]).toBe(LOGIN_HINT)
    wrapper.unmount()
  })
})

describe('T486 创建响应体形状（弹窗明文为何取自表单而不是响应）', () => {
  beforeEach(() => __resetOrgForTest())
  afterEach(() => __resetOrgForTest())

  it('响应只有脱敏号 + 一次性口令，没有明文字段 ⇒ 登录账号行只能来自管理员刚填的那格', () => {
    const { account, initialPassword } = mockCreateTechnician({ name: 'T486形状技师', phone: '13800000273', teamId: 'TEAM-001' })
    expect(account).not.toHaveProperty('phone')
    expect(account.phoneMasked).toBe('138****0273')
    expect(initialPassword).toMatch(/^Br[a-z0-9]{8}#7$/)
  })
})
