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
  /**
   * T337 契约补账：以下 6 项后端 PatientDTO 一直在回（GET /admin/patients 列表与详情同口径），
   * 此前只有前端摸样用、契约没写。声明为可选只为不打破既有 mock，实际读到的行都带这些键。
   * phone 是脱敏串（138****8000），库里无手机号时为空串，永不是明文。
   * T361/T491 登记：患者域没有三态字段，也没有「编辑手机号」入口，列表与详情的 phone 恒为空串。
   * T491 订正旧理由：读侧 repo/pg.go 的 patientSelect 现已带回 phone_enc（只供服务内脱敏取数，如改号审计的改前快照），
   * 但 toPatientDTO 不映射这一列，所以两个只读响应仍不回手机号，密文与脱敏号都不出接口。
   * 只有 POST /admin/patients 与 PUT /admin/patients/:id/team 两个写响应用当场生成的密文回填。声明保留为可选，别按它有值来写页面。
   */
  phone?: string;                     // 脱敏手机号
  heightCm?: number | null;           // T226 患者自助资料
  weightKg?: number | null;           // T226
  emergencyContactName?: string | null;    // T226
  emergencyContactPhone?: string | null;   // T226
  emergencyContactRelation?: string | null; // T226
}

/**
 * T361 手机号读侧三态（后端 services/user-service/internal/phone.PhoneState 同值）。
 *
 * 改造前接口只回 phoneMasked 一个字符串，「库里没有手机号」= 空串、「有密文但解不开」= "***"，
 * 调用方无法分辨，于是医护账号页把脱敏串预填进可编辑输入框，运营清空保存即把真号洗成 NULL。
 *  - absent：密文列 NULL，确实没有手机号
 *  - masked：密文可解密，phoneMasked 是脱敏号
 *  - unreadable：有密文但解不开（seed 占位 bytea / 密钥轮换 / 数据损坏），或密钥未配置
 *    ⇒ phoneMasked 固定为占位符 '***'（后端常量 phone.MaskUnavailable），编辑态必须禁止把它当可编辑的原值回传
 */
export type PhoneState = 'absent' | 'masked' | 'unreadable';

export interface Doctor {
  doctorId: string;
  name: string;
  title: string;
  department: string;
  teamId: string | null;         // DB doctors.team_id 可空
  phoneMasked: string;           // 展示脱敏（与 Technician 一致），联系走微信客服
  phoneState: PhoneState;        // T361：与 phoneMasked 配套，区分「没有」与「读不出」
  patientCount: number;          // T371 口径：主诊患者数（patients.primary_doctor_id 计数，不带团队维度）
  status: 'enabled' | 'disabled';
  // T314 医护账号页并进的 admins 侧三列（GET /api/v1/doctors 同一行返回）。
  // 键恒在（Go 侧无 omitempty），值可 null = 该档案未绑登录账号（seed D0002/D0003）⇒ 前端渲染破折号。
  // T418 归口：原先只在 admin-web 本地 DoctorWithAccount 声明，契约漏账被对拍门禁 ignore 三行。
  // accountStatus 是登录能力层（admins.status），与上面的 status（档案层）分列展示，别合并语义。
  username?: string | null;
  accountStatus?: string | null;
  createdAt?: string | null;
}

export interface Technician {
  techId: string;
  name: string;
  phoneMasked: string;           // 展示脱敏（138****5678）
  phoneState: PhoneState;        // T361
  teamId: string;
  installCount: number;
  status: 'enabled' | 'disabled';
  authStatus: 'authorized' | 'unauthorized';  // 对齐 DB technicians.auth_status
  createdAt?: string;            // T247 10.4: 创建时间（设计稿 技师管理.html:102）
  teamName?: string | null;      // T337 契约补账：T278-② 起后端 join 带出（未入队为 null，前端回落显示 teamId）
}

export interface Device {
  deviceId: string;
  model: 'PRS-ML05-RC';
  firmwareVersion: string;
  patientId: string | null;
  /** 绑定患者姓名（T030：GET /api/v1/devices 后端 join 返回；未绑定为 null） */
  patientName?: string | null;
  wifiSsid: string | null;
  /** 压力点有效受压面积 cm²（T508 / PRD §7A.2.1，后台设备管理配置）。
   *  null = 未配置 ⇒ kPa 档 fail-closed 显示「--」；🔴 前端不得用 0.64 之类常量补位
   *  （0.64 是配置项默认值，不是数据），也不得反推自整片 40mm×50mm 外形尺寸。 */
  contactAreaCm2: number | null;
  bindTime: string | null;
  status: 'online' | 'offline' | 'abnormal' | 'unbound';  // 对齐 DB 状态机
  lastReportAt: string | null;
}

export interface Team {
  teamId: string;
  name: string;
  memberCount: number;         // T385 口径：本团队成员数（按 doctors.team_id / technicians.team_id 实时计数，非 teams.member_count 维护列）；T429 起只数 status=enabled 的在职成员
  patientCount: number;          // T371 口径：本团队患者数（按 patients.team_id 实时计数，非 teams.patient_count 快照列）
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
 *  字段名对齐后端 model.TeamStatsDTO 的 json tag（services/user-service/internal/model/model.go，TeamStatsDTO 定义处） */
export interface TeamStats {
  teamCount: number;        // 团队总数
  memberCount: number;      // 成员总数（医生 + 技师，T385 口径：按 team_id 实时计数后按团队求和，非 teams.member_count 汇总；T429 口径：禁用账号不计入）
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
  phoneState: PhoneState;        // T361
  patientCount: number;          // T371 口径：该成员的主诊患者数（primary_doctor_id，不按团队收窄）
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
  /**
   * T366：库侧生成列 max_pressure 的**原始**值（= raw p01..p20 取最大，不做校准）。
   * 与 points[].pressureValue 不同层：后者是校准后的值，日聚合判的是本列。
   * 缺省 = 服务端未透出（旧版本镜像），不要当 0 用。
   */
  maxPressure?: number;
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
  resolvedStatus: 'active' | 'resolved';   // 恢复态（设备离线后恢复上报即自动 resolved；T419 G-6 显示口径，码值 wear_interrupt 不动）
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
  /** 患者姓名（T030：列表 join；T418 起详情接口 GetInstall 也 join 带出。关联行缺失为 null） */
  patientName?: string | null;
  /** 技师姓名（同上） */
  techName?: string | null;
  calibrateTime: string;
  baselineId: string | null;     // 引用 Baseline（单一数据源）
  notes: string;
  signatureUrl: string;
  /**
   * 对齐 DB install_records.wifi_status（迁移 000031 起为四值，Boss 2026-09-28 裁定②甲）。
   * 值集与后端 model.WifiStatus* / ValidWifiStatus 同一集合，由
   * services/device-service/internal/model/wifi_status_t447_test.go 比对字面值钉住。
   * 本类型只是「库里能存什么」；页面上显示哪个词由展示层决定，不归本类型（技师端 installStatus.ts）。
   */
  wifiStatus: 'connected' | 'unconfigured' | 'failed' | 'skipped';
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
  /**
   * 设备型号（设计稿 安装记录.html:177「设备型号」行 · T312 I-3 登记项）。
   * T418：详情接口 LEFT JOIN devices 带出；设备行缺失为 null（列表页 deviceListDTO 早有该列，
   * 详情此前没有 ⇒ 页面当时只能「不为此多拉一次设备列表」而放弃这一行）。
   */
  model?: string | null;
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
  discomfortAreas: string[];     // T370：现行取值 = 设计稿 feelings.html:104-111 的 8 区中文原词（写侧白名单同源）；
                                 // 旧四区口径 neck/thoracic/lumbar/pelvis 已作废（PRD §7A.7、§8.2），
                                 // 但历史行仍存这些英文码，展示层译名见 @bracesync/shared-utils 的 areaLabel（T505 上收，两端共用）
  notes: string;
  replyContent: string | null;   // 医生回复
  replyTime: string | null;
  /**
   * T289 8.1（T278 遗留项 L2）：跨患者流 GET /api/v1/admin/feeling-logs 由 patients.name join 带出；
   * 单患者端点不 join ⇒ 恒 null，前端回落 patientId。
   */
  patientName?: string | null;
  /**
   * 设计稿 矫形日志.html:127「提交时间」列。DB feeling_logs.created_at 有列，
   * 但 model.FeelingLogDTO 未下发 ⇒ 待后端补字段，缺失时前端显示占位而非造假时间。
   */
  createdAt?: string | null;
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
  patientName?: string | null;   // T337 契约补账：T278-③ 起后端 join 带出，患者行缺失为 null（前端回落 patientId）
}

// ===== 统计 / 看板 =====

export interface DashboardKPI {
  totalPatients: number;
  todayActiveWear: number;
  todayAlerts: number;
  avgWearHours: number;
  deviceOnlineRate: number;
  monthNewPatients: number;

  // T248 1.1 对比基准（PRD §7D.1「对比基准」列）。T418 归口：后端 09xx 起就随 KPI 同行返回，
  // 契约一直没登记，被对拍门禁 ignore 十二行。键恒在（Go 无 omitempty），值可 null：
  //  - prev* = 紧邻当前窗口的等长前窗原值（today→昨日、week→前一 7 日、month→前一 30 日）
  //  - *ChangePct = 相对前窗的变化百分比；avgWearHoursDelta = 绝对差（小时）
  //  - 前窗为 0 或无基准 ⇒ null（不以 0 冒充「持平」）
  // 🔴 deviceOnlineRate 无历史在线率表，prevDeviceOnlineRate / deviceOnlineRateDelta 恒 null
  //   （补齐需按日快照 = schema 变更，另卡）。
  prevTotalPatients?: number | null;
  prevTodayActiveWear?: number | null;
  prevTodayAlerts?: number | null;
  prevAvgWearHours?: number | null;
  prevDeviceOnlineRate?: number | null;
  prevMonthNewPatients?: number | null;
  activeWearChangePct?: number | null;
  alertsChangePct?: number | null;
  avgWearHoursDelta?: number | null;
  deviceOnlineRateDelta?: number | null;      // 恒 null，见上
  totalPatientsChangePct?: number | null;
  monthNewPatientsChangePct?: number | null;
}

export interface TeamRanking {
  rank: number;
  teamName: string;
  patientCount: number;          // T371 口径：按 patients.team_id 计数（与团队管理页同口径）
  avgDailyWear: number;
  complianceRate: number;
}

export interface DoctorRanking {
  rank: number;
  doctorName: string;
  teamName: string;
  patientCount: number;          // T371 口径：按 primary_doctor_id 计数；医护视角再与本团队患者取交集（T350）
  complianceRate: number;
}

// ===== 运营后台域（T030：admin 查询端点契约类型） =====

/** 管理端患者视图（Patient + 团队/医生姓名 join，user-service GET /api/v1/admin/patients） */
export interface AdminPatient extends Patient {
  teamName: string | null;    // teams.name join（无团队为 null）
  doctorName: string | null;  // doctors.name join（无主治为 null）
}

/**
 * 患者本人视角的档案载荷（T646，依 T637 设计稿 §八）。
 * 对齐后端 `model.PatientSelfDTO`：GET /api/v1/patient/profile 与 PUT /api/v1/patients/{patientId}
 * 两条患者侧载荷面共用这一张名册。
 *
 * 🔴 故意**不** extends Patient，也不复用 AdminPatient：
 *   · AdminPatient 带着 teamName / doctorName 两枚 join 键 —— 那正是本卡要从患者端收口的泄漏面；
 *   · Patient 带着 T337 补账的 teamName?，extends 回来就等于把它写进患者侧契约；
 *   · 契约对拍门（scripts/contract/go-json-tag-audit.mjs）会把 extends 展平后与 Go 的 json tag 比键集合，
 *     所以这里必须平铺声明，让「字段名集合」这件事在两侧同时可见。
 * 键恒在（Go 侧无 omitempty）、值可 null；与后台那两枚姓名键的差集由 T646 的结构面用例钉住。
 */
export interface PatientSelfProfile {
  patientId: string;
  name: string;
  gender: 'male' | 'female' | null;
  age: number | null;
  diagnosis: string | null;
  cobbAngle: number | null;
  deviceId: string | null;      // devices.patient_id 只读关联（T151 方案 1）
  teamId: string | null;        // 标识不是姓名：患者端「已绑定 / 未绑定」靠它
  doctorId: string | null;      // 同上
  phone: string;                // 患者侧两条读路都不映射，wire 上恒为 ""（T491 口径）
  status: 'active' | 'pending';
  createdAt: string;
  updatedAt: string;
  heightCm: number | null;           // T226 患者自助资料
  weightKg: number | null;           // T226
  emergencyContactName: string | null;    // T226
  emergencyContactPhone: string | null;   // T226
  emergencyContactRelation: string | null; // T226
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
   * - PUT **省略键** = 子权限维度不动，保住库里原值（T423，此前省略被升格成未细化）；
   *   PUT 显式 null = 落未细化（保持全勾语义）；`[]` = 显式全不勾（与省略、null **都不同义**）；
   * - PUT 给清单时是**全量语义，增量式调用方禁用**（T413 裁定一）：清单被当成完整答案，
   *   只报一部分勾选项等于取消其余项；
   * - PUT 非省略时**不是整体替换**（T413）：后端按「相对库里新增了哪些模块」并入那些模块的全部
   *   子权限（加权方向不丢授权），既有模块的清单原样尊重（故意漏勾的键不复活）；
   *   库里原本未细化且补齐后正好是全集 ⇒ 仍写 null，不被一次保存洗成显式清单；
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
  /**
   * T337 契约补账：T302 补的三项配置，后端 SystemSettingsDTO 一直没有对应的 shared-types 声明。
   * 三项共用同一个带 omitempty 的 DTO（PUT「省略即不改该键」），故类型上写可选；
   * GET 侧恒回数值（库里缺行按默认值），前端读取时不必判缺失。
   */
  calibrationOffsetN?: number | null; // threshold_calibration_offset（空载校准偏差上限，N）
  wechatTemplateId?: string | null;   // notify_wechat_template_id
  smsTemplateId?: string | null;      // notify_sms_template_id
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
