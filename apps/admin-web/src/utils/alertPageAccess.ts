// T351：告警管理页的角色分叉判据。
//
// 单独成文件（不 import 页面 SFC、不 import @logicflow/core）是必须的：本项目的告警页静态引入
// FlowDesigner.vue，而 LogicFlow 的 CJS 产物 require 了 ESM 的 lodash-es ⇒ CI 的 Node 18 下
// 加载即 ERR_REQUIRE_ESM，单测会变成「0 用例收集却显示全过」的假绿（T275 实测同坑，
// 见 flow/designer/kinds.ts 头注释）。
import type { RoleKey } from '../router/permissions'

/**
 * 告警管理页两张「配置」Tab（Tab2 告警规则配置 / Tab4 流程配置）是否可见。
 *
 * 依据链：
 * - 网关 `services/gateway/cmd/server/rbac.go` 的 adminOnlyPatterns 把这两张 Tab 背后的**配置写**端点
 *   （GET/PUT/POST `/api/v1/admin/alert-rules*`、POST/PUT/DELETE `/api/v1/admin/flow/templates*`）收口为
 *   admin 专属 ⇒ 医护 token 打过去必 403（T351 现场：进页面即一条 403 红条 + 控制台 403）。
 *   注：T359 之后 `GET /api/v1/admin/flow/templates`（列表/详情）已改判 staff 可读，
 *   那是 Tab3 运行态要按模板渲染流程；Tab4 是设计器（要改名/增删节点），仍按本判据整块摘掉。
 * - PRD §7D.11 权限矩阵给医护的是「🚨 告警管理 ✅（仅本团队患者）」= 页面级准入，本期不做子权限
 *   （Boss 2026-09-21 21:10 裁定），设计稿 `权限控制.html` 的权限树里「配置规则」也正是未勾选那一格。
 *
 * ⇒ 页面级准入保留，配置能力按角色摘掉：非 admin 既看不到这两张 Tab，也不发那两个请求。
 */
export function canConfigureAlerts(role: RoleKey | null | undefined): boolean {
  // 未知角色（roleId 映射失败 → role 为 null）按不可见处理：与路由守卫的 fail-closed 同向
  return role === 'admin'
}

/**
 * 告警「处理流程」Tab 里「转派候选人下拉」的数据源（GET /api/v1/doctors）可读判据（T359）。
 *
 * 与 canConfigureAlerts 判据同值但**依据不同**，故不合并：
 * - canConfigureAlerts 拦的是「配置面」——网关把模板/规则**写**端点收口 admin-only；
 * - 本判据拦的是「全院医护名录」——GET /api/v1/doctors 与 /api/v1/teams 同属团队管理页的
 *   admin 域（网关 rbac.go），返回全院医护的姓名/职称/科室/名下患者数/启停态。放行给客服
 *   （technician 未登记进 ROLE_ID_TO_KEY，映射不到 RoleKey ⇒ 同落 fail-closed）
 *   与 PRD §7D.11 医护「仅本团队患者」的数据范围取向相反。
 *
 * 它够不上「运行态必需」：设计稿 `告警管理.html:372-395` 的 Tab3 只有四个操作键 + 处理意见 +
 * 附件 + 时间线，没有人名下拉；控件本身是 filterable + allow-create 的手输框（占位文案即
 * 「选择或输入账号 ID」），后端也只校验字段长度（flow_t274.go validateFlowAccount）。
 * ⇒ 非 admin 不发这一枪，转派仍可用；同域先例 = T348 对 GET /api/v1/teams 403 的处置口径
 * （网关的 403 是正确行为，前端不照打、不让它把页面打空）。
 */
export function canReadDoctorRoster(role: RoleKey | null | undefined): boolean {
  return role === 'admin'
}
