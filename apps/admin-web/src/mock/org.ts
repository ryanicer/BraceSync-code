// 组织域 mock 数据（对齐 api-contracts.ts getTeams/getDoctors/getTechnicians/getInstallRecords）
import type { Team, Doctor, Technician, InstallRecord } from '@bracesync/shared-types'

const TEAMS: Team[] = [
  { teamId: 'TEAM-001', name: '脊柱侧弯一组', memberCount: 8, patientCount: 186 },
  { teamId: 'TEAM-002', name: '脊柱侧弯二组', memberCount: 7, patientCount: 204 },
  { teamId: 'TEAM-003', name: '术后康复组', memberCount: 6, patientCount: 158 },
  { teamId: 'TEAM-004', name: '儿童矫形组', memberCount: 9, patientCount: 312 },
  { teamId: 'TEAM-005', name: '成人矫形组', memberCount: 8, patientCount: 275 },
]

const DOCTORS: Doctor[] = [
  { doctorId: 'DOC-001', name: '张建国', title: '主任医师', department: '脊柱外科', teamId: 'TEAM-001', phoneMasked: '138****2201', patientCount: 68, status: 'enabled' },
  { doctorId: 'DOC-002', name: '陈小芳', title: '副主任医师', department: '脊柱外科', teamId: 'TEAM-002', phoneMasked: '139****3302', patientCount: 72, status: 'enabled' },
  { doctorId: 'DOC-003', name: '李明华', title: '主治医师', department: '康复科', teamId: 'TEAM-003', phoneMasked: '137****4403', patientCount: 55, status: 'enabled' },
  { doctorId: 'DOC-004', name: '王磊', title: '主治医师', department: '小儿骨科', teamId: 'TEAM-004', phoneMasked: '136****5504', patientCount: 84, status: 'enabled' },
  { doctorId: 'DOC-005', name: '赵敏', title: '副主任医师', department: '骨科', teamId: 'TEAM-005', phoneMasked: '135****6605', patientCount: 63, status: 'disabled' },
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
