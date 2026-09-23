import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, ADMIN_MOUNT, isLoginPath } from '../admin-helpers'

/**
 * T336 挂载点 / 深链回归（admin-web）
 *
 * 卡住的现场：staging 把后台挂在 /admin/ 下，而 SPA 按根路径构建，
 * 于是 /admin/patients 在路由表里匹配不到 → 落 catch-all → 弹回首页，
 * 表现为「深链打不开、子路由刷新丢页面、登录后回不到原页」。
 *
 * mock 用例跑的就是带 base 的 dev server（http://localhost:PORT/admin/），
 * 所以这三条在 PR 门禁里就等价复现了挂载点行为，不必等 staging 部署。
 */

/** 不带登录态直访深链：等守卫改跳登录页（地址仍须在挂载点内） */
async function gotoDeepLink(page: import('@playwright/test').Page, path: string): Promise<void> {
  await page.goto(path)
  await page.waitForURL((url) => isLoginPath(url.pathname), { timeout: 15_000 })
}

test.describe('挂载点与深链（T336）', () => {
  test('深链直达加载的是挂载点内的构建：入口脚本带 /admin/ 前缀', async ({ page }) => {
    await page.goto(adminRoutes.patients)
    const entrySrc = await page.evaluate(() => document.querySelector('script[src]')?.getAttribute('src') ?? '')
    expect(entrySrc, 'index.html 的入口脚本必须挂在挂载点下，否则线上取到的是根路径资源').toMatch(
      new RegExp(`^${ADMIN_MOUNT}/`),
    )
  })

  test('未登录直访 /admin/patients → 跳登录页并记住原目标页', async ({ page }) => {
    await gotoDeepLink(page, adminRoutes.patients)
    expect(new URL(page.url()).pathname).toBe(`${ADMIN_MOUNT}/login`)
    // redirect 参数带的是 router 内部路径（根路径），不含挂载前缀——回填时才能直接 push
    expect(new URL(page.url()).searchParams.get('redirect')).toBe('/patients')
    await expect(page.locator('.login-card')).toBeVisible()
  })

  test('上一步登录后回到原目标页，不再是首页', async ({ page }) => {
    await gotoDeepLink(page, adminRoutes.patients)
    await page.locator('.login-form input[type="password"]').fill('mock-password')
    await page.locator('.login-form').getByRole('button', { name: '登录' }).click()
    await expect(page).toHaveURL(new RegExp(`${ADMIN_MOUNT}/patients$`), { timeout: 15_000 })
    await expect(page.locator('.el-table__body-wrapper tbody tr').first()).toBeVisible({ timeout: 15_000 })
  })

  test('已登录在子路由刷新：地址不跳走、登录态不丢', async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.teams)
    await expect(page).toHaveURL(new RegExp(`${ADMIN_MOUNT}/teams$`))
    const tokenBefore = await page.evaluate(() => localStorage.getItem('admin_token'))
    expect(tokenBefore).toBeTruthy()

    await page.reload({ waitUntil: 'domcontentloaded' })

    expect(new URL(page.url()).pathname, '刷新后不该被弹回挂载点首页').toBe(`${ADMIN_MOUNT}/teams`)
    await expect(page).toHaveURL(new RegExp(`${ADMIN_MOUNT}/teams$`), { timeout: 15_000 })
    const tokenAfter = await page.evaluate(() => localStorage.getItem('admin_token'))
    expect(tokenAfter).toBe(tokenBefore)
    await expect(page.locator('.el-table__body-wrapper tbody tr').first()).toBeVisible({ timeout: 15_000 })
  })

  test('挂载点内点菜单换页：浏览器地址始终带 /admin/ 前缀', async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.locator('.el-menu .el-menu-item', { hasText: '患者管理' }).click()
    await expect(page).toHaveURL(new RegExp(`${ADMIN_MOUNT}/patients$`), { timeout: 15_000 })
    // 反证：SPA 内部路径不该漏出挂载前缀（router.push 用根路径，浏览器地址由 base 补齐）
    expect(new URL(page.url()).pathname).not.toContain('/admin/admin')
  })
})
