import { test, expect } from '@playwright/test'
import { techRoutes, fillTechInput, forceTechLoginMock, mockTechBLE } from '../tech-helpers'

/**
 * tech-wifi-config 页：WiFi 配网（5 步骤条 + WiFi 选择 + 密码输入 + 状态机 + 成功）
 * 对齐 T089 V2.1 wifi-config 页：5 步骤、2 个 WiFi、文案"配网成功"
 */

test.beforeEach(async ({ page }) => {
  await mockTechBLE(page)
  await forceTechLoginMock(page)
  await page.goto(techRoutes.wifiConfig)
})

test('页面标题与步骤条', async ({ page }) => {
  await expect(page.getByText('WiFi 配置').first()).toBeVisible()
  await expect(page.locator('.step')).toHaveCount(5)
  await expect(page.locator('.step-label').first()).toHaveText('收到')
  await expect(page.locator('.step-label').last()).toHaveText('成功')
})

test('手动输入 SSID', async ({ page }) => {
  const ssidInput = page.locator('.manual-wifi .form-input').first()
  await fillTechInput(ssidInput, 'My_Custom_WiFi')
})

test('密码输入与切换显示', async ({ page }) => {
  const pwdInput = page.locator('.password-input').first()
  await fillTechInput(pwdInput, 'test1234')
  // 密码默认为隐藏模式
  const innerInput = pwdInput.locator('input')
  await expect(innerInput).toHaveAttribute('type', 'password')
  // 点击切换显示密码
  await page.locator('.pwd-toggle').click()
  await expect(innerInput).toHaveAttribute('type', 'text')
})

test('手动输入 SSID 后点击扫描 WiFi 回填不清空（T117）', async ({ page }) => {
  const ssidInput = page.locator('.manual-wifi .form-input').first()
  await fillTechInput(ssidInput, 'My_Custom_WiFi')
  await expect(ssidInput.locator('input').first()).toHaveValue('My_Custom_WiFi')
})

test('配网全流程到成功', async ({ page }) => {
  test.setTimeout(60_000)
  // 输入 SSID + 密码
  await fillTechInput(page.locator('.manual-wifi .form-input').first(), 'My_Custom_WiFi')
  const pwdInput = page.locator('.password-input').first()
  await fillTechInput(pwdInput, 'test1234')

  await page.locator('.btn-primary', { hasText: '开始配网' }).click()
  // 步骤条推进 + 成功态
  await expect(page.getByText('配网成功')).toBeVisible({ timeout: 30_000 })
  await expect(page.getByText('WiFi 已连接，数据可达性验证通过')).toBeVisible()
})
