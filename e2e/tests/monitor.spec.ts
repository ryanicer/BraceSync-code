import { test, expect, type Page } from '@playwright/test'
import { routes, modal, setupPatientE2E } from '../helpers'

/**
 * monitor 页：日/周/月切换、热力图渲染（4×5 网格）、点选联动数值、下拉刷新
 * mock 数据：20 个传感器点（P01-P20），P12 固定 42.18N
 * T074 真实模式基建：beforeEach 顶部注入登录态 + route 拦截（setupPatientE2E），断言体不动
 */

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await page.goto(routes.monitor)
  await expect(page.getByText('压力分布热力图')).toBeVisible()
})

test('日/周/月切换联动趋势区', async ({ page }) => {
  const segBtn = (label: string) => page.locator('.segmented .seg-btn', { hasText: label })
  // 默认日维度
  await expect(segBtn('日')).toHaveClass(/seg-active/)
  await expect(page.locator('.trend-section .section-title')).toContainText('今日压力趋势')

  await segBtn('周').click()
  await expect(segBtn('周')).toHaveClass(/seg-active/)
  await expect(segBtn('日')).not.toHaveClass(/seg-active/)
  await expect(page.locator('.trend-section .section-title')).toContainText('本周压力趋势')

  await segBtn('月').click()
  await expect(segBtn('月')).toHaveClass(/seg-active/)
  await expect(page.locator('.trend-section .section-title')).toContainText('本月压力趋势')
})

test('热力图渲染 4×5 网格（20 个传感器点）', async ({ page }) => {
  await expect(page.locator('.heatmap-label')).toContainText('4×5 网格')
  await expect(page.locator('.sensor-grid .grid-row')).toHaveCount(4)
  await expect(page.locator('.sensor-grid .grid-cell')).toHaveCount(20)
  // 首尾点 ID 渲染
  await expect(page.locator('.grid-cell .cell-id', { hasText: 'P01' })).toBeVisible()
  await expect(page.locator('.grid-cell .cell-id', { hasText: 'P20' })).toBeVisible()
  // 默认选中最大压力点：hero 数值非占位符
  await expect(page.locator('.hero-number')).not.toHaveText('--')
})

test('热力图点选联动 hero 数值', async ({ page }) => {
  const heroLabel = page.locator('.hero-label')
  // 点选 P01
  await page.locator('.grid-cell', { has: page.locator('.cell-id', { hasText: 'P01' }) }).click()
  await expect(heroLabel).toContainText('P01 · 当前压力值')
  await expect(page.locator('.hero-number')).toHaveText(/^\d+\.\d{2}$/)
  // 趋势区标题联动点位
  await expect(page.locator('.trend-section .section-title')).toContainText('P01')

  // 再点选 P20，数值联动变化
  await page.locator('.grid-cell', { has: page.locator('.cell-id', { hasText: 'P20' }) }).click()
  await expect(heroLabel).toContainText('P20 · 当前压力值')
  await expect(page.locator('.trend-section .section-title')).toContainText('P20')
})

/**
 * T444 M-1：热力图详情行在「患者点选前」显示稿面 monitor.html:102 默认句，
 * 点选后才显示「点位 · 数值N · 阈值上限 N」数值行。
 */
test('热力图详情行：点选前出默认句，点选后出数值行', async ({ page }) => {
  const detail = page.locator('.heatmap-detail')
  // 首屏（页面预选最大点位供 hero/趋势联动，但用户未点选）→ 默认句，非数值行
  await expect(detail).toHaveText('点击网格查看详情')
  // 点选 P01 → 数值行，含点位与「阈值上限」
  await page.locator('.grid-cell', { has: page.locator('.cell-id', { hasText: 'P01' }) }).click()
  await expect(detail).toContainText('P01')
  await expect(detail).toContainText('阈值上限')
  await expect(detail).not.toHaveText('点击网格查看详情')
})

/**
 * T444 M-2：实时快照无帧（pressureRecords 空、点位不渲染）时，
 * 趋势区标题不得留前置分隔符「 · 今日压力趋势」，应为「今日压力趋势」。
 */
test('趋势标题：无点位时丢掉前置分隔符', async ({ page }) => {
  // setupPatientE2E 已注册通用 realtime mock；后注册先咨询（LIFO），覆盖成空帧快照
  await page.route(/\/api\/v1\/patients\/[^/]+\/realtime$/, (route) =>
    route.fulfill({
      json: { code: 0, message: 'ok', data: { deviceId: 'e2e', status: 'online', battery: 80, pressureRecords: [] } },
    }),
  )
  await page.goto(routes.monitor)
  await expect(page.locator('.hero-number')).toHaveText('--')
  const title = page.locator('.trend-section .section-title')
  await expect(title).toHaveText('今日压力趋势')
  await expect(title).not.toContainText('·')
})

/**
 * 下拉刷新：uni-app H5 的 onPullDownRefresh 需要真实（trusted）触摸事件，
 * 合成 TouchEvent 不被接受，改用 CDP Input.dispatchTouchEvent 模拟手指下拉。
 */
async function pullDown(page: Page) {
  const cdp = await page.context().newCDPSession(page)
  const tp = (y: number) => [{ x: 200, y, radiusX: 5, radiusY: 5, force: 1 }]
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: tp(150) })
  for (let y = 210; y <= 650; y += 55) {
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: tp(y) })
    await page.waitForTimeout(24)
  }
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  await cdp.detach()
}

test('下拉刷新触发数据重载', async ({ page }) => {
  await pullDown(page)
  await expect(page.locator('uni-toast').filter({ hasText: '数据已刷新' })).toBeVisible({ timeout: 10_000 })
})
