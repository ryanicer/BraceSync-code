import { userErrorCode, userErrorData } from '@bracesync/shared-utils'

/**
 * T727：患者手机号撞号的用户面点名。
 *
 * 后端 10606 的码表句是「该手机号已绑定其他患者档案，请先找到占用该号码的患者…」——
 * 方向对但没说「哪一名」。撞号时信封 data 带占用方 {reason, patientId, name}，
 * 这里把它拼成一句能直接照着办事的话（卡面硬要求 4：不许吞成「保存失败」）。
 *
 * 🔴 只在「码=10606 且 reason 相配且两个身份字段都是非空串」时出话，其余一律 null，
 * 由调用方回落 userErrorCopy。宁可少点名，不能把别的 409 报成撞号。
 */
export const PATIENT_PHONE_TAKEN_CODE = 10606
export const PATIENT_PHONE_TAKEN_REASON = 'patient_phone_taken'

export function patientPhoneConflictCopy(err: unknown): string | null {
  if (userErrorCode(err) !== PATIENT_PHONE_TAKEN_CODE) return null
  const data = userErrorData(err)
  if (!data || data.reason !== PATIENT_PHONE_TAKEN_REASON) return null
  const patientId = typeof data.patientId === 'string' ? data.patientId.trim() : ''
  const name = typeof data.name === 'string' ? data.name.trim() : ''
  if (!patientId || !name) return null
  return `该手机号已绑定患者：${name}（ID ${patientId}），请到患者列表找到该档案，改用其档案或为其更换号码后再保存`
}
