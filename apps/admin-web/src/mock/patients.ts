// 患者域 mock 数据（对齐 api-contracts.ts getPatients/getPatientDetail/getPatientRealtime）
import type { Patient, PressureRecord, SensorPoint, Alert } from '@bracesync/shared-types'

function makePoints(maxValue: number): SensorPoint[] {
  const points: SensorPoint[] = []
  for (let i = 1; i <= 20; i++) {
    const pressureValue = Math.round((10 + ((i * 7) % 40) + maxValue / 10) * 10) / 10
    points.push({
      pointId: `P${String(i).padStart(2, '0')}`,
      row: Math.ceil(i / 5),
      col: ((i - 1) % 5) + 1,
      label: `R${Math.ceil(i / 5)}C${((i - 1) % 5) + 1}`,
      pressureValue,
      status: pressureValue > 50 ? 'warning' : 'normal',
    })
  }
  return points
}

const PATIENTS: Patient[] = [
  { patientId: 'PT-001', name: '林小雨', gender: 'female', age: 13, diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 28, deviceId: 'DEV-A3F312', teamId: 'TEAM-001', doctorId: 'DOC-001', status: 'active', createdAt: '2026-03-12T09:00:00+08:00', updatedAt: '2026-08-10T18:00:00+08:00' },
  { patientId: 'PT-002', name: '陈子航', gender: 'male', age: 15, diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 35, deviceId: 'DEV-B7E456', teamId: 'TEAM-001', doctorId: 'DOC-001', status: 'active', createdAt: '2026-04-02T10:30:00+08:00', updatedAt: '2026-08-11T08:00:00+08:00' },
  { patientId: 'PT-003', name: '王梓萌', gender: 'female', age: 12, diagnosis: '先天性脊柱侧弯', cobbAngle: 22, deviceId: 'DEV-C9D789', teamId: 'TEAM-002', doctorId: 'DOC-002', status: 'active', createdAt: '2026-05-18T14:00:00+08:00', updatedAt: '2026-08-09T16:20:00+08:00' },
  { patientId: 'PT-004', name: '刘俊熙', gender: 'male', age: 14, diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 31, deviceId: 'DEV-D2A012', teamId: 'TEAM-002', doctorId: 'DOC-002', status: 'active', createdAt: '2026-06-01T09:30:00+08:00', updatedAt: '2026-08-11T07:40:00+08:00' },
  { patientId: 'PT-005', name: '赵欣然', gender: 'female', age: 16, diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 40, deviceId: null, teamId: 'TEAM-003', doctorId: 'DOC-003', status: 'pending', createdAt: '2026-08-08T11:00:00+08:00', updatedAt: '2026-08-08T11:00:00+08:00' },
  { patientId: 'PT-006', name: '孙浩然', gender: 'male', age: 13, diagnosis: '姿势性脊柱侧弯', cobbAngle: 18, deviceId: 'DEV-E5B347', teamId: 'TEAM-003', doctorId: 'DOC-003', status: 'active', createdAt: '2026-07-15T15:30:00+08:00', updatedAt: '2026-08-10T20:10:00+08:00' },
]

export function mockPatients(params: { keyword?: string; teamId?: string; page?: number; pageSize?: number }): { list: Patient[]; total: number; page: number; pageSize: number } {
  const page = params.page ?? 1
  const pageSize = params.pageSize ?? 10
  let list = [...PATIENTS]
  if (params.keyword) {
    const kw = params.keyword.toLowerCase()
    list = list.filter((p) => p.name.toLowerCase().includes(kw) || p.patientId.toLowerCase().includes(kw))
  }
  if (params.teamId) {
    list = list.filter((p) => p.teamId === params.teamId)
  }
  const start = (page - 1) * pageSize
  return { list: list.slice(start, start + pageSize), total: list.length, page, pageSize }
}

export function mockPatientDetail(patientId: string): Patient | null {
  return PATIENTS.find((p) => p.patientId === patientId) ?? null
}

export interface RealtimeSnapshot {
  status: 'online' | 'offline' | 'abnormal'
  todayHours: number
  maxPressure: number
  maxPoint: string
  events: number
  pressureRecords: PressureRecord[]
  alerts: Alert[]
}

export function mockPatientRealtime(patientId: string): RealtimeSnapshot {
  const patient = mockPatientDetail(patientId)
  const offline = !patient || !patient.deviceId
  const abnormal = patientId === 'PT-004'
  const status = offline ? 'offline' : abnormal ? 'abnormal' : 'online'
  const record: PressureRecord = {
    recordId: `REC-${patientId}-latest`,
    deviceId: patient?.deviceId ?? '',
    patientId,
    timestamp: '2026-08-11T14:30:00+08:00',
    points: makePoints(abnormal ? 68 : 35),
    uploadTime: '2026-08-11T14:30:01+08:00',
  }
  return {
    status,
    todayHours: offline ? 0 : 6.5 + (patientId.charCodeAt(4) % 40) / 10,
    maxPressure: abnormal ? 68.5 : 42.3,
    maxPoint: abnormal ? 'P10' : 'P05',
    events: abnormal ? 3 : patientId === 'PT-002' ? 1 : 0,
    pressureRecords: offline ? [] : [record],
    alerts: [],
  }
}

// ========== T057 写功能 mock ==========

export interface CreatePatientInput {
  name: string
  phone: string                    // 11 位手机号（mock 不做判重，后端走 phone_hash 查重）
  gender?: 'male' | 'female' | null
  age?: number | null
  diagnosis?: string | null
  cobbAngle?: number | null
  deviceId?: string | null
  teamId?: string | null
  doctorId?: string | null
}

export interface BatchBindFailure {
  patientId: string
  reason: string
}

export interface BatchBindResult {
  successCount: number
  failedCount: number
  failures: BatchBindFailure[]
}

// mock 患者自增序号（PT-100 起，避开预置 PT-001~PT-006）
let patientSeq = 100

/** 创建患者：push 到 PATIENTS，返回新行（phone 不入 Patient 类型，mock 忽略） */
export function mockCreatePatient(input: CreatePatientInput): Patient {
  patientSeq += 1
  const now = new Date().toISOString()
  const patient: Patient = {
    patientId: `PT-${String(patientSeq).padStart(3, '0')}`,
    name: input.name,
    gender: input.gender ?? null,
    age: input.age ?? null,
    diagnosis: input.diagnosis ?? null,
    cobbAngle: input.cobbAngle ?? null,
    deviceId: input.deviceId ?? null,
    teamId: input.teamId ?? null,
    doctorId: input.doctorId ?? null,
    status: 'active',
    createdAt: now,
    updatedAt: now,
  }
  PATIENTS.push(patient)
  return patient
}

/** 分配/更改患者团队（幂等：同 teamId no-op，不变更 updatedAt） */
export function mockAssignPatientTeam(patientId: string, teamId: string): Patient {
  const p = PATIENTS.find((x) => x.patientId === patientId)
  if (!p) {
    throw new Error('患者不存在')
  }
  if (p.teamId !== teamId) {
    p.teamId = teamId
    p.updatedAt = new Date().toISOString()
  }
  return p
}

/**
 * 批量绑定患者到团队（部分失败不回滚，HTTP 仍 200）
 * mock 策略：不存在的患者计入 failures；存在的更新 teamId
 */
export function mockBatchBindPatients(patientIds: string[], teamId: string): BatchBindResult {
  const failures: BatchBindFailure[] = []
  let successCount = 0
  for (const pid of patientIds) {
    const p = PATIENTS.find((x) => x.patientId === pid)
    if (!p) {
      failures.push({ patientId: pid, reason: '患者不存在' })
      continue
    }
    if (p.teamId !== teamId) {
      p.teamId = teamId
      p.updatedAt = new Date().toISOString()
    }
    successCount += 1
  }
  return {
    successCount,
    failedCount: failures.length,
    failures,
  }
}
