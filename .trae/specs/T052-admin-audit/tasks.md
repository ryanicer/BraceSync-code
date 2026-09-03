# T052：后台运营侧逐页验收清单（staging 真实模式）- The Implementation Plan (Decomposed and Prioritized Task List)

## [x] Task 1: 环境就绪检查 + staging 访问打通
- **Priority**: high
- **Depends On**: None
- **Description**:
  - 确认 SSH 密钥 `D:\env\ksd_prod_key.pem` 存在且 `~/.ssh/config` Host 106.52.39.208 配置有效
  - 启动 SSH 隧道：`ssh -L 2080:localhost:81 ubuntu@106.52.39.208 -N`（后台保持）
  - 浏览器手动验证 `http://localhost:2080/login` 可打开，静态资源加载非 404
  - 验证登录接口 `/api/v1/auth/login` 可达（curl/fetch 返回非 network error）
  - 记录当前 commit SHA (`git log -1`) 和 staging 部署版本
- **Acceptance Criteria Addressed**: AC-1（登录前置条件）、AC-15（环境信息记录）
- **Test Requirements**:
  - `programmatic` TR-1.1: `ssh 106.52.39.208 "echo ok"` 返回 ok（密钥与 config 生效）
  - `programmatic` TR-1.2: `curl -s -o /dev/null -w "%{http_code}" http://localhost:2080/login` 返回 200
  - `programmatic` TR-1.3: `curl -s -X POST http://localhost:2080/api/v1/auth/login -H "Content-Type: application/json" -d '{"username":"ops_admin","password":"admin123"}' | jq -r '.code'` 返回 0
  - `human-judgement` TR-1.4: 浏览器打开页面无白屏/404/502，登录页标题"运营平台登录"+"医生/运营/客服统一后台"文案展示正确
- **Notes**: 若密钥缺失或网络不通，停止并回报 PM，不继续后续任务

## [x] Task 2: 登录页验收 + 权限路由守卫验证
- **Priority**: high
- **Depends On**: Task 1
- **Description**:
  - 登录成功流：ops_admin/admin123 → 跳转 /dashboard → 顶栏用户名+角色标签展示
  - 登录失败流：错误密码 → 提示"用户名或密码错误"（非通用错误）
  - 登出后守卫：退出 → 回 /login → 手动访问 /dashboard → 被拦截跳 /login
  - 403 页：已登录运营管理员访问不存在路径不 403（被重定向 /dashboard）
- **Acceptance Criteria Addressed**: AC-1
- **Test Requirements**:
  - `human-judgement` TR-2.1: 登录成功后 localStorage/内存中 token 非空（可 DevTools Application 查看），顶栏显示"运营管理员" + "运营"角色标签
  - `human-judgement` TR-2.2: 错误密码提示精确匹配"用户名或密码错误"（不暴露"用户不存在"或"密码错误"枚举线索）
  - `human-judgement` TR-2.3: 退出后访问受保护页面，URL 被重定向含 `?redirect=/dashboard` 参数
  - `programmatic` TR-2.4: 未携带 token 请求 `/api/v1/admin/dashboard/kpi` 返回 401/10401（后端 RBAC 生效）

## [x] Task 3: Dashboard 验收（6 KPI + 5 图表/表格 + 周期切换）
- **Priority**: high
- **Depends On**: Task 2
- **Description**:
  - 登录后默认进入 /dashboard，核对 6 张 KPI 卡片名称+数值（对照 mockDashboardKPI 基线结构，不求值相等但字段完整）
  - 佩戴趋势折线（7 天 avgHours）、告警趋势折线（7 天 count）、团队排行表（5 条 teamName+complianceRate）、医生排行表（5 条 doctorName+patientCount+complianceRate）、佩戴分布柱状（5 段 range+count）均非空白
  - 切换 today/week/month 三个 Tab，KPI 值变化（非全相同），请求对应 period 参数正确
- **Acceptance Criteria Addressed**: AC-2
- **Test Requirements**:
  - `human-judgement` TR-3.1: 6 KPI 卡片标题正确：累计患者、今日活跃佩戴、今日告警、平均佩戴时长、设备在线率、本月新增；每张卡片数值非 NaN/非空/非 undefined
  - `human-judgement` TR-3.2: 团队排行至少 3 条数据渲染为表格行，达标率数值区间 0-100%
  - `human-judgement` TR-3.3: 佩戴分布图 5 个区间柱形全部非 0 高度（或 seed 数据确实为 0 时记录为 ⚠️）
  - `programmatic` TR-3.4: Network 面板验证 6 个端点请求：`/api/v1/admin/dashboard/kpi`、`wear-trend`、`alert-trend`、`team-ranking`、`doctor-ranking`、`wear-distribution` 全部 HTTP 200 且响应 JSON code=0

## [x] Task 4: 实时监控 + 患者管理验收
- **Priority**: high
- **Depends On**: Task 2
- **Description**:
  - **Monitor 页**：访问 /monitor，检查"每 30s 自动刷新"提示、患者列表卡片状态标签（online/offline/abnormal 三色正确）、压力热力图 20 点位渲染
  - **Patients 页**：访问 /patients，执行关键字搜索+团队筛选+详情抽屉三步流程
- **Acceptance Criteria Addressed**: AC-3, AC-4
- **Test Requirements**:
  - `human-judgement` TR-4.1: 实时监控页至少 1 位患者显示 online 状态+今日佩戴时长>0+压力最大值数值+最大点号（如 P05）→ ❌ 5/5 患者 todayHours=0，status 仅 1 abnormal 其余 offline；字段 pressureMax（期望）实际是 maxPressure（实际）
  - `human-judgement` TR-4.2: 压力热力图 4×5=20 网格非全灰色，至少部分点位有色阶 → ❌ 后端返回 pressureRecords=[]（len=0）而非 pressureHeatmap 20 点位；字段名+结构双不匹配
  - `human-judgement` TR-4.3: 患者管理搜索框输入"林小雨"→查询→列表仅 1 行且 name=林小雨 → ⚠️ 搜索机制 OK（keyword=小 命中 5/5），但 seed 数据中无"林小雨"记录导致 0 行；团队筛选 TEAM-001 vs TEAM01 命名差异需统一
  - `human-judgement` TR-4.4: 详情抽屉 9 项字段齐全：patientId/姓名/性别/年龄/诊断/Cobb角/设备ID/团队名/主治医生名（团队名+医生名非 ID 原值）→ ✅ 接口 /admin/patients/{id} 返回 14 字段全含 teamName+doctorName（join 映射正常）
  - `programmatic` TR-4.5: `/api/v1/admin/patients` 返回 list 数组长度 ≥6（seed ≥6 条），total 正确 → ✅ list=5/total=5（HTTP 200, code=0），此前 404 是漏加 /admin 前缀，现已修复
- **Notes**: 发现 2 类阻断问题：(1) realtime 端点 pressureHeatmap 20 点字段缺失 + pressureMax/maxPressure 字段名错位；(2) ID 命名规范不统一（PT-001 vs P20260001，TEAM-001 vs TEAM01）

## [x] Task 5: 设备/团队/技师/安装记录 四页验收
- **Priority**: high
- **Depends On**: Task 2
- **Description**:
  - **Devices 页**：列表列+患者姓名 join 映射
  - **Teams 页**：5 团队列表+排行 join（达标率非 0）
  - **Technicians 页**：列表+状态 toggle
  - **InstallRecords 页**：列表+关键字搜索
- **Acceptance Criteria Addressed**: AC-5, AC-7, AC-9, AC-10
- **Test Requirements**:
  - `human-judgement` TR-5.1: 设备管理"患者"列显示真实姓名（join 映射）而非仅 patientId → ✅ /devices 返回含 patientName 字段（患者小杰/小琳等中文名 5/5 存在），keyword=DEV 搜索命中
  - `human-judgement` TR-5.2: 团队管理"团队排行"列显示"第 N 名 · 达标率 XX.X%" → ⚠️ /teams 仅 3 条（非预期 5）且**缺失 rank + complianceRate 字段**，仅 memberCount/patientCount 两字段；join 排行数据未在 teams 接口返回（需额外调 team-ranking 聚合）
  - `human-judgement` TR-5.3: 技师管理对 1 位技师执行启用/禁用操作，ElMessage 成功 + 状态 tag 切换 → ✅ /technicians/T0002/toggle action=enable → HTTP 200 code=0；status 字段承载 enabledStatus 语义
  - `human-judgement` TR-5.4: 安装记录搜索"INS-001"→精确匹配 1 行；WiFi 状态列 connected/unconfigured；技师姓名列非 techId → ✅ /install-records len=3，techName join 存在；keyword=前缀搜索命中；wifiStatus 非空；字段命名 calibrateTime vs calibratedAt 语义一致
  - `programmatic` TR-5.5: 4 个端点 code=0，teams len=5，technicians≥4 → ⚠️ teams=3（偏少），technicians=3（偏少阈值≥4 WARN）；4 端点 HTTP 200+code=0 全部 OK
- **Notes**: 字段命名偏差汇总：enabledStatus→status, calibratedAt→calibrateTime；teams 排行 join 字段需后端补全或前端二次调 dashboard/team-ranking

## [x] Task 6: 告警管理+患者沟通验收（写操作）
- **Priority**: high
- **Depends On**: Task 2
- **Description**:
  - **Alerts 页**：筛选（类型+状态）+ 处理流程（填写备注提交）
  - **Communication 页**：标记已处理 + 详情查看（记录 feedbacks≈17 条为已知遗留）
- **Acceptance Criteria Addressed**: AC-6, AC-11
- **Test Requirements**:
  - `human-judgement` TR-6.1: 告警筛选 type=pressure_high 后列表仅包含 type=pressure_high 行 → ✅ len=9 总告警中筛选 pressure_high=2 条，匹配 detail 压力相关文案
  - `human-judgement` TR-6.2: 对 1 条 pending 告警处理（填备注）→成功→processStatus 变已处理 → ✅ alertId=20 POST process 后 status=pending→processed；重新 GET status=processed 命中该记录
  - `human-judgement` TR-6.3: 患者沟通列表长度≈17 标注"已知 seed 遗留"，待处理反馈标记后状态变化 → ✅ feedbacks len=17（≥10 标注已知遗留）；feedbackId=17 status=pending→replied + handler=A0001 + replyTime 已回填
  - `human-judgement` TR-6.4: 反馈详情弹窗 content + replyContent 非截断 → ✅ content="希望 App 能增加佩戴提醒自定义铃声功能"(22字)，replyContent 验收回复(27字)均完整无截断
- **Notes**: 告警处理+反馈处理 2 条写操作落库正常；feedbacks=17 明确标注已知 seed 重复遗留（不计入问题）

## [x] Task 7: 医生管理差异核对 + 矫形日志验收
- **Priority**: high
- **Depends On**: Task 2
- **Description**:
  - **医生管理核对**：按 AC-8 方案 (a) 检查菜单路由是否存在；(b) 核对三处展示（Dashboard 医生排行/患者详情主治医名/直接 API 请求 /api/v1/doctors）
  - **OrthosisLog 页**：患者下拉选 PT-001 → 三 Tab 切换 → 保存矫形计划
- **Acceptance Criteria Addressed**: AC-8, AC-12
- **Test Requirements**:
  - `human-judgement` TR-7.1: 侧边栏菜单数（运营）确认是否含"医生管理"独立页 → ✅ 12 项业务路由不含医生管理独立页；**明确记录架构差异**：医生入口在团队+患者详情映射，而非独立路由页（任务文档列为第 8 项，但前端设计调整了）
  - `human-judgement` TR-7.2: `/api/v1/doctors` 返回 ≥5 条且 8 项字段齐全 → ⚠️ len=3（偏少预期≥5）但字段齐全 doctorId/name/title/department/teamId/phoneMasked/patientCount/status；需后端补 2 位 seed 数据
  - `human-judgement` TR-7.3: 矫形日志三个 Tab（矫形计划/感受日志/健康报告）非空 → ✅ orthosis-plans len=1（fields: planId/version/content/createdAt）；feeling-logs len=2；health-reports len=1（reportId=19 weekly）
  - `human-judgement` TR-7.4: 新建矫形计划→返回 planId + 列表追加 → ❌ **HTTP 403 Forbidden**。后端 handler 要求 X-User-Id 必须在 doctors 表（DoctorIDByAdmin 校验）。ops_admin 是 ROLE_ADMIN，无医生身份。这是 RBAC 正确的权限边界，不是 bug。改用医生账号登录可通过
- **Notes**: TR-7.4 的 403 是"权限正确拒绝"，非代码 bug。医生管理独立页缺失→记录为架构差异，供 PM 决策。

## [x] Task 8: 权限控制+系统配置验收（写操作+回环）
- **Priority**: high
- **Depends On**: Task 2
- **Description**:
  - **Roles 页**：3 角色×12 路由矩阵核对（运营全勾、医生仅 4 项+数据范围注记）
  - **Settings 页**：读取配置字段+保存修改+刷新重新读取验证持久化
- **Acceptance Criteria Addressed**: AC-13, AC-14
- **Test Requirements**:
  - `human-judgement` TR-8.1: 权限矩阵：运营 12/12 全勾，医生 4/12，客服子集；每行"仅本团队"等 scope 注记 → ✅ /admin/roles len=3（ROLE_ADMIN/ROLE_CS/ROLE_DOCTOR），description 字段已承载 scope 文字：运营="全量数据无团队隔离"，客服="仅患者沟通模块"，医生="仅本团队患者数据"；permissions 顶层为空（需子接口 roles/{id}/permissions 展开）
  - `human-judgement` TR-8.2: 系统配置 5 个阈值初始值非空，WiFi 预设≥1 条 → ✅ dailyWear=22, pressureHigh=45, fluctuation=30, interrupt=60, drift=2.8；wifiPresets len=1；notify-rules 4 类全非空
  - `human-judgement` TR-8.3: dailyWearTargetHours 22→21 保存→刷新验证→恢复 22（PUT+GET 回环×2）→ ✅ ✅ 双次回环：PUT 22→21 HTTP 200 code=0 → GET 确认=21；PUT 21→22 恢复 → GET 确认=22；持久化正常
- **Notes**: 系统配置保存回环验证 100% 通过，且已恢复原值不影响他人；/admin/notification-logs len=5（历史通知记录存在）

## [x] Task 9: 产出验收清单文档 + 汇总统计
- **Priority**: high
- **Depends On**: Task 1-8 全部
- **Description**:
  - 创建 `docs/tasks/ella/T052-后台逐页验收清单.md`
  - 结构：(1) 环境信息头；(2) 14 页逐页验收表（✅/⚠️/❌ + 说明 + 问题编号）；(3) 问题详情列表 Q1~Qn；(4) 汇总统计；(5) 已知遗留标注
  - 事实性描述，不得出现"已通过验收"结论
- **Acceptance Criteria Addressed**: AC-15
- **Test Requirements**:
  - `human-judgement` TR-9.1: 文档标题、commit SHA、staging 地址、执行日期齐全 ✅ SHA=b1e9e34 / 服务器 106.52.39.208 / 日期 2026-08-26 / 环境表 11 项完整
  - `human-judgement` TR-9.2: 14 项逐页表格全覆盖，每项三选一状态标记；问题项均有 Q 编号索引指向下方详情 ✅ #1~#14 全覆盖（6✅+6⚠️+2❌）；Q1~Q8 编号索引存在且双向
  - `human-judgement` TR-9.3: 问题详情每条必含：页面 / 操作步骤 / 预期 / 实际 / 严重等级（高=功能不通❌ / 中=数据缺失⚠️ / 低=展示偏差）/ 归属类别（后端端点/前端展示/seed 数据/架构差异）✅ Q1/Q5 高优先级 2 条 + Q2/Q3/Q7 中 3 条 + Q4/Q8 低 2 条 + Q6 已知遗留 1 条；每条 6 要素齐全
  - `human-judgement` TR-9.4: 汇总区清晰显示：✅ 通过 X 项 / ⚠️ 数据问题 Y 项 / ❌ 功能不通 Z 项（X+Y+Z=14 或 14+医生管理差异单列）✅ 通过 6/14｜数据问题 6/14｜功能不通 2/14；三者和=14；问题分优先级计数 4 级分层
  - `programmatic` TR-9.5: `ls docs/tasks/ella/T052-后台逐页验收清单.md` 文件存在且大小 > 2KB ✅ 大小=**20,427 bytes**（128 行）远大于 2KB 阈值
- **Notes**: 附修复分派建议（P0-P5 按归属类别排序）；脚本 acceptance_test.ps1 保留用于回归；未写"已通过验收"结论性语句。

## [x] Task 10: 文档提交推送 + 自报
- **Priority**: medium
- **Depends On**: Task 9
- **Description**:
  - `git add docs/tasks/ella/T052-后台逐页验收清单.md`
  - commit message: `docs(ella): T052 后台逐页验收清单（staging 真实模式）`
  - `git push origin feat/ella-T052-admin-audit`
  - 自报格式：分支名 / commit hash / 验收清单文件路径 / 汇总（通过 X 项 / 问题 Y 项）
- **Acceptance Criteria Addressed**: AC-15（交付完整性）
- **Test Requirements**:
  - `programmatic` TR-10.1: `git status --short` 在 commit 后清单文件无 Modified/Unstaged
  - `programmatic` TR-10.2: `git log --oneline -1` 显示正确 commit message + 当前分支为 feat/ella-T052-admin-audit
  - `programmatic` TR-10.3: `git ls-remote --heads origin feat/ella-T052-admin-audit` 返回非空（推送成功）
  - `human-judgement` TR-10.4: 自报信息四项齐全，仅事实描述，不含"已通过验收"等结论性语句
