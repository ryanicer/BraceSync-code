// T728：设备验签密钥入口的角色闸门与批量限额（纯层）。
//
// 单独成文件（不 import 页面 SFC）的理由同 utils/patientPasswordAccess.ts：
// 本项目页面 SFC 在 vitest 里挂载成本高于收益，判据抽成纯层才测得到真值表。
import type { RoleKey } from '../router/permissions'

/**
 * 是否允许看到 / 点动「下载密钥」「批量下载」「重新生成」。
 *
 * 依据链：网关 `services/gateway/cmd/server/rbac.go` 把三条端点收在 adminOnlyPatterns
 * （T260 起未登记路径默认 403），device-service 侧 `handler.assertAdminRole` 是同判据的第二层，
 * 且是 allow-list 单值而非 deny-list —— 能取密钥＝能伪造设备上报，医护/客服连读面都不给开。
 *
 * ⇒ 前端闸门按后端真源写死；未知角色（roleId 映射失败为 null）与其它闸门同向 fail-closed，
 *   放出去也只是多点一次必吃 403 的按钮。
 */
export function canAccessDeviceSecret(role: RoleKey | null | undefined): boolean {
  return role === 'admin'
}

/**
 * 单次批量下载的设备数上限，与 device-service `service.MaxSecretExportBatch`（T728）同源。
 * 后端是真正的墙，这一格只是让页面在发枪前就把「勾选过多」讲清楚；改一侧必须同步另一侧。
 */
export const MAX_SECRET_EXPORT_BATCH = 50
