# T052：后台运营侧逐页验收清单（staging 真实模式）- Product Requirement Document

## Overview
- **Summary**: 在 BraceSync staging 真实部署环境（VITE_USE_MOCK=false）上，对运营后台 admin-web 的 14 个页面进行逐页人工验收，核对前端页面展示与真实 API 返回数据的一致性，记录通过/数据问题/功能不通三类状态，产出可复现的问题清单文档。
- **Purpose**: Boss 反馈"staging 后台和 demo 完全不一样"，根因定位为 seed 数据不足 + 真实模式切换。T051 已完成 seed 数据扩充并合并，本任务系统性地对 14 页逐一过一遍，暴露所有数据展示偏差、接口不通、字段缺失等问题，为后续分派修复（后端端点/前端展示/seed 数据）提供依据。
- **Target Users**: 测试专家 Ella 执行验收，PM 审核清单并分派修复，产出物供开发人员问题复现使用。

## Goals
- 覆盖任务文档列出的全部 14 个页面（含登录页 + 12 业务路由页 + 医生管理核对）
- 每个页面按 mock 期望数据结构核对真实 API 返回，状态分三类：✅ 通过 / ⚠️ 数据问题 / ❌ 功能不通
- 每个问题提供可复现描述：页面 + 操作 + 预期 + 实际
- 产出 `docs/tasks/ella/T052-后台逐页验收清单.md` 并附汇总统计（通过 X 项 / 问题 Y 项）

## Non-Goals (Out of Scope)
- **不修复问题**：本任务仅记录验收结果，不修改任何前端/后端代码
- **不写自动化 E2E 代码**：验收清单是人工/半自动执行的文档，非 T053 的自动化测试
- **不验证患者端/技师端小程序**：仅限 admin-web 运营后台
- **不做压力/性能测试**：仅功能与数据展示正确性核对
- **不验证非运营角色的深度业务逻辑**：权限矩阵仅验证展示层（运营/医生/客服菜单差异）

## Background & Context
- **项目状态**: T051 (seed 数据扩充) 已合并 PR #8 commit b1e9e34，staging 已由 Andy 重播种并冒烟全过
- **仓库纪律**: 主仓库 D:/proj/BraceSync 只读，开发在 `.worktrees/ella` worktree，已创建分支 `feat/ella-T052-admin-audit`
- **技术栈**: admin-web 为 Vue 3 + Element Plus + Vite，USE_MOCK=true 时走本地 mock，false 时走 `request()` 真实请求
- **已知现象（勿当 bug）**: feedbacks/orthosis_plans 表约 17 行（旧 seed 重复执行遗留），新版已幂等修复；验收时记录行数但不标记为问题
- **Staging 访问方式**:
  - SSH 隧道：`ssh -L 2080:localhost:81 ubuntu@106.52.39.208 -N`，浏览器访问 `http://localhost:2080`
  - 账号：运营管理员 ops_admin / admin123（T041/seed 播种）
  - 密钥：`D:\env\ksd_prod_key.pem`（需向 Boss/PM 索取）

## Functional Requirements
- **FR-1**: 登录验收：真实账号 (ops_admin/admin123) 登录成功，token 存储，路由守卫拦截受保护页面
- **FR-2**: Dashboard 验收：6 个 KPI 卡片 + 佩戴趋势折线 + 告警趋势折线 + 团队排行表 + 医生排行表 + 佩戴时长分布柱状图，共 6 个聚合端点数据正确渲染
- **FR-3**: 实时监控验收：患者状态列表 + 单患者实时快照（在线/离线/异常、今日佩戴时长、压力最大点、压力热力图），30s 自动刷新提示
- **FR-4**: 患者管理验收：列表分页（关键字/团队筛选）+ 详情抽屉（基本信息/团队/医生/佩戴状态）
- **FR-5**: 设备管理验收：设备列表 + 患者姓名 join 映射展示
- **FR-6**: 告警管理验收：列表筛选（类型/状态/患者）+ 处理流程（标记处理 + 填写处理备注）
- **FR-7**: 团队管理验收：团队列表（ID/名称/成员数/患者数）+ 团队排行数据 join（达标率/名次）
- **FR-8**: 医生管理验收：核对 fetchDoctors API 与数据展示（如无独立路由页面则核对 API 可用性 + Dashboard 医生排行 + 患者详情医生名映射）
- **FR-9**: 技师管理验收：技师列表分页 + 启用/禁用 toggle 操作
- **FR-10**: 安装记录验收：安装记录列表（设备/患者/技师/校准时间/WiFi 状态）+ 关键字搜索
- **FR-11**: 患者沟通验收：反馈列表（FB-001~FB-004 级别数据）+ 标记处理 + 详情查看
- **FR-12**: 矫形日志验收：患者下拉切换 + 三个 Tab（矫形计划/感受日志/健康报告）+ 计划新建保存
- **FR-13**: 权限控制验收：角色矩阵表格（运营/医生/客服三预置角色 × 12 路由页权限）
- **FR-14**: 系统配置验收：配置参数读取（阈值/WiFi 预设）+ 保存写入回环验证

## Non-Functional Requirements
- **NFR-1**: 验收清单文档可读性强：每项问题可独立复现（页面 + 操作 + 预期 + 实际），无需额外上下文
- **NFR-2**: 状态标记一致性：✅ 全部正常 / ⚠️ 字段缺失/格式不对/空数据但页面未崩 / ❌ 404/500/白屏/接口超时
- **NFR-3**: 汇总统计：文档末尾附 14 项各状态计数 + 问题条目编号索引
- **NFR-4**: 已知异常区分：feeds/orthosis_plans 行数偏多明确标注"已知遗留，不计入问题"

## Constraints
- **Technical**:
  - Staging 为真实部署，非本地 dev server；通过 SSH 隧道访问（需密钥文件）
  - VITE_USE_MOCK=false 真实模式，所有请求走后端微服务（gateway → user/data/alert/device/msg-service）
  - 不得对 staging 数据库做写操作以外的破坏性测试（标记处理/保存配置为正常验收流程）
- **Business**:
  - 验收仅记录事实，不得写"已通过验收"结论性语句
  - 问题清单需可由 PM 按归属分派（后端端点/前端展示/seed 数据三类）
- **Dependencies**:
  - 依赖 T051 seed 数据（b1e9e34）已在 staging 生效
  - 依赖 staging 服务器 106.52.39.208 网络可达 + SSH 密钥就位
  - 依赖 admin-web mock 数据作为期望基线（`apps/admin-web/src/mock/*.ts`）

## Assumptions
- staging 已按 T051 seed.sql 重新播种，每表数据量 > 1 行（Andy 已确认冒烟全过）
- ops_admin / admin123 账号在 user-service 中存在且角色为运营管理员
- 所有 26+ 个 API 端点的网关路由已配置（gateway proxy_admin + proxy_services）
- SSH 隧道方式 A（2080→81）可正常访问 nginx 反代的 admin-web 静态资源 + /api 前缀代理

## Acceptance Criteria

### AC-1: 登录功能验收
- **Given**: staging SSH 隧道已建立，浏览器打开 `http://localhost:2080/login`
- **When**: 输入 ops_admin / admin123 点击登录
- **Then**: 跳转到 /dashboard，顶栏显示"运营管理员"用户名，localStorage 存有 token；退出登录后访问 /dashboard 被拦截回 /login
- **Verification**: `human-judgment`
- **Notes**: 同时验证错误密码提示"用户名或密码错误"（后端 code=10401）

### AC-2: Dashboard 6 个 KPI + 6 图表渲染
- **Given**: 已登录运营管理员账号
- **When**: 访问 /dashboard，等待所有请求完成
- **Then**: 6 张 KPI 卡片（累计患者/今日活跃佩戴/今日告警/平均佩戴时长/设备在线率/本月新增）均有数值；佩戴趋势、告警趋势、团队排行、医生排行、佩戴分布 5 个图表/表格无空白；周期切换（today/week/month）KPI 值变化
- **Verification**: `human-judgment`

### AC-3: 实时监控数据展示
- **Given**: 已登录，staging data-service `/api/v1/patients/:id/realtime` 端点可用
- **When**: 访问 /monitor，选择不同患者
- **Then**: 页面显示"每 30s 自动刷新"提示；每个患者卡片有在线/离线/异常状态标签、今日佩戴小时数、压力最大值/点号；压力热力图非空（20 个点位）
- **Verification**: `human-judgment`

### AC-4: 患者管理列表+筛选+详情
- **Given**: 已登录，至少 6 条患者 seed 数据
- **When**: 访问 /patients，关键字搜索"林小雨"，按团队筛选，点击列表第一行
- **Then**: 搜索结果 ≥1 行且姓名匹配；团队筛选后列表过滤生效；详情抽屉打开并展示患者 ID、性别、年龄、诊断、Cobb 角、团队名（teamNameOf 映射）、主治医生名（doctorNameOf 映射）等字段
- **Verification**: `human-judgment`

### AC-5: 设备管理 + 患者姓名映射
- **Given**: 已登录，设备表至少 DEV-A3F312 等记录
- **When**: 访问 /devices，等待列表加载
- **Then**: 表格列包含设备 ID、绑定患者 ID、患者姓名（patientNameOf join 映射非 patientId 原值）、固件版本、在线状态
- **Verification**: `human-judgment`

### AC-6: 告警管理筛选+处理流程
- **Given**: 已登录，告警表至少 6 条记录
- **When**: 访问 /alerts，按类型=pressure_high 筛选+按状态=pending 筛选；对第 1 条待处理告警点击"处理"按钮，填写备注并确认
- **Then**: 筛选后列表行数变化；处理对话框弹出，填写后提交提示"处理成功"，该行 processStatus 变更为 processed
- **Verification**: `human-judgment`

### AC-7: 团队管理+排行数据 join
- **Given**: 已登录，团队表 5 条 + 团队排行接口可用
- **When**: 访问 /teams
- **Then**: 表格列：团队ID/名称/成员数/管理患者数/团队排行（名次+达标率%）；达标率非 0 且数值合理
- **Verification**: `human-judgment`

### AC-8: 医生管理核对（API+展示层）
- **Given**: 已登录，`/api/v1/doctors` 端点 + mock DOCTORS 5 条基线
- **When**: (a) 检查侧边栏是否有"医生管理"菜单；(b) 若无独立页则核对 Dashboard 医生排行表 + 患者详情"主治医生"名映射 + 直接请求 `/api/v1/doctors` 返回 ≥5 条
- **Then**: 医生数据（ID/姓名/职称/科室/团队/手机掩码/患者数/状态）至少在一个展示位正确显示；独立页面缺失需记录为架构问题
- **Verification**: `human-judgment`
- **Notes**: 任务文档列为第 8 项独立页面，但当前路由 12 项不含医生管理；此差异为重要核对点

### AC-9: 技师管理+启用/禁用
- **Given**: 已登录，技师表 TECH-001~TECH-004
- **When**: 访问 /technicians，对 TECH-004（当前 disabled）点击"启用"按钮
- **Then**: 表格列：技师ID/姓名/手机掩码/所属团队/安装次数/状态/授权状态；toggle 操作成功提示，状态标签切换
- **Verification**: `human-judgment`

### AC-10: 安装记录列表+搜索
- **Given**: 已登录，安装记录表 ≥5 条
- **When**: 访问 /install-records，关键字框输入设备 ID 前缀如"DEV-A"
- **Then**: 表格列：安装ID/设备ID/患者ID/技师名（techNameOf 映射）/校准时间/基线ID/备注/WiFi 状态；搜索过滤生效
- **Verification**: `human-judgment`

### AC-11: 患者沟通反馈列表+处理
- **Given**: 已登录，feedback 表约 17 条（已知偏多，非 bug）
- **When**: 访问 /communication，对 1 条待处理反馈点击"标记已处理"并填写回复；对另一条点击"详情"
- **Then**: 列表至少 ≥10 条（含重复遗留）；标记处理后状态变更；详情对话框展示完整反馈内容+已回复内容
- **Verification**: `human-judgment`

### AC-12: 矫形日志三 Tab+保存计划
- **Given**: 已登录，orthosis_plans 约 17 条（已知偏多）+ feeling_logs + health_reports 均有数据
- **When**: 访问 /orthosis-log，从患者下拉选 PT-001，依次切换矫形计划/感受日志/健康报告三个 Tab；在矫形计划 Tab 填写内容并保存
- **Then**: 三个 Tab 切换均非空表；保存新计划后返回 planId（非空）和 version，列表追加一条
- **Verification**: `human-judgment`

### AC-13: 权限控制角色矩阵展示
- **Given**: 已登录运营管理员
- **When**: 访问 /roles
- **Then**: 矩阵表格行=3 个预置角色（运营/医生/客服），列=12 个路由页复选框；运营全勾选（12/12），医生仅 4 项（Dashboard/Monitor/Alerts/OrthosisLog），客服对应子集；数据范围注记文字（医生仅本团队/客服仅标记）显示
- **Verification**: `human-judgment`

### AC-14: 系统配置读取+保存回环
- **Given**: 已登录，DEFAULT_THRESHOLDS 常量为期望基线
- **When**: 访问 /settings，修改 dailyWearTargetHours 为 21 后保存，刷新页面重新读取
- **Then**: 初始值字段填充（每日佩戴目标 22h/压力高阈值/波动百分比/中断分钟数/传感器漂移 N + WiFi 预设 2 条）；保存成功提示，刷新后持久化值为 21（或确认 PUT 接口 200）
- **Verification**: `human-judgment`

### AC-15: 产出物文档规范
- **Given**: 14 项全部验收完毕
- **When**: 编写 `docs/tasks/ella/T052-后台逐页验收清单.md`
- **Then**: 文档包含：(1) 环境信息（staging IP/访问方式/commit SHA）；(2) 14 项逐页表格（状态+说明+问题编号引用）；(3) 问题详情列表（Q1~Qn，每项：页面+操作+预期+实际+严重等级+归属类别）；(4) 汇总统计（通过 X/数据问题 Y/功能不通 Z）；(5) 已知遗留标注（feeds/orthosis_plans 行数）
- **Verification**: `human-judgment`

## Open Questions
- [ ] SSH 密钥文件 `D:\env\ksd_prod_key.pem` 是否已就位？（否则无法建立 staging 隧道）
- [ ] 任务文档 14 项含"医生管理"独立页面，但当前路由仅 12 业务页。是确认缺失（标记为问题）还是并入团队管理内？（当前先按 AC-8 处理：核对 API+展示层差异）
- [ ] 系统配置保存后是否会影响其他测试人员？（如需隔离，仅做 GET 读取验收 + PUT 记录请求响应但不改实际值）
