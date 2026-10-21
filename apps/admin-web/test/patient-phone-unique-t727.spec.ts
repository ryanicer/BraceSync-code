// T727 患者手机号唯一性：撞号错误点名到「占用该号码的患者是谁」
//
// 卡面硬要求 4「admin-web 患者管理页收到该错误时，提示『该手机号已绑定患者：<姓名>』，
// 不要吞成"保存失败"」。后端 10606 的信封 message 是英文技术原文（T464 双通道，只进日志），
// 码表句又不点名 ⇒ 页面必须读 data.patientId/name 自己拼话。
//
// 三层各一枪（卡面自验第五条）：
//  ① 纯谓词：只有「码 10606 + reason 相配 + 两个身份字段齐全」才点名，其余一律 null；
//  ② API/mock 层：mock 与真实传输层同形（attachErrorMeta 带 code/httpStatus/data），
//     建档与改号都撞、改号排除自身；
//  ③ UI 层：页面 catch 真出点名句，且不透后端英文原文。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createPinia } from 'pinia'
import { attachErrorMeta, userErrorCopy } from '@bracesync/shared-utils'
import type { Patient } from '@bracesync/shared-types'
import PatientsPage from '../src/pages/patients/index.vue'
import { patientPhoneConflictCopy } from '../src/utils/patientPhoneConflict'
import { mockCreatePatient, mockUpdatePatientPhone } from '../src/mock/patients'

const api = vi.hoisted(() => {
  const r1: Patient = {
    patientId: 'PT-001', name: '林小雨', gender: 'female', age: 13,
    diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 28,
    deviceId: null, teamId: 'TEAM-001', doctorId: null,
    status: 'active', createdAt: '2026-03-12T09:00:00+08:00', updatedAt: '2026-08-10T18:00:00+08:00',
  }
  const m = {
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
  return { m, rows: [r1] }
})

vi.mock('../src/api', () => api.m)

/** 后端撞号响应经 request.ts 解出来的错误形状（message 是英文技术原文，身份在 data） */
const taken = (patientId: string, name: string) => attachErrorMeta(
  new Error('phone already exists'),
  {
    code: 10606,
    httpStatus: 409,
    data: { reason: 'patient_phone_taken', patientId, name },
  },
)
/** 并发兜底路径：唯一索引 23505 命中但取不到占用方 ⇒ data 为字面 null */
const takenWithoutPayload = () => attachErrorMeta(
  new Error('duplicate key value violates unique constraint "idx_patients_phone_hash"'),
  { code: 10606, httpStatus: 409, data: null },
)

describe('T727 ① 点名谓词：宁可少点名，不能点错名', () => {
  it('码 10606 + 完整负载 ⇒ 出「该手机号已绑定患者：<姓名>（ID <id>）」', () => {
    expect(patientPhoneConflictCopy(taken('P20260009', '王小宇'))).toBe(
      '该手机号已绑定患者：王小宇（ID P20260009），请到患者列表找到该档案，改用其档案或为其更换号码后再保存',
    )
  })

  it('只有码没有负载（23505 兜底）⇒ null，由调用方回落码表通用句', () => {
    expect(patientPhoneConflictCopy(takenWithoutPayload())).toBeNull()
    expect(userErrorCopy(takenWithoutPayload(), { scope: 'admin', fallback: '创建失败' })).toBe(
      '该手机号已绑定其他患者档案，请先找到占用该号码的患者，改用其档案或更换号码',
    )
  })

  it('反证：别的 409（10409）即便带同形 data 也不点名，防把状态冲突报成撞号', () => {
    const wrongCode = attachErrorMeta(new Error('conflict'), {
      code: 10409,
      httpStatus: 409,
      data: { reason: 'patient_phone_taken', patientId: 'P20260009', name: '王小宇' },
    })
    expect(patientPhoneConflictCopy(wrongCode)).toBeNull()
  })

  it('反证：reason 不相配／身份缺一律 null（后端字段口径变了要显式改，不许猜）', () => {
    const cases: unknown[] = [
      attachErrorMeta(new Error('x'), {
        code: 10606,
        httpStatus: 409,
        data: { reason: 'other', patientId: 'P1', name: '甲' },
      }),
      attachErrorMeta(new Error('x'), { code: 10606, httpStatus: 409, data: { reason: 'patient_phone_taken', name: '甲' } }),
      attachErrorMeta(new Error('x'), { code: 10606, httpStatus: 409, data: { reason: 'patient_phone_taken', patientId: 'P1' } }),
      attachErrorMeta(new Error('x'), {
        code: 10606,
        httpStatus: 409,
        data: { reason: 'patient_phone_taken', patientId: 'P1', name: '   ' },
      }),
      // 后端回体是 data:null，传输层原样带上 ⇒ 不能当对象读
      attachErrorMeta(new Error('x'), { code: 10606, httpStatus: 409, data: null }),
    ]
    for (const c of cases) expect(patientPhoneConflictCopy(c)).toBeNull()
  })
})

describe('T727 ② mock 与真实同形：建档与改号都撞，改号排除自身', () => {
  it('建档撞号 ⇒ 抛 10606 且 data 指认占用方', () => {
    mockCreatePatient({ name: '甲', phone: '13800138000' })
    let caught: unknown = null
    try {
      mockCreatePatient({ name: '乙', phone: '13800138000' })
    } catch (e) {
      caught = e
    }
    expect(caught).not.toBeNull()
    expect(patientPhoneConflictCopy(caught)).toContain('该手机号已绑定患者：甲')
  })

  it('改号抢别人的号 ⇒ 10606 点名占用方；把原号重写回自己 ⇒ 放行（后端排除自身）', () => {
    mockCreatePatient({ name: '甲', phone: '13700002222' })
    expect(() => mockUpdatePatientPhone('PT-001', '13700002222', '抢号')).toThrow(/已被其他患者/)
    expect(() => mockUpdatePatientPhone('PT-001', '13900001111', '本人换号')).not.toThrow()
    expect(() => mockUpdatePatientPhone('PT-001', '13900001111', '同号重写自己')).not.toThrow()
  })
})

// ===== ③ UI 层 =====

let wrapper: VueWrapper | null = null

function dialogOf(title: string): HTMLElement {
  const all = [...document.querySelectorAll<HTMLElement>('.el-dialog')]
  const el = all.find((d) => (d.querySelector('.el-dialog__title')?.textContent ?? '').trim() === title)
  if (!el) throw new Error(`未找到弹窗「${title}」`)
  return el
}

function byPlaceholder(scope: HTMLElement, ph: string): HTMLInputElement | HTMLTextAreaElement {
  const field = scope.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[placeholder="${ph}"]`)
  if (!field) throw new Error(`找不到 placeholder 为「${ph}」的输入框`)
  return field
}

async function settle() {
  for (let i = 0; i < 6; i++) {
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

/** 抽屉打开后「改手机号」入口 */
async function openPhoneDialog() {
  const rows = wrapper!.findAll('.el-table__body tr')
  await rows[0].trigger('click')
  await settle()
  await clickButton(document.querySelector('.drawer-actions') as HTMLElement, '改手机号')
  return dialogOf('修改手机号')
}

/** 「添加患者」在页面工具条上（不在 teleport 出去的弹窗里），故只在组件容器内找 */
async function openCreateDialog() {
  await clickButton(wrapper!.element as HTMLElement, '添加患者')
}

function toastText(): string {
  return [...document.querySelectorAll<HTMLElement>('.el-message')]
    .map((m) => (m.textContent ?? '').trim())
    .join(' / ')
}

describe('T727 ③ 患者管理页：撞号提示点名，不吞成保存失败', () => {
  beforeEach(async () => {
    api.m.fetchPatients.mockResolvedValue({ list: api.rows, total: api.rows.length, page: 1, pageSize: 10 })
    api.m.fetchTeams.mockResolvedValue([])
    api.m.fetchDoctors.mockResolvedValue([])
    api.m.createPatientApi.mockRejectedValue(taken('P20260009', '王小宇'))
    api.m.updatePatientPhoneApi.mockRejectedValue(taken('P20260009', '王小宇'))
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { render: () => null } },
        { path: '/patients', component: { render: () => null } },
        { path: '/abnormal-report', component: { render: () => null } },
      ],
    })
    wrapper = mount(PatientsPage, { attachTo: document.body, global: { plugins: [router, createPinia(), ElementPlus] } })
    await settle()
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('新建患者撞号 ⇒ toast 点名「王小宇 / P20260009」，不透英文原文、不出「创建失败」', async () => {
    await openCreateDialog()
    const dlg = dialogOf('新建患者')
    await setInput(dlg, '请输入姓名', '张小明')
    await setInput(dlg, '请输入手机号', '13800138000')
    await clickButton(dlg, '确定')
    expect(api.m.createPatientApi).toHaveBeenCalledTimes(1)
    const toast = toastText()
    expect(toast).toContain('该手机号已绑定患者：王小宇（ID P20260009）')
    expect(toast).not.toContain('phone already exists')
    expect(toast).not.toContain('创建失败')
  })

  it('改手机号撞号 ⇒ 同一句点名，不出「修改失败」', async () => {
    const dlg = await openPhoneDialog()
    await setInput(dlg, '请输入11位新手机号', '13800138000')
    await setInput(dlg, '例如：患者换号，本人来电申请', '患者来电换号')
    await clickButton(dlg, '确定')
    expect(api.m.updatePatientPhoneApi).toHaveBeenCalledTimes(1)
    const toast = toastText()
    expect(toast).toContain('该手机号已绑定患者：王小宇（ID P20260009）')
    expect(toast).not.toContain('修改失败')
  })

  it('反证：10606 无负载时回落码表通用句，仍不吞成「创建失败」', async () => {
    api.m.createPatientApi.mockRejectedValue(takenWithoutPayload())
    await openCreateDialog()
    const dlg = dialogOf('新建患者')
    await setInput(dlg, '请输入姓名', '张小红')
    await setInput(dlg, '请输入手机号', '13800138001')
    await clickButton(dlg, '确定')
    const toast = toastText()
    expect(toast).toContain('该手机号已绑定其他患者档案')
    expect(toast).not.toContain('该手机号已绑定患者：')
    expect(toast).not.toContain('创建失败')
  })
})
