// T315 医护账号管理页（依据：设计稿 docs/design/admin/医护账号.html + PRD §7D.10）
// 断言里的中文串逐条抄自设计稿，改文案必须同时改设计稿，否则本卡先变红。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import DoctorsPage from '../src/pages/doctors/index.vue'
import {
  MEDICAL_TITLES,
  __resetMedicalAccountsForTest,
  mockCreateMedicalAccount,
  mockMedicalAccounts,
  mockSetMedicalAccountStatus,
} from '../src/mock/medicalAccounts'

async function flushAll() {
  // mock 层带 150ms 延迟；el-dialog / MessageBox 有过渡，推进多轮确保落定
  for (let i = 0; i < 8; i++) {
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
  }
}

/** ElMessage 默认 3s 自动消失，fake timers 下要立刻读，别用 flushAll；写操作后还要等 mock 延迟 + 列表刷新 */
async function flushToast(ms = 200) {
  await vi.advanceTimersByTimeAsync(ms)
  await flushPromises()
}

function mountPage(): VueWrapper {
  return mount(DoctorsPage, { global: { plugins: [ElementPlus] } })
}

/**
 * 在给定子树里取按钮/表单项。
 * VTU 默认把 wrapper 挂在 document 之外，所以 el-dialog 要用 wrapper 查；
 * ElMessageBox / ElMessage 自带 portal 到 document.body，所以那两处传 document。
 */
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

/** 按 label 元素精确匹配 —— 整项 textContent 会含 form-help 正文，「手机号」的说明里就写着「登录账号」 */
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
  if (!box) throw new Error('确认框未渲染')
  return box
}

function toastText(): string {
  return document.body.textContent ?? ''
}

describe('医护账号页 列表（设计稿 :141 十列 + :135 计数）', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    __resetMedicalAccountsForTest()
  })

  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('表头 10 列且顺序与设计稿一致', async () => {
    const wrapper = mountPage()
    await flushAll()
    const headers = wrapper.findAll('.el-table__header th').map((th) => th.text().trim()).filter(Boolean)
    expect(headers).toEqual(['姓名', '登录账号', '手机号', '科室', '所属团队', '职称', '管理患者数', '状态', '创建时间', '操作'])
    wrapper.unmount()
  })

  it('账号取自医生档案：登录账号 doc + 5 位序号、管理患者数是主诊数', async () => {
    const wrapper = mountPage()
    await flushAll()
    const first = wrapper.find('tbody tr')
    expect(first.text()).toContain('张建国')
    expect(first.text()).toContain('doc00001')
    expect(first.text()).toContain('68')
    wrapper.unmount()
  })

  it('手机号空值显示横杠（§9.2 / 设计稿 :299），不留空白格', async () => {
    const wrapper = mountPage()
    await flushAll()
    const row = wrapper.findAll('tbody tr').find((r) => r.text().includes('刘医生'))
    expect(row).toBeTruthy()
    expect(row!.text()).toContain('—')
    wrapper.unmount()
  })

  it('计数提示 = 共 N 个账号（启用 X / 禁用 Y），随筛选变化', async () => {
    const wrapper = mountPage()
    await flushAll()
    expect(wrapper.find('.count-hint').text()).toBe('共 9 个账号（启用 8 / 禁用 1）')
    await wrapper.find('.search-input input').setValue('赵敏')
    await flushAll()
    expect(wrapper.find('.count-hint').text()).toBe('共 1 个账号（启用 0 / 禁用 1）')
    wrapper.unmount()
  })

  it('筛选无命中时走设计稿文案「无匹配的医护账号」', async () => {
    const wrapper = mountPage()
    await flushAll()
    await wrapper.find('.search-input input').setValue('不存在的人')
    await flushAll()
    expect(wrapper.find('.el-table__empty-text').text()).toBe('无匹配的医护账号')
    wrapper.unmount()
  })
})

describe('医护账号页 模态框与写操作（PRD §7D.10 八字段）', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    __resetMedicalAccountsForTest()
    document.body.innerHTML = ''
  })

  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('新建：登录账号只读且提示系统自动生成，初始密码只读占位', async () => {
    const wrapper = mountPage()
    await flushAll()
    clickByText(wrapper.element, '新建医护账号')
    await flushAll()
    const dlg = dialogIn(wrapper.element)
    expect(dlg.querySelector('.el-dialog__title')?.textContent?.trim()).toBe('新建医护账号')
    const acct = formItem(dlg, '登录账号')
    const acctInput = acct.querySelector('input') as HTMLInputElement | null
    expect(acctInput?.disabled).toBe(true) // 系统生成、运营不可输入（设计稿 :226 readonly+disabled）
    expect(acctInput?.value).toContain('系统自动生成')
    const pwd = formItem(dlg, '初始密码').querySelector('input') as HTMLInputElement | null
    expect(pwd?.value).toBe('系统随机生成，创建成功后一次性展示')
    // 手机号选填（设计稿 :221）：label 上不带必填星号
    const phone = formItem(dlg, '手机号')
    expect(phone.className).not.toContain('is-required')
    expect(phone.querySelector('input')?.placeholder).toBe('选填，11位手机号')
    wrapper.unmount()
  })

  it('编辑：标题与按钮改文案，且隐藏初始密码组（设计稿 :365）', async () => {
    const wrapper = mountPage()
    await flushAll()
    clickByText(wrapper.findAll('tbody tr')[0].element, '编辑')
    await flushAll()
    const dlg = dialogIn(wrapper.element)
    expect(dlg.querySelector('.el-dialog__title')?.textContent?.trim()).toBe('编辑医护账号')
    expect(dlg.textContent).not.toContain('初始密码')
    const acct = formItem(dlg, '登录账号').querySelector('input') as HTMLInputElement | null
    expect(acct?.value).toBe('doc00001（系统生成，不可改）')
    expect(buttonsIn(dlg).some((b) => b.textContent?.includes('保存修改'))).toBe(true)
    wrapper.unmount()
  })

  it('编辑：手机号框永不预填脱敏串，留空保存 ⇒ 库内号码原样保留（T361 回归）', async () => {
    const wrapper = mountPage()
    await flushAll()
    clickByText(wrapper.findAll('tbody tr')[0].element, '编辑')
    await flushAll()
    const dlg = dialogIn(wrapper.element)
    const phoneInput = formItem(dlg, '手机号').querySelector('input') as HTMLInputElement
    // 缺陷原貌：这里预填的是列表的 '138****2201'，运营清掉它保存即把真号写成 NULL
    expect(phoneInput.value).toBe('')
    expect(phoneInput.placeholder).toBe('已绑定手机号，留空即不修改')
    typeInto(dlg, '姓名', '张建国')
    clickByText(dlg, '保存修改')
    await flushToast(400) // 写请求 150ms + 列表刷新 150ms 之后才弹 toast
    expect(toastText()).not.toContain('手机号需为 11 位号码')
    expect(toastText()).toContain('修改成功')
    const row = mockMedicalAccounts().find((r) => r.doctorId === 'DOC-001')
    expect(row?.phoneMasked).toBe('138****2201')
    expect(row?.phoneState).toBe('masked')
    wrapper.unmount()
  })

  it('编辑：填 11 位新号才换号（列表回显新脱敏串、状态转 masked）', async () => {
    const wrapper = mountPage()
    await flushAll()
    clickByText(wrapper.findAll('tbody tr')[0].element, '编辑')
    await flushAll()
    const dlg = dialogIn(wrapper.element)
    typeInto(dlg, '手机号', '13900002222')
    clickByText(dlg, '保存修改')
    await flushToast(400)
    expect(toastText()).toContain('修改成功')
    const row = mockMedicalAccounts().find((r) => r.doctorId === 'DOC-001')
    expect(row?.phoneMasked).toBe('139****2222')
    expect(row?.phoneState).toBe('masked')
    wrapper.unmount()
  })

  it('编辑：填了非 11 位号码停在提示上，不发写请求（号码不变）', async () => {
    const wrapper = mountPage()
    await flushAll()
    clickByText(wrapper.findAll('tbody tr')[0].element, '编辑')
    await flushAll()
    const dlg = dialogIn(wrapper.element)
    typeInto(dlg, '手机号', '138****2201') // 有人仍会把星号串敲进去
    clickByText(dlg, '保存修改')
    await flushToast(400)
    expect(toastText()).toContain('手机号需为 11 位号码，或留空')
    const row = mockMedicalAccounts().find((r) => r.doctorId === 'DOC-001')
    expect(row?.phoneMasked).toBe('138****2201')
    wrapper.unmount()
  })

  it('编辑：读不出的行（unreadable）提示「号码读取失败」，保存既不回填 *** 也不洗掉密文', async () => {
    const wrapper = mountPage()
    await flushAll()
    const row = wrapper.findAll('tbody tr').find((r) => r.text().includes('王护士'))
    expect(row).toBeTruthy()
    expect(row!.text()).toContain('***') // 列表按占位符展示，页面上不假装是号码
    clickByText(row!.element, '编辑')
    await flushAll()
    const dlg = dialogIn(wrapper.element)
    const phoneInput = formItem(dlg, '手机号').querySelector('input') as HTMLInputElement
    expect(phoneInput.value).toBe('')
    expect(phoneInput.placeholder).toBe('号码读取失败（***），留空即不修改；填 11 位新号可覆盖')
    clickByText(dlg, '保存修改')
    await flushToast(400)
    expect(toastText()).toContain('修改成功')
    const after = mockMedicalAccounts().find((r) => r.doctorId === 'DOC-102')
    expect(after?.phoneMasked).toBe('***')
    expect(after?.phoneState).toBe('unreadable') // 不是 absent：解不开 ≠ 没有
    wrapper.unmount()
  })

  it('编辑：改「初始状态」为禁用并保存 ⇒ 列表该行转为禁用（设计稿 :378 编辑态可改状态）', async () => {
    const wrapper = mountPage()
    await flushAll()
    clickByText(wrapper.findAll('tbody tr')[0].element, '编辑')
    await flushAll()
    const dlg = dialogIn(wrapper.element)
    const select = formItem(dlg, '初始状态').querySelector<HTMLElement>('.el-select__wrapper')
    expect(select).toBeTruthy()
    select!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushAll()
    const opt = [...document.querySelectorAll<HTMLElement>('.el-select-dropdown__item')]
      .find((li) => (li.textContent ?? '').includes('禁用'))
    if (!opt) throw new Error('找不到「禁用（暂不开放登录）」选项')
    opt.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushAll()
    clickByText(dlg, '保存修改')
    await flushToast(400)
    expect(toastText()).toContain('修改成功')
    expect(mockMedicalAccounts().find((r) => r.doctorId === 'DOC-001')?.status).toBe('disabled')
    wrapper.unmount()
  })

  it('必填校验卡姓名/科室/团队/职称，手机号不在其列', async () => {
    const wrapper = mountPage()
    await flushAll()
    clickByText(wrapper.element, '新建医护账号')
    await flushAll()
    clickByText(dialogIn(wrapper.element), '确认创建')
    await flushToast()
    expect(toastText()).toContain('请填写姓名')
    expect(wrapper.findAll('tbody tr').length).toBe(9) // 校验未过不发写请求
    wrapper.unmount()
  })

  it('创建：团队/职称未选齐时停在提示上，不落列表', async () => {
    const wrapper = mountPage()
    await flushAll()
    clickByText(wrapper.element, '新建医护账号')
    await flushAll()
    const dlg = dialogIn(wrapper.element)
    typeInto(dlg, '姓名', '测试医护')
    typeInto(dlg, '科室', '骨科')
    await flushAll()
    clickByText(dlg, '确认创建')
    await flushToast()
    expect(toastText()).toContain('请选择所属团队')
    expect(wrapper.text()).not.toContain('测试医护')
    wrapper.unmount()
  })

  it('一次性凭据：重置密码成功后展示账号与初始密码，确认按钮「我已转交本人」', async () => {
    const wrapper = mountPage()
    await flushAll()
    const row = wrapper.findAll('tbody tr').find((r) => r.text().includes('张建国'))!
    clickByText(row.element, '重置密码')
    await flushAll()
    clickByText(lastMessageBox(), '确认')
    await flushAll()
    const cred = lastMessageBox()
    expect(cred.textContent).toContain('重置成功')
    expect(cred.textContent).toContain('登录账号：doc00001')
    expect(cred.textContent).toMatch(/初始密码：Br[a-z0-9]{8}#7/)
    expect(cred.textContent).toContain('仅此一次展示，关闭后不可再看')
    expect(buttonsIn(cred).some((b) => b.textContent?.includes('我已转交本人'))).toBe(true)
    wrapper.unmount()
  })

  it('禁用确认文案含「无法登录运营后台」与历史记录的承诺', async () => {
    const wrapper = mountPage()
    await flushAll()
    const row = wrapper.findAll('tbody tr').find((r) => r.text().includes('张建国'))!
    clickByText(row.element, '禁用')
    await flushAll()
    const box = lastMessageBox()
    expect(box.textContent).toContain('确认禁用')
    expect(box.textContent).toContain('将无法登录运营后台，其历史操作记录保留不变')
    expect(box.textContent).toContain('张建国（doc00001）')
    clickByText(box, '取消')
    await flushAll()
    // 取消后不改状态：该行仍是「启用」态（操作按钮显示「禁用」）
    const after = wrapper.findAll('tbody tr').find((r) => r.text().includes('张建国'))!
    expect(after.text()).toContain('禁用')
    wrapper.unmount()
  })

  it('职称下拉 = 设计稿 :282 四项预置词表', () => {
    expect(MEDICAL_TITLES).toEqual(['主任医师', '主治医师', '康复师', '护士'])
  })
})

describe('医护账号 mock 写通道（设计稿 :294-297 系统生成口径）', () => {
  beforeEach(() => {
    __resetMedicalAccountsForTest()
  })

  it('登录账号 = doc + 5 位序号，取现有序号最大值 + 1', () => {
    const { account } = mockCreateMedicalAccount({
      name: '新医护', phone: '13800001234', department: '骨科', teamId: 'TEAM-005', title: '护士', status: 'enabled',
    })
    expect(account.username).toBe('doc00010')
    expect(account.patientCount).toBe(0)
    // 入库前即脱敏，列表不再二次掩码（否则会把 * 当数字截）
    expect(account.phoneMasked).toBe('138****1234')
  })

  it('手机号选填：留空即空串，列表按 §9.2 显示横杠', () => {
    const { account, initialPassword } = mockCreateMedicalAccount({
      name: '无手机号', phone: '', department: '骨科', teamId: 'TEAM-005', title: '护士', status: 'disabled',
    })
    expect(account.phoneMasked).toBe('')
    expect(initialPassword).toMatch(/^Br[a-z0-9]{8}#7$/)
  })

  it('禁用只改状态，账号仍在列表里（本卡无删除入口）', () => {
    mockSetMedicalAccountStatus('DOC-001', 'disabled')
    const rows = mockMedicalAccounts()
    expect(rows).toHaveLength(9)
    expect(rows.find((r) => r.doctorId === 'DOC-001')?.status).toBe('disabled')
  })
})
