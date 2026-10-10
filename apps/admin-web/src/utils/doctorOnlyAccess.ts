// T629 方案C：doctor-only 动作的前端闸门（只改提示，不动权限矩阵）。
//
// 单独成文件（不写在页面 SFC 里）同 utils/patientPasswordAccess.ts 的口径——纯层才测得到真值表。
//
// 依据链（本轮现读代码，不是推断）：
// - 网关 services/gateway/cmd/server/rbac.go 的 doctorAdminOnlyPatterns 收的是「医生 + 管理员」，
//   所以运营管理员能过网关；403 出在业务层下一步。
// - user-service handler.go savePlan 里 DoctorIDByAdmin(x-user-id → doctors.admin_id) 查不到行就
//   fail(model.ErrForbidden("doctor identity required to save orthosis plan"))，
//   信封码 10403 ⇒ 展示层按码查表得到通用句「没有该操作的权限，请联系管理员」，用户读不出所以然。
// - 同矩阵其余端点（复查记录创建 review.go:29、模板管理 review_template.go:70、患者删除
//   admin_patient.go:307）的角色 allow-list 都含 ROLE_ADMIN，管理员照打是成功的
//   ⇒ 闸门只收「后端真要求医生身份」的这一条，其余不动，免得把能用的按钮锁死（T629 验收第 3 条）。
import type { RoleKey } from '../router/permissions'

/** 友好提示语（T629 派发单 §三：明确告知「该功能仅对医生开放 / 当前用户无权限」） */
export const DOCTOR_ONLY_HINT = '该功能仅对医生开放，当前账号无权限，请由医生账号操作'

/**
 * 是否可保存矫形方案（POST /api/v1/patients/:id/orthosis-plans）。
 *
 * 只有 doctor 放行：未知角色（roleId 映射失败为 null）与未登录同向 fail-closed，
 * 与路由守卫、patientPasswordAccess 一致。
 */
export function canSaveOrthosisPlan(role: RoleKey | null | undefined): boolean {
  return role === 'doctor'
}
