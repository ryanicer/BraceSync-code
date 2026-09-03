import { test, expect } from '@playwright/test'
import { switchTabBy, fillUniInput, HOTSPOT_NAME, loginPage, setupPatientE2E } from '../helpers'

/**
 * T080: 全链路 - 微信登录契约版（T074 迁移）
 * 登录流程：协议勾选 → 微信按钮点击 → POST /api/v1/patient/wx-login
 * T074 真实模式基建：test 顶部注册 route mock（断言体不动）
 */
test('患者端核心全链路：微信登录到配网成功', async ({ page }) => {
  test.setTimeout(120_000)
  await setupPatientE2E(page, { withLogin: false })

  // ===== 1. login (WeChat contract) =====
  await page.goto('/#/pages/login/index')
  const el = loginPage(page)
  
  // agreed 默认 true → 直接点击微信按钮
  await el.wechatBtn.click()
  await expect(page.locator('uni-toast').filter({ hasText: /登录成功/ })).toBeVisible()
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })

  // ===== 2. monitor：热力图 + 点选 + 日/周切换 =====
  await expect(page.locator('.sensor-grid .grid-cell')).toHaveCount(20)
  await page.locator('.grid-cell', { has: page.locator('.cell-id', { hasText: 'P01' }) }).click()
  await expect(page.locator('.hero-label')).toContainText('P01 · 当前压力值')
  await page.locator('.segmented .seg-btn', { hasText: '周' }).click()
  await expect(page.locator('.trend-section .section-title')).toContainText('本周压力趋势')

  // ===== 3. history：tabBar 切换 + 双 Tab =====
  await switchTabBy(page, '异常监测')
  await page.waitForURL('**/pages/history/**', { timeout: 10_000 })
  await expect(page.locator('.wearing-row')).toHaveCount(15)
  await page.locator('.segmented .seg-btn', { hasText: '压力异常' }).click()
  await expect(page.locator('.p-group')).toHaveCount(7)

  // ===== 4. device：tabBar 切换 + 设备卡片 =====
  await switchTabBy(page, '设备管理')
  await page.waitForURL('**/pages/device/**', { timeout: 10_000 })
  await expect(page.locator('.device-name')).toHaveText(HOTSPOT_NAME)

  // ===== 5. wifi-setup：配网到成功 =====
  await page.locator('.action-btn', { hasText: '开始添加设备' }).click()
  await page.waitForURL('**/pages/wifi-setup/**', { timeout: 10_000 })
  await expect(page.locator('.steps .step')).toHaveCount(4)
  await fillUniInput(page.locator('uni-input.password-input input'), 'secret123')
  await page.locator('.btn-primary', { hasText: '开始配网' }).click()
  await expect(page.locator('.success-text')).toHaveText('配网成功!', { timeout: 30_000 })

  // ===== 6. 返回设备管理 =====
  await page.locator('.btn-primary', { hasText: '返回设备管理' }).click()
  await page.waitForURL('**/pages/device/**', { timeout: 10_000 })
  await expect(page.locator('.device-card')).toBeVisible()
})
