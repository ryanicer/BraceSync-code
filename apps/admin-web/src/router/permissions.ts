// 预置角色与权限矩阵（对齐 PRD §7D.11 预置角色权限矩阵）
export type RoleKey = 'admin' | 'doctor' | 'cs'

export interface RoleInfo {
  key: RoleKey
  name: string
  description: string
}

export const PRESET_ROLES: RoleInfo[] = [
  { key: 'admin', name: '运营管理员', description: '全量数据，无团队隔离，可访问全部页面' },
  { key: 'doctor', name: '医生', description: '仅限本团队患者数据（矫形日志/告警/实时监控/复查报告）' },
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
    '/review-records',
    '/review-templates', // T135 复查模板管理
    '/technicians',
    '/doctor-accounts', // T315 医护账号（设计稿 医护账号.html:184 要求登记路径 id；账号管理页，仅运营管理员）
    '/roles',
    '/settings',
  ],
  doctor: ['/dashboard', '/monitor', '/alerts', '/orthosis-log', '/review-records', '/review-templates'], // T135 医生可下载空白模板
  cs: ['/communication'],
}

export function canAccess(role: RoleKey | string, path: string): boolean {
  const pages = ROLE_PAGE_MATRIX[role as RoleKey]
  if (!pages) return false
  return pages.includes(path)
}

/**
 * 各角色登录 / 「返回首页」的落地页（T269 D3）。
 * 写死 /dashboard 会让无 dashboard 权限的客服落 403 后点不回有权页，形成死循环。
 * 必须落在该角色有权访问的页面内（由 permissions.spec.ts 断言防漂移）。
 */
export const ROLE_HOME_PAGE: Record<RoleKey, string> = {
  admin: '/dashboard',
  doctor: '/dashboard',
  cs: '/communication',
}

/** 未知角色（roleId 映射失败，role=null）→ 无落地页，交守卫 fail-closed 落 403 */
export function landingPathFor(role: RoleKey | string | null | undefined): string {
  return role ? ROLE_HOME_PAGE[role as RoleKey] ?? '/403' : '/403'
}
