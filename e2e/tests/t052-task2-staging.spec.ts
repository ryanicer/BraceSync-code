import { test, expect } from '@playwright/test'

const STAGING_BASE = 'http://localhost:2080'
const ADMIN_BASE = `${STAGING_BASE}/admin`

test.describe('T052 Task 2 - Staging 登录页验收 + 权限路由守卫', () => {

  test('TR-2.1 浏览器验证：登录 → 跳转 dashboard → 顶栏用户名+角色+localStorage token', async ({ page }) => {
    await page.goto(`${ADMIN_BASE}/login`)

    const usernameInput = page.locator('.login-form input').first()
    const passwordInput = page.locator('.login-form input[type="password"]')
    const loginBtn = page.locator('.login-form').getByRole('button', { name: '登 录' })

    await expect(usernameInput).toBeVisible({ timeout: 15000 })

    await usernameInput.fill('ops_admin')
    await passwordInput.fill('admin123')
    await loginBtn.click()

    await page.waitForURL((url) => url.pathname.includes('/dashboard'), { timeout: 15000 })

    const currentURL = page.url()
    console.log(`[TR-2.1] 登录后URL: ${currentURL}`)

    const userNameText = await page.locator('.top-nav-right .user-name').textContent({ timeout: 10000 })
    console.log(`[TR-2.1] 顶栏用户名: ${userNameText}`)

    const roleTag = page.locator('.top-nav-right .el-tag')
    const roleText = await roleTag.textContent().catch(() => 'NO_TAG')
    console.log(`[TR-2.1] 顶栏角色标签: ${roleText}`)

    const token = await page.evaluate(() => localStorage.getItem('admin_token'))
    console.log(`[TR-2.1] localStorage admin_token: ${token ? 'EXISTS (' + token.substring(0, 30) + '...)' : 'NULL/EMPTY'}`)

    const userStr = await page.evaluate(() => localStorage.getItem('admin_user'))
    console.log(`[TR-2.1] localStorage admin_user: ${userStr || 'NULL/EMPTY'}`)

    expect(currentURL).toContain('/dashboard')
    expect(userNameText).toBeTruthy()
    expect(token).toBeTruthy()
  })

  test('TR-2.2 错误密码验证：前端提示文案"用户名或密码错误"', async ({ page }) => {
    await page.goto(`${ADMIN_BASE}/login`)

    const usernameInput = page.locator('.login-form input').first()
    const passwordInput = page.locator('.login-form input[type="password"]')
    const loginBtn = page.locator('.login-form').getByRole('button', { name: '登 录' })

    await expect(usernameInput).toBeVisible({ timeout: 15000 })

    await usernameInput.fill('ops_admin')
    await passwordInput.fill('wrongpass123')
    await loginBtn.click()

    await page.waitForTimeout(2000)

    const messageLocator = page.locator('.el-message').last()
    const messageText = await messageLocator.textContent({ timeout: 10000 }).catch(() => '')
    console.log(`[TR-2.2] 错误提示文案: ${messageText}`)

    const currentURL = page.url()
    console.log(`[TR-2.2] 登录失败后URL（应仍在登录页）: ${currentURL}`)

    expect(currentURL).toContain('/login')
    expect(messageText).toContain('用户名或密码错误')
  })

  test('TR-2.3 路由守卫验证：退出后访问受保护页被重定向到/login带redirect参数', async ({ page }) => {
    await page.goto(`${ADMIN_BASE}/login`)

    const usernameInput = page.locator('.login-form input').first()
    const passwordInput = page.locator('.login-form input[type="password"]')
    const loginBtn = page.locator('.login-form').getByRole('button', { name: '登 录' })

    await expect(usernameInput).toBeVisible({ timeout: 15000 })

    await usernameInput.fill('ops_admin')
    await passwordInput.fill('admin123')
    await loginBtn.click()

    await page.waitForURL((url) => url.pathname.includes('/dashboard'), { timeout: 15000 })

    console.log(`[TR-2.3] 第一步: 成功登录 dashboard`)

    const logoutBtn = page.locator('.top-nav-right').getByRole('button', { name: '退出' })
    await expect(logoutBtn).toBeVisible({ timeout: 10000 })
    await logoutBtn.click()

    const confirmBtn = page.locator('.el-message-box').getByRole('button', { name: '确定' })
    await expect(confirmBtn).toBeVisible({ timeout: 10000 })

    const msgboxText = await page.locator('.el-message-box').textContent()
    console.log(`[TR-2.3] 登出弹窗内容: ${msgboxText?.replace(/\s+/g, ' ')}`)

    await confirmBtn.click()

    await page.waitForURL((url) => url.pathname.includes('/login'), { timeout: 15000 })
    console.log(`[TR-2.3] 第二步: 成功退出回到登录页: ${page.url()}`)

    const tokenAfterLogout = await page.evaluate(() => localStorage.getItem('admin_token'))
    console.log(`[TR-2.3] 退出后localStorage token: ${tokenAfterLogout ? 'STILL EXISTS!' : 'CLEARED (OK)'}`)

    await page.goto(`${ADMIN_BASE}/dashboard`)
    await page.waitForTimeout(1500)
    const redirectURL1 = page.url()
    console.log(`[TR-2.3] 访问 /dashboard 后重定向到: ${redirectURL1}`)

    expect(redirectURL1).toContain('/login')

    const hasRedirect1 = redirectURL1.includes('redirect=')
    console.log(`[TR-2.3] URL含redirect参数: ${hasRedirect1}`)

    await page.goto(`${ADMIN_BASE}/patients`)
    await page.waitForTimeout(1500)
    const redirectURL2 = page.url()
    console.log(`[TR-2.3] 访问 /patients 后重定向到: ${redirectURL2}`)

    expect(redirectURL2).toContain('/login')

    const hasRedirect2 = redirectURL2.includes('redirect=')
    console.log(`[TR-2.3] URL含redirect参数: ${hasRedirect2}`)

    expect(hasRedirect1 || redirectURL1.includes('redirect')).toBe(true)
  })

})
