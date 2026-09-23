// T315 医护账号管理 API 层（PRD §7D.10 / 设计稿 医护账号.html）。
//
// T338 起接后端 T314 真实端点（读 + 写四条全部走真接口，mock 分支保留给本地开发）：
//   读 —— GET /api/v1/doctors。T314 已把 admins 侧三列并入同一行（username / accountStatus /
//         createdAt），未绑登录档案的行这三列为 null ⇒ 前端渲染横杠。
//   写 —— POST /api/v1/admin/doctors                       创建（服务端发号 + 随机初始密码）
//         PUT  /api/v1/admin/doctors/:doctorId             编辑档案（服务端不收 status）
//         POST /api/v1/admin/doctors/:doctorId/status      禁用·启用（body {action}）
//         POST /api/v1/admin/doctors/:doctorId/reset-password  重置密码（一次性返回新密码）
// 🔴 后端 PUT 不收 status（services/user-service/.../doctor_accounts_t314.go 的
// doctorAccountUpdateRequest 只有姓名/科室/团队/职称/手机号），而设计稿 :378 的编辑态允许改状态
// ⇒ 编辑时状态另发 /status，一次点「保存修改」在最多两个请求内落定。
import type { Doctor } from '@bracesync/shared-types'
import { USE_MOCK, request } from '../utils/request'
import * as medicalMock from '../mock/medicalAccounts'
import type { CreateMedicalAccountInput, MedicalAccount, UpdateMedicalAccountInput } from '../mock/medicalAccounts'

export type { CreateMedicalAccountInput, MedicalAccount, UpdateMedicalAccountInput }
export { MEDICAL_TITLES } from '../mock/medicalAccounts'

function delay(ms = 150): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/**
 * GET /api/v1/doctors 的 T314 扩展列。
 * 未绑登录账号（admins 行缺失）时三列同为 null —— staging 实况：D0001 有账号，D0002/D0003 为 null。
 * shared-types 的 Doctor 是跨页共用契约（团队管理等页也吃），本页的三列扩展只在 admin-web 侧声明，
 * 不去动 packages/shared-types。
 */
export interface DoctorWithAccount extends Doctor {
  username?: string | null
  accountStatus?: string | null
  createdAt?: string | null
}

/**
 * 一行 = doctors 六列 + admins 侧三列。
 * 状态列取 doctors.status（档案层）：服务端 SetDoctorAccountStatus 两层同写，
 * 有账号的行两列必然一致；「禁用到底禁哪一层」的设计稿 :194 待裁 ⇒ 本页不加第二个开关。
 */
function fromDoctorRow(d: DoctorWithAccount): MedicalAccount {
  return {
    doctorId: d.doctorId,
    username: d.username ?? '',
    name: d.name,
    phoneMasked: d.phoneMasked,
    department: d.department,
    teamId: d.teamId,
    title: d.title,
    patientCount: d.patientCount,
    status: d.status,
    createdAt: d.createdAt ?? '',
  }
}

export async function fetchMedicalAccounts(): Promise<MedicalAccount[]> {
  if (USE_MOCK) {
    await delay()
    return medicalMock.mockMedicalAccounts()
  }
  const doctors = await request<DoctorWithAccount[]>({ url: '/api/v1/doctors' })
  return doctors.map(fromDoctorRow)
}

export interface CreateMedicalAccountResult {
  account: MedicalAccount
  /** 一次性凭据：只在这一次返回里出现，页面关闭弹窗后不再有入口（设计稿 :232/:388） */
  initialPassword: string
}

/** 创建响应 = 整行档案 + initialPassword（后端 DoctorAccountCreateDTO 是 DoctorDTO 的展开） */
type DoctorAccountCreateResponse = DoctorWithAccount & { initialPassword?: string }

export async function createMedicalAccountApi(input: CreateMedicalAccountInput): Promise<CreateMedicalAccountResult> {
  if (USE_MOCK) {
    await delay()
    return medicalMock.mockCreateMedicalAccount(input)
  }
  const res = await request<DoctorAccountCreateResponse>({
    url: '/api/v1/admin/doctors',
    method: 'POST',
    data: { ...input } as unknown as Record<string, unknown>,
  })
  return {
    account: fromDoctorRow(res),
    // 服务端 bcrypt 后不留明文，只在本响应出现一次；取不到即当缺失，不拿空串冒充「密码为空」
    initialPassword: res.initialPassword ?? '',
  }
}

export async function updateMedicalAccountApi(
  doctorId: string,
  input: UpdateMedicalAccountInput,
): Promise<MedicalAccount> {
  if (USE_MOCK) {
    await delay()
    return medicalMock.mockUpdateMedicalAccount(doctorId, input)
  }
  // 逐字段挑着发：后端 PUT 是「给了才改」的指针语义，整包发会把未碰的字段当成清空指令
  const profile: Record<string, unknown> = {}
  if (input.name !== undefined) profile.name = input.name
  if (input.title !== undefined) profile.title = input.title
  if (input.department !== undefined) profile.department = input.department
  if (input.teamId !== undefined) profile.teamId = input.teamId
  // 手机号三态：key 缺席 = 不改；'' = 清空；其余 = 换成新号（脱敏串绝不能回传）
  if (input.phone !== undefined) profile.phone = input.phone

  let row = await request<DoctorWithAccount>({
    url: `/api/v1/admin/doctors/${doctorId}`,
    method: 'PUT',
    data: profile,
  })
  if (input.status) {
    row = await request<DoctorWithAccount>({
      url: `/api/v1/admin/doctors/${doctorId}/status`,
      method: 'POST',
      data: { action: input.status === 'enabled' ? 'enable' : 'disable' },
    })
  }
  return fromDoctorRow(row)
}

export async function setMedicalAccountStatusApi(
  doctorId: string,
  status: 'enabled' | 'disabled',
): Promise<MedicalAccount> {
  if (USE_MOCK) {
    await delay()
    return medicalMock.mockSetMedicalAccountStatus(doctorId, status)
  }
  const row = await request<DoctorWithAccount>({
    url: `/api/v1/admin/doctors/${doctorId}/status`,
    method: 'POST',
    data: { action: status === 'enabled' ? 'enable' : 'disable' },
  })
  return fromDoctorRow(row)
}

/**
 * 重置密码。设计稿 :181：结果反馈必须走本写接口自己的返回码，
 * 不能复用登录侧 10401（凭据错误与账号已禁用同码，登录通道不携带「被禁用」信息）。
 */
export async function resetMedicalPasswordApi(doctorId: string): Promise<string> {
  if (USE_MOCK) {
    await delay()
    return medicalMock.mockResetMedicalPassword(doctorId)
  }
  const res = await request<{ doctorId: string; username: string; password: string }>({
    url: `/api/v1/admin/doctors/${doctorId}/reset-password`,
    method: 'POST',
    data: {},
  })
  return res.password
}
