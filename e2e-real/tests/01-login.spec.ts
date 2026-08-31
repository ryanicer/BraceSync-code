import { test, expect } from '@playwright/test'
import {
  realLogin,
  realLogout,
  adminMessage,
  topBarUserName,
  LS_TOKEN_KEY,
  realRoutes,
} from '../real-helpers'

/**
 * T053 - 01 登录模块（真实模式：用户名/密码 + JWT）
 * 覆盖：登录页渲染 / 登录成功 / 错误密码拒绝 / 路由守卫与退出
 */
test.describe('01-登录模块', () => {

  test.describe('登录页渲染', () => {
    test('1.1 显示标题 / 用户名框 / 密码框 / 登录按钮', async ({ page }) => {
      await page.goto(realRoutes.login)
      await expect(page.locator('.login-title')).toContainText('矫智通运营平台')
      // 用户名（非 password input）
      await expect(page.locator('.login-form input:not([type="password"])').first()).toBeVisible()
      // 密码
      await expect(page.locator('.login-form input[type="password"]')).toBeVisible()
      // 「登 录」按钮（注意中间空格）
      await expect(
        page.locator('.login-form').getByRole('button', { name: '登 录' }),
      ).toBeVisible()
    })
  })

  test.describe('真实账号登录成功', () => {
    test('1.2 ops_admin 登录 → Dashboard + localStorage JWT + 顶栏用户名', async ({ page }) => {
      await realLogin(page)
      // 1) 跳转到 /admin/dashboard
      await expect(page).toHaveURL(/\/admin\/dashboard/, { timeout: 20_000 })
      // 2) ElMessage 欢迎提示（非空即可，文案为「欢迎，xxx」）
      await expect(adminMessage(page)).toBeVisible({ timeout: 10_000 })
      // 3) 顶栏用户名非空（真实显示名由后端返回，不做精确匹配）
      const userName = topBarUserName(page)
      await expect(userName).toBeVisible()
      await expect(userName).toHaveText((t) => t.trim().length > 0, { timeout: 5_000 })
      // 4) localStorage 中存在 token
      const token = await page.evaluate((k) => localStorage.getItem(k), LS_TOKEN_KEY)
      expect(token).toBeTruthy()
      expect(typeof token).toBe('string')
      expect(token!.length).toBeGreaterThan(20) // JWT 长度一般 >100，但 20 作为最小阈值
    })
  })

  test.describe('错误密码拒绝', () => {
    test('1.3 错误密码 → ElMessage 「用户名或密码错误」 + 仍在登录页', async ({ page }) => {
      await realLogin(page, 'ops_admin', 'wrongpass123')
      // ElMessage 提示（真实后端统一错误文案，对齐 T052 经验）
      const msg = adminMessage(page)
      await expect(msg).toBeVisible({ timeout: 10_000 })
      await expect(msg).toContainText(/用户名或密码错误/)
      // 仍在 /admin/login（不跳 dashboard）
      const path = new URL(page.url()).pathname
      expect(path === '/admin/login' || path.endsWith('/admin/login')).toBe(true)
    })
  })

  test.describe('路由守卫与退出', () => {
    test('1.4 退出后访问受保护页被重定向到 /login?redirect=', async ({ page }) => {
      // 第一步：登录
      await realLogin(page)
      await expect(page).toHaveURL(/\/admin\/dashboard/, { timeout: 20_000 })
      // 第二步：退出
      await realLogout(page)
      await expect(page).toHaveURL(/\/admin\/login/, { timeout: 15_000 })
      // 第三步：确认 token 已清
      const tokenAfter = await page.evaluate((k) => localStorage.getItem(k), LS_TOKEN_KEY)
      expect(tokenAfter).toBeFalsy()
      // 第四步：直接访问 /admin/patients → 被重定向回 /admin/login 并带 redirect
      await page.goto(realRoutes.patients)
      await page.waitForTimeout(1_500) // 给前端守卫跳转留时间
      const urlAfter = page.url()
      expect(urlAfter).toContain('/admin/login')
      // redirect 参数应包含 patients（允许编码或原路径）
      expect(urlAfter).toMatch(/redirect=.*patients/)
    })
  })
})
