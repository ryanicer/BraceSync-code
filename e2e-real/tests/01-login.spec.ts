import { test, expect, type Page } from '@playwright/test'
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
 *
 * T279 补：1.5 = 验收卡 A-FLOW-03 步骤 4「空用户名前端拦截」（docs/tests/acceptance/admin/核心流程.md:68）。
 *          mock 侧结构上验不了（登录页只有角色下拉，没有用户名框），故只能在真实模式补。
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
      // 1) 跳转到 /dashboard（应用 vue-router base=/，根路径）
      await expect(page).toHaveURL(/\/dashboard/, { timeout: 20_000 })
      // 2) ElMessage 欢迎提示（非空即可，文案为「欢迎，xxx」）
      await expect(adminMessage(page)).toBeVisible({ timeout: 10_000 })
      // 3) 顶栏用户名非空（真实显示名由后端返回，不做精确匹配）
      const userName = topBarUserName(page)
      await expect(userName).toBeVisible()
      await expect(userName).not.toBeEmpty({ timeout: 5_000 })
      // 4) localStorage 中存在 token
      const token = await page.evaluate((k) => localStorage.getItem(k), LS_TOKEN_KEY)
      expect(token).toBeTruthy()
      expect(typeof token).toBe('string')
      expect(token!.length).toBeGreaterThan(20) // JWT 长度一般 >100，但 20 作为最小阈值
    })
  })

  test.describe('错误密码拒绝', () => {
    test('1.3 错误密码 → ElMessage 「用户名或密码错误」 + 仍在登录页', async ({ page }) => {
      // 内联登录：不用 realLogin（其 waitForURL 在错误密码时会白等 20s，期间 ElMessage 已消失）
      await page.goto(realRoutes.login, { waitUntil: 'domcontentloaded' })
      await expect(page.locator('.login-card')).toBeVisible({ timeout: 15_000 })
      const userInput = page.locator('.login-form input:not([type="password"])').first()
      const passInput = page.locator('.login-form input[type="password"]')
      await userInput.click()
      await userInput.fill('ops_admin')
      await passInput.fill('wrongpass123')
      await page.locator('.login-form').getByRole('button', { name: '登 录' }).click()
      // 立即检查 ElMessage（错误密码 URL 不会离开 /login，不等 URL 变化）
      const msg = adminMessage(page)
      await expect(msg).toBeVisible({ timeout: 10_000 })
      await expect(msg).toContainText(/用户名或密码错误/)
      // 仍在 /login（不跳 dashboard）
      const path = new URL(page.url()).pathname
      expect(path === '/login' || path.endsWith('/login')).toBe(true)
    })
  })

  test.describe('路由守卫与退出', () => {
    test('1.4 退出后访问受保护页被重定向到 /login?redirect=', async ({ page }) => {
      // 第一步：登录
      await realLogin(page)
      await expect(page).toHaveURL(/\/dashboard/, { timeout: 20_000 })
      // 第二步：退出
      await realLogout(page)
      await expect(page).toHaveURL(/\/login/, { timeout: 15_000 })
      // 第三步：确认 token 已清
      const tokenAfter = await page.evaluate((k) => localStorage.getItem(k), LS_TOKEN_KEY)
      expect(tokenAfter).toBeFalsy()
      // 第四步：直接访问受保护页 → 被守卫重定向回 /login 并带 redirect 参数
      // 只验证 redirect 参数存在（不绑死具体值——绑死就会踩到下面这条实测结论）
      await page.goto(realRoutes.patients)
      await page.waitForTimeout(1_500) // 给前端守卫跳转留时间
      const urlAfter = page.url()
      expect(urlAfter).toContain('/login')
      expect(urlAfter).toMatch(/redirect=/)
      // ⚠️ T279 实测（headless，未登录 new context 逐个试过 /admin/patients、/patients、/admin/teams）：
      //    三者一律落到 /login?redirect=/dashboard —— redirect 恒为 /dashboard，从不回填原目标页。
      //    ⇒ 本用例守的是「未登录进不去 + 会跳回登录页」；「登录后回跳到原目标页」这半步在 staging
      //    根本不可能成立，故未断言，已作为缺陷登记（T279 报告 F-2）。别把本条读成 A-FLOW-02 的回跳已覆盖。
    })
  })

  test.describe('空用户名前端拦截（T279 补 A-FLOW-03 步骤 4）', () => {
    test('1.5 用户名留空点登录 → 字段下方红字「请输入用户名」+ 不发任何登录请求', async ({ page }) => {
      // 计数口径：真实网络请求（不用 page.route 改写响应，只观察）
      const loginCalls: string[] = []
      page.on('request', (req) => {
        if (req.url().includes('/api/v1/auth/login')) loginCalls.push(`${req.method()} ${req.url()}`)
      })

      await page.goto(realRoutes.login, { waitUntil: 'domcontentloaded' })
      await expect(page.locator('.login-card')).toBeVisible({ timeout: 15_000 })

      // 只填密码，用户名保持空串（验收卡步骤 4：用户名留空 + 密码 admin123）
      const usernameInput = page.locator('.login-form input:not([type="password"])').first()
      await expect(usernameInput).toHaveValue('')
      await page.locator('.login-form input[type="password"]').fill('admin123')
      await page.locator('.login-form').getByRole('button', { name: '登 录' }).click()

      // 1) 前端表单校验红字出现在「用户名」字段下方（el-form-item__error 挂在对应 item 内）
      const usernameItem = page
        .locator('.login-form .el-form-item')
        .filter({ hasText: '用户名' })
        .first()
      await expect(usernameItem.locator('.el-form-item__error')).toHaveText('请输入用户名')

      // 2) 零请求：给可能的异步发送留出观察窗口后再判定
      await page.waitForTimeout(2_000)
      expect(loginCalls).toEqual([])

      // 3) 前端拦截 ⇒ 不该出现后端错误提示（后端原文是「invalid username or password」）
      await expect(adminMessage(page)).toHaveCount(0)

      // 4) 仍停在登录页，没有进入任何后台页
      expect(new URL(page.url()).pathname).toBe('/login')
      await expect(page.locator('.el-menu')).toHaveCount(0)
    })
  })

  test.describe('不存在的用户名（T270 收尾补 A-FLOW-03 步骤 3）', () => {
    /** 用错凭据发一次真实登录，回读错误提示原文（不 await URL 变化：失败时本就不跳） */
    async function tryLogin(page: Page, username: string, password: string): Promise<string> {
      await page.goto(realRoutes.login, { waitUntil: 'domcontentloaded' })
      await expect(page.locator('.login-card')).toBeVisible({ timeout: 15_000 })
      const userInput = page.locator('.login-form input:not([type="password"])').first()
      const passInput = page.locator('.login-form input[type="password"]')
      await userInput.click()
      await userInput.fill(username)
      await passInput.fill(password)
      await page.locator('.login-form').getByRole('button', { name: '登 录' }).click()
      const msg = adminMessage(page)
      await expect(msg).toBeVisible({ timeout: 10_000 })
      const text = (await msg.textContent())?.trim() ?? ''
      // 等这条 toast 自己消失，否则下一次抓文案会抓到同一条（adminMessage 取首个 .el-message）
      await expect(adminMessage(page)).toHaveCount(0, { timeout: 10_000 })
      return text
    }

    test('1.6 用户名不存在 → 与「密码错误」逐字同文案（不泄露账号是否存在）+ 停在 /login + 不签发 token', async ({
      page,
    }) => {
      // A-FLOW-03 步骤 1（ops_admin + 错密码）与步骤 3（不存在的用户名 + 正确密码形状）
      const wrongPass = await tryLogin(page, 'ops_admin', 'WrongPass123')
      const noSuchUser = await tryLogin(page, 'no_such_user_xyz', 'admin123')

      // 1) 两次必须是同一句提示 —— 后端/前端都不得区分「用户不存在」与「密码错」（枚举防护）
      expect(noSuchUser).toBe(wrongPass)
      expect(noSuchUser.length).toBeGreaterThan(0)
      expect(noSuchUser).toMatch(/用户名或密码错误/)

      // 2) 未签发登录态
      expect(new URL(page.url()).pathname).toBe('/login')
      const token = await page.evaluate((k) => localStorage.getItem(k), LS_TOKEN_KEY)
      expect(token).toBeFalsy()
      await expect(page.locator('.el-menu')).toHaveCount(0)
    })
  })
})
