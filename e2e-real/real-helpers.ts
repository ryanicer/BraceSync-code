import { expect, type Page, type Locator } from '@playwright/test'

/*
 * ⚠️ 本文件为「真实模式」E2E 专用 helper（T053）。
 * 禁止使用 e2e/admin-helpers.ts 中的 adminLogin()（mock 角色下拉登录）。
 * 登录必须使用本文件提供的 realLogin()（用户名 + 密码 → 真实接口）。
 *
 * 真实 staging 预置账号（T051 seed）：
 *   ops_admin / admin123   （运营，全权限）
 */

// ─────────────────────────────────────────────────────────────
// Re-export 通用 selector helper（直接复用 mock admin-helpers）
// ─────────────────────────────────────────────────────────────
export {
  adminRoutes,
  pickSelectOption,
  tableRows,
  menuItems,
  gotoMenu,
  adminMessage,
  topBarUserName,
  adminLogout,
} from '../e2e/admin-helpers'

// ─────────────────────────────────────────────────────────────
// 资源命名前缀 + 唯一命名工具（写操作可重放，避免污染 seed）
// ─────────────────────────────────────────────────────────────
export const E2E_PATIENT_NAME_PREFIX = 'T053测试'
export const E2E_TEAM_NAME_PREFIX = 'T053团队'
export const E2E_REPLY_PREFIX = 'T053回复'

/**
 * 生成带时间戳的唯一资源名（6 位秒级后缀，staging 单人测试足够唯一）。
 *   uniqueName(E2E_TEAM_NAME_PREFIX) → "T053团队-123456"
 */
export function uniqueName(prefix: string): string {
  const ts = Date.now().toString().slice(-6)
  return `${prefix}-${ts}`
}

// ─────────────────────────────────────────────────────────────
// 真实模式登录：用户名 + 密码（不是 mock 角色下拉）
// ─────────────────────────────────────────────────────────────

/** 默认 staging 运营账号（T051 seed） */
export const DEFAULT_REAL_USERNAME = 'ops_admin'
export const DEFAULT_REAL_PASSWORD = 'admin123'

/**
 * 真实模式登录（USE_MOCK=false 下的 login 页表单）。
 * 对齐 apps/admin-web/src/pages/login/index.vue 「v-else 真实模式」结构：
 *   - 用户名框：.login-form 下第一个非 password input（el-input 包装 input）
 *   - 密码框：.login-form input[type="password"]
 *   - 登录按钮：文案「登 录」（中间有空格，对齐 T052 实跑成功案例）
 */
export async function realLogin(
  page: Page,
  username: string = DEFAULT_REAL_USERNAME,
  password: string = DEFAULT_REAL_PASSWORD,
): Promise<void> {
  await page.goto('/login', { waitUntil: 'domcontentloaded' })
  // 等待登录卡片渲染
  await expect(page.locator('.login-card')).toBeVisible({ timeout: 15_000 })

  // 用户名：.login-form 下「未带 type=password」的第一个可输入 input
  const usernameInput = page.locator('.login-form input:not([type="password"])').first()
  const passwordInput = page.locator('.login-form input[type="password"]')
  const loginBtn = page
    .locator('.login-form')
    .getByRole('button', { name: '登 录' })

  await expect(usernameInput).toBeVisible()
  await expect(passwordInput).toBeVisible()
  await expect(loginBtn).toBeVisible()

  // 清空前先点击聚焦，再 fill（避免 el-input 残留 value）
  await usernameInput.click()
  await usernameInput.fill('')
  await usernameInput.fill(username)

  await passwordInput.click()
  await passwordInput.fill('')
  await passwordInput.fill(password)

  await loginBtn.click()

  // 登录成功：离开 /login（成功跳 dashboard 或 redirect 查询参数指定的路径；
  // 失败也会变 URL，但这里用 waitForURL 非登录页路径 + 同时用 ElMessage 兜底）
  try {
    await page.waitForURL((url) => !url.pathname.startsWith('/login'), { timeout: 20_000 })
  } catch {
    // 兜底：如果被 redirect 回 /login（账号异常），不抛，由上层断言判断
  }
}

// ─────────────────────────────────────────────────────────────
// 更多辅助工具
// ─────────────────────────────────────────────────────────────

/** localStorage key（T052 实跑验证：admin_token + admin_user） */
export const LS_TOKEN_KEY = 'admin_token'
export const LS_USER_KEY = 'admin_user'

/** 从 localStorage 取 JWT token（用于 page.request 直接调 API 清理资源） */
export async function getAuthToken(page: Page): Promise<string | null> {
  return page.evaluate((k) => localStorage.getItem(k), LS_TOKEN_KEY)
}

/** 等待表格首行加载完成（列表通用） */
export async function waitForTableLoaded(page: Page): Promise<void> {
  await expect(page.locator('.el-table__body-wrapper tbody tr')).toHaveCount(
    (n) => n >= 1,
    { timeout: 20_000 },
  )
}

/** 取所有表格行中可见的 tag 文本（用于状态存在性验证） */
export async function getAllTagTexts(scope: Locator | Page): Promise<string[]> {
  const root: Locator = 'locator' in scope ? scope : scope.locator('body')
  return root.locator('.el-tag').allTextContents()
}
