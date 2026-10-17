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
  // T240 回归：成功态下 5 个步骤全部显示 ✓（不应被 clear 的 notify 0 打回数字）
  await expect(page.locator('.step-check')).toHaveCount(5)
})

/**
 * T459·C2（PM 2026-09-28 裁定）：四条失败文案里只有 -4 给「跳过配网」入口，
 * 也只有 -4 会落库 failed（写侧判据在 apps/tech-miniapp/test/install-status.spec.ts）。
 * 这条钉的是"别把 -1 也当成云端不可达"——文案与入口都不同形。
 */
test('-1 连接失败：出 T713 分档文案（不再单一归因密码），且不给跳过入口（T459·C2）', async ({ page }) => {
  test.setTimeout(60_000)
  await page.evaluate(() => {
    ;(window as unknown as { __mockWifiSeq: number[] }).__mockWifiSeq = [0, -1]
  })
  await fillTechInput(page.locator('.manual-wifi .form-input').first(), 'My_Custom_WiFi')
  await fillTechInput(page.locator('.password-input').first(), 'test1234')
  await page.locator('.btn-primary', { hasText: '开始配网' }).click()
  await expect(page.locator('.error-title')).toHaveText('连接失败：请检查密码是否正确、信号强度、以及是否为 2.4GHz 频段', { timeout: 20_000 })
  await expect(page.locator('.error-title')).not.toContainText('密码错误')
  await expect(page.locator('.skip-hint')).toHaveCount(0)
})

/**
 * T713（Boss 真机 5G 实锤）：-2＝设备扫不到该 SSID，最常见真因是填了 5G 网络。
 * 这条钉的是「-2 出频段口径提示、且不误导成密码问题」，文案走 PRD §7A.9.1 ④ 只认信道不认名字。
 */
test('-2 找不到 WiFi：出 2.4GHz 频段提示（T713）', async ({ page }) => {
  test.setTimeout(60_000)
  await page.evaluate(() => {
    ;(window as unknown as { __mockWifiSeq: number[] }).__mockWifiSeq = [0, -2]
  })
  await fillTechInput(page.locator('.manual-wifi .form-input').first(), 'My_Custom_WiFi')
  await fillTechInput(page.locator('.password-input').first(), 'test1234')
  await page.locator('.btn-primary', { hasText: '开始配网' }).click()
  await expect(page.locator('.error-title')).toHaveText('找不到该 WiFi：请确认设备支持 2.4GHz 频段（5G 不支持）', { timeout: 20_000 })
})
