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
  // T418 归口（原 ignore 三行）：DoctorDTO 的 username / accountStatus / createdAt 是 T314 并进的
  // admins 侧三列，页面此前用 admin-web 本地 DoctorWithAccount 声明。三列已补进契约 Doctor
  // （Go 无 omitempty ⇒ 键恒在、值可 null = 未绑登录账号），本地类型收口为 Doctor 别名。
  DoctorDTO: { ts: 'Doctor' },
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
  // T477 患者设密响应：与上面医护 reset-password 同形（明文口令只在这一个响应里出现一次），
  // 契约侧不声明该信封——给契约加 password 键等于鼓励前端留存明文。
  PatientPasswordSetDTO: { ts: null, reason: '写响应：一次性返回新口令，契约未声明该信封（同 DoctorAccountResetDTO 口径）' },
  // T480 技师口令两条写响应：与上面医护/患者同形（明文只在这一个响应里出现一次）。
  // 🔴 不给契约加 initialPassword/password 键 —— 那等于鼓励前端留存明文；
  // 页面按 DoctorAccountCreateResponse 的先例做本地交叉类型。
  TechnicianCreateDTO: { ts: null, reason: '写响应：整行 TechnicianDTO + 一次性初始口令，契约未声明该信封（同 DoctorAccountCreateDTO 口径）' },
  TechnicianPasswordResetDTO: { ts: null, reason: '写响应：一次性返回新口令，契约未声明该信封（同 PatientPasswordSetDTO 口径）' },

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
  // T418 归口（原 ignore 两行）：契约 InstallRecordDetail 按 extends InstallRecordRow 继承了
  // patientName / techName，此前详情 DTO 从不回、靠前端拿列表行兜底。现 GetInstall 补了
  // LEFT JOIN patients/technicians，两列在详情里真回；同时新增 model 列
  // （设计稿 安装记录.html:177「设备型号」，T312 I-3 登记项），三列都是 nullable。
  installDetailDTO: { ts: 'InstallRecordDetail' },
  BindingDTO: { ts: null, reason: '契约未声明（绑定历史条目，运营后台用本地类型）' },
  BindResponseDTO: { ts: null, reason: '写响应信封（绑定/换绑结果），契约未声明' },

  // ===== data-service =====
  healthReportDTO: { ts: 'HealthReport' },
  PressureRecordDTO: { ts: 'PressureRecord' },
  // T418 归口（原 ignore 十二行）：T248 1.1 的环比列（prev* / *ChangePct / *Delta）自 09xx 起
  // 就与 KPI 同行返回，契约 DashboardKPI 只声明了 6 列。十二列已补进契约（键恒在、值可 null：
  // 前窗为 0 或无基准 ⇒ null），页面消费与否是另一回事。
  // 🔴 prevDeviceOnlineRate / deviceOnlineRateDelta 恒 null —— 设备在线率无历史表，
  //    补齐要新增按日快照（schema 变更），已作为待裁项另卡跟，不因「恒 null」再豁免这两列。
  DashboardKPIDTO: { ts: 'DashboardKPI' },
  TeamRankingDTO: { ts: 'TeamRanking' },
  DoctorRankingDTO: { ts: 'DoctorRanking' },
  DailyWearDayDTO: { ts: null, reason: '契约未声明（患者端日佩戴聚合，小程序用本地类型）' },
  SensorPoint: { ts: 'SensorPoint' },
  // ⚠ 命名撞车：这个 Go DeviceConfig 是「云端→设备」的协议捎带下发结构（协议 §4.1，键是
  // snake_case 的 interval_minutes / config_version），跟 shared-types 里那个 camelCase 的
  // DeviceConfig 同名不同物；且 shared-types DeviceConfig 在 admin-web 里零消费。
  // 对拍会判出 4 处键名不一致，实为两回事 —— 登记不比对。
  // T418 结论「不补」（Boss 拍「补不了的书面论证」）：
  //  1) 两侧不是同一个东西：Go 侧是下行协议帧（键名由固件按 snake_case 解析），TS 侧是
  //     一期设想的「设备配置」展示模型；把任一侧改成对齐另一侧 = 改协议或改展示语义。
  //  2) 改 Go 侧 json tag ⇒ 现网设备收不到 config_version 的解析口径变化，属设备协议变更，
  //     不在前端/契约层能自修的范围（需固件侧确认，走设备协议文档改版）。
  //  3) 改 TS 侧（或删掉）⇒ shared-types DeviceConfig 零消费方，删了没有读者受益，
  //     留着也只是命名撞车；改名为 DeviceConfigView 反而会让协议文档 §4.1 的引用失配。
  //  4) 门禁面：C1c 要求「Go 与 TS 同名结构体必须登记」，所以本条 ts:null 不能删——
  //     删了会判「未登记」红，而不是变干净。
  // 稿面登记交 Peter 回写（api-contracts.ts 侧的 DeviceConfig 注释加「非设备协议帧」限定）。
  DeviceConfig: { ts: null, reason: '设备协议结构（snake_case，与同名契约接口不是一回事；T418 结论不补，理由见本段注释）' },

  // ===== msg-service =====
  NotifyRuleDTO: { ts: 'NotifyRule' },
  SubscriptionQuotaDTO: { ts: 'SubscriptionQuota' },
  WearReminderDTO: { ts: 'WearReminderSettings' },
  NotificationRecordDTO: { ts: 'NotificationRecord' },
}
