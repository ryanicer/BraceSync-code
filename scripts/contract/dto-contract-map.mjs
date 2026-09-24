// T356 契约对拍登记表：Go 响应体（*DTO / 与契约同名的结构体） <-> packages/shared-types 契约
//
// 这张表是「谁对谁」的唯一事实源。三类条目：
//   1) { ts: 'X' }                 —— 对拍：两侧键集合必须相等，且可选性一致
//   2) { ts: 'X', ignore: {k:理由} } —— 对拍，个别键显式豁免（必须逐键写理由）
//   3) { ts: null, reason }        —— 登记「不比对」及其原因（契约未声明 / 入参 / 命名撞车）
//
// 新增 *DTO 而不进这张表 = 门禁判红（C1 未登记），这是卡片 ② 要的「忘配就显式失败」。
// 下面每一条 ignore / ts:null 都是一次实测的结果，不是猜的；已登记真实不一致清单见 T356 交件。

export const CONTRACT_MAP = {
  // ===== user-service：医生 / 技师 / 团队 / 患者 / 反馈 / 复查 =====
  AdminPatientDTO: { ts: 'AdminPatient' },
  TeamDTO: { ts: 'Team' },
  TeamDetailDTO: { ts: 'TeamDetail' },
  TeamStatsDTO: { ts: 'TeamStats' },
  TeamMemberDTO: { ts: 'TeamMember' },
  TeamMembersDTO: { ts: 'TeamMembers' },
  DoctorDTO: {
    ts: 'Doctor',
    // T356 实测：T314 把 admins 侧三列并进了 GET /api/v1/doctors 的同一行，
    // 页面在 apps/admin-web/src/api/medicalAccount.ts:33 自己声明了 accountStatus 来读，
    // 契约 Doctor 一直没补这三列 —— 真实不一致，报 PM 归口，本卡不改契约。
    ignore: {
      username: 'T314 并列，契约 Doctor 未声明（真实不一致，见交件 5.4）',
      accountStatus: 'T314 并列，页面用本地类型读，契约未声明（真实不一致）',
      createdAt: 'T314 并列，契约 Doctor 未声明（真实不一致）',
    },
  },
  TechnicianDTO: { ts: 'Technician' },
  FeedbackDTO: { ts: 'Feedback' },
  OrthosisPlanDTO: { ts: 'OrthosisPlan' },
  FeelingLogDTO: { ts: 'FeelingLog' },
  ReviewRecordDTO: { ts: 'ReviewRecord' },
  ReviewTemplateDTO: { ts: 'ReviewTemplate' },
  AdminRoleDTO: { ts: 'AdminRole' },
  RolePermissionsDTO: { ts: 'RolePermissions' },
  PermissionItemDTO: { ts: 'PermissionItem' },
  PermissionGroupDTO: { ts: 'PermissionGroup' },
  PermissionCatalogDTO: { ts: 'PermissionCatalog' },
  MyPermissionsDTO: { ts: 'MyPermissions' },
  WifiPresetDTO: { ts: 'WifiPreset' },
  SystemSettingsDTO: { ts: 'SystemSettings' },
  LoginResultDTO: { ts: 'AdminLoginResult' },
  TechLoginResultDTO: { ts: 'TechLoginResult' },
  PatientLoginResultDTO: { ts: 'PatientLoginResult' },
  // 入参方向：契约里确实声明了这三个请求体，一起对拍（后端漏 tag = 前端发的字段被丢）
  CreateReviewRecordRequest: { ts: 'CreateReviewRecordRequest' },
  CreateReviewTemplateRequest: { ts: 'CreateReviewTemplateRequest' },
  ReplaceReviewTemplateRequest: { ts: 'ReplaceReviewTemplateRequest' },

  // ===== user-service：契约里还没有声明的响应体（覆盖缺口，逐条给理由） =====
  AlertPointRuleDTO: { ts: null, reason: '契约未声明（告警规则页用 admin-web 本地类型），本卡不改契约' },
  AlertGlobalRulesDTO: { ts: null, reason: '契约未声明，同上' },
  AlertRulesDTO: { ts: null, reason: '契约未声明，同上' },
  AuditLogDTO: { ts: null, reason: '契约未声明（操作日志页用 admin-web 本地类型）' },
  RoleTemplateDTO: { ts: null, reason: '契约未声明（角色模板下拉用 admin-web 本地类型）' },
  FeedbackStatsDTO: { ts: null, reason: '契约未声明（沟通页统计栏 3 个计数）' },
  FeedbackCreatedDTO: { ts: null, reason: '写响应只回 feedbackId，契约未声明该信封' },
  FlowTemplateDTO: { ts: null, reason: '契约未声明（T274 流程模板，前端用本地类型）' },
  FlowInstanceDTO: { ts: null, reason: '契约未声明（T274 流程实例）' },
  FlowNodeStateDTO: { ts: null, reason: '契约未声明（T274 流程节点状态）' },
  FlowNodeActionDTO: { ts: null, reason: '契约未声明（T274 流程节点操作）' },
  BatchBindResultDTO: { ts: null, reason: '写响应（批量绑定结果信封），契约未声明' },
  BatchBindFailureDTO: { ts: null, reason: '写响应的子项，契约未声明' },
  DoctorAccountCreateDTO: { ts: null, reason: '写响应：整行 DoctorDTO + 一次性初始密码，契约未声明该信封' },
  DoctorAccountResetDTO: { ts: null, reason: '写响应：新密码 + 生效时间，契约未声明该信封' },

  // ===== user-service：入参方向且契约未声明请求类型（本卡只判响应侧键名/可选性） =====
  CreatePatientRequestDTO: { ts: null, reason: '入参：契约未声明请求类型' },
  AssignTeamRequestDTO: { ts: null, reason: '入参：契约未声明请求类型' },
  BatchBindRequestDTO: { ts: null, reason: '入参：契约未声明请求类型' },
  CreateTeamRequestDTO: { ts: null, reason: '入参：契约未声明请求类型' },
  UpdateTeamRequestDTO: { ts: null, reason: '入参：契约未声明请求类型' },
  AddMemberRequestDTO: { ts: null, reason: '入参：契约未声明请求类型' },
  UpdateMemberRequestDTO: { ts: null, reason: '入参：契约未声明请求类型' },
  WXLoginRequestDTO: { ts: null, reason: '入参：契约未声明请求类型' },
  AlertPointRuleUpdateDTO: { ts: null, reason: '入参：契约未声明请求类型' },

  // ===== device-service =====
  // DeviceDTO 是设备档案本体（内嵌在绑定/换绑响应里），契约 Device.patientName 由列表接口
  // 的 deviceListDTO 通过 join 带出，不属于 DeviceDTO 的语义。
  DeviceDTO: {
    ts: 'Device',
    ignore: { patientName: 'patientName 是 GET /devices 列表 join 出的扩展字段，只有 deviceListDTO 会回' },
  },
  deviceListDTO: { ts: 'Device' },
  installListDTO: { ts: 'InstallRecordRow' },
  installDetailDTO: {
    ts: 'InstallRecordDetail',
    // T356 实测：契约把 InstallRecordDetail 写成 extends InstallRecordRow，于是「继承」了
    // 列表专用的两个 join 名字列，而详情 DTO 从来不回它们 —— 真实不一致（契约建模口径），报 PM。
    ignore: {
      patientName: '列表 join 列；详情接口不回，契约按继承声明它（真实不一致，见交件 5.4）',
      techName: '同上',
    },
  },
  BindingDTO: { ts: null, reason: '契约未声明（绑定历史条目，运营后台用本地类型）' },
  BindResponseDTO: { ts: null, reason: '写响应信封（绑定/换绑结果），契约未声明' },

  // ===== data-service =====
  healthReportDTO: { ts: 'HealthReport' },
  PressureRecordDTO: { ts: 'PressureRecord' },
  DashboardKPIDTO: {
    ts: 'DashboardKPI',
    // T356 实测：后端 09xx 起把环比（prev* / *ChangePct / *Delta）12 列一起回了，
    // 契约 DashboardKPI 只声明 6 列，页面也没消费环比（grep 无命中）——
    // 属「后端多回 + 契约未声明」，不影响读侧，登记后报 PM 决定是否补契约。
    ignore: {
      prevTodayActiveWear: '环比列，契约未声明且页面未消费（见交件 5.4）',
      prevTodayAlerts: '同上',
      prevAvgWearHours: '同上',
      prevTotalPatients: '同上',
      prevMonthNewPatients: '同上',
      prevDeviceOnlineRate: '同上',
      activeWearChangePct: '同上',
      alertsChangePct: '同上',
      avgWearHoursDelta: '同上',
      deviceOnlineRateDelta: '同上',
      totalPatientsChangePct: '同上',
      monthNewPatientsChangePct: '同上',
    },
  },
  TeamRankingDTO: { ts: 'TeamRanking' },
  DoctorRankingDTO: { ts: 'DoctorRanking' },
  DailyWearDayDTO: { ts: null, reason: '契约未声明（患者端日佩戴聚合，小程序用本地类型）' },
  SensorPoint: { ts: 'SensorPoint' },
  // ⚠ 命名撞车：这个 Go DeviceConfig 是「云端→设备」的协议捎带下发结构（协议 §4.1，键是
  // snake_case 的 interval_minutes / config_version），跟 shared-types 里那个 camelCase 的
  // DeviceConfig 同名不同物；且 shared-types DeviceConfig 在 admin-web 里零消费。
  // 对拍会判出 4 处键名不一致，实为两回事 —— 登记不比对，另报 PM（改产品码不在本卡范围）。
  DeviceConfig: { ts: null, reason: '设备协议结构（snake_case，与同名契约接口不是一回事；见交件 5.4 命名撞车）' },

  // ===== msg-service =====
  NotifyRuleDTO: { ts: 'NotifyRule' },
  SubscriptionQuotaDTO: { ts: 'SubscriptionQuota' },
  WearReminderDTO: { ts: 'WearReminderSettings' },
  NotificationRecordDTO: { ts: 'NotificationRecord' },
}
