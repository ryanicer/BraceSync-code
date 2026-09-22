// 医护账号（T315 / PRD §7D.10）mock 数据。
//
// 医生档案侧字段（姓名 / 职称 / 科室 / 团队 / 手机号 / 管理患者数 / 状态）取自 mock/org.ts 的 DOCTORS，
// 与「团队管理」等页共用同一批档案，避免同一医生在两页显示不同职称。
// 账号侧字段（登录账号 / 创建时间）落在 admins 表，契约无列表端点（设计稿 医护账号.html:178），
// 故按设计稿 :284-290 的样例在本地补齐，T314 的 GET /api/v1/admin/accounts 落地后整体替换。
import type { Doctor } from '@bracesync/shared-types'
import { mockDoctors } from './org'

/** 设计稿 :131/:282 职称词表（doctors.title，预置 4 项、可扩展；≠ 登录角色） */
export const MEDICAL_TITLES = ['主任医师', '主治医师', '康复师', '护士']

/**
 * 设计稿 :289 赵敏 phone:'' 用于演示 §9.2 空值横杠；mock 档案里唯一没有其它页面消费的医生是
 * DOC-104（刘医生，未分配团队、0 患者，不出现在团队管理成员列表），故用他承载这一格。
 */
const EMPTY_PHONE_DOCTORS = new Set(['DOC-104'])

export interface MedicalAccount {
  doctorId: string
  /** 登录账号：系统按 doc + 5 位序号生成，只读不可改（设计稿 :226） */
  username: string
  name: string
  /** 服务端即已脱敏；空串 ⇒ 列表显示「—」（设计稿 :299 maskPhone） */
  phoneMasked: string
  department: string
  teamId: string | null
  title: string
  /** 主诊患者数，不是所属团队患者总数（设计稿 :146） */
  patientCount: number
  status: 'enabled' | 'disabled'
  /** 'YYYY-MM-DD'；admins 侧字段，真实端点缺失时为 '' ⇒ 显示「—」 */
  createdAt: string
}

export interface CreateMedicalAccountInput {
  name: string
  phone: string
  department: string
  teamId: string
  title: string
  status: 'enabled' | 'disabled'
}

export type UpdateMedicalAccountInput = Partial<CreateMedicalAccountInput>

const CREATED_AT: Record<string, string> = {
  'DOC-001': '2026-01-15',
  'DOC-002': '2026-01-15',
  'DOC-003': '2026-02-03',
  'DOC-004': '2026-03-11',
  'DOC-005': '2026-04-02',
  'DOC-101': '2026-01-15',
  'DOC-102': '2026-01-20',
  'DOC-103': '2026-02-20',
  'DOC-104': '2026-03-05',
}

function fromDoctor(doctor: Doctor, index: number): MedicalAccount {
  return {
    doctorId: doctor.doctorId,
    username: `doc${String(index + 1).padStart(5, '0')}`,
    name: doctor.name,
    phoneMasked: EMPTY_PHONE_DOCTORS.has(doctor.doctorId) ? '' : doctor.phoneMasked,
    department: doctor.department,
    teamId: doctor.teamId,
    title: doctor.title,
    patientCount: doctor.patientCount,
    status: doctor.status,
    createdAt: CREATED_AT[doctor.doctorId] ?? '',
  }
}

let accounts: MedicalAccount[] = mockDoctors().map(fromDoctor)

/** 设计稿 :295 nextAcct()：取现有序号最大值 + 1，doc + 5 位补零 */
function nextUsername(): string {
  const max = accounts.reduce((acc, a) => Math.max(acc, Number(a.username.replace(/^doc/, '')) || 0), 0)
  return `doc${String(max + 1).padStart(5, '0')}`
}

/** 设计稿 :297 genPwd()：初始密码系统随机生成，仅在创建/重置成功弹窗一次性展示 */
function genPassword(): string {
  return `Br${Math.random().toString(36).slice(2, 10)}#7`
}

function today(): string {
  return new Date().toISOString().slice(0, 10)
}

export function mockMedicalAccounts(): MedicalAccount[] {
  return accounts.map((a) => ({ ...a }))
}

export function mockCreateMedicalAccount(input: CreateMedicalAccountInput): { account: MedicalAccount; initialPassword: string } {
  const account: MedicalAccount = {
    doctorId: `DOC-${String(accounts.length + 1).padStart(3, '0')}`,
    username: nextUsername(),
    name: input.name,
    phoneMasked: input.phone ? `${input.phone.slice(0, 3)}****${input.phone.slice(7)}` : '',
    department: input.department,
    teamId: input.teamId,
    title: input.title,
    patientCount: 0,
    status: input.status,
    createdAt: today(),
  }
  accounts = [...accounts, account]
  return { account: { ...account }, initialPassword: genPassword() }
}

export function mockUpdateMedicalAccount(doctorId: string, input: UpdateMedicalAccountInput): MedicalAccount {
  const row = accounts.find((a) => a.doctorId === doctorId)
  if (!row) throw new Error(`医护账号不存在：${doctorId}`)
  if (input.name) row.name = input.name
  if (input.department) row.department = input.department
  if (input.teamId) row.teamId = input.teamId
  if (input.title) row.title = input.title
  if (input.status) row.status = input.status
  // 页面只在用户真改了手机号时传新号；列表要的仍是脱敏值
  if (input.phone) row.phoneMasked = `${input.phone.slice(0, 3)}****${input.phone.slice(7)}`
  return { ...row }
}

export function mockSetMedicalAccountStatus(doctorId: string, status: 'enabled' | 'disabled'): MedicalAccount {
  const row = accounts.find((a) => a.doctorId === doctorId)
  if (!row) throw new Error(`医护账号不存在：${doctorId}`)
  row.status = status
  return { ...row }
}

export function mockResetMedicalPassword(doctorId: string): string {
  if (!accounts.some((a) => a.doctorId === doctorId)) throw new Error(`医护账号不存在：${doctorId}`)
  return genPassword()
}

/** 仅供单测复位模态框写操作的副作用 */
export function __resetMedicalAccountsForTest(): void {
  accounts = mockDoctors().map(fromDoctor)
}
