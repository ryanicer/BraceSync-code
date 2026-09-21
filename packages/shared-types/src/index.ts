// ===== 核心实体 =====

export interface Patient {
  patientId: string;
  name: string;
  gender: 'male' | 'female' | null;   // pending 患者可能未填
  age: number | null;
  diagnosis: string | null;
  cobbAngle: number | null;
  deviceId: string | null;
  teamId: string | null;
  doctorId: string | null;       // 主治医生（primary_doctor_id）
  status: 'active' | 'pending';  // 活跃/待分配（对齐 DB patients.status）
  createdAt: string;
  updatedAt: string;
}

export interface Doctor {
  doctorId: string;
  name: string;
  title: string;
  department: string;
  teamId: string | null;         // DB doctors.team_id 可空
  phoneMasked: string;           // 展示脱敏（与 Technician 一致），联系走微信客服
  patientCount: number;
  status: 'enabled' | 'disabled';
}

export interface Technician {
  techId: string;
  name: string;
  phoneMasked: string;           // 展示脱敏（138****5678）
  teamId: string;
  installCount: number;
  status: 'enabled' | 'disabled';
  authStatus: 'authorized' | 'unauthorized';  // 对齐 DB technicians.auth_status
  createdAt?: string;            // T247 10.4: 创建时间（设计稿 技师管理.html:102）
}

export interface Device {
  deviceId: string;
  model: 'PRS-ML05-RC';
  firmwareVersion: string;
  patientId: string | null;
  /** 绑定患者姓名（T030：GET /api/v1/devices 后端 join 返回；未绑定为 null） */
  patientName?: string | null;
  wifiSsid: string | null;
  bindTime: string | null;
  status: 'online' | 'offline' | 'abnormal' | 'unbound';  // 对齐 DB 状态机
  lastReportAt: string | null;
}

export interface Team {
  teamId: string;
  name: string;
  memberCount: number;
  patientCount: number;
  /** T059 团队管理写功能（GET /teams 扩展返回，可选字段对齐既有读路径） */
  leader?: string | null;         // 负责人 doctorId
  leaderName?: string | null;     // 负责人姓名（后端 join）
  description?: string | null;    // 团队描述
  status?: 'active' | 'deleted';  // 状态
  createdAt?: string;            // 创建时间
}

/** 团队详情（T059 写端点 POST/PUT /teams 返回，对齐后端 TeamDetailDTO） */
export interface TeamDetail extends Team {
  leader: string | null;
  leaderName: string | null;
  description: string | null;
  status: 'active' | 'deleted';
  createdAt: string;
}

/** 团队管理统计卡（T256 #1：GET /admin/teams/stats，4 个计数）
 *  字段名对齐后端 model.TeamStatsDTO 的 json tag（services/user-service/internal/model/model.go:205-210） */
export interface TeamStats {
  teamCount: number;        // 团队总数
  memberCount: number;      // 成员总数
  managedPatientCount: number;    // 管理患者数（已分配团队的患者）
  unassignedPatientCount: number; // 待分配患者数（未分配团队）
}

/** 团队成员（T059 成员管理，对齐后端 TeamMemberDTO） */
export interface TeamMember {
  memberId: string;
  memberType: 'doctor' | 'technician';
  name: string;
  role: string | null;           // 角色（doctor.title 更新值，technician 无则 null）
  title: string | null;          // 职称/科室
  phoneMasked: string;
  patientCount: number;
  joinTime: string;
  status: 'enabled' | 'disabled';
}

// ===== 业务实体 =====

export interface SensorPoint {
  pointId: string;      // P01–P20（与 DB p01-p20 / alerts.sensor_point 一致）
  row: number;          // 1–4
  col: number;          // 1–5
  label: string;        // e.g. "R3C2"
  pressureValue: number;
  status: 'normal' | 'warning' | 'critical';
}

/** 设备配置（采集间隔等），随设备上报响应下发（设备协议 §4.1） */
export interface DeviceConfig {
  intervalMinutes: number;        // 采集间隔
  configVersion: number;          // 对齐 sys_configs 的 device_config_version，设备比对不一致则应用
}

/** 压力记录帧实体（对齐 data-service PressureRecordDTO；api-contracts.ts getPatientHistory / getPatientRealtime 公开使用） */
export interface PressureRecord {
  recordId: string;
  deviceId: string;
  patientId: string;
  timestamp: string;
  points: SensorPoint[];
  uploadTime: string;
  /** T173：是否已应用基线校准（减偏移）。false = 设备无基线，读数为 raw 值 */
  calibrated?: boolean;
}

export interface Alert {
  alertId: string;
  patientId: string;
  patientName?: string | null;
  deviceId: string;
  type: AlertType;
  detail: string;
  sensorPoint: string;
  thresholdValue: number;
  actualValue: number;
  timestamp: string;
  readStatus: 'read' | 'unread';                  // 患者侧
  processStatus: 'pending' | 'processing' | 'processed'; // 处理侧（T257 2.7 三态）
  resolvedStatus: 'active' | 'resolved';   // 恢复态（佩戴中断设备恢复后自动 resolved）
  resolvedAt: string | null;
  /** T257 2.7：进入「处理中」的时刻；可选 + null 均表示从未进入过处理中（含三态上线前的历史行） */
  inProgressAt?: string | null;
  processedBy: string | null;
  processedAt: string | null;
  processNote: string | null;
}

/** T289 9.3：设计稿「校准」列口径（api-contracts.ts getInstallRecords，T248 9.3） */
export type CalibStatus = 'uncalibrated' | 'normal' | 'abnormal';

export interface InstallRecord {
  installId: string;
  deviceId: string;
  patientId: string;
  techId: string;
  /** 患者姓名（T030：GET /api/v1/install-records 后端 join 返回） */
  patientName?: string | null;
  /** 技师姓名（T030：同上） */
  techName?: string | null;
  calibrateTime: string;
  baselineId: string | null;     // 引用 Baseline（单一数据源）
  notes: string;
  signatureUrl: string;
  wifiStatus: 'connected' | 'unconfigured';  // 对齐 DB install_records.wifi_status
}

/**
 * 管理端安装记录行（契约 InstallRecord & { calibStatus }）。
 * 单独成类型而不是往 InstallRecord 上加必填字段：技师小程序复用 InstallRecord 造 seed，
 * 加必填字段会牵动端外改动（见 api-contracts.ts:423 的同款写法）。
 */
export type InstallRecordRow = InstallRecord & {
  /** 校准状态（T289 9.3）：后端由 baseline + 20 点偏移派生，异常判定阈值走配置不在前端硬编 */
  calibStatus: CalibStatus;
};

/** T289 9.1/9.2：GET /api/v1/install-records/:id（契约 getInstallDetail） */
export interface InstallRecordDetail extends InstallRecordRow {
  /** 基线 20 点偏移值；未校准为 []（非 null，前端可直接 map） */
  offsetValues: number[];
  /** 建档时间（设计稿 安装记录.html:181「安装时间」，列表 DTO 无该字段） */
  createdAt: string;
}

export interface Baseline {
  baselineId: string;
  installId: string;
  deviceId: string;
  offsetValues: number[];
  calibratorId: string;
  createdAt: string;             // 对齐 DB baselines.created_at
}

export interface FeelingLog {
  logId: string;
  patientId: string;
  logDate: string;               // 对齐 DB feeling_logs.log_date（YYYY-MM-DD）
  comfortScore: number | null;   // 0.5–5（可半星）；T256 #3 历史星级口径，写入口径以 feeling 为准
  feeling: 'fitted' | 'discomfort' | null; // T256 #3：贴合(fitted)/不适(discomfort)两档，来自 comfort_level 列
  discomfortAreas: string[];     // neck/thoracic/lumbar/pelvis
  notes: string;
  replyContent: string | null;   // 医生回复
  replyTime: string | null;
}

export interface OrthosisPlan {
  planId: string;
  patientId: string;
  doctorId: string;
  content: string;
  version: string;               // v{主}.{次}（对齐 DB varchar）
  createdAt: string;
}

export interface Feedback {
  feedbackId: string;
  patientId: string;
  type: string;
  content: string;
  submitTime: string;
  handler: string | null;
  replyContent: string | null;   // 医生/客服回复
  replyTime: string | null;
  status: 'pending' | 'replied' | 'resolved';
}

export interface HealthReport {
  reportId: string;
  patientId: string;
  reportType: 'weekly' | 'monthly';
  periodStart: string;
  periodEnd: string;
  wearComplianceRate: number;
  avgPressure: number;
  trendJudgment: 'up' | 'flat' | 'down';
  suggestion: string;
  generateTime: string;
}

// ===== 复查记录域（T130，合同患者端「复查管理」） =====

/** 复查记录（含报告文件信息 + 下载 URL） */
export interface ReviewRecord {
  reviewId: string;
  patientId: string;
  reviewDate: string;        // YYYY-MM-DD
  reviewType: 'initial' | 'follow-up';
  findings: string | null;
  nextReviewDate: string | null; // YYYY-MM-DD
  doctorId: string | null;
  reportFileId: string | null;
  // 报告文件元数据（由 file-service 提供，可能为空）
  reportFileName: string | null;
  reportContentType: string | null;
  reportSize: number | null;
  reportUploadedAt: string | null;
  reportDownloadUrl: string | null;
  createdAt: string;
  updatedAt: string;
}

/** 创建复查记录请求体（admin-web 提交） */
export interface CreateReviewRecordRequest {
  patientId: string;
  reviewDate: string;               // YYYY-MM-DD
  reviewType: 'initial' | 'follow-up';
  findings?: string;
  nextReviewDate?: string;          // YYYY-MM-DD（可空）
  doctorId?: string;
  reportFileId?: string;            // file-service 上传完成后返回的 file_id
}

// ===== T135 复查报告模板（合同运营后台「复查报告模板管理」） =====

/** 复查报告模板（列表条目 = 每模板组当前 active 版本） */
export interface ReviewTemplate {
  templateId: string;
  groupId: string;
  name: string;
  version: number;
  fileId: string;
  status: 'active' | 'retired';
  uploadedBy: string;
  uploadedAt: string;               // YYYY-MM-DD（页面要求）
  updatedAt: string;
  // 文件元数据（file-service 提供，可能为空）
  fileName: string | null;
  contentType: string | null;
  fileSize: number | null;
  downloadUrl: string | null;       // 预签名 GET URL（5min）
}

/** 上传/创建复查报告模板请求体（admin-web 提交） */
export interface CreateReviewTemplateRequest {
  name: string;                     // 模板显示名
  fileId: string;                   // file-service 上传完成后返回的 file_id
}

/** 模板版本替换请求体（确认后生效，旧版 retired） */
export interface ReplaceReviewTemplateRequest {
  fileId: string;
}

export interface PatientPreference {
  patientId: string;
  reminderEnabled: boolean;
  reminderTime: string | null;   // HH:mm（业务时区）
  subscriptionAuthStatus: 'authorized' | 'rejected' | 'closed';
  subscriptionQuota: number;     // 订阅授权剩余额度（DB patient_preferences.subscription_quota，默认 3）
}

// ===== 消息 / 通知 / 额度域（msg-service，对齐架构 §2.5/§3.3/§7D.6） =====

/** 告警类型枚举（对齐 DB alerts.type + alert_notify_rules.type）
 *  T257 2.6：新增 wear_duration_short（佩戴时长不足）；pressure_fluctuation（压力波动）
 *  自本卡起引擎不再产生，**保留成员**用于渲染历史告警行，不得当作可产生类型使用。 */
export type AlertType =
  | 'pressure_high'
  | 'wear_interrupt'
  | 'pressure_fluctuation'
  | 'sensor_drift'
  | 'wear_duration_short';

/** 通知渠道 */
export type NotifyChannel = 'wechat' | 'sms';

/** 通知目标角色 */
export type NotifyTarget = 'patient' | 'doctor' | 'tech' | 'ops';

/** 告警通知规则（对齐 DB alert_notify_rules，owner: alert-service） */
export interface NotifyRule {
  type: AlertType;
  channels: NotifyChannel[];
  notifyTargets: NotifyTarget[];
  updatedBy?: string;
  updatedAt?: string;
}

/** 订阅授权额度快照（患者端 T016 查询用） */
export interface SubscriptionQuota {
  patientId: string;
  remaining: number;              // 剩余可用次数
  total: number;                  // 总额度（默认 3）
  isLow: boolean;                 // 低额度警告（≤1，需引导患者重新授权；架构 §2.5）
  updatedAt: string | null;       // 最近一次额度变更时间（映射 DB patient_preferences.updated_at，不新增列）
}

/** 佩戴提醒设置（对齐 DB patient_preferences.reminder_*，患者端 T016 读写） */
export interface WearReminderSettings {
  reminderEnabled: boolean;
  reminderTime: string | null;    // HH:mm（Asia/Shanghai 业务时区）
}

/** 通知发送记录（msg-service 发送历史，患者端与管理后台可查） */
export interface NotificationRecord {
  recordId: string;
  patientId: string;
  alertId?: string;               // 关联告警（非告警通知如佩戴提醒则为空）
  alertType?: AlertType;
  channel: NotifyChannel;
  status: 'pending' | 'sent' | 'failed' | 'degraded';  // degraded=额度耗尽降级短信
  content: string;                // 推送内容文本
  retryCount: number;             // 重试次数
  sentAt: string | null;          // ISO 8601，实际发送时间
  createdAt: string;              // 创建时间
}

// ===== 统计 / 看板 =====

export interface DashboardKPI {
  totalPatients: number;
  todayActiveWear: number;
  todayAlerts: number;
  avgWearHours: number;
  deviceOnlineRate: number;
  monthNewPatients: number;
}

export interface TeamRanking {
  rank: number;
  teamName: string;
  patientCount: number;
  avgDailyWear: number;
  complianceRate: number;
}

export interface DoctorRanking {
  rank: number;
  doctorName: string;
  teamName: string;
  patientCount: number;
  complianceRate: number;
}

// ===== 运营后台域（T030：admin 查询端点契约类型） =====

/** 管理端患者视图（Patient + 团队/医生姓名 join，user-service GET /api/v1/admin/patients） */
export interface AdminPatient extends Patient {
  teamName: string | null;    // teams.name join（无团队为 null）
  doctorName: string | null;  // doctors.name join（无主治为 null）
}

/** RBAC 角色行（对齐 DB roles + admins 计数，user-service GET /api/v1/admin/roles） */
export interface AdminRole {
  roleId: string;
  name: string;
  description: string;
  memberCount: number;         // admins 表该角色账号数
  createdAt: string;
  status: 'enabled' | 'disabled';
  preset: boolean;             // 预置角色（ROLE_ADMIN/ROLE_DOCTOR/ROLE_CS，权限系统锁定）
}

/** 角色权限矩阵（对齐 DB roles.permissions_json，PRD §7D.11） */
export interface RolePermissions {
  scope: 'all' | 'team' | 'all_patients';  // 数据范围（架构 §3.3 RBAC）
  modules: string[];                        // 可访问模块清单
  /**
   * T257 11.5 子权限（设计稿 权限控制.html:121-187 组内勾选项），key 形如 `alerts.process`。
   * - GET 恒回数组：库里未细化（老角色 / seed 预置三个）时后端按目录物化为「modules 下全部子权限」；
   * - PUT 省略 / null = 不细化（保持全勾语义）；`[]` = 显式全不勾（与省略**不同义**）；
   * - 写入校验：key 必须在 GET /admin/permissions/catalog 目录内，且其模块已在 modules 里。
   * 🔴 PM 裁定 Q2=(c)：**只用于前端隐藏菜单/按钮，不参与后端鉴权**——网关与服务层仍只判角色级，
   * 少勾一个 item 不会让任何接口返 403。
   */
  items?: string[];
}

/** 子权限条目（T257 11.5） */
export interface PermissionItem {
  key: string;      // 「模块.动作」，如 comm.reply
  label: string;    // 设计稿中文标签，直接渲染
}

/** 子权限分组 = 一个后台页面（T257 11.5，设计稿 .perm-group） */
export interface PermissionGroup {
  module: string;   // 与 RolePermissions.modules 同一套词表
  label: string;
  items: PermissionItem[];
}

/** GET /api/v1/admin/permissions/catalog —— 全量子权限目录（9 组 23 项，admin 专属） */
export interface PermissionCatalog {
  groups: PermissionGroup[];
}

/**
 * GET /api/v1/admin/me/permissions —— 当前登录人的有效权限（全 staff 可读，患者 403）。
 * 前端据此渲染菜单/按钮，不用再按 roleId 硬编码；身份取网关注入的 X-User-Id / X-Role。
 * 角色不在 roles 表（technician / patient / 已删角色）⇒ 200 + 空清单（不是 404）。
 */
export interface MyPermissions {
  adminId: string;
  roleId: string;
  scope: string;      // 数据范围；角色未落库时为空串
  modules: string[];
  items: string[];    // 已按目录物化，直接渲染
}

/** 团队成员明细（T030 #10：getTeams 概要之外的成员清单，一期只读） */
export interface TeamMembers {
  doctors: Doctor[];
  technicians: Technician[];
}

/** WiFi 预设条目（sys_configs.wifi_presets JSON 数组元素） */
export interface WifiPreset {
  ssid: string;
  password?: string;           // GET 返回时非空密码脱敏为 ********
}

/** 系统参数（PRD §7D.12，sys_configs KV 映射；user-service GET/PUT /api/v1/admin/settings） */
export interface SystemSettings {
  dailyWearTargetHours: number;      // wear_target_hours
  pressureHighThresholdN: number;    // threshold_pressure_high（设计稿 系统配置.html:98「偏高上限」）
  /**
   * T257 12.4（三档合两键，PM 裁定 Q3）：设计稿「低压上限」≡ 告警管理页 Tab2「统一压力下限」
   * ≡ sys_configs threshold_pressure_low —— 同一个键，两个页面读写同一份值。
   * GET 恒回数值；PUT 省略 / null = 不改该键（沿用库里现值）。
   * 🔴 设计稿第三档「正常上限（绿/黄分界）」**后端不落库**：两键只承载 低压/偏高 两条边界，
   * 热力图中间档由前端派生或另议（T257 交件说明已登记待裁）。
   */
  pressureLowThresholdN?: number | null;
  pressureFluctuationPct: number;    // threshold_pressure_fluctuation_pct（T257 2.6：引擎已停产生该类型告警，键仍可配）
  wearInterruptMinutes: number;      // threshold_wear_interrupt_minutes（≥2×采集间隔；≡ 告警页 deviceOfflineMinutes）
  sensorDriftN: number;              // threshold_sensor_drift
  wifiPresets: WifiPreset[];         // wifi_presets
  collectIntervalSeconds: number;    // T256 #4：采集间隔（秒，设计稿口径；内部存储 collect_interval_minutes）
  retentionDays: number;             // T256 #4：数据保留天数（data_retention_days）
  maxPatients: number;               // T256 #4：最大患者数（max_patients）
}

/** 运营后台登录响应（T030 #9：user-service 签发，gateway Phase 1 JWT 校验消费） */
export interface AdminLoginResult {
  token: string;               // HS256 JWT（JWT_SECRET 与 gateway 共享）
  adminId: string;
  username: string;
  name: string;
  roleId: string;              // ROLE_ADMIN / ROLE_DOCTOR / ROLE_CS
  scope: string;               // 数据范围（对齐 roles.permissions_json.scope）
}

/** 技师登录响应（T037：user-service 签发，gateway JWT 白名单放行登录接口） */
export interface TechLoginResult {
  token: string;               // HS256 JWT（载荷 sub=techId, role=technician, team_id）
  techId: string;
  name: string;
  teamId: string;              // 可空（未分配团队的技师）
  role: string;                // 固定 "technician"
}

/** 患者登录响应（T037：user-service 签发，gateway JWT 白名单放行登录接口） */
export interface PatientLoginResult {
  token: string;               // HS256 JWT（载荷 sub=patientId, role=patient）
  patientId: string;
  name: string;
  role: string;                // 固定 "patient"
}

// ===== API 通用 =====

export interface ApiResponse<T> {
  code: number;
  message: string;
  data: T;
}

export interface PaginatedResponse<T> {
  list: T[];
  total: number;
  page: number;
  pageSize: number;
}
