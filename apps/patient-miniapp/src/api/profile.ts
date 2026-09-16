/**
 * 患者本人只读档案（T187 ← T186 契约）
 *
 * GET /api/v1/patient/profile
 * - self-scope：查询对象由网关从 JWT `sub` 注入 `X-User-Id`，路径/参数均不携带患者 ID，
 *   前端无需传 patientId（传了也不生效）。
 * - bind 态 token（`sub` 以 `openid_` 开头）会被网关 403/40301 拒绝，须用完整登录态。
 */

import { request } from '../utils/request'

/**
 * T186 响应体，逐字对齐 user-service `model.AdminPatientDTO`
 * （services/user-service/internal/model/model.go:139-155，camelCase，可空字段为 null）。
 */
export interface PatientProfile {
  patientId: string
  name: string
  /** 'male' | 'female' | null —— 字符串枚举，不是数字 */
  gender: 'male' | 'female' | null
  age: number | null
  diagnosis: string | null
  cobbAngle: number | null
  /** 来源 devices.patient_id 只读关联，非 patients.device_id（T151 方案 1） */
  deviceId: string | null
  teamId: string | null
  doctorId: string | null
  /** T186 的 toPatientDTO 未映射此字段，wire 上恒为 ""（且 repo 不查 phone_enc）；本页不展示 */
  phone: string
  status: 'active' | 'pending'
  /** RFC3339 UTC */
  createdAt: string
  /** RFC3339 UTC */
  updatedAt: string
  teamName: string | null
  doctorName: string | null
}

export async function getPatientProfile(): Promise<PatientProfile> {
  return request<PatientProfile>({
    url: '/api/v1/patient/profile',
    method: 'GET',
  })
}
