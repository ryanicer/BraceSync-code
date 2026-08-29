// 组织域 mock 数据（对齐 api-contracts.ts getTeams/getDoctors/getTechnicians/getInstallRecords）
import type { Team, TeamDetail, TeamMember, Doctor, Technician, InstallRecord } from '@bracesync/shared-types'

/** T059 成员管理视图（GET /teams/:teamId/members 本地类型；shared-types.TeamMembers 为一期只读契约，此处用 TeamMember 统一字段） */
export interface TeamMembersView {
  doctors: TeamMember[]
  technicians: TeamMember[]
}

const TEAMS: Team[] = [
  { teamId: 'TEAM-001', name: '脊柱侧弯一组', memberCount: 8, patientCount: 186 },
  { teamId: 'TEAM-002', name: '脊柱侧弯二组', memberCount: 7, patientCount: 204 },
  { teamId: 'TEAM-003', name: '术后康复治疗组', memberCount: 6, patientCount: 158 },
  { teamId: 'TEAM-004', name: '儿童矫形组', memberCount: 9, patientCount: 312 },
  { teamId: 'TEAM-005', name: '成人矫形组', memberCount: 8, patientCount: 275 },
  // T059 团队管理写功能：骨科一组（被引用，删除返回 409）/ 康复组（可删）
  { teamId: 'TEAM-101', name: '骨科一组', memberCount: 4, patientCount: 86, leader: 'DOC-101', leaderName: '张主任', description: '骨科诊疗一组', status: 'active', createdAt: '2026-01-15T09:00:00+08:00' },
  { teamId: 'TEAM-102', name: '康复组', memberCount: 3, patientCount: 42, leader: 'DOC-103', leaderName: '王康复师', description: '康复诊疗组', status: 'active', createdAt: '2026-02-20T10:00:00+08:00' },
]

const DOCTORS: Doctor[] = [
  { doctorId: 'DOC-001', name: '张建国', title: '主任医师', department: '脊柱外科', teamId: 'TEAM-001', phoneMasked: '138****2201', patientCount: 68, status: 'enabled' },
  { doctorId: 'DOC-002', name: '陈小芳', title: '副主任医师', department: '脊柱外科', teamId: 'TEAM-002', phoneMasked: '139****3302', patientCount: 72, status: 'enabled' },
  { doctorId: 'DOC-003', name: '李明华', title: '主治医师', department: '康复科', teamId: 'TEAM-003', phoneMasked: '137****4403', patientCount: 55, status: 'enabled' },
  { doctorId: 'DOC-004', name: '王磊', title: '主治医师', department: '小儿骨科', teamId: 'TEAM-004', phoneMasked: '136****5504', patientCount: 84, status: 'enabled' },
  { doctorId: 'DOC-005', name: '赵敏', title: '副主任医师', department: '骨科', teamId: 'TEAM-005', phoneMasked: '135****6605', patientCount: 63, status: 'disabled' },
  // T059 团队管理写功能：张主任（骨科一组 leader）/ 王康复师（可作负责人）/ 刘医生（可添加成员）/ 王护士（骨科一组，可移除）
  { doctorId: 'DOC-101', name: '张主任', title: '主任医师', department: '骨科', teamId: 'TEAM-101', phoneMasked: '138****1101', patientCount: 36, status: 'enabled' },
  { doctorId: 'DOC-102', name: '王护士', title: '护士', department: '骨科', teamId: 'TEAM-101', phoneMasked: '139****2202', patientCount: 12, status: 'enabled' },
  { doctorId: 'DOC-103', name: '王康复师', title: '康复师', department: '康复科', teamId: 'TEAM-102', phoneMasked: '137****3303', patientCount: 28, status: 'enabled' },
  { doctorId: 'DOC-104', name: '刘医生', title: '主治医师', department: '骨科', teamId: null, phoneMasked: '136****4404', patientCount: 0, status: 'enabled' },
]

const TECHNICIANS: Technician[] = [
  { techId: 'TECH-001', name: '周师傅', phoneMasked: '138****5678', teamId: 'TEAM-001', installCount: 46, status: 'enabled', authStatus: 'authorized' },
  { techId: 'TECH-002', name: '吴师傅', phoneMasked: '139****6789', teamId: 'TEAM-002', installCount: 38, status: 'enabled', authStatus: 'authorized' },
  { techId: 'TECH-003', name: '郑师傅', phoneMasked: '137****7890', teamId: 'TEAM-003', installCount: 29, status: 'enabled', authStatus: 'unauthorized' },
  { techId: 'TECH-004', name: '冯师傅', phoneMasked: '136****8901', teamId: 'TEAM-004', installCount: 52, status: 'disabled', authStatus: 'authorized' },
]

const INSTALL_RECORDS: InstallRecord[] = [
  { installId: 'INS-001', deviceId: 'DEV-A3F312', patientId: 'PT-001', techId: 'TECH-001', calibrateTime: '2026-03-12T10:00:00+08:00', baselineId: 'BL-001', notes: '首次安装，空载校准通过', signatureUrl: '', wifiStatus: 'connected' },
  { installId: 'INS-002', deviceId: 'DEV-B7E456', patientId: 'PT-002', techId: 'TECH-001', calibrateTime: '2026-04-02T14:00:00+08:00', baselineId: 'BL-002', notes: '首次安装', signatureUrl: '', wifiStatus: 'connected' },
  { installId: 'INS-003', deviceId: 'DEV-C9D789', patientId: 'PT-003', techId: 'TECH-002', calibrateTime: '2026-05-18T15:30:00+08:00', baselineId: 'BL-003', notes: '首次安装，WiFi 待配网', signatureUrl: '', wifiStatus: 'unconfigured' },
  { installId: 'INS-004', deviceId: 'DEV-D2A012', patientId: 'PT-004', techId: 'TECH-002', calibrateTime: '2026-06-01T10:30:00+08:00', baselineId: 'BL-004', notes: '首次安装', signatureUrl: '', wifiStatus: 'connected' },
  { installId: 'INS-005', deviceId: 'DEV-E5B347', patientId: 'PT-006', techId: 'TECH-003', calibrateTime: '2026-07-15T16:00:00+08:00', baselineId: null, notes: '基线待保存', signatureUrl: '', wifiStatus: 'unconfigured' },
]

export function mockTeams(): Team[] {
  return TEAMS.map((t) => ({ ...t }))
}

export function mockTeamName(teamId: string | null): string {
  if (!teamId) return '-'
  return TEAMS.find((t) => t.teamId === teamId)?.name ?? teamId
}

export function mockDoctors(): Doctor[] {
  return DOCTORS.map((d) => ({ ...d }))
}

export function mockDoctorName(doctorId: string | null): string {
  if (!doctorId) return '-'
  return DOCTORS.find((d) => d.doctorId === doctorId)?.name ?? doctorId
}

export function mockTechnicians(params: { page?: number; pageSize?: number }): { list: Technician[]; total: number; page: number; pageSize: number } {
  const page = params.page ?? 1
  const pageSize = params.pageSize ?? 10
  const start = (page - 1) * pageSize
  return { list: TECHNICIANS.slice(start, start + pageSize), total: TECHNICIANS.length, page, pageSize }
}

export function mockInstallRecords(params: { keyword?: string; page?: number; pageSize?: number }): { list: InstallRecord[]; total: number; page: number; pageSize: number } {
  const page = params.page ?? 1
  const pageSize = params.pageSize ?? 10
  let list = INSTALL_RECORDS.map((r) => ({ ...r }))
  if (params.keyword) {
    const kw = params.keyword.toLowerCase()
    list = list.filter((r) => r.deviceId.toLowerCase().includes(kw) || r.patientId.toLowerCase().includes(kw) || r.installId.toLowerCase().includes(kw))
  }
  const start = (page - 1) * pageSize
  return { list: list.slice(start, start + pageSize), total: list.length, page, pageSize }
}

export function mockTechName(techId: string): string {
  return TECHNICIANS.find((t) => t.techId === techId)?.name ?? techId
}

// ========== T059 团队管理写功能 mock ==========

/** 团队成员明细（对齐 GET /teams/:teamId/members，按 team_id 过滤 doctors/technicians） */
export function mockTeamMembers(teamId: string): TeamMembersView {
  const team = TEAMS.find((t) => t.teamId === teamId)
  if (!team) {
    throw new Error('团队不存在')
  }
  const doctors = DOCTORS.filter((d) => d.teamId === teamId).map((d) => toDoctorMember(d, team.createdAt))
  const technicians = TECHNICIANS.filter((t) => t.teamId === teamId).map((t) => toTechMember(t, team.createdAt))
  return { doctors, technicians }
}

function toDoctorMember(d: Doctor, joinTimeFallback: string | undefined): TeamMember {
  return {
    memberId: d.doctorId,
    memberType: 'doctor',
    name: d.name,
    role: d.title,
    title: d.department,
    phoneMasked: d.phoneMasked,
    patientCount: d.patientCount,
    joinTime: joinTimeFallback ?? new Date().toISOString(),
    status: d.status,
  }
}

function toTechMember(t: Technician, joinTimeFallback: string | undefined): TeamMember {
  return {
    memberId: t.techId,
    memberType: 'technician',
    name: t.name,
    role: null,
    title: null,
    phoneMasked: t.phoneMasked,
    patientCount: 0,
    joinTime: joinTimeFallback ?? new Date().toISOString(),
    status: t.status,
  }
}

export interface CreateTeamInput {
  name: string                    // 必填，≤50
  leader: string                  // 必填，doctorId 存在性校验
  description?: string            // 可选，≤200
}

export interface UpdateTeamInput {
  name: string
  leader: string
  description?: string
}

export interface AddMemberInput {
  memberType: 'doctor' | 'technician'
  memberId: string
  role?: string
}

export interface UpdateMemberInput {
  memberType: 'doctor' | 'technician'
  role?: string
}

/** mock 团队自增序号（TEAM-200 起，避开预置） */
let teamSeq = 200

/** 创建团队：name 查重命中 → 抛错；leader 不存在 → 抛错；否则返回 TeamDetail（mock 不持久化，前端乐观 push） */
export function mockCreateTeam(input: CreateTeamInput): TeamDetail {
  const name = input.name.trim()
  if (TEAMS.some((t) => t.name === name)) {
    throw new Error('团队名称已存在')
  }
  const leader = DOCTORS.find((d) => d.doctorId === input.leader)
  if (!leader) {
    throw new Error('负责人不存在')
  }
  teamSeq += 1
  const now = new Date().toISOString()
  return {
    teamId: `TEAM-${String(teamSeq).padStart(3, '0')}`,
    name,
    leader: leader.doctorId,
    leaderName: leader.name,
    memberCount: 0,
    patientCount: 0,
    description: input.description ?? null,
    status: 'active',
    createdAt: now,
  }
}

/** 编辑团队：团队不存在 → 抛错；name 被他人占用 → 抛错；否则返回更新后的 TeamDetail */
export function mockUpdateTeam(teamId: string, input: UpdateTeamInput): TeamDetail {
  const team = TEAMS.find((t) => t.teamId === teamId)
  if (!team) {
    throw new Error('团队不存在')
  }
  const name = input.name.trim()
  if (TEAMS.some((t) => t.teamId !== teamId && t.name === name)) {
    throw new Error('团队名称已存在')
  }
  const leader = DOCTORS.find((d) => d.doctorId === input.leader)
  if (!leader) {
    throw new Error('负责人不存在')
  }
  return {
    ...team,
    name,
    leader: leader.doctorId,
    leaderName: leader.name,
    description: input.description ?? null,
    status: team.status ?? 'active',
    createdAt: team.createdAt ?? new Date().toISOString(),
  }
}

/** 被引用团队（删除返回 409 + 引用计数）；其余团队删除成功（mock 不持久化，前端乐观移除） */
const REFERENCED_TEAM_IDS = new Set(['TEAM-101'])

/** 删除团队：被引用 → 抛错含引用计数；否则返回 void */
export function mockDeleteTeam(teamId: string): void {
  const team = TEAMS.find((t) => t.teamId === teamId)
  if (!team) {
    throw new Error('团队不存在')
  }
  if (REFERENCED_TEAM_IDS.has(teamId)) {
    const patientCount = team.patientCount
    const memberCount = DOCTORS.filter((d) => d.teamId === teamId).length + TECHNICIANS.filter((t) => t.teamId === teamId).length
    throw new Error(`团队被引用：${patientCount} 患者，${memberCount} 成员，请先迁移再删`)
  }
}

/** 添加成员：member 已在本团队 → 抛错；否则返回 TeamMember（mock 不持久化） */
export function mockAddTeamMember(teamId: string, input: AddMemberInput): TeamMember {
  const team = TEAMS.find((t) => t.teamId === teamId)
  if (!team) {
    throw new Error('团队不存在')
  }
  if (input.memberType === 'doctor') {
    const d = DOCTORS.find((x) => x.doctorId === input.memberId)
    if (!d) {
      throw new Error('成员不存在')
    }
    if (d.teamId === teamId) {
      throw new Error('成员已在本团队')
    }
    return toDoctorMember({ ...d, title: input.role || d.title }, team.createdAt)
  }
  const t = TECHNICIANS.find((x) => x.techId === input.memberId)
  if (!t) {
    throw new Error('成员不存在')
  }
  if (t.teamId === teamId) {
    throw new Error('成员已在本团队')
  }
  return toTechMember(t, team.createdAt)
}

/** 编辑成员：返回更新后的 TeamMember（role 更新，mock 不持久化） */
export function mockUpdateTeamMember(teamId: string, memberId: string, input: UpdateMemberInput): TeamMember {
  const team = TEAMS.find((t) => t.teamId === teamId)
  if (!team) {
    throw new Error('团队不存在')
  }
  if (input.memberType === 'doctor') {
    const d = DOCTORS.find((x) => x.doctorId === memberId && x.teamId === teamId)
    if (!d) {
      throw new Error('成员不存在')
    }
    return toDoctorMember({ ...d, title: input.role || d.title }, team.createdAt)
  }
  const t = TECHNICIANS.find((x) => x.techId === memberId && x.teamId === teamId)
  if (!t) {
    throw new Error('成员不存在')
  }
  return toTechMember(t, team.createdAt)
}

/** 移除成员：幂等 no-op（mock 不持久化，前端乐观移除） */
export function mockRemoveTeamMember(_teamId: string, _memberId: string, _memberType: 'doctor' | 'technician'): void {
  // 幂等：不校验存在性，直接返回成功
}
