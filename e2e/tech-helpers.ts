import { expect, type Page, type Locator } from '@playwright/test'

/**
 * 技师端 e2e 公共工具（T027 tech-miniapp mock 数据基线）
 *
 * 选择器约定：tech-miniapp 的 <input class="form-input"> 在 H5 下被 uni-app
 * 渲染为 <uni-input class="form-input"> 包裹的原生 <input>。
 * 原生 input 可能不在 DOM 中（shadow DOM 或动态渲染），使用 evaluate 兜底。
 */

export const TECH_PHONE = '13900000001' // T041 播种技师账号（T037 联调验证通过）
export const MOCK_TECH_TOKEN = 'mock-tech-token-ci-001'
export const MOCK_TECH_ID = 'TECH_CI_001'
export const MOCK_DEVICE_ID = 'PRS-ML05-RC-001'

/** uni-app H5 hash 路由直达（技师端无 tabBar） */
export const techRoutes = {
  login: '/#/pages/login/index',
  home: '/#/pages/home/index',
  bind: '/#/pages/bind/index',
  install: '/#/pages/install/index',
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

/**
 * 强制 Mock 技师登录（登录态注入）
 * Key 参照 apps/tech-miniapp/src/utils/token.ts
 *
 * 为什么用 addInitScript 而不是 goto 后 evaluate：
 * bind/matrix 页 onMounted 有登录守卫（未登录 reLaunch 到 login），
 * 且 pinia authStore 的 isLoggedIn 只在初始化时读一次。若先加载页面
 * 再写 token，store 已初始化为 false，写 token 不会更新，守卫仍拦截。
 * addInitScript 在页面任何脚本执行前注入 localStorage（H5 端
 * uni.setStorageSync 底层即 localStorage，key 原样），确保 app 初始化时读到。
 * @param page Playwright Page 实例
 */
export async function forceTechLoginMock(page: Page) {
  await page.addInitScript(({ token, id }) => {
    try {
      localStorage.setItem('bracesync_tech_token', token)
      localStorage.setItem('bracesync_tech_id', id)
    } catch {
      // 忽略非浏览器上下文异常
    }
  }, { token: MOCK_TECH_TOKEN, id: MOCK_TECH_ID })
  await page.goto(techRoutes.bind) // 加载即已登录
}

/**
 * 执行真实登录流程（仅用于本地验证登录页 UI，CI 跳过）
 * @description 对齐 T089 登录页交互：导航/login → 填手机号/密码 → 勾选协议 → 点击登录 → 跳转 home
 * @param page Playwright Page 实例
 */
export async function doTechRealLogin(page: Page) {
  await page.goto(techRoutes.login)

  // 填充手机号（第一个 .input-field）
  const phoneInput = page.locator('.input-field').first()
  await fillTechInput(phoneInput, TECH_PHONE)

  // 填充密码（第二个 .input-field）
  const pwdInput = page.locator('.input-field').nth(1)
  await fillTechInput(pwdInput, 'admin123') // T041 播种密码

  // 勾选协议（必须）
  await page.locator('.agree-row .checkbox').click()

  // 点击登录按钮
  await page.locator('.btn-primary', { hasText: '登录' }).click()

  // 断言跳转至 home 页（T089: 登录后跳首页，非 bind）
  await page.waitForURL('**/pages/home/**', { timeout: 10_000 })
  await expect(page.getByText('开始工作')).toBeVisible({ timeout: 10_000 })
}

/**
 * Mock BLE 模块（H5 环境下蓝牙不可用，拦截 ble.ts 改写 H5 分支）
 *
 * T089 实现中 H5 下 initBluetooth 抛错、createBLEConnection 返回 false、
 * discoverDevices 返回空数组，导致 install 阶段②校准按钮禁用、bind BLE 扫描无设备。
 * 通过 page.route 拦截 /src/utils/ble.ts，将整个模块替换为 H5 友好的 mock 版本，
 * 使 bind→install 校准链路与 BLE 扫描可在 E2E 中跑通。
 */
export async function mockTechBLE(page: Page) {
  await page.route('**/utils/ble.ts', async (route) => {
    const mockBody = `
// T089 E2E MOCK: H5 蓝牙友好版（替换原始 ble.ts）
const isH5 = () => true

export async function initBluetooth() { return true }

export async function discoverDevices() {
  await new Promise(r => setTimeout(r, 800))
  return [
    { deviceId: 'PRS-ML05-RC-001', name: 'PRS-ML05-RC-001', RSSI: -45 },
  ]
}

export async function createBLEConnection(deviceId) { return true }

export async function writeWiFiConfig(ssid, password) { return true }

export async function closeBLEConnection(deviceId) { return }

export async function readCalibrationData(deviceId) { return [] }

export async function writeCalibrationCommand(deviceId, command) { return true }

export async function readFirmwareVersion(deviceId) {
  return { deviceId, firmware: 'v1.2.3', battery: 85 }
}

// ===== 实时压力推送 mock（1Hz，20 点接近 0 的随机值）=====
let realtimeTimer = null
let realtimeCallback = null

export async function startRealtimePressure(deviceId) {
  realtimeTimer = setInterval(() => {
    const frame = Array.from({ length: 20 }, () => Math.random() * 0.3 - 0.15)
    realtimeCallback && realtimeCallback(frame)
  }, 1000)
}

export async function stopRealtimePressure(deviceId) {
  if (realtimeTimer) { clearInterval(realtimeTimer); realtimeTimer = null }
}

export function onRealtimeFrame(cb) { realtimeCallback = cb }

// ===== WiFi 配网 mock =====
let wifiStatusTimer = null
let wifiStatusCallback = null

export async function writeWifiConfigV2(deviceId, encryptedHex) { return true }

export function onWifiStatus(cb) { wifiStatusCallback = cb }

export function startMockWifiStatusSequence() {
  const seq = [0, 1, 2, 3, 9]
  let idx = 0
  if (wifiStatusTimer) clearInterval(wifiStatusTimer)
  wifiStatusTimer = setInterval(() => {
    if (idx < seq.length) {
      wifiStatusCallback && wifiStatusCallback(seq[idx])
      idx++
    } else {
      if (wifiStatusTimer) clearInterval(wifiStatusTimer)
    }
  }, 500)
}

export function stopMockWifiStatusSequence() {
  if (wifiStatusTimer) { clearInterval(wifiStatusTimer); wifiStatusTimer = null }
}

export async function readDeviceInfo(deviceId) {
  return { deviceId, firmware: 'v1.2.3', battery: 85 }
}
`
    await route.fulfill({
      status: 200,
      contentType: 'application/javascript',
      body: mockBody,
    })
  })
}



/** uni.showToast 渲染为 uni-toast 自定义元素 */
export function uniToast(page: Page, text: string | RegExp) {
  return page.locator('uni-toast').filter({ hasText: text })
}

/** bind 页自实现 toast（非 uni.showToast，v-if 控制 .toast/.toast-text） */
export function bindToast(page: Page) {
  return {
    root: page.locator('.toast'),
    text: page.locator('.toast-text'),
  }
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


