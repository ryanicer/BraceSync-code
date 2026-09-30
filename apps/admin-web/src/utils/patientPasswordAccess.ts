// T500：患者「设登录口令」入口的角色闸门。
//
// 单独成文件（不 import 页面 SFC）是为了让判据可单测——本项目里页面 SFC 在 vitest 下挂不起来
// 或挂不起断言（同 utils/alertPageAccess.ts 头的坑），抽成纯层才测得到真值表。
import type { RoleKey } from '../router/permissions'

/**
 * 是否允许看到 / 点动「设登录口令」。
 *
 * 依据链：
 * - 网关 `services/gateway/cmd/server/rbac.go` 把 POST `/api/v1/admin/patients/:patientId/password`
 *   登记进 adminOnlyPatterns，handler 侧 `requireAdminRole` 是同判据的双层防御
 *   （T477 原话：能给别人设口令 = 能登别人账号，危害与「改手机号」同级甚至更高）。
 * - 患者页本身由 `ROLE_PAGE_MATRIX` 限 admin 可见，看起来不必再分叉；但那是**页面**粒度，
 *   设密是这条页面上危害最大的一枪。页面准入日后一旦放宽（自定义角色 modules 已在权限页落地），
 *   入口跟着放出去就成了前端-only 的越权口子——后端照样 403，运营看到的却是点一次错一次。
 *
 * ⇒ 闸门按后端真源写死，未知角色（roleId 映射失败为 null）与 alertPageAccess 同向 fail-closed。
 */
export function canSetPatientPassword(role: RoleKey | null | undefined): boolean {
  return role === 'admin'
}
