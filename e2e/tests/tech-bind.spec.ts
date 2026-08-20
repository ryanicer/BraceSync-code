import { test, expect } from '@playwright/test'
import {
  techRoutes, techLoginPage, techToast, fillTechInput,
  TECH_PHONE, TECH_SMS_CODE, MOCK_DEVICE_ID,
} from '../tech-helpers'

/**
 * tech-bind 页：技师 mock 登录 + 设备绑定（扫码/手动输入/BLE 扫描）
 * 对齐 T018 技师端 bind 页功能清单（mock 数据，不依赖后端）
 */

test.describe('技师 mock 登录', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(techRoutes.bind)
  })

  test('未登录时显示登录表单', async ({ page }) => {
    await expect(page.getByText('技师登录')).toBeVisible()
    await expect(page.locator('uni-input.form-input').first()).toBeAttached()
    await expect(page.locator('.sms-btn')).toBeVisible()
  })

  test('手机号与验证码输入', async ({ page }) => {
    const el = techLoginPage(page)
    await fillTechInput(el.phone, TECH_PHONE)
    await fillTechInput(el.smsCode, TECH_SMS_CODE)
  })

  test('手机号格式错误时提示', async ({ page }) => {
    const el = techLoginPage(page)
    await fillTechInput(el.phone, '123')
    await el.smsBtn.click()
    await expect(page.locator('uni-toast')).toContainText('请输入正确的手机号', { timeout: 5_000 })
  })

  test('登录成功', async ({ page }) => {
    const el = techLoginPage(page)
    await fillTechInput(el.phone, TECH_PHONE)
    await fillTechInput(el.smsCode, TECH_SMS_CODE)
    await el.loginBtn.click()
    await expect(techToast(page).text).toContainText('登录成功', { timeout: 10_000 })
    await expect(page.getByText('扫码绑定')).toBeVisible({ timeout: 10_000 })
  })
})

test.describe('设备绑定', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(techRoutes.bind)
    const el = techLoginPage(page)
    await fillTechInput(el.phone, TECH_PHONE)
    await fillTechInput(el.smsCode, TECH_SMS_CODE)
    await el.loginBtn.click()
    await expect(techToast(page).text).toContainText('登录成功', { timeout: 10_000 })
  })

  test('扫码绑定自动填入设备 ID', async ({ page }) => {
    await page.locator('.scan-card').click()
    await expect(page.locator('uni-toast')).toContainText('扫码成功', { timeout: 5_000 })
  })

  test('手动输入设备 ID 绑定成功并跳转 matrix', async ({ page }) => {
    // 手动输入区域：第二个 .section 内的第一个 uni-input
    const deviceInput = page.locator('.section').nth(1).locator('uni-input.form-input').first()
    await fillTechInput(deviceInput, MOCK_DEVICE_ID)
    await page.locator('.btn-primary', { hasText: '绑定设备' }).click()
    await expect(techToast(page).text).toContainText('设备绑定成功')
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
