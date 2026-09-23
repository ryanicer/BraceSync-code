// 预置角色与权限矩阵（对齐 PRD §7D.11 预置角色权限矩阵）
export type RoleKey = 'admin' | 'doctor' | 'cs'

export interface RoleInfo {
  key: RoleKey
  name: string
  description: string
}

export const PRESET_ROLES: RoleInfo[] = [
  { key: 'admin', name: '运营管理员', description: '全量数据，无团队隔离，可访问全部页面' },
  { key: 'doctor', name: '医护', description: '仅限本团队患者数据（矫形日志/告警/实时监控/复查报告）' },
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
 * 页面模块词表（T345）：DB `roles.permissions_json.modules` 短键 与 前端路由路径 的唯一映射。
 *
 * 为什么要这张表：全仓原本两套词表并存且无映射 —— 库里存短键（`realtime` `comm` `perm`），
 * 路由与权限页用路径（`/monitor` `/communication` `/roles`），于是权限页勾选的是路径、
 * 落库要存短键，两端各说各话。
 * key 与后端 roleTemplates（`services/user-service/internal/handler/roles_t252.go`）、
 * 子权限目录（`permissions_t257.go` 的 Module）同一套词表；顺序与 `router/index.ts` 的
 * pageRoutes 一致（= 设计稿侧栏顺序），页面中文标签取路由 `meta.title`，此处不重复登记。
 *
 * 🔴 本表 15 项而非设计稿侧栏 16 项：「异常报告」既无独立路由也无模块键，是否新建待 Boss 裁
 * （T345 挂起项，见 docs PRD §7D 与 `docs/tasks/joe/T345-逐页对照清单-16页.md` §4）。
 */
export interface PageModule {
  key: string
  path: string
}

export const PAGE_MODULES: PageModule[] = [
  { key: 'dashboard', path: '/dashboard' },
  { key: 'realtime', path: '/monitor' },
  { key: 'patients', path: '/patients' },
  { key: 'teams', path: '/teams' },
  { key: 'devices', path: '/devices' },
  { key: 'alerts', path: '/alerts' },
  { key: 'comm', path: '/communication' },
  { key: 'orthosis', path: '/orthosis-log' },
  { key: 'install', path: '/install-records' },
  { key: 'review', path: '/review-records' }, // T345 补键（ROLE_PAGE_MATRIX 已放开、库里缺）
  { key: 'review_tpl', path: '/review-templates' }, // T345 补键（同上）
  { key: 'tech', path: '/technicians' },
  { key: 'doctor_acct', path: '/doctor-accounts' }, // T345 补键（T315 加了页面没加模块键）
  { key: 'perm', path: '/roles' },
  { key: 'config', path: '/settings' },
]

export function moduleKeyOfPath(path: string): string | undefined {
  return PAGE_MODULES.find((m) => m.path === path)?.key
}

export function pathOfModule(key: string): string | undefined {
  return PAGE_MODULES.find((m) => m.key === key)?.path
}

/** 预置角色的模块短键（由路径矩阵换算，供 mock 与断言用；不代表库里该角色的实际值） */
export function modulesForRole(role: RoleKey | string): string[] {
  const pages = ROLE_PAGE_MATRIX[role as RoleKey] ?? []
  return pages.map((p) => moduleKeyOfPath(p)).filter((k): k is string => !!k)
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
