import { test, expect } from '@playwright/test'
import type { Locator, Page } from '@playwright/test'
import { routes, fillUniInput, setupPatientE2E } from '../helpers'

/**
 * T192 患者端配网 — 按设计稿 11 页 + PRD §7A.9/§7A.9.1 重写
 *
 * 页面链路（单路由多视图，视图名与设计稿文件一一对应）：
 *   01-entry → 02-scan → 03-connect → 04-progress → 05-success
 *                                 └→ 06a–06e（五类失败态）→ 07-contact
 *
 * H5 mock 边界（utils/ble.ts）：
 *   - 蓝牙适配器/位置权限：H5 直接视为就绪
 *   - 扫描：800ms 后推入 BSYNC- 设备，6s 结束
 *   - 状态机：写 B511 后按 0→1→2→3→9（每帧 500ms）推进；
 *     可用 ?mock=0,-1 之类的查询串注入失败序列（H5 专用，真机不受影响）
 *   - 真机项（真实 BLE 建连、路由器重启、5G 频段）不在本文件覆盖范围
 *
 * 断言一律限定在 uni-page-body：uni-page-head 里是同页导航标题，
 * 与页面主文案同名时会命中两个元素（如「配网成功」）。
 */

const START_BTN = '开始配置家庭 WiFi'
const SSID_VALUE = 'Home_WiFi_2.4G'
const PWD_VALUE = 'secret123'

const body = (page: Page): Locator => page.locator('uni-page-body')
const ssidInput = (page: Page) => page.locator('uni-input.input-field').first().locator('input')
const pwdInput = (page: Page) => page.locator('uni-input.input-field').nth(1).locator('input')

/** uni-app H5 的 toast 文案节点 */
const toastText = (page: Page, text: string) =>
  page.locator('.uni-simple-toast__text', { hasText: text })

/** hash 路由下仅变 query 是同文档导航，会复用上一个页面实例 → 必须整页重载回 01-entry */
async function openPage(page: Page, query = '') {
  await page.goto(`${routes.wifiSetup}${query}`)
  await page.reload()
}

/** 进入页面并走到 03-connect 表单（01 → 02 扫描 → 选中设备 → BLE 建连） */
async function gotoConnectForm(page: Page, query = '') {
  await openPage(page, query)
  await expect(body(page).getByText('配置前准备')).toBeVisible()
  await body(page).getByText(START_BTN).click()

  // 02-scan：列表只出现 BSYNC- 前缀设备，信号只说"良好/弱"
  await expect(body(page).getByText('正在搜索附近的监测器')).toBeVisible()
  const item = body(page).locator('.device-item').first()
  await expect(item).toBeVisible({ timeout: 5_000 })
  await expect(item).toContainText('BSYNC-')
  await expect(item).toContainText('信号良好')
  await expect(item).not.toContainText('dBm')
  await item.click()

  // 03-connect：先建立 BLE 连接，才允许进入凭据输入
  await expect(body(page).getByText('正在连接您的监测器')).toBeVisible()
  await expect(body(page).getByText('连接中')).toBeVisible()
  await expect(body(page).getByText('已连接')).toBeVisible({ timeout: 10_000 })
  await body(page).getByText('连接成功，下一步').click()
  await expect(body(page).getByText('家庭 WiFi 名称')).toBeVisible()
}

/** 从 03 表单发起配网 */
async function startProvision(page: Page, ssid = SSID_VALUE, pwd = PWD_VALUE) {
  await fillUniInput(ssidInput(page), ssid)
  await fillUniInput(pwdInput(page), pwd)
  await body(page).getByText('开始配网').click()
}

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
})

test('01-entry：入口卡片 + 三项前置 + 2.4GHz 前置提示', async ({ page }) => {
  await openPage(page)

  await expect(body(page).getByText('当前设备')).toBeVisible()
  await expect(body(page).getByText('矫形支具监测器')).toBeVisible()
  // 患者端只给广播名，不给完整 device_id（PRD §7A.9 患技差异表）
  await expect(body(page).getByText(/设备编号 BSYNC-\S+ · (未|已)连接家庭网络/)).toBeVisible()
  await expect(body(page).getByText('打开手机蓝牙')).toBeVisible()
  await expect(body(page).getByText('允许位置权限')).toBeVisible()
  await expect(body(page).getByText('设备已上电')).toBeVisible()
  await expect(body(page).getByText('本设备仅支持 2.4GHz 家庭 WiFi')).toBeVisible()
  // 设备上电无法前端探测 → 不给假绿态
  await expect(body(page).getByText('请确认')).toBeVisible()

  // H5 无真实蓝牙：前置判定为已开启/已授权，点击直达扫描页
  await expect(body(page).getByText('已开启')).toBeVisible()
  await expect(body(page).getByText('已授权')).toBeVisible()
  await body(page).getByText(START_BTN).click()
  await expect(body(page).getByText('正在搜索附近的监测器')).toBeVisible()
})

test('02-scan：扫描列表只含 BSYNC- 设备且无 RSSI 数值', async ({ page }) => {
  await openPage(page)
  await body(page).getByText(START_BTN).click()

  const item = body(page).locator('.device-item').first()
  await expect(item).toBeVisible({ timeout: 5_000 })
  await expect(body(page).locator('.device-item')).toHaveCount(1)
  await expect(item).toContainText('信号良好')
  await expect(body(page).locator('.scan')).not.toContainText(/-?\d+\s*dBm/)
})

test('逐页导航标题随视图切换（设计稿 01/02/03/04 标题）', async ({ page }) => {
  await openPage(page)
  await expect(page).toHaveTitle('配置家庭 WiFi')
  await body(page).getByText(START_BTN).click()
  await expect(page).toHaveTitle('搜索监测器')
  await expect(body(page).locator('.device-item').first()).toBeVisible({ timeout: 5_000 })
  await body(page).locator('.device-item').first().click()
  await expect(page).toHaveTitle('连接监测器')
  await expect(body(page).getByText('已连接')).toBeVisible({ timeout: 10_000 })
  await body(page).getByText('连接成功，下一步').click()
  await startProvision(page)
  await expect(page).toHaveTitle('正在配置')
})

test('03-connect：凭据校验在下发之前，空表单不进 04', async ({ page }) => {
  await gotoConnectForm(page)

  await body(page).getByText('开始配网').click()
  await expect(toastText(page, '请输入 WiFi 名称')).toBeVisible()
  await expect(page.locator('.progress-ring')).toHaveCount(0)
  await expect(body(page).getByText('家庭 WiFi 名称')).toBeVisible()
})

test('03-connect：未建连时「连接成功，下一步」无效（设计稿 03 连接中态）', async ({ page }) => {
  await openPage(page)
  await body(page).getByText(START_BTN).click()
  await body(page).locator('.device-item').first().click()

  await expect(body(page).getByText('连接中')).toBeVisible()
  await body(page).getByText('连接成功，下一步').click()
  await expect(body(page).getByText('家庭 WiFi 名称')).toHaveCount(0)

  // 建连完成后才允许进入表单
  await expect(body(page).getByText('已连接')).toBeVisible({ timeout: 10_000 })
  await body(page).getByText('连接成功，下一步').click()
  await expect(body(page).getByText('家庭 WiFi 名称')).toBeVisible()
})

test('03-connect：WiFi 名称引导弹窗四步', async ({ page }) => {
  await gotoConnectForm(page)
  await body(page).getByText('WiFi 名称怎么找？').click()
  await expect(page.locator('.modal-head')).toContainText('WiFi 名称怎么找？')
  await expect(page.locator('.modal-step')).toHaveCount(4)
  await body(page).getByText('我知道了').click()
  await expect(page.locator('.modal-head')).toHaveCount(0)
})

test('03-connect：密码显隐切换', async ({ page }) => {
  await gotoConnectForm(page)
  const pwd = pwdInput(page)
  await fillUniInput(pwd, PWD_VALUE)
  await expect(pwd).toHaveAttribute('type', 'password')
  await body(page).getByText('显示').click()
  await expect(pwd).toHaveAttribute('type', 'text')
  await body(page).getByText('隐藏').click()
  await expect(pwd).toHaveAttribute('type', 'password')
})

test('03-connect：网络名前后空格自动清理（§7A.9.1 ③-2）', async ({ page }) => {
  test.setTimeout(90_000)
  await gotoConnectForm(page)
  await startProvision(page, `  ${SSID_VALUE}  `)

  await expect(body(page).getByText('配网成功')).toBeVisible({ timeout: 30_000 })
  // 成功页展示清理后的名称，不是带空格的原值
  await expect(body(page).getByText(SSID_VALUE, { exact: true })).toBeVisible()
})

test('04→05：状态机 0→1→2→3→9 全链路到配网成功', async ({ page }) => {
  test.setTimeout(90_000)
  await gotoConnectForm(page)
  await startProvision(page)

  // 04-progress：四步清单（对应状态 0/1/2/3），患者口径无状态码
  const list = page.locator('.step-list')
  await expect(list.locator('.step')).toHaveCount(4)
  for (const label of ['正在同步设置', '正在连接家庭 WiFi', '网络连接成功', '正在连接服务器']) {
    await expect(list.getByText(label)).toBeVisible()
  }
  await expect(list).not.toContainText(/状态码|B51\d|Notify/)

  // 05-success
  await expect(body(page).getByText('配网成功')).toBeVisible({ timeout: 30_000 })
  await expect(body(page).getByText('您的监测器已连接家庭网络')).toBeVisible()
  await expect(body(page).getByText('连接信息')).toBeVisible()
  await expect(body(page).getByText('已联网')).toBeVisible()
  await expect(body(page).getByText('实时查看孩子的支具佩戴压力数据')).toBeVisible()

  // 「查看佩戴数据」→ 监测页
  await body(page).getByText('查看佩戴数据').click()
  await page.waitForURL('**/pages/monitor/**', { timeout: 10_000 })
})

test('05-success：返回设备管理', async ({ page }) => {
  test.setTimeout(90_000)
  await gotoConnectForm(page)
  await startProvision(page)
  await expect(body(page).getByText('配网成功')).toBeVisible({ timeout: 30_000 })
  await body(page).getByText('返回设备管理').click()
  await page.waitForURL('**/pages/device/**', { timeout: 10_000 })
})

test.describe('06a–06e：五类失败态各自独立页', () => {
  const cases: Array<{ query: string; title: string; action: string; primary: string }> = [
    {
      query: '?mock=0,-1',
      title: 'WiFi 密码不正确',
      action: '回到上一步，重新输入 WiFi 密码（注意区分大小写）',
      primary: '重新输入密码',
    },
    {
      query: '?mock=0,-2',
      title: '找不到您的 WiFi 网络',
      action: '检查 WiFi 名称是否输入正确（区分大小写，名称前后不要有空格）',
      primary: '换一个网络',
    },
    {
      query: '?mock=0,1,-3',
      title: '网络连接异常',
      action: '拔掉路由器电源，等待 10 秒后重新插上',
      primary: '重启后重试',
    },
    {
      query: '?mock=0,1,2,-4',
      title: '暂时无法连接服务器',
      action: '确认您的 WiFi 能正常上网',
      primary: '稍后重试',
    },
  ]

  for (const c of cases) {
    test(c.title, async ({ page }) => {
      await gotoConnectForm(page, c.query)
      await startProvision(page)

      await expect(body(page).getByText(c.title)).toBeVisible({ timeout: 20_000 })
      await expect(page.locator('.action-item')).toHaveCount(3)
      await expect(body(page).getByText(c.action)).toBeVisible()
      await expect(body(page).getByText('建议操作')).toBeVisible()
      await expect(body(page).getByText('联系技师', { exact: true })).toBeVisible()
      await expect(body(page).getByText(c.primary)).toBeVisible()

      // 失败页不给技术术语（PRD §7A.9 约束）
      await expect(body(page).locator('.failure')).not.toContainText(/DHCP|SSID|B51\d|状态码/)
    })
  }

  test('06e：15s 无状态推送 → 设备响应超时', async ({ page }) => {
    test.setTimeout(90_000)
    // 只推第一帧后不再推送，触发 PRD §7A.9 的 15s 无响应超时
    await gotoConnectForm(page, '?mock=0')
    await startProvision(page)

    await expect(body(page).getByText('设备响应超时')).toBeVisible({ timeout: 25_000 })
    await expect(body(page).getByText('重新配网')).toBeVisible()
  })

  test('06a 主按钮回 03 重填、有次按钮；06c 无次按钮', async ({ page }) => {
    await gotoConnectForm(page, '?mock=0,-1')
    await startProvision(page)
    await expect(body(page).getByText('WiFi 密码不正确')).toBeVisible({ timeout: 20_000 })
    await expect(body(page).getByText('重试配网')).toBeVisible()
    await body(page).getByText('重新输入密码').click()
    await expect(body(page).getByText('家庭 WiFi 名称')).toBeVisible()
    await expect(body(page).getByText('已连接')).toBeVisible()

    await gotoConnectForm(page, '?mock=0,1,-3')
    await startProvision(page)
    await expect(body(page).getByText('网络连接异常')).toBeVisible({ timeout: 20_000 })
    await expect(body(page).getByText('重启后重试')).toBeVisible()
    await expect(body(page).getByText('重试配网')).toHaveCount(0)
  })
})

test('07-contact：进入即提交反馈，问题类型/设备/时间三项齐', async ({ page }) => {
  await gotoConnectForm(page, '?mock=0,-1')
  await startProvision(page)
  await expect(body(page).getByText('WiFi 密码不正确')).toBeVisible({ timeout: 20_000 })

  await body(page).getByText('联系技师', { exact: true }).click()
  await expect(body(page).getByText('正在为您联系客服')).toBeVisible()
  await expect(body(page).getByText('已附上的信息')).toBeVisible()
  await expect(body(page).getByText('问题类型')).toBeVisible()
  await expect(body(page).getByText('WiFi 密码不正确').first()).toBeVisible()
  await expect(body(page).getByText('BSYNC-')).toBeVisible()
  await expect(body(page).getByText('发生时间')).toBeVisible()
  await expect(body(page).getByText(/\d{4}-\d{2}-\d{2} \d{2}:\d{2}/)).toBeVisible()
  await expect(page.locator('.step')).toHaveCount(3)
  await expect(body(page).getByText('打开客服对话')).toBeVisible()

  // 后端 POST /api/v1/feedbacks 尚未落地（T192 上报 §D1）：
  // 第 1 步必须如实显示提交失败，不得画成已完成
  await expect(body(page).getByText('问题信息提交失败，请在对话中直接说明情况')).toBeVisible({
    timeout: 15_000,
  })
  await expect(body(page).getByText('问题信息已提交到客服系统')).toHaveCount(0)
})
