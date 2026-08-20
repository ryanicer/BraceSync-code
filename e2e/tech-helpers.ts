import { expect, type Page, type Locator } from '@playwright/test'

/**
 * 技师端 e2e 公共工具（T027 tech-miniapp mock 数据基线）
 *
 * 选择器约定：tech-miniapp 的 <input class="form-input"> 在 H5 下被 uni-app
 * 渲染为 <uni-input class="form-input"> 包裹的原生 <input>。
 * 原生 input 可能不在 DOM 中（shadow DOM 或动态渲染），使用 evaluate 兜底。
 */

export const TECH_PHONE = '13800138000'
export const TECH_SMS_CODE = '1234'
export const MOCK_DEVICE_ID = 'PRS-ML05-RC-001'

/** uni-app H5 hash 路由直达（技师端无 tabBar，全部为 navigateTo） */
export const techRoutes = {
  bind: '/#/pages/bind/index',
  matrix: '/#/pages/matrix/index',
  saveBaseline: '/#/pages/save-baseline/index',
  wifiConfig: '/#/pages/wifi-config/index',
  records: '/#/pages/records/index',
  complete: '/#/pages/complete/index',
} as const

/**
 * uni-app H5 输入填充：uni-input 内部原生 input 可能不在 CSS 可达的 DOM 中，
 * 优先尝试原生 input 定位，不可用时通过 evaluate + querySelector 兜底。
 */
export async function fillTechInput(locator: Locator, value: string) {
  // 策略 1：尝试直接 fill（适用于原生 input 元素）
  const tag = await locator.evaluate(el => el.tagName.toLowerCase()).catch(() => '')
  if (tag === 'input' || tag === 'textarea') {
    await locator.fill(value, { force: true })
    await expect(locator).toHaveValue(value)
    return
  }
  // 策略 2：uni-input 组件 → 查找内部原生 input
  const innerInput = locator.locator('input')
  const innerCount = await innerInput.count()
  if (innerCount > 0) {
    await innerInput.first().fill(value, { force: true })
    await expect(innerInput.first()).toHaveValue(value)
    return
  }
  // 策略 3：通过 evaluate 直接操作 DOM
  await locator.evaluate((el, val) => {
    const input = el.querySelector('input') || el.shadowRoot?.querySelector('input')
    if (input) {
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype, 'value'
      )?.set
      nativeInputValueSetter?.call(input, val)
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
    }
  }, value)
}

/** 技师登录页元素（嵌入在 bind 页内） */
export const techLoginPage = (page: Page) => ({
  phone: page.locator('uni-input.form-input:not(.sms-input)').first(),
  smsCode: page.locator('uni-input.sms-input').first(),
  smsBtn: page.locator('.sms-btn').first(),
  loginBtn: page.locator('.btn-primary').first(),
})

/** 页内自定义 toast（非 uni.showToast，bind 页自实现） */
export function techToast(page: Page) {
  return {
    root: page.locator('.toast'),
    text: page.locator('.toast-text'),
  }
}

/** uni.showToast 渲染为 uni-toast 自定义元素 */
export function uniToast(page: Page, text: string | RegExp) {
  return page.locator('uni-toast').filter({ hasText: text })
}

/** uni.showModal 渲染为 uni-modal 自定义元素 */
export function uniModal(page: Page) {
  const m = page.locator('uni-modal')
  return {
    root: m,
    cancel: m.locator('.uni-modal__btn_default'),
    confirm: m.locator('.uni-modal__btn_primary'),
  }
}

/** 完成技师 mock 登录（在 bind 页操作）并等待登录成功 toast */
export async function doTechLogin(page: Page) {
  await page.goto(techRoutes.bind)
  const el = techLoginPage(page)
  await fillTechInput(el.phone, TECH_PHONE)
  await fillTechInput(el.smsCode, TECH_SMS_CODE)
  await el.loginBtn.click()
  await expect(techToast(page).text).toContainText('登录成功', { timeout: 10_000 })
}
