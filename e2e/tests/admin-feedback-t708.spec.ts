import { test, expect } from '@playwright/test'
import { adminLogin, adminPath, menuItems, type AdminRole } from '../admin-helpers'

/**
 * T708 运维反馈入口（案 C：侧栏工具区 + /admin/feedback iframe 宿主，零表零后端接口）。
 *
 * mock dev server 不注入 VITE_FEEDBACK_SERVICE_URL / VITE_FEEDBACK_ENV（那两个值等 T709
 * 代理 service 落地后由部署侧写进构建），所以这里只钉三件与配置无关的不变量：
 *   ① 入口在侧栏底部工具区，且**不进业务菜单计数**（el-menu 条数由 RBAC 矩阵钉死）；
 *   ② 任何已登录角色都进得去 /feedback，不落 403（客服 1 个业务页照样能报障）；
 *      未登录进来先跳登录并带 redirect。
 *   ③ 运维可见区域零第三方系统字样（Boss 拍板②「不要让用户知道 TAPD 的存在」）。
 * env 拼接口径与 iframe 渲染由 apps/admin-web/test/t708-feedback.spec.ts 单测覆盖。
 */

const FEEDBACK = adminPath('/feedback')

test.describe('T708 运维反馈入口', () => {
  for (const role of ['admin', 'doctor', 'cs'] as AdminRole[]) {
    test(`${role}：侧栏工具区有「问题反馈」，点进去不落 403`, async ({ page }) => {
      await adminLogin(page, role)
      const tool = page.locator('.sidebar-tool')
      await expect(tool).toHaveCount(1)
      await expect(tool).toContainText('问题反馈')
      await tool.click()
      await expect(page).toHaveURL(/\/admin\/feedback$/)
      await expect(page.locator('.forbidden-card')).toHaveCount(0)
      await expect(page.locator('.top-nav-title')).toContainText('问题反馈')
      await expect(page.locator('body')).not.toContainText('TAPD')
    })
  }

  test('工具区不混进业务菜单：cs 侧栏业务项仍 1 项', async ({ page }) => {
    await adminLogin(page, 'cs')
    await expect(menuItems(page)).toHaveCount(1)
    await expect(page.locator('.sidebar-menu .sidebar-tool')).toHaveCount(0)
  })

  test('未登录直达 /feedback → 跳登录并带 redirect', async ({ page }) => {
    await page.goto(FEEDBACK)
    await expect(page).toHaveURL(/\/admin\/login\?redirect=/)
    await expect(page.locator('.login-form')).toBeVisible()
  })
})
