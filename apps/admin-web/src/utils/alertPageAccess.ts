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
 * - 网关 `services/gateway/cmd/server/rbac.go` 的 adminOnlyPatterns 把这两张 Tab 背后的端点
 *   （GET/PUT/POST `/api/v1/admin/alert-rules*`、GET `/api/v1/admin/flow/templates`）收口为
 *   admin 专属 ⇒ 医护 token 打过去必 403（T351 现场：进页面即一条 403 红条 + 控制台 403）。
 * - PRD §7D.11 权限矩阵给医护的是「🚨 告警管理 ✅（仅本团队患者）」= 页面级准入，本期不做子权限
 *   （Boss 2026-09-21 21:10 裁定），设计稿 `权限控制.html` 的权限树里「配置规则」也正是未勾选那一格。
 *
 * ⇒ 页面级准入保留，配置能力按角色摘掉：非 admin 既看不到这两张 Tab，也不发那两个请求。
 */
export function canConfigureAlerts(role: RoleKey | null | undefined): boolean {
  // 未知角色（roleId 映射失败 → role 为 null）按不可见处理：与路由守卫的 fail-closed 同向
  return role === 'admin'
}
