// T315 医护账号管理 API 层（PRD §7D.10 / 设计稿 医护账号.html）。
// 契约现状（设计稿 :177-181 已核对）：
//   读 —— 医生档案 6 列走已有 GET /api/v1/doctors；登录账号 / 创建时间在 admins 表，
//         契约无列表端点，等 T314 的 GET /api/v1/admin/accounts（建议形状），届时换成单端点。
//   写 —— 新建 / 编辑 / 重置密码 / 禁用启用四类端点契约全无（技师侧已有 POST/PUT /api/v1/admin/technicians
//         可照形状补：POST/PUT /api/v1/admin/doctors、/reset-password、/toggle）。
//         真实模式下写操作直接抛错，不猜端点、不发无契约的 POST。
import type { Doctor } from '@bracesync/shared-types'
import { USE_MOCK, request } from '../utils/request'
import * as medicalMock from '../mock/medicalAccounts'
import type { CreateMedicalAccountInput, MedicalAccount, UpdateMedicalAccountInput } from '../mock/medicalAccounts'

export type { CreateMedicalAccountInput, MedicalAccount, UpdateMedicalAccountInput }
export { MEDICAL_TITLES } from '../mock/medicalAccounts'

/** 写通道契约未就绪的统一文案（页面直接 ElMessage.error 透出，不伪装成网络错误） */
export const MEDICAL_WRITE_GAP = '医护账号写端点契约未就绪（等 T314），当前仅 mock 模式可提交'

function delay(ms = 150): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

export async function fetchMedicalAccounts(): Promise<MedicalAccount[]> {
  if (USE_MOCK) {
    await delay()
    return medicalMock.mockMedicalAccounts()
  }
  const doctors = await request<Doctor[]>({ url: '/api/v1/doctors' })
  return doctors.map((d) => ({
    doctorId: d.doctorId,
    username: '',
    name: d.name,
    phoneMasked: d.phoneMasked,
    department: d.department,
    teamId: d.teamId,
    title: d.title,
    patientCount: d.patientCount,
    status: d.status,
    createdAt: '',
  }))
}

export interface CreateMedicalAccountResult {
  account: MedicalAccount
  /** 一次性凭据：只在这一次返回里出现，页面关闭弹窗后不再有入口（设计稿 :232/:388） */
  initialPassword: string
}

export async function createMedicalAccountApi(input: CreateMedicalAccountInput): Promise<CreateMedicalAccountResult> {
  if (!USE_MOCK) throw new Error(MEDICAL_WRITE_GAP)
  await delay()
  return medicalMock.mockCreateMedicalAccount(input)
}

export async function updateMedicalAccountApi(
  doctorId: string,
  input: UpdateMedicalAccountInput,
): Promise<MedicalAccount> {
  if (!USE_MOCK) throw new Error(MEDICAL_WRITE_GAP)
  await delay()
  return medicalMock.mockUpdateMedicalAccount(doctorId, input)
}

export async function setMedicalAccountStatusApi(
  doctorId: string,
  status: 'enabled' | 'disabled',
): Promise<MedicalAccount> {
  if (!USE_MOCK) throw new Error(MEDICAL_WRITE_GAP)
  await delay()
  return medicalMock.mockSetMedicalAccountStatus(doctorId, status)
}

/**
 * 重置密码。设计稿 :181：结果反馈必须走本写接口自己的返回码，
 * 不能复用登录侧 10401（凭据错误与账号已禁用同码，登录通道不携带「被禁用」信息）。
 */
export async function resetMedicalPasswordApi(doctorId: string): Promise<string> {
  if (!USE_MOCK) throw new Error(MEDICAL_WRITE_GAP)
  await delay()
  return medicalMock.mockResetMedicalPassword(doctorId)
}
