import { test, expect } from '@playwright/test'
import { switchTabBy, fillUniInput, HOTSPOT_NAME, loginPage, TEST_PHONE, TEST_SMS_CODE } from '../helpers'

/**
 * 全链路：login → monitor → history → device → wifi-setup → 配网成功（一次跑通）
 */
test('患者端核心全链路：登录到配网成功', async ({ page }) => {
  test.setTimeout(120_000)

  // ===== 1. login =====
  await page.goto('/#/pages/login/index')
  const el = loginPage(page)
  await fillUniInput(el.phone, TEST_PHONE)
  await fillUniInput(el.smsCode, TEST_SMS_CODE)
  await el.loginBtn.click()
  await expect(page.locator('.toast-text')).toContainText('登录成功')
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
