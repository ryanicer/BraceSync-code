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
}

export interface Alert {
  alertId: string;
  patientId: string;
  patientName?: string | null;
  deviceId: string;
  type: 'pressure_high' | 'wear_interrupt' | 'pressure_fluctuation' | 'sensor_drift';
  detail: string;
  sensorPoint: string;
  thresholdValue: number;
  actualValue: number;
  timestamp: string;
  readStatus: 'read' | 'unread';           // 患者侧
  processStatus: 'pending' | 'processed';  // 处理侧
  resolvedStatus: 'active' | 'resolved';   // 恢复态（佩戴中断设备恢复后自动 resolved）
  resolvedAt: string | null;
  processedBy: string | null;
  processedAt: string | null;
  processNote: string | null;
}

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
  comfortScore: number;          // 0.5–5（可半星）
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

export interface PatientPreference {
  patientId: string;
  reminderEnabled: boolean;
  reminderTime: string | null;   // HH:mm（业务时区）
  subscriptionAuthStatus: 'authorized' | 'rejected' | 'closed';
  subscriptionQuota: number;     // 订阅授权剩余额度（DB patient_preferences.subscription_quota，默认 3）
}

// ===== 消息 / 通知 / 额度域（msg-service，对齐架构 §2.5/§3.3/§7D.6） =====

/** 告警类型枚举（对齐 DB alerts.type + alert_notify_rules.type） */
export type AlertType = 'pressure_high' | 'wear_interrupt' | 'pressure_fluctuation' | 'sensor_drift';

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
  pressureHighThresholdN: number;    // threshold_pressure_high
  pressureFluctuationPct: number;    // threshold_pressure_fluctuation_pct
  wearInterruptMinutes: number;      // threshold_wear_interrupt_minutes（≥2×采集间隔）
  sensorDriftN: number;              // threshold_sensor_drift
  wifiPresets: WifiPreset[];         // wifi_presets
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
