import { expect, type Page, type Locator } from '@playwright/test'

/**
 * admin-web E2E 公共 helper（T029）
 *
 * 选择器策略：admin-web（T020）业务代码不加 data-testid（红线：不碰 apps/admin-web），
 * 用例基于 Element Plus 稳定类名（.el-menu-item/.el-select-dropdown 等）+ 中文文案定位。
 * Element Plus 的 el-select / el-dialog / el-message 等组件渲染为 teleport 到 body 的
 * popper，选择器需用全局可见域（:visible）而非限定在 page 局部容器内。
 */

/** 预置角色（对齐 stores/auth.ts MOCK_ACCOUNTS 与 router/permissions.ts PRESET_ROLES） */
export type AdminRole = 'admin' | 'doctor' | 'cs'

export const ROLE_ACCOUNT: Record<AdminRole, { name: string; label: string }> = {
  admin: { name: '运营管理员', label: '运营管理员' },
  doctor: { name: '张建国医生', label: '医生' },
  cs: { name: '客服小美', label: '客服' },
}

/** 12 页路由（对齐 router/index.ts pageRoutes） */
export const adminRoutes = {
  login: '/login',
  forbidden: '/403',
  dashboard: '/dashboard',
  monitor: '/monitor',
  patients: '/patients',
  teams: '/teams',
  devices: '/devices',
  alerts: '/alerts',
  communication: '/communication',
  orthosisLog: '/orthosis-log',
  installRecords: '/install-records',
  technicians: '/technicians',
  roles: '/roles',
  settings: '/settings',
} as const

/** admin 全量 12 页路径（权限矩阵 ROLE_PAGE_MATRIX.admin） */
export const ADMIN_PAGES: string[] = [
  '/dashboard', '/monitor', '/patients', '/teams', '/devices', '/alerts',
  '/communication', '/orthosis-log', '/install-records', '/technicians', '/roles', '/settings',
]

/** doctor 可见 4 页 / cs 可见 1 页（ROLE_PAGE_MATRIX） */
export const DOCTOR_PAGES: string[] = ['/dashboard', '/monitor', '/alerts', '/orthosis-log']
export const CS_PAGES: string[] = ['/communication']

/**
 * 在当前可见的 Element Plus 下拉面板中选择选项。
 * el-select 的 dropdown teleport 到 body，用 :visible 限定当前打开的面板。
 */
export async function pickSelectOption(page: Page, select: Locator, optionText: string): Promise<void> {
  await select.click()
  const option = page.locator('.el-select-dropdown:visible .el-select-dropdown__item').filter({ hasText: optionText })
  await option.first().click()
  // 等待下拉收起，避免遮挡后续点击
  await expect(page.locator('.el-select-dropdown:visible')).toHaveCount(0, { timeout: 5_000 }).catch(() => { /* 个别 EP 版本收起动画较慢，不阻塞 */ })
}

/**
 * mock 角色登录：进入 /login → 选择角色 → 点击登录。
 * admin 为默认选中角色，其余角色需打开下拉选择。
 */
export async function adminLogin(page: Page, role: AdminRole): Promise<void> {
  await page.goto(adminRoutes.login)
  if (role !== 'admin') {
    await pickSelectOption(page, page.locator('.login-form .el-select'), ROLE_ACCOUNT[role].label)
  }
  await page.locator('.login-form input[type="password"]').fill('mock-password')
  await page.locator('.login-form').getByRole('button', { name: '登录' }).click()
  // 登录成功即离开 /login（cs 默认落地 /403，admin/doctor 落地 /dashboard）
  await page.waitForURL((url) => !url.pathname.startsWith('/login'), { timeout: 10_000 })
}

/** ElMessage 全局提示（teleport 到 body；多条消息会堆叠，取最新一条） */
export function adminMessage(page: Page): Locator {
  return page.locator('.el-message').last()
}

/** 侧边栏菜单项 */
export function menuItems(page: Page): Locator {
  return page.locator('.el-menu .el-menu-item')
}

/** 点击侧边栏菜单（按文案） */
export async function gotoMenu(page: Page, title: string): Promise<void> {
  await menuItems(page).filter({ hasText: title }).click()
}

/** 表格行（Element Plus el-table body 行） */
export function tableRows(page: Page, scope?: Locator): Locator {
  const root = scope ?? page
  return root.locator('.el-table__body-wrapper tbody tr')
}

/** 顶栏用户名 */
export function topBarUserName(page: Page): Locator {
  return page.locator('.top-nav-right .user-name')
}

/** 退出登录（含 ElMessageBox 确认） */
export async function adminLogout(page: Page): Promise<void> {
  await page.locator('.top-nav-right').getByRole('button', { name: '退出' }).click()
  await page.locator('.el-message-box').getByRole('button', { name: '确定' }).click()
  await page.waitForURL('**/login', { timeout: 10_000 })
}
