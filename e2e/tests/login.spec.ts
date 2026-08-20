import { test, expect } from '@playwright/test'
import { routes, loginPage, toast, fillUniInput, TEST_PHONE, TEST_SMS_CODE } from '../helpers'

/**
 * login 页：手机号+验证码输入、60s 倒计时、协议勾选、登录跳转 monitor
 * 对齐 T016 自报功能清单（mock 登录，不依赖后端）
 */

test.beforeEach(async ({ page }) => {
  await page.goto(routes.login)
})

test('手机号与验证码输入', async ({ page }) => {
  const el = loginPage(page)
  await fillUniInput(el.phone, TEST_PHONE)
  await fillUniInput(el.smsCode, TEST_SMS_CODE)
})

test('获取验证码后出现 60s 倒计时', async ({ page }) => {
  const el = loginPage(page)
  await fillUniInput(el.phone, TEST_PHONE)
  await el.smsBtn.click()
  // 倒计时出现：60s 起步，按钮进入禁用样式
  await expect(el.smsBtn).toHaveText(/60s/)
  await expect(el.smsBtn).toHaveClass(/sms-disabled/)
  // 倒计时递减（1s 后不再是 60s，且不允许再次发送）
  await expect(el.smsBtn).toHaveText(/^5\ds$/, { timeout: 5_000 })
  await expect(el.smsBtn).not.toHaveText('获取验证码')
})

test('手机号格式错误时提示', async ({ page }) => {
  const el = loginPage(page)
  await fillUniInput(el.phone, '123')
  await el.smsBtn.click()
  await expect(toast(page, '请输入正确的手机号')).toBeVisible()
})

test('未勾选协议时登录被拦截', async ({ page }) => {
  const el = loginPage(page)
  await fillUniInput(el.phone, TEST_PHONE)
  await fillUniInput(el.smsCode, TEST_SMS_CODE)
  // 默认已勾选，先取消
  await expect(el.checkbox).toHaveClass(/checkbox-checked/)
  await el.checkbox.click()
  await expect(el.checkbox).not.toHaveClass(/checkbox-checked/)
  await el.loginBtn.click()
  await expect(toast(page, '请先同意用户协议和隐私政策')).toBeVisible()
  // 仍停留在登录页
  await expect(page).toHaveURL(/pages\/login/)
})

test('未输入验证码时登录被拦截', async ({ page }) => {
  const el = loginPage(page)
  await fillUniInput(el.phone, TEST_PHONE)
  await el.loginBtn.click()
  await expect(toast(page, '请输入验证码')).toBeVisible()
})

test('登录成功跳转 monitor', async ({ page }) => {
  const el = loginPage(page)
  await fillUniInput(el.phone, TEST_PHONE)
  await fillUniInput(el.smsCode, TEST_SMS_CODE)
  await el.loginBtn.click()
  // 登录页用页内自定义 toast（非 uni.showToast），展示 1.5s 后 switchTab 到 monitor
  await expect(page.locator('.toast-text')).toContainText('登录成功')
  await page.waitForURL('**/pages/monitor/**', { timeout: 15_000 })
  await expect(page.getByText('实时监测').first()).toBeVisible()
})
