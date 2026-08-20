import { test, expect } from '@playwright/test'
import {
  techRoutes, techLoginPage, techToast, fillTechInput,
  TECH_PHONE, TECH_SMS_CODE, MOCK_DEVICE_ID,
} from '../tech-helpers'

/**
 * 技师端全链路：bind → matrix(阶段 1-4) → save-baseline → records → complete
 * 对齐 T018 技师端完整安装流程（mock 数据，不依赖后端）
 *
 * 注：matrix 阶段 4 的 goSaveBaseline 是 navigateTo，navigateBack 后组件
 * 重建（stage 重置），因此全链路覆盖到 save-baseline 保存 + 后续页面独立验证。
 * WiFi 配置和签名完成在各自独立 spec 中覆盖。
 */

test('技师端核心链路：登录→绑定→安装→基线保存', async ({ page }) => {
  test.setTimeout(120_000)

  // ===== 1. bind：登录 + 手动绑定 =====
  await page.goto(techRoutes.bind)
  const el = techLoginPage(page)
  await fillTechInput(el.phone, TECH_PHONE)
  await fillTechInput(el.smsCode, TECH_SMS_CODE)
  await el.loginBtn.click()
  await expect(techToast(page).text).toContainText('登录成功', { timeout: 10_000 })

  const deviceInput = page.locator('.section').nth(1).locator('uni-input.form-input').first()
  await fillTechInput(deviceInput, MOCK_DEVICE_ID)
  await page.locator('.btn-primary', { hasText: '绑定设备' }).click()
  await expect(techToast(page).text).toContainText('设备绑定成功')
  await page.waitForURL('**/pages/matrix/**', { timeout: 15_000 })

  // ===== 2. matrix 阶段 1：患者信息确认 =====
  await expect(page.locator('.card-title', { hasText: '患者信息确认' })).toBeVisible()
  await expect(page.getByText('张小明')).toBeVisible()
  await page.locator('.btn-primary', { hasText: '确认，下一步' }).click()

  // ===== 3. matrix 阶段 2：传感器安装定位 =====
  await expect(page.locator('.card-title', { hasText: '传感器安装定位' })).toBeVisible()
  await expect(page.locator('.sensor-cell-mini')).toHaveCount(20)
  await page.locator('.btn-primary', { hasText: '确认安装，下一步' }).click()

  // ===== 4. matrix 阶段 3：设备校准 =====
  await expect(page.locator('.card-title', { hasText: '设备校准' })).toBeVisible()
  await page.locator('.btn-outline', { hasText: '开始校准' }).click()
  await expect(page.getByText('校准完成')).toBeVisible({ timeout: 15_000 })
  await page.locator('.btn-primary', { hasText: '校准完成，下一步' }).click()

  // ===== 5. matrix 阶段 4：保存基线入口 =====
  await expect(page.locator('.card-title', { hasText: '保存基线数据' })).toBeVisible()
  await page.locator('.btn-primary', { hasText: '保存基线' }).click()
  await page.waitForURL('**/pages/save-baseline/**', { timeout: 10_000 })

  // ===== 6. save-baseline：校验 + 保存 =====
  await expect(page.getByText('基线数据校验')).toBeVisible()
  await expect(page.locator('.offset-cell')).toHaveCount(20)
  await expect(page.locator('.val-pass').filter({ hasText: '数据点数' })).toBeVisible()
  await expect(page.locator('.val-pass').filter({ hasText: '范围校验' })).toBeVisible()
  await page.locator('.btn-primary', { hasText: '确认保存基线' }).click()
  await expect(page.locator('.toast-text')).toContainText('基线已保存', { timeout: 5_000 })
  await page.waitForURL('**/pages/matrix/**', { timeout: 15_000 })

  // 回到 matrix（navigateBack 后 URL 变化即可验证）
  await expect(page).not.toHaveURL(/pages\/save-baseline/, { timeout: 15_000 })
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
  // redirectTo 在 H5 中导航离开 complete 页
  await expect(page).not.toHaveURL(/pages\/complete/, { timeout: 10_000 })
})
