import { request, USE_MOCK } from '../utils/request'
import type { PatientProfile } from '../types/app-extends'

/**
 * 获取患者档案（真实：GET /api/v1/admin/patients/:patientId）
 * V2.1 §4.1 数据来源：云端患者档案（绑定后拉取）
 */
export async function getPatient(patientId: string): Promise<PatientProfile> {
  if (USE_MOCK) {
    // T089-MOCK: 真实端点已存在，mock 模式兜底
    await new Promise((r) => setTimeout(r, 200))
    return {
      patientId,
      name: '张明远',
      age: 14,
      diagnosis: '特发性脊柱侧弯',
    }
  }
  // T089-TODO: 真实响应字段以实际 DTO 为准（patient → PatientProfile 映射）
  const raw = await request<{
    patientId: string
    name: string
    age: number | null
    diagnosis: string | null
  }>({
    url: `/api/v1/admin/patients/${patientId}`,
    method: 'GET',
  })
  return {
    patientId: raw.patientId,
    name: raw.name,
    age: raw.age,
    diagnosis: raw.diagnosis,
  }
}
