// T432 患者详情抽屉三入口（编辑档案 / 改手机号 / 解绑微信）页面级守卫。
//
// 为什么放在这一层：API 层的线形守卫在 contract-drift-gate.spec.ts 里已经钉过（体只带改动的键），
// 但「谁往体里塞了没改的键」和「谁把列表里的脱敏号原样预填进输入框」这两件事都发生在页面里 ——
// 后者正是 T361 在医护页修掉的那类缺陷（编辑框预填脱敏串 ⇒ 运营清空 ⇒ 库内真号被写没）。
// 所以这里把 api 模块整个换掉，并故意让列表行带一个 phone 脱敏串回来，看页面会不会去填它。
import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createPinia } from 'pinia'
import type { Patient } from '@bracesync/shared-types'
import PatientsPage from '../src/pages/patients/index.vue'

interface ApiMock {
  fetchPatients: ReturnType<typeof vi.fn>
  fetchTeams: ReturnType<typeof vi.fn>
  fetchDoctors: ReturnType<typeof vi.fn>
  teamNameOf: Mock<[id: string | null | undefined], string>
  doctorNameOf: Mock<[id: string | null | undefined], string>
  createPatientApi: ReturnType<typeof vi.fn>
  assignPatientTeamApi: ReturnType<typeof vi.fn>
  batchBindPatientsApi: ReturnType<typeof vi.fn>
  updatePatientPhoneApi: ReturnType<typeof vi.fn>
  updatePatientProfileApi: ReturnType<typeof vi.fn>
  unbindPatientWechatApi: ReturnType<typeof vi.fn>
}

// vi.mock 会被提到文件顶部，夹具只能在 hoisted 里造
const api = vi.hoisted(() => {
  // 行1 故意带脱敏手机号：真实读侧虽恒为空串（T361），但「不许预填」这条判据只有喂它非空值才有牙
  const r1: Patient & { teamName?: string; doctorName?: string } = {
    patientId: 'PT-001', name: '林小雨', gender: 'female', age: 13,
    diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 28,
    deviceId: 'DEV-A3F312', teamId: 'TEAM-001', doctorId: 'DOC-001',
    status: 'active', phone: '138****0001',
    createdAt: '2026-03-12T09:00:00+08:00', updatedAt: '2026-08-10T18:00:00+08:00',
    teamName: '脊柱侧弯一组', doctorName: '李医师',
  }
  const r2: Patient & { teamName?: string; doctorName?: string } = {
    patientId: 'PT-002', name: '陈子航', gender: 'male', age: 15,
    diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 35,
    deviceId: 'DEV-B7E456', teamId: 'TEAM-001', doctorId: 'DOC-001',
    status: 'active', createdAt: '2026-04-02T10:30:00+08:00', updatedAt: '2026-08-11T08:00:00+08:00',
    teamName: '脊柱侧弯一组', doctorName: '李医师',
  }
  const m: ApiMock = {
    fetchPatients: vi.fn(),
    fetchTeams: vi.fn(),
    fetchDoctors: vi.fn(),
    teamNameOf: vi.fn((id: string | null | undefined) => id ?? '-'),
    doctorNameOf: vi.fn((id: string | null | undefined) => id ?? '-'),
    createPatientApi: vi.fn(),
    assignPatientTeamApi: vi.fn(),
    batchBindPatientsApi: vi.fn(),
    updatePatientPhoneApi: vi.fn(),
    updatePatientProfileApi: vi.fn(),
    unbindPatientWechatApi: vi.fn(),
  }
  return { m, rows: [r1, r2] }
})

vi.mock('../src/api', () => api.m)

let wrapper: VueWrapper | null = null

const noop = { render: () => null }

/** 弹窗 teleport 到 body，按标题取；同名弹窗一个用例里只开一个 */
function dialogOf(title: string): HTMLElement {
  const all = [...document.querySelectorAll<HTMLElement>('.el-dialog')]
  const el = all.find((d) => (d.querySelector('.el-dialog__title')?.textContent ?? '').trim() === title)
  if (!el) throw new Error(`未找到弹窗「${title}」，body 内现有：${all.map((d) => d.querySelector('.el-dialog__title')?.textContent).join(',')}`)
  return el
}

/** 变更原因那格是 textarea，故 input 与 textarea 一起找 */
function byPlaceholder(scope: HTMLElement, ph: string): HTMLInputElement | HTMLTextAreaElement {
  const field = scope.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[placeholder="${ph}"]`)
  if (!field) throw new Error(`弹窗内找不到 placeholder 为「${ph}」的输入框`)
  return field
}

/** 表单校验是「点一次按钮串两个 await」，错误文案又要过一层 transition ⇒ 单刷一次 promise 数不到 */
async function settle() {
  for (let i = 0; i < 4; i++) {
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))
  }
}

async function setInput(scope: HTMLElement, ph: string, value: string) {
  const input = byPlaceholder(scope, ph)
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await settle()
}

function buttonOf(scope: HTMLElement, label: string): HTMLButtonElement {
  const btn = [...scope.querySelectorAll('button')].find((b) => (b.textContent ?? '').trim() === label)
  if (!btn) throw new Error(`找不到文案为「${label}」的按钮`)
  return btn
}

async function clickButton(scope: HTMLElement, label: string) {
  buttonOf(scope, label).click()
  await settle()
}

function errorTexts(scope: HTMLElement): string[] {
  return [...scope.querySelectorAll('.form-error, .el-form-item__error')].map((e) => (e.textContent ?? '').trim())
}

async function openDrawer(rowIndex: number) {
  const rows = wrapper!.findAll('.el-table__body tr')
  await rows[rowIndex].trigger('click')
  await flushPromises()
}

beforeEach(async () => {
  api.m.fetchPatients.mockResolvedValue({ list: api.rows, total: api.rows.length, page: 1, pageSize: 10 })
  api.m.fetchTeams.mockResolvedValue([])
  api.m.fetchDoctors.mockResolvedValue([])
  api.m.updatePatientProfileApi.mockResolvedValue(api.rows[0])
  api.m.updatePatientPhoneApi.mockResolvedValue({ patientId: 'PT-001' })
  api.m.unbindPatientWechatApi.mockResolvedValue({ patientId: 'PT-001' })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: noop },
      { path: '/patients', component: noop },
      { path: '/abnormal-report', component: noop },
    ],
  })
  // attachTo 必须传：不挂进 document 时，弹层里的节点用 document.querySelector 数不到（VTU 默认挂在游离容器上）
  // pinia 必须装：T500 起页面 setup 读 auth store 做「设登录口令」的角色闸门；这里不给登录态 ⇒ role=null ⇒ 该按钮不渲染，
  // 下面那条「抽屉五入口逐字对平」的判据才维持 T432 的口径
  wrapper = mount(PatientsPage, { attachTo: document.body, global: { plugins: [router, createPinia(), ElementPlus] } })
  await flushPromises()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  // el-dialog / el-message-box 走 teleport 挂到 body，不清会串到下一个用例
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

describe('T432 抽屉三入口', () => {
  it('抽屉里有「编辑档案 / 改手机号 / 解绑微信」，且解绑是危险色', async () => {
    await openDrawer(0)
    const drawer = document.querySelector('.el-drawer') as HTMLElement
    const labels = [...drawer.querySelectorAll('.drawer-actions button')].map((b) => (b.textContent ?? '').trim())
    expect(labels).toEqual(['分配团队', '异常报告', '编辑档案', '改手机号', '解绑微信'])
    expect(buttonOf(drawer.querySelector('.drawer-actions') as HTMLElement, '解绑微信').className)
      .toContain('el-button--danger')
  })
})

describe('T432 编辑档案弹窗', () => {
  it('打开时按当前档案预填五项', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '编辑档案')
    const dlg = dialogOf('编辑档案')
    expect(byPlaceholder(dlg, '请输入姓名').value).toBe('林小雨')
    expect(byPlaceholder(dlg, '请输入年龄').value).toBe('13')
    expect(byPlaceholder(dlg, '请输入诊断').value).toBe('青少年特发性脊柱侧弯')
    expect(byPlaceholder(dlg, '请输入Cobb角').value).toBe('28')
  })

  it('一个字都没改 ⇒ 保存置灰（空编辑后端判 400，不该发出去）', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '编辑档案')
    const dlg = dialogOf('编辑档案')
    expect(buttonOf(dlg, '保存').disabled, '未改动时保存必须不可点').toBe(true)
    expect(api.m.updatePatientProfileApi).not.toHaveBeenCalled()
  })

  it('只改诊断 ⇒ 请求体里只有 diagnosis，其余四项一个键都不下发', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '编辑档案')
    const dlg = dialogOf('编辑档案')
    await setInput(dlg, '请输入诊断', '先天性侧弯')
    expect(buttonOf(dlg, '保存').disabled).toBe(false)
    await clickButton(dlg, '保存')
    expect(api.m.updatePatientProfileApi).toHaveBeenCalledTimes(1)
    expect(api.m.updatePatientProfileApi.mock.calls[0]).toEqual(['PT-001', { diagnosis: '先天性侧弯' }])
  })

  // T450-②b 乙案：Alice 第 70 轮登记的缺陷是「基线 null，还原后空串」——指针表达不出「改回 NULL」，
  // 所以清空诊断必须走显式 clearFields，而不是把 '' 当一个值写进去。
  it('清空诊断 ⇒ 下发 clearFields:[diagnosis]，绝不带 diagnosis 空串键', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '编辑档案')
    const dlg = dialogOf('编辑档案')
    await setInput(dlg, '请输入诊断', '')
    expect(buttonOf(dlg, '保存').disabled).toBe(false)
    await clickButton(dlg, '保存')
    expect(api.m.updatePatientProfileApi).toHaveBeenCalledTimes(1)
    expect(api.m.updatePatientProfileApi.mock.calls[0]).toEqual(['PT-001', { clearFields: ['diagnosis'] }])
  })

  it('改诊断同时又清空 ⇒ 只有清空那条通道生效（同列既给值又列为清空，后端判 400）', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '编辑档案')
    const dlg = dialogOf('编辑档案')
    await setInput(dlg, '请输入诊断', '   ')
    await clickButton(dlg, '保存')
    const patch = api.m.updatePatientProfileApi.mock.calls[0]?.[1] as Record<string, unknown>
    expect(patch).toEqual({ clearFields: ['diagnosis'] })
    expect(patch.diagnosis).toBeUndefined()
  })

  it('清空姓名会被当场拦住：出说明文案且保存回到置灰', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '编辑档案')
    const dlg = dialogOf('编辑档案')
    await setInput(dlg, '请输入诊断', '先天性侧弯')
    await setInput(dlg, '请输入姓名', '')
    expect(errorTexts(dlg).join('；')).toContain('姓名不可清空')
    expect(buttonOf(dlg, '保存').disabled).toBe(true)
    expect(api.m.updatePatientProfileApi).not.toHaveBeenCalled()
  })

  it('Cobb 角越界（后端值域 0-180）在提交前就拦', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '编辑档案')
    const dlg = dialogOf('编辑档案')
    await setInput(dlg, '请输入Cobb角', '200')
    expect(errorTexts(dlg).join('；')).toContain('Cobb角需为 0-180 之间的数字')
    expect(buttonOf(dlg, '保存').disabled).toBe(true)
  })
})

describe('T432 改手机号弹窗', () => {
  it('列表行带着脱敏号也不预填（预填＝T361 那个把真号洗没的坑）', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '改手机号')
    const dlg = dialogOf('修改手机号')
    expect(byPlaceholder(dlg, '请输入11位新手机号').value).toBe('')
    expect(dlg.textContent).toContain('原因随请求写入服务端审计日志')
  })

  // 「格式错/缺原因 ⇒ 拦下不发」两格刻意不在这里钉：EP 2.14.4 的 FormItem.validate 在 happy-dom 下
  // 以 undefined 作 rejection 值（实测 item.validate('') → rejected:undefined，validateState 停在
  // is-validating），Form 侧收集到的 validationErrors 为空 ⇒ validateField 对非法值返回 true。
  // 同环境直接跑 async-validator 是正常的（reject {errors,fields}），坏的一段在 EP 内部，不是页面逻辑。
  // ⇒ 拦截与文案判据放到真实浏览器的 e2e/tests/admin-patient-writes.spec.ts。

  it('号码 + 原因齐 ⇒ PUT 收到三个入参', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '改手机号')
    const dlg = dialogOf('修改手机号')
    await setInput(dlg, '请输入11位新手机号', '13900001234')
    await setInput(dlg, '例如：患者换号，本人来电申请', '本人来电换号')
    await clickButton(dlg, '确定')
    expect(api.m.updatePatientPhoneApi).toHaveBeenCalledTimes(1)
    expect(api.m.updatePatientPhoneApi.mock.calls[0]).toEqual(['PT-001', '13900001234', '本人来电换号'])
  })
})

describe('T432 解绑微信二次确认', () => {
  it('确认框讲清后果（解绑后再登录走重新绑定手机号）', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '解绑微信')
    const box = document.querySelector('.el-message-box') as HTMLElement
    expect(box.textContent).toContain('PT-001')
    expect(box.textContent).toContain('重新绑定手机号')
    expect(box.textContent).toContain('审计日志')
  })

  it('点取消 ⇒ 一个请求都不发', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '解绑微信')
    await clickButton(document.querySelector('.el-message-box') as HTMLElement, '取消')
    expect(api.m.unbindPatientWechatApi).not.toHaveBeenCalled()
  })

  it('点确认解绑 ⇒ 按患者 ID 发 POST', async () => {
    await openDrawer(0)
    await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '解绑微信')
    await clickButton(document.querySelector('.el-message-box') as HTMLElement, '确认解绑')
    expect(api.m.unbindPatientWechatApi).toHaveBeenCalledTimes(1)
    expect(api.m.unbindPatientWechatApi.mock.calls[0]).toEqual(['PT-001'])
  })
})
