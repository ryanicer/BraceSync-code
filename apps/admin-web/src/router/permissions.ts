// 预置角色与权限矩阵（对齐 PRD §7D.11 预置角色权限矩阵）
export type RoleKey = 'admin' | 'doctor' | 'cs'

export interface RoleInfo {
  key: RoleKey
  name: string
  description: string
}

export const PRESET_ROLES: RoleInfo[] = [
  { key: 'admin', name: '运营管理员', description: '全量数据，无团队隔离，可访问全部 12 页' },
  { key: 'doctor', name: '医生', description: '仅限本团队患者数据（矫形日志/告警/实时监控）' },
  { key: 'cs', name: '客服', description: '仅患者沟通模块，全量患者（查看与标记）' },
]

export function roleName(key: RoleKey | string): string {
  const found = PRESET_ROLES.find((r) => r.key === key)
  return found ? found.name : String(key)
}

// 后端 roleId（登录响应 roleId / admins.role_id）→ 前端 RoleKey 映射（T046 真实登录）
export const ROLE_ID_TO_KEY: Record<string, RoleKey> = {
  ROLE_ADMIN: 'admin',
  ROLE_DOCTOR: 'doctor',
  ROLE_CS: 'cs',
}

/** 未知 roleId 返回 null（守卫 fail-closed → 403） */
export function roleKeyFromRoleId(roleId: string): RoleKey | null {
  return ROLE_ID_TO_KEY[roleId] ?? null
}

/**
 * 预置角色 × 页面 权限矩阵（PRD §7D.11 表格倒推）。
 * 数据范围规则（医生仅本团队 / 客服仅查看标记）由后端 RBAC + 页面内提示体现，
 * 前端一期按角色过滤可见页面。
 */
export const ROLE_PAGE_MATRIX: Record<RoleKey, string[]> = {
  admin: [
    '/dashboard',
    '/monitor',
    '/patients',
    '/teams',
    '/devices',
    '/alerts',
    '/communication',
    '/orthosis-log',
    '/install-records',
    '/technicians',
    '/roles',
    '/settings',
  ],
  doctor: ['/dashboard', '/monitor', '/alerts', '/orthosis-log'],
  cs: ['/communication'],
}

export function canAccess(role: RoleKey | string, path: string): boolean {
  const pages = ROLE_PAGE_MATRIX[role as RoleKey]
  if (!pages) return false
  return pages.includes(path)
}
