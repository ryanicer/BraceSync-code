import { test, expect } from '@playwright/test'
import {
  techRoutes, fillTechInput,
  MOCK_TECH_TOKEN, MOCK_DEVICE_ID,
  forceTechLoginMock, doTechRealLogin,
} from '../tech-helpers'

/**
 * tech-bind 页：技师 mock 登录 + 设备绑定（扫码/手动输入/BLE 扫描）
 * 对齐 T018 技师端 bind 页功能清单（mock 数据，不依赖后端）
 */

test.describe('技师 mock 登录', () => {
  test.beforeEach(async ({ page }) => {
    await forceTechLoginMock(page)
  })

  // === T037 真实登录流程验证（仅本地）===
  test('真实登录流程：navigate /login → 跳转 bind', async ({ page }) => {
    test.skip(!!process.env.CI, '真实登录依赖后端 /api/v1/tech/login，CI 无后端')
    await doTechRealLogin(page)
  })

  // === Mock Token 注入后验证 bind 页可用 ===
  test('bind 页标题显示', async ({ page }) => {
    await expect(page.getByText('扫码绑定')).toBeVisible({ timeout: 10_000 })
  })

  test('手动输入设备 ID', async ({ page }) => {
    const deviceInput = page.locator('.section').nth(1).locator('uni-input.form-input').first()
    await fillTechInput(deviceInput, MOCK_DEVICE_ID)
  })
})

test.describe('设备绑定', () => {
  test.beforeEach(async ({ page }) => {
    await forceTechLoginMock(page)
    await page.goto(techRoutes.bind)
  })

  test('扫码绑定自动填入设备 ID', async ({ page }) => {
    await page.locator('.scan-card').click()
    await expect(page.locator('uni-toast')).toContainText('扫码成功', { timeout: 5_000 })
  })

  test('手动输入设备 ID 绑定成功并跳转 matrix', async ({ page }) => {
    const deviceInput = page.locator('.section').nth(1).locator('uni-input.form-input').first()
    await fillTechInput(deviceInput, MOCK_DEVICE_ID)
    await page.locator('.btn-primary', { hasText: '绑定设备' }).click()
    // bind 页自实现 toast（非 uni.showToast），跳转 matrix 前显示 1.2s
    await expect(page.locator('.toast-text')).toContainText('设备绑定成功', { timeout: 3_000 })
    await page.waitForURL('**/pages/matrix/**', { timeout: 15_000 })
  })

  test('BLE 扫描显示附近设备列表', async ({ page }) => {
    await page.locator('.refresh-btn').click()
    await expect(page.locator('.refresh-btn')).toContainText('扫描中...')
    await expect(page.locator('.device-item')).toHaveCount(3, { timeout: 15_000 })
    await expect(page.locator('.device-name').first()).toHaveText('PRS-ML05-RC-001')
    await expect(page.locator('.device-rssi').first()).toContainText('dBm')
  })

  test('安装记录快捷入口跳转', async ({ page }) => {
    await page.locator('.quick-entry').click()
    await page.waitForURL('**/pages/records/**', { timeout: 10_000 })
    await expect(page.getByText('安装记录').first()).toBeVisible()
  })
})
