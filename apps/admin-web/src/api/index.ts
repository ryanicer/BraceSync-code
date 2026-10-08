// admin-web API 层：USE_MOCK=true 走 mock；false 走 request()（端点路径待后端确认，偏差见 T020 契约偏差清单）。
// Dashboard 域契约对齐 api-contracts.ts（T021 聚合接口）；告警域复用 T019B 已验证端点。
import type {
  AdminLoginResult, ApiResponse, DashboardKPI, TeamRanking, DoctorRanking, PaginatedResponse, Patient, Device,
  Alert, InstallRecordRow, InstallRecordDetail, Technician, Team, TeamDetail, TeamMember, TeamStats, Doctor, Feedback, OrthosisPlan,
  FeelingLog, HealthReport, NotifyRule, NotificationRecord, AlertType,
  ReviewRecord, CreateReviewRecordRequest, ReviewTemplate, CreateReviewTemplateRequest,
  RolePermissions,
} from '@bracesync/shared-types'
import { USE_MOCK, request, expiredSession } from '../utils/request'
import { attachErrorMeta, markUserCopy } from '@bracesync/shared-utils'
import { isAuthExpired } from '../utils/sessionExpiry'
import { getToken } from '../utils/token'
import { reactive } from 'vue'
import * as dashboardMock from '../mock/dashboard'
import * as patientMock from '../mock/patients'
import * as alertMock from '../mock/alerts'
import * as orgMock from '../mock/org'
import * as deviceMock from '../mock/devices'
import * as feedbackMock from '../mock/communication'
import * as orthosisMock from '../mock/orthosis'
import * as reviewMock from '../mock/review'
import * as systemMock from '../mock/system'
import type { RealtimeSnapshot } from '../mock/patients'
import type { SystemSettings, AdminRoleRow } from '../mock/system'
import type { DailyWearDay } from '../utils/workbenchData'

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
  // T465：这句刻意自撰（防枚举口径），打 userCopy 标记，页面传任何 fallback 都不会被覆盖成通用句
  if (body?.code === 10401) {
    throw markUserCopy(new Error('用户名或密码错误'), '用户名或密码错误')
  }
  // T465：码与 HTTP 状态挂到错误对象上；message 仍留后端原文给日志
  throw attachErrorMeta(new Error(body?.message || `登录失败（HTTP ${res.status}）`), {
    code: body?.code,
    httpStatus: res.status,
  })
}

/**
 * T487 自助改密：POST /api/v1/auth/change-password（后台 JWT 鉴权）。
 * 请求体只有 old_password / new_password 两个键 —— 身份取网关注入的 X-User-Id（由 JWT 重签），
 * 从体里收账号标识就等于「谁能替别人设密码」，故这条端点不收 adminId。
 * mock 分支只走通交互（本地开发没有口令存储），真实校验在 USE_MOCK=false 下由后端判定。
 */
export async function changeOwnPasswordApi(oldPassword: string, newPassword: string): Promise<void> {
  if (USE_MOCK) { await delay(); return }
  await request<null>({ url: '/api/v1/auth/change-password', method: 'POST', data: { old_password: oldPassword, new_password: newPassword } })
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

export async function fetchTeamRanking(period: 'today' | 'week' | 'month'): Promise<TeamRanking[]> {
  if (USE_MOCK) { await delay(); return dashboardMock.mockTeamRanking(period) }
  return request<TeamRanking[]>({ url: '/api/v1/admin/dashboard/team-ranking', data: { period } })
}

export async function fetchDoctorRanking(period: 'today' | 'week' | 'month'): Promise<DoctorRanking[]> {
  if (USE_MOCK) { await delay(); return dashboardMock.mockDoctorRanking(period) }
  return request<DoctorRanking[]>({ url: '/api/v1/admin/dashboard/doctor-ranking', data: { period } })
}

export async function fetchWearDistribution(period: 'today' | 'week' | 'month'): Promise<{ range: string; count: number }[]> {
  if (USE_MOCK) { await delay(); return dashboardMock.mockWearDistribution(period) }
  return request<{ range: string; count: number }[]>({ url: '/api/v1/admin/dashboard/wear-distribution', data: { period } })
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

/**
 * T344 工作台「数据视图」取数：按日佩戴聚合（data-service getDailyWear，T076）。
 * start/end 为东八区闭区间；一个端点同时供压力趋势（avgPressure）与每日佩戴时长（wearMinutes）。
 * 不用 /records：它的 period 只有 day/week/month 三档自然周期且 date 必填
 * （services/data-service/internal/service/record.go:689-708），给不出稿面要的滚动 7/14/30 天。
 */
export async function fetchPatientDailyWear(patientId: string, start: string, end: string): Promise<DailyWearDay[]> {
  if (USE_MOCK) { await delay(); return patientMock.mockPatientDailyWear(patientId, start, end) }
  return request<DailyWearDay[]>({ url: `/api/v1/patients/${patientId}/daily-wear`, data: { start, end } })
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

/**
 * T432 管理端改手机号（PUT /admin/patients/:id/phone，T085）。
 * reason 必填属前端表单口径 —— 后端只把它写进审计日志、不校验（admin_patient.go:125-132）。
 * 成功响应只有 {patientId}，回不了新号，故页面不指望它刷新号码展示。
 */
export async function updatePatientPhoneApi(patientId: string, phone: string, reason: string): Promise<{ patientId: string }> {
  if (USE_MOCK) { await delay(); return patientMock.mockUpdatePatientPhone(patientId, phone, reason) }
  const url = `/api/v1/admin/patients/${encodeURIComponent(patientId)}/phone`
  return request<{ patientId: string }>({ url, method: 'PUT', data: { phone, reason } })
}

/**
 * T432 档案编辑（PUT /admin/patients/:id，T248 4.3）。
 * 🔴 只下发改过的键：后端是指针语义（nil=不改）+ DisallowUnknownFields，
 * 多带 phone / teamId / status 任一 key 就整单 400，一个 key 都不给也是 400。
 */
export async function updatePatientProfileApi(patientId: string, patch: patientMock.PatientProfilePatch): Promise<Patient> {
  if (USE_MOCK) { await delay(); return patientMock.mockUpdatePatientProfile(patientId, patch) }
  const url = `/api/v1/admin/patients/${encodeURIComponent(patientId)}`
  return request<Patient>({ url, method: 'PUT', data: patch as unknown as Record<string, unknown> })
}

/**
 * T432 解绑微信（POST /admin/patients/:id/unbind-wechat，T085）。
 * 后端无条件置 NULL，未绑定的患者亦返回 200；患者域读侧无 openid，故页面无法显示绑定态。
 */
export async function unbindPatientWechatApi(patientId: string): Promise<{ patientId: string }> {
  if (USE_MOCK) { await delay(); return patientMock.mockUnbindPatientWechat(patientId) }
  const url = `/api/v1/admin/patients/${encodeURIComponent(patientId)}/unbind-wechat`
  return request<{ patientId: string }>({ url, method: 'POST' })
}

/**
 * T500 患者设登录口令（POST /admin/patients/:id/password，T477 端点）。
 * 口令由服务端随机生成、bcrypt 落库，明文只在本次响应返回一次，之后任何端点都取不回来
 * （遗失就再调一次重设，同技师/医护侧口径）。响应形状 {patientId, password} 未登记进契约
 * （scripts/contract/dto-contract-map.mjs 把这类一次性写响应显式记为 ts:null），故本地声明。
 */
export async function setPatientPasswordApi(patientId: string): Promise<string> {
  if (USE_MOCK) { await delay(); return patientMock.mockSetPatientPassword(patientId).password }
  // 这里的请求地址必须写成内联字符串字面量，不能先存进变量再简写进参数对象：
  // T379 的跨源门禁（test/perm-gateway-cross-source.spec.ts）是从源码里派生「页面 import 的函数打了哪些端点」，
  // 派生正主要认的就是参数对象里的地址键。写成变量简写它读不到这条 ⇒
  // 「患者页只对 admin 端点」那一格对拍会静默失明（前端把这条挂给非 admin 页面时没人拦）。
  const res = await request<{ patientId: string; password: string }>({
    url: `/api/v1/admin/patients/${encodeURIComponent(patientId)}/password`,
    method: 'POST',
    data: {},
  })
  return res.password
}

// ========== Alert（复用 T019B 已验证端点） ==========

export async function fetchAlerts(params: { patientId?: string; type?: string; status?: string; page?: number; pageSize?: number }): Promise<PaginatedResponse<Alert>> {
  if (USE_MOCK) { await delay(); return alertMock.mockAlerts(params) }
  return request<PaginatedResponse<Alert>>({ url: '/api/v1/alerts', data: params as Record<string, unknown> })
}

/** 处理备注随体提交：后端 alerts.process_note 列已存在但写路径未落库（T269 D4 已报 PM） */
export async function processAlertApi(alertId: string, note?: string | null): Promise<void> {
  if (USE_MOCK) { await delay(); return }
  await request<null>({ url: `/api/v1/alerts/${alertId}/process`, method: 'POST', data: note ? { note } : undefined })
}

/** T289 2.7：开始处理（pending→processing，后端幂等；已 processed 返回 409） */
export interface AlertStartProcessingResult {
  alertId: string
  processStatus: string
  inProgressAt: string
}

export async function startProcessingAlertApi(alertId: string): Promise<AlertStartProcessingResult> {
  if (USE_MOCK) { await delay(); return alertMock.mockStartProcessing(alertId) }
  return request<AlertStartProcessingResult>({ url: `/api/v1/alerts/${alertId}/processing`, method: 'POST' })
}

// ========== 告警规则配置（T253-2.2，契约 docs/api/api-contracts.ts AlertRules，T252 后端） ==========

export async function fetchAlertRules(): Promise<alertMock.AlertRules> {
  if (USE_MOCK) { await delay(); return alertMock.mockAlertRules() }
  return request<alertMock.AlertRules>({ url: '/api/v1/admin/alert-rules' })
}

export async function saveAlertPointRulesApi(input: {
  unifiedUpperN?: number
  unifiedLowerN?: number
  points?: alertMock.AlertPointRuleUpdate[]
}): Promise<alertMock.AlertRules> {
  if (USE_MOCK) { await delay(); return alertMock.mockSaveAlertPointRules(input) }
  return request<alertMock.AlertRules>({ url: '/api/v1/admin/alert-rules/points', method: 'PUT', data: input as unknown as Record<string, unknown> })
}

export async function resetAlertPointRulesApi(): Promise<alertMock.AlertRules> {
  if (USE_MOCK) { await delay(); return alertMock.mockResetAlertPointRules() }
  return request<alertMock.AlertRules>({ url: '/api/v1/admin/alert-rules/points/reset', method: 'POST' })
}

export async function saveAlertGlobalRulesApi(input: Partial<alertMock.AlertGlobalRules>): Promise<alertMock.AlertRules> {
  if (USE_MOCK) { await delay(); return alertMock.mockSaveAlertGlobalRules(input) }
  return request<alertMock.AlertRules>({ url: '/api/v1/admin/alert-rules/global', method: 'PUT', data: input as unknown as Record<string, unknown> })
}

// ========== 患者异常报告（T300，端点由 alert-service 提供，网关已转发 + staff 专属） ==========

export async function fetchAbnormalReport(q: alertMock.AbnormalReportQuery): Promise<alertMock.AbnormalReport> {
  if (USE_MOCK) { await delay(); return alertMock.mockAbnormalReport(q) }
  return request<alertMock.AbnormalReport>({
    url: '/api/v1/admin/abnormal-reports',
    data: { patientId: q.patientId, start: q.start, end: q.end },
  })
}

/**
 * 导出 CSV。走带 Authorization 的 fetch 取回二进制再触发下载——
 * window.open / <a href> 不带凭据头，真实模式下会被网关 401（现有两处下载走的是预签名 URL，不适用于此）。
 * T384：401 必须走 expiredSession() 唯一出口。此前这条通道只 throw 一句「导出失败」，
 * 现场形态是「页面数据走 request() 会被弹回登录页，但在页上点导出不会」——凭据不清、不跳登录。
 */
export async function exportAbnormalReportApi(q: alertMock.AbnormalReportQuery): Promise<void> {
  if (USE_MOCK) {
    await delay()
    saveCsvText(`abnormal-report-${q.patientId}-${q.start}_${q.end}.csv`, alertMock.mockAbnormalReportCsv(q))
    return
  }
  const params = new URLSearchParams({ patientId: q.patientId, start: q.start, end: q.end })
  const token = getToken()
  let res: Response
  try {
    res = await fetch(`/api/v1/admin/abnormal-reports/export?${params.toString()}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
  } catch {
    throw new Error('网络错误，导出失败')
  }
  if (!res.ok) {
    // 失败时后端回 JSON 信封而非 CSV，不能把错误体当文件存盘
    const body = (await res.json().catch(() => null)) as ApiResponse<unknown> | null
    if (isAuthExpired(res.status, body?.code)) expiredSession()
    throw attachErrorMeta(new Error(body?.message || `导出失败（HTTP ${res.status}）`), {
      code: body?.code,
      httpStatus: res.status,
    })
  }
  saveCsvBlob(dispositionFilename(res.headers.get('Content-Disposition')), await res.blob())
}

/** 从 Content-Disposition 取后端给的文件名（含患者编号与日期范围） */
export function dispositionFilename(header: string | null): string {
  // 只认带引号形态：alert-service 的 reportFilename 恒发 filename="..."，
  // 且把 patientId 收敛到 [A-Za-z0-9_-]（services/alert-service/internal/handler/t300_report_test.go 逐字钉住）。
  // T401：上一版 `filename="?([^";]+?)"?` 的惰性量词配可选引号会最短匹配到首字符，落地文件名被截成 a.csv。
  const m = /filename="([^"]+)"/.exec(header ?? '')
  return m ? m[1].trim() : 'abnormal-report.csv'
}

function saveCsvText(filename: string, text: string): void {
  saveCsvBlob(filename, new Blob([text], { type: 'text/csv;charset=utf-8' }))
}

function saveCsvBlob(filename: string, blob: Blob): void {
  const href = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = href
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(href)
}

// ========== Device / Team / Doctor / Technician / Install ==========

export async function fetchDevices(params: { keyword?: string }): Promise<PaginatedResponse<Device>> {
  if (USE_MOCK) { await delay(); return deviceMock.mockDevices(params) }
  return request<PaginatedResponse<Device>>({ url: '/api/v1/devices', data: params as Record<string, unknown> })
}

// T268: 设备详情 / 绑定历史 / 注册（对齐 device-service：GET /devices/:id、GET /devices/:id/bindings、POST /devices 幂等）
export interface DeviceBindingRecord {
  bindingId: string
  deviceId: string
  patientId: string
  bindAt: string
  unbindAt: string | null
  reason: string | null
  operatorId: string | null
}

export async function fetchDeviceDetail(deviceId: string): Promise<Device> {
  if (USE_MOCK) { await delay(); return deviceMock.mockDeviceDetail(deviceId) }
  return request<Device>({ url: `/api/v1/devices/${encodeURIComponent(deviceId)}` })
}

export async function fetchDeviceBindings(deviceId: string): Promise<DeviceBindingRecord[]> {
  if (USE_MOCK) { await delay(); return deviceMock.mockDeviceBindings(deviceId) }
  const res = await request<{ list: DeviceBindingRecord[] }>({ url: `/api/v1/devices/${encodeURIComponent(deviceId)}/bindings` })
  return res.list
}

export async function registerDeviceApi(data: { deviceId: string; model?: string }): Promise<Device> {
  if (USE_MOCK) { await delay(); return deviceMock.mockRegisterDevice(data) }
  return request<Device>({ url: '/api/v1/devices', method: 'POST', data: data as unknown as Record<string, unknown> })
}

export async function fetchTeams(): Promise<Team[]> {
  if (USE_MOCK) { await delay(); return orgMock.mockTeams() }
  const teams = await request<Team[]>({ url: '/api/v1/teams' })
  for (const t of teams) orgNames.teams[t.teamId] = t.name
  return teams
}

/** T289 5.1 团队管理 4 张统计卡（T256 #1 端点，后端字段见 model.TeamStatsDTO） */
export async function fetchTeamStats(): Promise<TeamStats> {
  if (USE_MOCK) { await delay(); return orgMock.mockTeamStats() }
  return request<TeamStats>({ url: '/api/v1/admin/teams/stats' })
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
  const doctors = await request<Doctor[]>({ url: '/api/v1/doctors' })
  for (const d of doctors) orgNames.doctors[d.doctorId] = d.name
  return doctors
}

export async function fetchTechnicians(params: { page?: number; pageSize?: number }): Promise<PaginatedResponse<Technician>> {
  if (USE_MOCK) { await delay(); return orgMock.mockTechnicians(params) }
  return request<PaginatedResponse<Technician>>({ url: '/api/v1/technicians', data: params as Record<string, unknown> })
}

export async function toggleTechnicianApi(techId: string, action: 'enable' | 'disable'): Promise<void> {
  if (USE_MOCK) { await delay(); return }
  await request<null>({ url: `/api/v1/technicians/${techId}/toggle`, method: 'POST', data: { action } })
}

// T247: 技师新建 / 编辑（后端 handler.go:869 / :913 已实现，网关 proxy_admin.go:132-133 已放行）
export interface CreateTechnicianInput {
  name: string
  phone: string
  teamId: string
}

export interface CreateTechnicianResult {
  account: Technician
  /** 一次性凭据：只在创建响应里出现这一次，页面关窗后不再有入口（T480，同医护账号口径） */
  initialPassword: string
}

/** 创建响应 = 整行 Technician + initialPassword（后端 TechnicianCreateDTO 是 TechnicianDTO 的展开） */
type TechnicianCreateResponse = Technician & { initialPassword?: string }

export async function createTechnicianApi(input: CreateTechnicianInput): Promise<CreateTechnicianResult> {
  if (USE_MOCK) { await delay(); return orgMock.mockCreateTechnician(input) }
  const res = await request<TechnicianCreateResponse>({
    url: '/api/v1/admin/technicians',
    method: 'POST',
    data: input as unknown as Record<string, unknown>,
  })
  const { initialPassword, ...account } = res
  // 服务端 bcrypt 后不留明文，只在本响应出现一次；取不到即当缺失，不拿空串冒充「密码为空」
  return { account, initialPassword: initialPassword ?? '' }
}

/**
 * T480 重置技师登录口令：服务端重新随机发号、旧口令即时失效、明文只在本次响应返回一次。
 * 技师端登录用「手机号 + 口令」⇒ 弹窗要连手机号一起给，否则拿到口令也不知道登哪个账号。
 */
export async function resetTechnicianPasswordApi(techId: string): Promise<string> {
  if (USE_MOCK) { await delay(); return orgMock.mockResetTechnicianPassword(techId) }
  const res = await request<{ techId: string; password: string }>({
    url: `/api/v1/admin/technicians/${techId}/reset-password`,
    method: 'POST',
    data: {},
  })
  return res.password
}

export async function updateTechnicianApi(techId: string, input: Partial<CreateTechnicianInput>): Promise<Technician> {
  if (USE_MOCK) { await delay(); return orgMock.mockUpdateTechnician(techId, input) }
  return request<Technician>({ url: `/api/v1/admin/technicians/${techId}`, method: 'PUT', data: input as unknown as Record<string, unknown> })
}

export async function fetchInstallRecords(params: { keyword?: string; page?: number; pageSize?: number }): Promise<PaginatedResponse<InstallRecordRow>> {
  if (USE_MOCK) { await delay(); return orgMock.mockInstallRecords(params) }
  return request<PaginatedResponse<InstallRecordRow>>({ url: '/api/v1/install-records', data: params as Record<string, unknown> })
}

/** T289 9.1：单条安装记录详情（契约 getInstallDetail，网关已放行；含 20 点偏移值与校准状态） */
export async function fetchInstallRecordDetail(installId: string): Promise<InstallRecordDetail> {
  if (USE_MOCK) { await delay(); return orgMock.mockInstallRecordDetail(installId) }
  return request<InstallRecordDetail>({ url: `/api/v1/install-records/${installId}` })
}

// ========== Feedback（患者沟通） ==========

export async function fetchFeedbacks(params: { keyword?: string }): Promise<Feedback[]> {
  if (USE_MOCK) { await delay(); return feedbackMock.mockFeedbacks(params) }
  return request<Feedback[]>({ url: '/api/v1/feedbacks', data: params as Record<string, unknown> })
}

// T374：本页两个写动作分流 —— 带 replyContent 是「保存处理备注」，带 markResolved 是「标记为已处理」
export async function processFeedbackApi(
  feedbackId: string,
  payload: { replyContent?: string; markResolved?: boolean },
): Promise<void> {
  if (USE_MOCK) { await delay(); feedbackMock.mockProcessFeedback(feedbackId, payload); return }
  await request<null>({ url: `/api/v1/feedbacks/${feedbackId}/process`, method: 'POST', data: payload })
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

/** T289 8.1：跨患者佩戴感受日志流（契约 getFeelingLogsAdmin，T256 #2 端点，staffOnly） */
export async function fetchFeelingLogsAdmin(params: {
  keyword?: string
  startDate?: string
  endDate?: string
  feeling?: 'fitted' | 'discomfort'
  page?: number
  pageSize?: number
}): Promise<PaginatedResponse<FeelingLog>> {
  if (USE_MOCK) { await delay(); return orthosisMock.mockFeelingLogsAdmin(params) }
  return request<PaginatedResponse<FeelingLog>>({ url: '/api/v1/admin/feeling-logs', data: params as Record<string, unknown> })
}

// T247: 医生回复感受日志（后端已放行，网关 proxy_admin.go:144）
export async function replyFeelingLogApi(logId: string, replyContent: string): Promise<void> {
  if (USE_MOCK) { await delay(); orthosisMock.mockReplyFeelingLog(logId, replyContent); return }
  await request<null>({ url: `/api/v1/feeling-logs/${logId}/reply`, method: 'POST', data: { replyContent } })
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

// T247 / T345：角色权限读写。形状一律用契约类型 RolePermissions（packages/shared-types，
// 对齐后端 model.RolePermissionsDTO = {scope, modules, items}）。
// 本文件此处曾自造 {roleId, permissions: 页面路径[]} 影子接口：后端 ShouldBindJSON 拿不到
// scope 直接 400，且 mock 按这个错误形状自洽实现，故三层 CI 全绿、只有 staging 暴露。
export async function fetchRolePermissionsApi(roleId: string): Promise<RolePermissions> {
  if (USE_MOCK) { await delay(); return systemMock.mockRolePermissions(roleId) }
  return request<RolePermissions>({ url: `/api/v1/admin/roles/${roleId}/permissions` })
}

/** items 传 null = 不细化子权限（后端读时按目录物化为全勾）；[] 才是显式全不勾 */
export async function updateRolePermissionsApi(roleId: string, permissions: RolePermissions): Promise<void> {
  if (USE_MOCK) { await delay(); systemMock.mockUpdateRolePermissions(roleId, permissions); return }
  await request<null>({ url: `/api/v1/admin/roles/${roleId}/permissions`, method: 'PUT', data: permissions as unknown as Record<string, unknown> })
}

// T253-11.2: 角色增删改 + 模板（对齐 T252 契约 api-contracts.ts createAdminRole/updateAdminRole/deleteAdminRole）
export interface RoleTemplateItem {
  key: string
  name: string
  description: string
  permissions: { scope: string; modules: string[] }
}

export async function fetchRoleTemplates(): Promise<RoleTemplateItem[]> {
  if (USE_MOCK) { await delay(); return systemMock.mockRoleTemplates() }
  return request<RoleTemplateItem[]>({ url: '/api/v1/admin/role-templates' })
}

export async function createRoleApi(data: {
  name: string
  description?: string
  template?: string
  permissions?: { scope: string; modules: string[] }
}): Promise<AdminRoleRow> {
  if (USE_MOCK) { await delay(); return systemMock.mockCreateRole(data) }
  return request<AdminRoleRow>({ url: '/api/v1/admin/roles', method: 'POST', data: data as unknown as Record<string, unknown> })
}

export async function updateRoleApi(roleId: string, data: {
  name?: string
  description?: string
  status?: 'enabled' | 'disabled'
}): Promise<AdminRoleRow> {
  if (USE_MOCK) { await delay(); return systemMock.mockUpdateRole(roleId, data) }
  return request<AdminRoleRow>({ url: `/api/v1/admin/roles/${roleId}`, method: 'PUT', data: data as unknown as Record<string, unknown> })
}

export async function deleteRoleApi(roleId: string): Promise<void> {
  if (USE_MOCK) { await delay(); systemMock.mockDeleteRole(roleId); return }
  await request<null>({ url: `/api/v1/admin/roles/${roleId}`, method: 'DELETE' })
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

// ========== 操作日志（T253-12.3，契约 docs/api/api-contracts.ts getAuditLogs，T252 后端） ==========

export async function fetchAuditLogsApi(params: {
  date?: string
  from?: string
  to?: string
  action?: string
  operator?: string
  targetType?: string
  targetId?: string
  page?: number
  pageSize?: number
}): Promise<PaginatedResponse<systemMock.AuditLog>> {
  if (USE_MOCK) { await delay(); return systemMock.mockAuditLogs(params) }
  return request<PaginatedResponse<systemMock.AuditLog>>({ url: '/api/v1/admin/audit-logs', data: params as Record<string, unknown> })
}

// ========== 展示辅助（mock 期姓名映射，真实模式后端 join 返回后可移除） ==========

/**
 * T269 D1：真实模式组织名字典，由 fetchTeams / fetchDoctors 用后端全量列表填充。
 * mock 查表的 ID 命名空间是 TEAM-001/DOC-001，与后端 TEAM01/D0001 不通，
 * 真实模式继续查 mock 表只会回落成原始编号。
 * reactive 存储：字典到货前显示原始 ID，到货后模板自动重渲染。
 */
const orgNames = reactive<{ teams: Record<string, string>; doctors: Record<string, string> }>({
  teams: {},
  doctors: {},
})

export function patientNameOf(patientId: string | null): string {
  if (USE_MOCK) {
    return deviceMock.mockPatientName(patientId)
  }
  // 真实模式：姓名由后端 join 提供至 row.patientName，此处仅作 patientId 兜底
  return patientId || '-'
}

export function teamNameOf(teamId: string | null): string {
  if (USE_MOCK) return orgMock.mockTeamName(teamId)
  if (!teamId) return '-'
  return orgNames.teams[teamId] ?? teamId
}

export function doctorNameOf(doctorId: string | null): string {
  if (USE_MOCK) return orgMock.mockDoctorName(doctorId)
  if (!doctorId) return '-'
  return orgNames.doctors[doctorId] ?? doctorId
}

export function techNameOf(techId: string): string {
  if (USE_MOCK) return orgMock.mockTechName(techId)
  // /technicians 分页返回，无全量字典可建 ⇒ 回落原始编号
  return techId
}

// ========== T130 复查报告（文件上传 + 复查记录） ==========

/** file-service 预签名直传响应（调用方侧口径，camelCase） */
export interface PresignResult {
  fileId: string
  uploadUrl: string
  objectKey: string
  expiresAt: string
}

/**
 * file-service 域的响应 JSON key 是 snake_case（docs/api/api-contracts.ts 现状口径：
 * 改后端属破契约需另立卡），故本层负责把线上形状映射成调用方形状。
 * T622：此前直接按 camelCase 读响应，presign.uploadUrl 恒 undefined。
 */
interface PresignWireResult {
  file_id: string
  object_key: string
  signature_url: string
  expires_at: string
  expires_in_seconds: number
}

/** 申请预签名上传 URL（file-service T022） */
export async function presignFile(params: {
  fileName: string
  contentType: string
  fileType: 'review_report'
  ownerType?: string
  ownerId?: string
  fileHeader?: string // T130 增补单：文件头魔数指纹（base64，前 8 字节）
}): Promise<PresignResult> {
  if (USE_MOCK) {
    await delay()
    // T307：mock 侧登记待传文件，uploadFileDirect 的 mock 分支按 fileId 标记已传，
    // 写记录/模板时据此回填文件名与下载链接（此前这里只造 URL，文件元数据全程丢失）。
    const fileId = reviewMock.mockNextFileId()
    reviewMock.mockRegisterPendingFile({ fileId, fileName: params.fileName, contentType: params.contentType })
    return {
      fileId,
      uploadUrl: `https://mock-cos.example.com/upload/${fileId}`,
      objectKey: `review-reports/${params.fileName}`,
      expiresAt: new Date(Date.now() + 5 * 60 * 1000).toISOString(),
    }
  }
  // 请求体与响应体都是 snake_case（api-contracts.ts presignUpload / completeUpload 条目）
  const wire = await request<PresignWireResult>({
    url: '/api/v1/files/presign',
    method: 'POST',
    data: {
      file_type: params.fileType,
      owner_type: params.ownerType,
      owner_id: params.ownerId,
      content_type: params.contentType,
      file_name: params.fileName,
      file_header: params.fileHeader,
    },
  })
  return {
    fileId: wire.file_id,
    uploadUrl: wire.signature_url,
    objectKey: wire.object_key,
    expiresAt: wire.expires_at,
  }
}

/** 直传文件到 COS（使用预签名 URL，不走网关 request） */
export async function uploadFileDirect(uploadUrl: string, file: File, contentType: string): Promise<void> {
  if (USE_MOCK) {
    // T307 G11：此前 mock 下这里也会对 mock-cos 域名发真 PUT（既不成也不败）。
    // 页面没有进度条 DOM，可观测的只有按钮 loading 与「已上传」tag，
    // 故 mock 侧以分段延时模拟传输耗时后落一次 uploaded 状态，不伪造百分比。
    await reviewMock.mockDirectUpload(uploadUrl, file)
    return
  }
  // 空 URL 会被 fetch 当成相对路径「undefined」，PUT 落到本站 nginx 静态层回 405（T622 症状），
  // 故在这一层就把它报成可读错误。
  if (!uploadUrl) {
    throw new Error('预签名地址缺失（presign 未返回 signature_url）')
  }
  const res = await fetch(uploadUrl, {
    method: 'PUT',
    headers: { 'Content-Type': contentType },
    body: file,
  })
  if (!res.ok) {
    throw new Error(`文件直传失败（HTTP ${res.status}）`)
  }
}

/** 确认上传完成，返回 file_id */
export async function completeUpload(fileId: string): Promise<{ fileId: string }> {
  if (USE_MOCK) {
    await delay()
    return { fileId }
  }
  // 后端绑定的是 file_id（binding required），响应也只有 file_id
  await request<{ file_id: string }>({
    url: '/api/v1/files/upload-complete',
    method: 'POST',
    data: { file_id: fileId },
  })
  return { fileId }
}

/** 创建复查记录（医生/管理员） */
export async function createReviewRecordApi(input: CreateReviewRecordRequest): Promise<ReviewRecord> {
  if (USE_MOCK) {
    await delay()
    return reviewMock.mockCreateReviewRecord(input)
  }
  return request<ReviewRecord>({
    url: '/api/v1/admin/review-records',
    method: 'POST',
    data: input as unknown as Record<string, unknown>,
  })
}

/** 查询某患者的复查记录列表（admin 可查任意患者） */
export async function fetchReviewRecords(patientId: string): Promise<ReviewRecord[]> {
  if (USE_MOCK) {
    await delay()
    return reviewMock.mockReviewRecords(patientId)
  }
  return request<ReviewRecord[]>({ url: `/api/v1/patients/${patientId}/review-records` })
}

// ========== T135 复查报告模板（合同运营后台「复查报告模板管理」） ==========

/** 模板列表（每模板组当前 active 版本，含下载 URL；admin/doctor） */
export async function fetchReviewTemplates(): Promise<ReviewTemplate[]> {
  if (USE_MOCK) {
    await delay()
    return reviewMock.mockReviewTemplates()
  }
  return request<ReviewTemplate[]>({ url: '/api/v1/admin/review-templates' })
}

/** 上传/创建模板（版本 v1；重名 409；admin/doctor） */
export async function createReviewTemplateApi(input: CreateReviewTemplateRequest): Promise<ReviewTemplate> {
  if (USE_MOCK) {
    await delay()
    return reviewMock.mockCreateReviewTemplate(input)
  }
  return request<ReviewTemplate>({
    url: '/api/v1/admin/review-templates',
    method: 'POST',
    data: input as unknown as Record<string, unknown>,
  })
}

/** 模板版本替换（新版本 active、旧版 retired；admin/doctor） */
export async function replaceReviewTemplateApi(groupId: string, fileId: string): Promise<ReviewTemplate> {
  if (USE_MOCK) {
    await delay()
    return reviewMock.mockReplaceReviewTemplate(groupId, fileId)
  }
  return request<ReviewTemplate>({
    url: `/api/v1/admin/review-templates/${groupId}/replace`,
    method: 'POST',
    data: { fileId },
  })
}

/** 模板下载（组当前 active 版本文件） */
export async function downloadReviewTemplateApi(groupId: string): Promise<{ downloadUrl: string }> {
  if (USE_MOCK) {
    await delay()
    return { downloadUrl: '' }
  }
  return request<{ downloadUrl: string }>({ url: `/api/v1/admin/review-templates/${groupId}/download` })
}
