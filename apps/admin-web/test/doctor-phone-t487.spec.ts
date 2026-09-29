// T487 ③ 医护账号页：手机号撞号（后端 23505 → 10409）时把提示点名到手机号那一格
//
// 卡面判据：「唯一性 23505 友好提示」。后端只回一个 10409，而 T464 的码表句是
// 「当前状态与该操作冲突，请刷新后重试」——运营看完不知道回去改哪一格，
// 故 index.vue 的 catch 里按「本次提交里可能撞它的只有手机号」补一句点名的。
//
// 🔴 那条判断带一个前件（noStatusLeg）：编辑态改状态是紧随 PUT 的**第二个**请求
// （api/medicalAccount.ts 的 updateMedicalAccountApi 内部 PUT → POST /status），
// /status 对未绑登录账号的存量档案也回 10409（doctor_accounts_t314.go 的 ErrDoctorNoAccount）。
// 那一刻手机号其实已经写进去了，报成「手机号被占用」会把人往反方向引。
// ⇒ 反证两格：① 没填手机号 ② 填了号但这次确实改了状态 —— 两格都必须回到码表通用句。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { attachErrorMeta } from '@bracesync/shared-utils'
import DoctorsPage from '../src/pages/doctors/index.vue'
import { __resetMedicalAccountsForTest } from '../src/mock/medicalAccounts'
import { updateMedicalAccountApi } from '../src/api/medicalAccount'

// 只换写侧两条，读侧继续走仓内 mock（列表 9 行、团队/职称下拉的既有装配都不动）
vi.mock('../src/api/medicalAccount', async (importOriginal) => {
  const mod = await importOriginal<typeof import('../src/api/medicalAccount')>()
  return { ...mod, updateMedicalAccountApi: vi.fn(), createMedicalAccountApi: vi.fn() }
})

async function flushAll() {
  for (let i = 0; i < 8; i++) {
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
  }
}

/** ElMessage 3s 自动消失，fake timers 下要立刻读；写请求 + 列表刷新各带 150ms 延迟 */
async function flushToast(ms = 400) {
  await vi.advanceTimersByTimeAsync(ms)
  await flushPromises()
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

function clickByText(root: ParentNode, text: string): void {
  const btn = [...root.querySelectorAll<HTMLButtonElement>('button')]
    .find((b) => (b.textContent ?? '').includes(text))
  if (!btn) throw new Error(`找不到文案为「${text}」的按钮`)
  btn.click()
}

/** el-select 面板 teleport 到 body，且未展开的那个也挂在那里 ⇒ 只认当前展开的那一个 */
async function pickDropdownItem(trigger: Element, text: string): Promise<void> {
  const wrapper = trigger.querySelector<HTMLElement>('.el-select__wrapper') ?? (trigger as HTMLElement)
  wrapper.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushAll()
  const pops = [...document.querySelectorAll<HTMLElement>('.el-popper[aria-hidden="false"]')]
  if (pops.length !== 1) throw new Error(`展开中的下拉面板应恰有 1 个，实到 ${pops.length} 个`)
  const opt = [...pops[0].querySelectorAll<HTMLElement>('.el-select-dropdown__item')]
    .find((li) => (li.textContent ?? '').trim() === text)
  if (!opt) throw new Error(`展开中的下拉里没有「${text}」，实到：` +
    [...pops[0].querySelectorAll<HTMLElement>('.el-select-dropdown__item')]
      .map((li) => (li.textContent ?? '').trim()).join(' / '))
  opt.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushAll()
}

const phoneTaken = () => attachErrorMeta(
  new Error('phone already used as login credential by another account'),
  { code: 10409, httpStatus: 409 })

/** 打开第一行（张建国 / DOC-001，已绑登录账号）的编辑弹窗 */
async function openEdit(wrapper: VueWrapper): Promise<HTMLElement> {
  clickByText(wrapper.findAll('tbody tr')[0].element, '编辑')
  await flushAll()
  return dialogIn(wrapper.element)
}

describe('T487 医护账号页：手机号撞号提示', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    __resetMedicalAccountsForTest()
    document.body.innerHTML = ''
    vi.mocked(updateMedicalAccountApi).mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('填新号保存回 10409 ⇒ 点名手机号，不透后端英文原文', async () => {
    vi.mocked(updateMedicalAccountApi).mockRejectedValue(phoneTaken())
    const wrapper = mount(DoctorsPage, { global: { plugins: [ElementPlus] } })
    await flushAll()
    const dlg = await openEdit(wrapper)
    typeInto(dlg, '手机号', '13900002222')
    clickByText(dlg, '保存修改')
    await flushToast()
    const toast = document.body.textContent ?? ''
    expect(toast).toContain('该手机号已被其他后台账号用作登录凭据')
    expect(toast).not.toContain('phone already used') // 后端英文原文绝不给用户
    wrapper.unmount()
  })

  it('反证①：没填手机号也回 10409 ⇒ 回码表通用句，不许报手机号', async () => {
    vi.mocked(updateMedicalAccountApi).mockRejectedValue(phoneTaken())
    const wrapper = mount(DoctorsPage, { global: { plugins: [ElementPlus] } })
    await flushAll()
    const dlg = await openEdit(wrapper)
    // 手机号一格留空 = 这次提交根本没碰号（T361 口径），10409 只可能来自别处
    clickByText(dlg, '保存修改')
    await flushToast()
    const toast = document.body.textContent ?? ''
    expect(toast).toContain('当前状态与该操作冲突，请刷新后重试')
    expect(toast).not.toContain('该手机号已被其他后台账号')
    wrapper.unmount()
  })

  it('反证②：这次同时改了状态 ⇒ 10409 归状态那一发，不许报手机号', async () => {
    vi.mocked(updateMedicalAccountApi).mockRejectedValue(phoneTaken())
    const wrapper = mount(DoctorsPage, { global: { plugins: [ElementPlus] } })
    await flushAll()
    const dlg = await openEdit(wrapper)
    typeInto(dlg, '手机号', '13900002222')
    // 设计稿 :378 编辑态可改状态；稿面这一格叫「初始状态」，禁用项整句带括号说明
    await pickDropdownItem(formItem(dlg, '初始状态'), '禁用（暂不开放登录）')
    clickByText(dlg, '保存修改')
    await flushToast()
    const toast = document.body.textContent ?? ''
    // 走到这里时 PUT 已经成功、号已经落库，红的是紧随其后的 POST /status
    expect(toast).toContain('当前状态与该操作冲突，请刷新后重试')
    expect(toast).not.toContain('该手机号已被其他后台账号')
    wrapper.unmount()
  })

  it('帮助语口径：填了号就能用它登录，旧句「不承担登录职责」不得残留', async () => {
    // 🔴 这条锁的是文案而非交互：派发单第 3 条让手机号重新成为登录凭据，
    // 而本页帮助语在 T487 前写的是「手机号不承担登录职责」——运营看完这句就不会
    // 理解「为什么填了号保存会被拒（号已被别人占作凭据）」。DOM 直接读，不用读源码，
    // 因为这句是给用户看的，渲染路径坏了同样算没交付。
    const wrapper = mount(DoctorsPage, { global: { plugins: [ElementPlus] } })
    await flushAll()
    const dlg = await openEdit(wrapper)
    const help = formItem(dlg, '手机号').querySelector('.form-help')?.textContent ?? ''
    expect(help).toContain('也能用来登录后台')
    expect(help).not.toContain('不承担登录职责')
    wrapper.unmount()
  })
})
