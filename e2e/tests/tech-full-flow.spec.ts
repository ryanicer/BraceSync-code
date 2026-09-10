import { test, expect } from '@playwright/test'
import {
  techRoutes, fillTechInput,
  MOCK_DEVICE_ID,
  forceTechLoginMock, mockTechBLE,
} from '../tech-helpers'

/**
 * 技师端全链路：bind → install(3 阶段) → complete → records
 * 对齐 T089 V2.1 完整安装流程（mock 数据，不依赖后端）
 */

test.describe('技师端全链路', () => {
  test.beforeEach(async ({ page }) => {
    await mockTechBLE(page)
    await forceTechLoginMock(page)
  })

  test('核心链路：bind→install(3阶段)→complete→records', async ({ page }) => {
    test.setTimeout(180_000)
    await page.goto(techRoutes.bind)

    // === bind: 绑定设备（设备 ID + 患者 ID）===
    const deviceInput = page.locator('.section').nth(1).locator('.form-input').first()
    await fillTechInput(deviceInput, MOCK_DEVICE_ID)
    const patientInput = page.locator('.section').nth(1).locator('.form-input').nth(1)
    await fillTechInput(patientInput, 'pat-001')
    await page.locator('.btn-primary', { hasText: '绑定设备' }).click()
    await expect(page.locator('.toast-text')).toContainText('设备绑定成功', { timeout: 3_000 })
    await page.waitForURL('**/pages/install/**', { timeout: 15_000 })

    // === install 阶段一：患者确认 ===
    await expect(page.locator('.card-title', { hasText: '患者信息确认' })).toBeVisible()
    await expect(page.getByText('张明远')).toBeVisible()
    await page.locator('.btn-primary', { hasText: '确认，下一步' }).click()

    // === install 阶段二：空载校准归零 ===
    await expect(page.locator('.card-title', { hasText: '设备校准' })).toBeVisible()
    await page.locator('.btn-primary', { hasText: '开始校准' }).click()
    await expect(page.getByText('校准后静态压力已归零')).toBeVisible({ timeout: 20_000 })
    await page.locator('.btn-primary', { hasText: '校准完成，下一步' }).click()

    // === install 阶段三：WiFi 配网 ===
    await expect(page.locator('.card-title', { hasText: 'WiFi 网络配置' })).toBeVisible()
    await page.locator('.btn-primary', { hasText: '配置 WiFi' }).click()
    await page.waitForURL('**/pages/wifi-config/**', { timeout: 10_000 })

    // wifi-config: 选 WiFi + 输密码 + 配网
    await page.locator('.wifi-item').first().click()
    const pwdInput = page.locator('.password-input').first()
    await fillTechInput(pwdInput, 'test1234')
    await page.locator('.btn-primary', { hasText: '开始配网' }).click()
    await expect(page.getByText('配网成功')).toBeVisible({ timeout: 30_000 })
    // 3s 后自动返回 install
    await page.waitForURL('**/pages/install/**', { timeout: 10_000 })

    // install 阶段三：配网成功 + 可达性验证通过 → 完成安装
    await expect(page.getByText('WiFi 已连接')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('数据可达性验证通过')).toBeVisible()
    await page.locator('.btn-primary', { hasText: '完成安装' }).click()
    await page.waitForURL('**/pages/complete/**', { timeout: 10_000 })

    // === complete ===
    await expect(page.locator('.success-title')).toHaveText('安装完成')
    await expect(page.getByText('数据可达性')).toBeVisible()

    // === records ===
    await page.locator('.action-btn', { hasText: '查看安装记录' }).click()
    await expect(page).toHaveURL(/pages\/records/, { timeout: 10_000 })
    await expect(page.getByText('安装记录').first()).toBeVisible()
    await expect(page.locator('.record-card')).toHaveCount(6)
  })
})

test.describe('快捷入口跳转', () => {
  test.beforeEach(async ({ page }) => {
    await forceTechLoginMock(page)
  })

  test('完成页查看安装记录跳转', async ({ page }) => {
    await page.goto(techRoutes.complete)
    await expect(page.locator('.success-title')).toBeVisible()
    await page.locator('.action-btn', { hasText: '查看安装记录' }).click()
    await expect(page).toHaveURL(/pages\/records/, { timeout: 10_000 })
    await expect(page.getByText('安装记录').first()).toBeVisible()
    await expect(page.locator('.record-card')).toHaveCount(6)
  })

  test('完成页继续安装下一台跳转', async ({ page }) => {
    await page.goto(techRoutes.complete)
    await page.locator('.action-btn', { hasText: '继续安装下一台' }).click()
    await expect(page).not.toHaveURL(/pages\/complete/, { timeout: 10_000 })
  })
})
