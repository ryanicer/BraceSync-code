import { test, expect } from '@playwright/test'
import {
  techRoutes, fillTechInput,
  MOCK_DEVICE_ID,
  forceTechLoginMock,
} from '../tech-helpers'

/**
 * 技师端全链路：bind → matrix(阶段 1-4) → save-baseline → records → complete
 * 对齐 T018 技师端完整安装流程（mock 数据，不依赖后端）
 *
 * 注：matrix 阶段 4 的 goSaveBaseline 是 navigateTo，navigateBack 后组件
 * 重建（stage 重置），因此全链路覆盖到 save-baseline 保存 + 后续页面独立验证。
 * WiFi 配置和签名完成在各自独立 spec 中覆盖。
 */

test.describe('技师端全链路', () => {
  test.beforeEach(async ({ page }) => {
    await forceTechLoginMock(page)
  })

  test('核心链路：bind→matrix(阶段 1-4)→save-baseline', async ({ page }) => {
    test.setTimeout(120_000)
    await page.goto(techRoutes.bind)

    // === bind: 绑定设备 ===
    const deviceInput = page.locator('.section').nth(1).locator('uni-input.form-input').first()
    await fillTechInput(deviceInput, MOCK_DEVICE_ID)
    await page.locator('.btn-primary', { hasText: '绑定设备' }).click()
    // bind 页自实现 toast（非 uni.showToast），跳转 matrix 前显示 1.2s
    await expect(page.locator('.toast-text')).toContainText('设备绑定成功', { timeout: 3_000 })
    await page.waitForURL('**/pages/matrix/**', { timeout: 15_000 })

    // === matrix 阶段 1 ===
    await expect(page.locator('.card-title', { hasText: '患者信息确认' })).toBeVisible()
    await expect(page.getByText('张小明')).toBeVisible()
    await page.locator('.btn-primary', { hasText: '确认，下一步' }).click()

    // === matrix 阶段 2 ===
    await expect(page.locator('.card-title', { hasText: '传感器安装定位' })).toBeVisible()
    await expect(page.locator('.sensor-cell-mini')).toHaveCount(20)
    await page.locator('.btn-primary', { hasText: '确认安装，下一步' }).click()

    // === matrix 阶段 3 ===
    await expect(page.locator('.card-title', { hasText: '设备校准' })).toBeVisible()
    await page.locator('.btn-outline', { hasText: '开始校准' }).click()
    await expect(page.getByText('校准完成')).toBeVisible({ timeout: 15_000 })
    await page.locator('.btn-primary', { hasText: '校准完成，下一步' }).click()

    // === matrix 阶段 4 ===
    await expect(page.locator('.card-title', { hasText: '保存基线数据' })).toBeVisible()
    await page.locator('.btn-primary', { hasText: '保存基线' }).click()
    await page.waitForURL('**/pages/save-baseline/**', { timeout: 10_000 })

    // === save-baseline ===
    await expect(page.getByText('基线数据校验')).toBeVisible()
    await expect(page.locator('.offset-cell')).toHaveCount(20)
    await expect(page.locator('.val-pass').filter({ hasText: '数据点数' })).toBeVisible()
    await expect(page.locator('.val-pass').filter({ hasText: '范围校验' })).toBeVisible()
    await page.locator('.btn-primary', { hasText: '确认保存基线' }).click()
    await expect(page.locator('.toast-text')).toContainText('基线已保存', { timeout: 5_000 })
    await page.waitForURL('**/pages/matrix/**', { timeout: 15_000 })

    // === verify navigateBack ===
    await expect(page).not.toHaveURL(/pages\/save-baseline/, { timeout: 15_000 })
  })
})

test.describe('快捷入口跳转', () => {
  test.beforeEach(async ({ page }) => {
    await forceTechLoginMock(page)
  })

  test('安装记录与完成页快捷入口', async ({ page }) => {
    await page.goto(techRoutes.complete)
    await expect(page.locator('.success-title')).toBeVisible()
    await page.locator('.action-btn', { hasText: '查看安装记录' }).click()
    await expect(page).toHaveURL(/pages\/records/, { timeout: 10_000 })
    await expect(page.getByText('安装记录').first()).toBeVisible()
    await expect(page.locator('.record-card')).toHaveCount(5)
  })

  test('完成页继续安装下一台跳转', async ({ page }) => {
    await page.goto(techRoutes.complete)
    await page.locator('.action-btn', { hasText: '继续安装下一台' }).click()
    await expect(page).not.toHaveURL(/pages\/complete/, { timeout: 10_000 })
  })
})
