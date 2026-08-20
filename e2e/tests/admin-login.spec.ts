import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminLogout, adminMessage, topBarUserName, pickSelectOption } from '../admin-helpers'

/**
 * admin-web 登录：三角色 mock 预置账号登录 + 未登录守卫 + 退出
 * 对齐 T020 login 页（角色下拉选择，mock 阶段任意密码）
 */

test.describe('登录页渲染', () => {
  test('显示标题 / 角色下拉 / 密码输入 / 登录按钮', async ({ page }) => {
    await page.goto(adminRoutes.login)
    await expect(page.locator('.login-title')).toContainText('矫智通运营平台')
    await expect(page.locator('.login-form .el-select')).toBeVisible()
    await expect(page.locator('.login-form input[type="password"]')).toBeVisible()
    await expect(page.locator('.login-form').getByRole('button', { name: '登录' })).toBeVisible()
  })

  test('角色下拉包含三个预置角色', async ({ page }) => {
    await page.goto(adminRoutes.login)
    await page.locator('.login-form .el-select').click()
    const options = page.locator('.el-select-dropdown:visible .el-select-dropdown__item')
    await expect(options).toHaveCount(3)
    await expect(options.nth(0)).toContainText('运营管理员')
    await expect(options.nth(1)).toContainText('医生')
    await expect(options.nth(2)).toContainText('客服')
  })
})

test.describe('三角色登录', () => {
  test('运营管理员登录进入 Dashboard', async ({ page }) => {
    await adminLogin(page, 'admin')
    await expect(page).toHaveURL(/\/dashboard/)
    await expect(adminMessage(page)).toContainText('欢迎，运营管理员')
    await expect(topBarUserName(page)).toHaveText('运营管理员')
  })

  test('医生登录进入 Dashboard', async ({ page }) => {
    await adminLogin(page, 'doctor')
    await expect(page).toHaveURL(/\/dashboard/)
    await expect(adminMessage(page)).toContainText('欢迎，张建国医生')
    await expect(topBarUserName(page)).toHaveText('张建国医生')
    await expect(page.locator('.top-nav-right .el-tag')).toContainText('医生')
  })

  test('客服登录后默认落地 403（无 dashboard 权限，唯一可见页为患者沟通）', async ({ page }) => {
    await adminLogin(page, 'cs')
    await expect(page).toHaveURL(/\/403/)
    await expect(page.locator('.forbidden-card')).toContainText('403 · 无访问权限')
    await expect(page.locator('.forbidden-card')).toContainText('客服')
    // 客服手动进入唯一授权页正常
    await page.goto(adminRoutes.communication)
    await expect(page).toHaveURL(/\/communication/)
    await expect(page.locator('.role-hint')).toContainText('客服角色：仅可查看反馈与标记处理状态')
  })

  test('切换角色下拉后登录生效', async ({ page }) => {
    await page.goto(adminRoutes.login)
    await pickSelectOption(page, page.locator('.login-form .el-select'), '医生')
    await expect(page.locator('.login-form .el-select')).toContainText('医生')
  })
})

test.describe('登录守卫与退出', () => {
  test('未登录访问受保护页重定向到 /login 并带 redirect', async ({ page }) => {
    await page.goto(adminRoutes.patients)
    await expect(page).toHaveURL(/\/login\?redirect=.*patients/)
    await expect(page.locator('.login-title')).toBeVisible()
  })

  test('登录后按 redirect 回跳目标页', async ({ page }) => {
    await page.goto(`${adminRoutes.alerts}`)
    await expect(page).toHaveURL(/\/login\?redirect=.*alerts/)
    await page.locator('.login-form input[type="password"]').fill('mock-password')
    await page.locator('.login-form').getByRole('button', { name: '登录' }).click()
    await expect(page).toHaveURL(/\/alerts/)
  })

  test('刷新页面保持登录态（localStorage 持久化）', async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.reload()
    await expect(page).toHaveURL(/\/dashboard/)
    await expect(topBarUserName(page)).toHaveText('运营管理员')
  })

  test('退出登录回到登录页', async ({ page }) => {
    await adminLogin(page, 'admin')
    await adminLogout(page)
    await expect(page.locator('.login-title')).toBeVisible()
    // 退出后再访问受保护页仍被拦截
    await page.goto(adminRoutes.dashboard)
    await expect(page).toHaveURL(/\/login/)
  })
})
