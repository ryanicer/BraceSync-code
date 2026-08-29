// admin-web API 层：USE_MOCK=true 走 mock；false 走 request()（端点路径待后端确认，偏差见 T020 契约偏差清单）。
// Dashboard 域契约对齐 api-contracts.ts（T021 聚合接口）；告警域复用 T019B 已验证端点。
import type {
  AdminLoginResult, ApiResponse, DashboardKPI, TeamRanking, DoctorRanking, PaginatedResponse, Patient, Device,
  Alert, InstallRecord, Technician, Team, TeamDetail, TeamMember, Doctor, Feedback, OrthosisPlan,
  FeelingLog, HealthReport, NotifyRule, NotificationRecord, AlertType,
} from '@bracesync/shared-types'
import { USE_MOCK, request } from '../utils/request'
import * as dashboardMock from '../mock/dashboard'
import * as patientMock from '../mock/patients'
import * as alertMock from '../mock/alerts'
import * as orgMock from '../mock/org'
import * as deviceMock from '../mock/devices'
import * as feedbackMock from '../mock/communication'
import * as orthosisMock from '../mock/orthosis'
import * as systemMock from '../mock/system'
import type { RealtimeSnapshot } from '../mock/patients'
import type { SystemSettings, AdminRoleRow } from '../mock/system'

/** mock 模式模拟网络延迟 */
function delay(ms = 150): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

// ========== Auth（T046 真实登录） ==========

/** 真实登录：POST /api/v1/auth/login（T030 契约）。不走 request()——登录无需 token，且需读错误响应体的后端文案 */
export async function adminLogin(username: string, password: string): Promise<AdminLoginResult> {
  let res: Response
  try {
    res = await fetch('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    })
  } catch {
    throw new Error('网络错误，请稍后重试')
  }
  const body = (await res.json().catch(() => null)) as ApiResponse<AdminLoginResult> | null
  if (res.ok && body && body.code === 0 && body.data) {
    return body.data
  }
  // 10401 = 凭据错误/账号禁用（user-service CodeUnauthorized），文案对齐后端防账号枚举
  if (body?.code === 10401) {
    throw new Error('用户名或密码错误')
  }
  throw new Error(body?.message || `登录失败（HTTP ${res.status}）`)
}

// ========== Dashboard（T021 聚合接口，契约已定） ==========

export async function fetchDashboardKPI(period: 'today' | 'week' | 'month'): Promise<DashboardKPI> {
  if (USE_MOCK) { await delay(); return dashboardMock.mockDashboardKPI(period) }
  return request<DashboardKPI>({ url: '/api/v1/admin/dashboard/kpi', data: { period } })
}

export async function fetchWearTrend(days = 7): Promise<{ date: string; avgHours: number }[]> {
  if (USE_MOCK) { await delay(); return dashboardMock.mockWearTrend(days) }
  return request<{ date: string; avgHours: number }[]>({ url: '/api/v1/admin/dashboard/wear-trend', data: { days } })
}

export async function fetchAlertTrend(days = 7): Promise<{ date: string; count: number }[]> {
  if (USE_MOCK) { await delay(); return dashboardMock.mockAlertTrend(days) }
  return request<{ date: string; count: number }[]>({ url: '/api/v1/admin/dashboard/alert-trend', data: { days } })
}

export async function fetchTeamRanking(): Promise<TeamRanking[]> {
  if (USE_MOCK) { await delay(); return dashboardMock.mockTeamRanking() }
  return request<TeamRanking[]>({ url: '/api/v1/admin/dashboard/team-ranking' })
}

export async function fetchDoctorRanking(): Promise<DoctorRanking[]> {
  if (USE_MOCK) { await delay(); return dashboardMock.mockDoctorRanking() }
  return request<DoctorRanking[]>({ url: '/api/v1/admin/dashboard/doctor-ranking' })
}

export async function fetchWearDistribution(): Promise<{ range: string; count: number }[]> {
  if (USE_MOCK) { await delay(); return dashboardMock.mockWearDistribution() }
  return request<{ range: string; count: number }[]>({ url: '/api/v1/admin/dashboard/wear-distribution' })
}

// ========== Patient / Realtime ==========

export async function fetchPatients(params: { keyword?: string; teamId?: string; page?: number; pageSize?: number }): Promise<PaginatedResponse<Patient>> {
  if (USE_MOCK) { await delay(); return patientMock.mockPatients(params) }
  return request<PaginatedResponse<Patient>>({ url: '/api/v1/admin/patients', data: params as Record<string, unknown> })
}

export async function fetchPatientDetail(patientId: string): Promise<Patient | null> {
  if (USE_MOCK) { await delay(); return patientMock.mockPatientDetail(patientId) }
  return request<Patient>({ url: `/api/v1/admin/patients/${patientId}` })
}

export async function fetchPatientRealtime(patientId: string): Promise<RealtimeSnapshot> {
  if (USE_MOCK) { await delay(80); return patientMock.mockPatientRealtime(patientId) }
  // 契约已定（api-contracts.ts getPatientRealtime，data-service）
  return request<RealtimeSnapshot>({ url: `/api/v1/patients/${patientId}/realtime` })
}

// T057 患者写功能
export async function createPatientApi(input: patientMock.CreatePatientInput): Promise<Patient> {
  if (USE_MOCK) { await delay(); return patientMock.mockCreatePatient(input) }
  return request<Patient>({ url: '/api/v1/admin/patients', method: 'POST', data: input as unknown as Record<string, unknown> })
}

export async function assignPatientTeamApi(patientId: string, teamId: string): Promise<Patient> {
  if (USE_MOCK) { await delay(); return patientMock.mockAssignPatientTeam(patientId, teamId) }
  return request<Patient>({ url: `/api/v1/admin/patients/${patientId}/team`, method: 'PUT', data: { teamId } })
}

export async function batchBindPatientsApi(patientIds: string[], teamId: string): Promise<patientMock.BatchBindResult> {
  if (USE_MOCK) { await delay(); return patientMock.mockBatchBindPatients(patientIds, teamId) }
  return request<patientMock.BatchBindResult>({ url: '/api/v1/admin/patients/batch-bind', method: 'POST', data: { patientIds, teamId } })
}

// ========== Alert（复用 T019B 已验证端点） ==========

export async function fetchAlerts(params: { patientId?: string; type?: string; status?: string; page?: number; pageSize?: number }): Promise<PaginatedResponse<Alert>> {
  if (USE_MOCK) { await delay(); return alertMock.mockAlerts(params) }
  return request<PaginatedResponse<Alert>>({ url: '/api/v1/alerts', data: params as Record<string, unknown> })
}

export async function processAlertApi(alertId: string): Promise<void> {
  if (USE_MOCK) { await delay(); return }
  await request<null>({ url: `/api/v1/alerts/${alertId}/process`, method: 'POST' })
}

// ========== Device / Team / Doctor / Technician / Install ==========

export async function fetchDevices(params: { keyword?: string }): Promise<PaginatedResponse<Device>> {
  if (USE_MOCK) { await delay(); return deviceMock.mockDevices(params) }
  return request<PaginatedResponse<Device>>({ url: '/api/v1/devices', data: params as Record<string, unknown> })
}

export async function fetchTeams(): Promise<Team[]> {
  if (USE_MOCK) { await delay(); return orgMock.mockTeams() }
  return request<Team[]>({ url: '/api/v1/teams' })
}

// T059 团队管理写功能（6 写端点 + 1 成员明细读端点）
export async function fetchTeamMembersApi(teamId: string): Promise<orgMock.TeamMembersView> {
  if (USE_MOCK) { await delay(); return orgMock.mockTeamMembers(teamId) }
  return request<orgMock.TeamMembersView>({ url: `/api/v1/teams/${teamId}/members` })
}

export async function createTeamApi(input: orgMock.CreateTeamInput): Promise<TeamDetail> {
  if (USE_MOCK) { await delay(); return orgMock.mockCreateTeam(input) }
  return request<TeamDetail>({ url: '/api/v1/teams', method: 'POST', data: input as unknown as Record<string, unknown> })
}

export async function updateTeamApi(teamId: string, input: orgMock.UpdateTeamInput): Promise<TeamDetail> {
  if (USE_MOCK) { await delay(); return orgMock.mockUpdateTeam(teamId, input) }
  return request<TeamDetail>({ url: `/api/v1/teams/${teamId}`, method: 'PUT', data: input as unknown as Record<string, unknown> })
}

export async function deleteTeamApi(teamId: string): Promise<void> {
  if (USE_MOCK) { await delay(); return orgMock.mockDeleteTeam(teamId) }
  await request<null>({ url: `/api/v1/teams/${teamId}`, method: 'DELETE' })
}

export async function addTeamMemberApi(teamId: string, input: orgMock.AddMemberInput): Promise<TeamMember> {
  if (USE_MOCK) { await delay(); return orgMock.mockAddTeamMember(teamId, input) }
  return request<TeamMember>({ url: `/api/v1/teams/${teamId}/members`, method: 'POST', data: input as unknown as Record<string, unknown> })
}

export async function updateTeamMemberApi(teamId: string, memberId: string, input: orgMock.UpdateMemberInput): Promise<TeamMember> {
  if (USE_MOCK) { await delay(); return orgMock.mockUpdateTeamMember(teamId, memberId, input) }
  return request<TeamMember>({ url: `/api/v1/teams/${teamId}/members/${memberId}`, method: 'PUT', data: input as unknown as Record<string, unknown> })
}

export async function removeTeamMemberApi(teamId: string, memberId: string, memberType: 'doctor' | 'technician'): Promise<void> {
  if (USE_MOCK) { await delay(); return orgMock.mockRemoveTeamMember(teamId, memberId, memberType) }
  // DELETE 无 body：memberType 走 query（对齐规格端点 6）
  await request<null>({ url: `/api/v1/teams/${teamId}/members/${memberId}?memberType=${memberType}`, method: 'DELETE' })
}

export async function fetchDoctors(): Promise<Doctor[]> {
  if (USE_MOCK) { await delay(); return orgMock.mockDoctors() }
  return request<Doctor[]>({ url: '/api/v1/doctors' })
}

export async function fetchTechnicians(params: { page?: number; pageSize?: number }): Promise<PaginatedResponse<Technician>> {
  if (USE_MOCK) { await delay(); return orgMock.mockTechnicians(params) }
  return request<PaginatedResponse<Technician>>({ url: '/api/v1/technicians', data: params as Record<string, unknown> })
}

export async function toggleTechnicianApi(techId: string, action: 'enable' | 'disable'): Promise<void> {
  if (USE_MOCK) { await delay(); return }
  await request<null>({ url: `/api/v1/technicians/${techId}/toggle`, method: 'POST', data: { action } })
}

export async function fetchInstallRecords(params: { keyword?: string; page?: number; pageSize?: number }): Promise<PaginatedResponse<InstallRecord>> {
  if (USE_MOCK) { await delay(); return orgMock.mockInstallRecords(params) }
  return request<PaginatedResponse<InstallRecord>>({ url: '/api/v1/install-records', data: params as Record<string, unknown> })
}

// ========== Feedback（患者沟通） ==========

export async function fetchFeedbacks(params: { keyword?: string }): Promise<Feedback[]> {
  if (USE_MOCK) { await delay(); return feedbackMock.mockFeedbacks(params) }
  return request<Feedback[]>({ url: '/api/v1/feedbacks', data: params as Record<string, unknown> })
}

export async function processFeedbackApi(feedbackId: string, reply?: string): Promise<void> {
  if (USE_MOCK) { await delay(); return }
  await request<null>({ url: `/api/v1/feedbacks/${feedbackId}/process`, method: 'POST', data: { replyContent: reply } })
}

// ========== Orthosis（矫形日志 / 医生工作台） ==========

export async function fetchOrthosisPlans(patientId: string): Promise<OrthosisPlan[]> {
  if (USE_MOCK) { await delay(); return orthosisMock.mockOrthosisPlans(patientId) }
  return request<OrthosisPlan[]>({ url: `/api/v1/patients/${patientId}/orthosis-plans` })
}

export async function saveOrthosisPlanApi(patientId: string, content: string): Promise<OrthosisPlan | null> {
  if (USE_MOCK) {
    await delay()
    return { planId: `PLAN-${Date.now()}`, patientId, doctorId: 'DOC-001', content, version: 'v2.2', createdAt: new Date().toISOString() }
  }
  return request<OrthosisPlan>({ url: `/api/v1/patients/${patientId}/orthosis-plans`, method: 'POST', data: { content } })
}

export async function fetchFeelingLogs(patientId: string): Promise<FeelingLog[]> {
  if (USE_MOCK) { await delay(); return orthosisMock.mockFeelingLogs(patientId) }
  return request<FeelingLog[]>({ url: `/api/v1/patients/${patientId}/feeling-logs` })
}

export async function fetchHealthReports(patientId: string): Promise<HealthReport[]> {
  if (USE_MOCK) { await delay(); return orthosisMock.mockHealthReports(patientId) }
  return request<HealthReport[]>({ url: `/api/v1/patients/${patientId}/health-reports` })
}

// ========== 权限控制 / 系统配置 / 通知 ==========

export async function fetchAdminRoles(): Promise<AdminRoleRow[]> {
  if (USE_MOCK) { await delay(); return systemMock.mockAdminRoles() }
  return request<AdminRoleRow[]>({ url: '/api/v1/admin/roles' })
}

export async function fetchSystemSettings(): Promise<SystemSettings> {
  if (USE_MOCK) { await delay(); return systemMock.mockSystemSettings() }
  return request<SystemSettings>({ url: '/api/v1/admin/settings' })
}

export async function saveSystemSettingsApi(settings: SystemSettings): Promise<void> {
  if (USE_MOCK) { await delay(); return }
  await request<null>({ url: '/api/v1/admin/settings', method: 'PUT', data: settings as unknown as Record<string, unknown> })
}

export async function fetchNotifyRules(): Promise<NotifyRule[]> {
  if (USE_MOCK) { await delay(); return systemMock.mockNotifyRules() }
  // 契约已定（api-contracts.ts getNotifyRules，msg-service）
  return request<NotifyRule[]>({ url: '/api/v1/admin/notify-rules' })
}

export async function updateNotifyRuleApi(type: AlertType, data: Partial<Pick<NotifyRule, 'channels' | 'notifyTargets'>>): Promise<void> {
  if (USE_MOCK) { await delay(); return }
  await request<NotifyRule>({ url: `/api/v1/admin/notify-rules/${type}`, method: 'PUT', data: data as Record<string, unknown> })
}

export async function fetchNotificationLogs(params: { patientId?: string; channel?: string; status?: string; page?: number; pageSize?: number }): Promise<PaginatedResponse<NotificationRecord>> {
  if (USE_MOCK) { await delay(); return systemMock.mockNotificationLogs(params) }
  // 契约已定（api-contracts.ts getNotificationLogs，msg-service）
  return request<PaginatedResponse<NotificationRecord>>({ url: '/api/v1/admin/notification-logs', data: params as Record<string, unknown> })
}

// ========== 展示辅助（mock 期姓名映射，真实模式后端 join 返回后可移除） ==========

export function patientNameOf(patientId: string | null): string {
  if (USE_MOCK) {
    return deviceMock.mockPatientName(patientId)
  }
  // 真实模式：姓名由后端 join 提供至 row.patientName，此处仅作 patientId 兜底
  return patientId || '-'
}

export function teamNameOf(teamId: string | null): string {
  return orgMock.mockTeamName(teamId)
}

export function doctorNameOf(doctorId: string | null): string {
  return orgMock.mockDoctorName(doctorId)
}

export function techNameOf(techId: string): string {
  return orgMock.mockTechName(techId)
}
