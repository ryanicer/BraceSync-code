import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminLogout, adminMessage, menuItems, topBarUserName, pickSelectOption } from '../admin-helpers'

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
    await expect(options.nth(1)).toContainText('医护')
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
    await expect(page.locator('.top-nav-right .el-tag')).toContainText('医护')
  })

  test('客服登录直达患者沟通（T269 D3 订正：旧断言把「落 403」当期望，等于给缺陷盖章）', async ({ page }) => {
    await adminLogin(page, 'cs')
    // 客服无 dashboard 权限 ⇒ 落地页须是其矩阵内首页，而非 403
    await expect(page).toHaveURL(/\/communication/)
    await expect(page.locator('.forbidden-card')).toHaveCount(0)
    // 侧边栏只列有权页，且可从菜单进入患者沟通（A-FLOW-22 硬判据：不动地址栏、只点页上的东西走得通）
    await expect(menuItems(page)).toHaveCount(1)
    await expect(menuItems(page).first()).toContainText('患者沟通')
    await menuItems(page).filter({ hasText: '患者沟通' }).click()
    await expect(page).toHaveURL(/\/communication/)
    await expect(page.locator('.pane-title').first()).toHaveText('反馈列表')
    // 越权直达 /dashboard 才落 403，且 403 页给出可达路径
    await page.goto(adminRoutes.dashboard)
    await expect(page).toHaveURL(/\/403/)
    await expect(page.locator('.forbidden-card')).toContainText('403 · 无访问权限')
    await expect(page.locator('.forbidden-card')).toContainText('客服')
    await expect(page.locator('.forbidden-card .reachable')).toContainText('患者沟通')
    await page.locator('.forbidden-card .reachable-link').first().click()
    await expect(page).toHaveURL(/\/communication/)
    await expect(page.locator('.role-hint')).toContainText('客服角色：仅可查看反馈与标记处理状态')
    /**
     * D3 的回环本体：旧实现 403 页「返回首页」写死跳 /dashboard，客服点一次就被守卫
     * 弹回 /403，出不去。故必须真点这个按钮并断言落到有权页、且不再停在 403。
     */
    await page.goto(adminRoutes.dashboard)
    await expect(page).toHaveURL(/\/403/)
    await page.locator('.forbidden-card').getByRole('button', { name: '返回首页' }).click()
    await expect(page).toHaveURL(/\/communication/)
    await expect(page.locator('.forbidden-card')).toHaveCount(0)
  })

  test('切换角色下拉后登录生效', async ({ page }) => {
    await page.goto(adminRoutes.login)
    await pickSelectOption(page, page.locator('.login-form .el-select'), '医护')
    await expect(page.locator('.login-form .el-select')).toContainText('医护')
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
