import { test, expect } from '@playwright/test'
import {
  techRoutes, fillTechInput,
  MOCK_DEVICE_ID, SEED_PATIENT_ID,
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

  test('患者ID 输入框示例为真实形态（T362：不再是 pat-001）', async ({ page }) => {
    // uni-app H5 把 placeholder 渲染成 div.uni-input-placeholder，原生 input 上没有该属性
    const ph = page.locator('.section').nth(1).locator('.form-input').nth(1).locator('.uni-input-placeholder')
    await expect(ph).toHaveText(/^例: P\d{4}[0-9a-f]{12}$/)
  })

  test('扫码入口走真实 uni.scanCode，不再注入假设备 ID', async ({ page }) => {
    const deviceInput = page.locator('.section').nth(1).locator('.form-input').first()
    await page.locator('.scan-card').click()
    // H5 构建里 uni.scanCode 是 createUnsupportedAsyncApi（必然 reject），
    // 所以真机上唯一的成功路径不可能在这里出现：断言「没有假成功」而不是断言旧 mock 的「扫码成功」
    await expect(page.locator('uni-toast')).toContainText('扫码失败，请手动输入设备 ID', { timeout: 5_000 })
    await expect(deviceInput.locator('input')).toHaveValue('')
  })

  test('手动输入设备 ID + 患者 ID 绑定成功并跳转 install', async ({ page }) => {
    // section 1 内两个 form-input：第一个设备 ID，第二个患者 ID
    const deviceInput = page.locator('.section').nth(1).locator('.form-input').first()
    await fillTechInput(deviceInput, MOCK_DEVICE_ID)
    const patientInput = page.locator('.section').nth(1).locator('.form-input').nth(1)
    await fillTechInput(patientInput, SEED_PATIENT_ID)

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
