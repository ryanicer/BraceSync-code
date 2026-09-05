import { test, expect } from '@playwright/test'
import {
  techRoutes, fillTechInput,
  MOCK_DEVICE_ID,
  forceTechLoginMock, doTechRealLogin, mockTechBLE,
} from '../tech-helpers'

/**
 * tech-bind 页：技师 mock 登录 + 设备绑定（扫码/手动输入/BLE 扫描）
 * 对齐 T089 V2.1 bind 页：标题"设备绑定"、需填患者 ID、绑定后跳 install、BLE 扫描仍在本页
 */

test.describe('技师 mock 登录', () => {
  test.beforeEach(async ({ page }) => {
    await mockTechBLE(page)
    await forceTechLoginMock(page)
  })

  // === T089 真实登录流程验证（仅本地）===
  test('真实登录流程：navigate /login → 跳转 home', async ({ page }) => {
    test.skip(!!process.env.CI, '真实登录依赖后端 /api/v1/tech/login，CI 无后端')
    await doTechRealLogin(page)
  })

  // === Mock Token 注入后验证 bind 页可用 ===
  test('bind 页标题显示', async ({ page }) => {
    await expect(page.getByText('设备绑定')).toBeVisible({ timeout: 10_000 })
  })

  test('手动输入设备 ID', async ({ page }) => {
    const deviceInput = page.locator('.section').nth(1).locator('.form-input').first()
    await fillTechInput(deviceInput, MOCK_DEVICE_ID)
  })
})

test.describe('设备绑定', () => {
  test.beforeEach(async ({ page }) => {
    await mockTechBLE(page)
    await forceTechLoginMock(page)
    await page.goto(techRoutes.bind)
  })

  test('扫码绑定自动填入设备 ID', async ({ page }) => {
    await page.locator('.scan-card').click()
    await expect(page.locator('uni-toast')).toContainText('扫码成功', { timeout: 5_000 })
  })

  test('手动输入设备 ID + 患者 ID 绑定成功并跳转 install', async ({ page }) => {
    // section 1 内两个 form-input：第一个设备 ID，第二个患者 ID
    const deviceInput = page.locator('.section').nth(1).locator('.form-input').first()
    await fillTechInput(deviceInput, MOCK_DEVICE_ID)
    const patientInput = page.locator('.section').nth(1).locator('.form-input').nth(1)
    await fillTechInput(patientInput, 'pat-001')

    await page.locator('.btn-primary', { hasText: '绑定设备' }).click()
    // bind 页自实现 toast（非 uni.showToast），跳转 install 前显示 1.2s
    await expect(page.locator('.toast-text')).toContainText('设备绑定成功', { timeout: 3_000 })
    await page.waitForURL('**/pages/install/**', { timeout: 15_000 })
  })

  test('BLE 扫描显示附近设备列表', async ({ page }) => {
    await page.locator('.refresh-btn').click()
    await expect(page.locator('.refresh-btn')).toContainText('扫描中...')
    await expect(page.locator('.device-item')).toHaveCount(1, { timeout: 15_000 })
    await expect(page.locator('.device-name').first()).toHaveText('PRS-ML05-RC-001')
    await expect(page.locator('.device-rssi').first()).toContainText('dBm')
  })
})
