import { test, expect } from '@playwright/test'
import { routes, setupPatientE2E } from '../helpers'
import { wearing15 } from '../../apps/patient-miniapp/tests/e2e/fixtures/patient'

/**
 * anomaly 页（T221 设计稿日历版）：日历月视图 + 佩戴/压力分段 + 点日期详情卡
 * mock 数据：15 条佩戴记录（2026-06-28 → 2026-07-12）、7 组压力异常
 * 合成圆点（佩戴+压力取严）：2026年7月 error=07-08/07-10/07-12（3 红），warn=02/04/05/07/09/11（6 橙）
 */

function todayKey(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** 逐月回退直到标题为 2026年7月（mock 数据所在月份，与运行日期解耦） */
async function gotoJuly2026(page: import('@playwright/test').Page) {
  for (let i = 0; i < 24; i++) {
    const title = await page.locator('.cal-title').textContent()
    if (title === '2026年7月') return
    await page.locator('.cal-nav').first().click()
  }
  throw new Error('未能导航到 2026年7月')
}

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await page.goto(routes.anomaly)
  await expect(page.locator('.page-title')).toHaveText('异常监测')
})

test('默认当月日历 + 图例 + 佩戴分段激活', async ({ page }) => {
  const now = new Date()
  await expect(page.locator('.cal-title')).toHaveText(`${now.getFullYear()}年${now.getMonth() + 1}月`)
  // 表头周日起始（uni-app H5：<text> 编译为 <uni-text>）
  await expect(page.locator('.cal-weekdays uni-text').first()).toHaveText('日')
  await expect(page.locator('.cal-weekdays uni-text').last()).toHaveText('六')
  // 图例三项
  await expect(page.locator('.cal-legend-item')).toHaveCount(3)
  await expect(page.locator('.cal-legend-item', { hasText: '严重异常' })).toBeVisible()
  await expect(page.locator('.cal-legend-item', { hasText: '警告' })).toBeVisible()
  await expect(page.locator('.cal-legend-item', { hasText: '无异常' })).toBeVisible()
  // 佩戴分段默认激活
  await expect(page.locator('.segmented .seg-btn', { hasText: '佩戴异常' })).toHaveClass(/seg-active/)
  // 当月格子数与当月天数一致
  const daysInMonth = new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate()
  await expect(page.locator('.cal-cell:not(.cal-empty)')).toHaveCount(daysInMonth)
})

test('初始详情卡随今天数据有无而定', async ({ page }) => {
  if (wearing15().some((r) => r.date === todayKey())) {
    await expect(page.locator('.detail-card .detail-date-header')).toContainText(todayKey())
  } else {
    await expect(page.locator('.detail-empty', { hasText: '该日期无佩戴记录' })).toBeVisible()
  }
})

test('切到 2026年7月 显示 3 红 6 橙异常圆点', async ({ page }) => {
  await gotoJuly2026(page)
  await expect(page.locator('.cal-dot.error')).toHaveCount(3)
  await expect(page.locator('.cal-dot.warn')).toHaveCount(6)
})

test('点选日期展示佩戴详情卡（阈值线 0/16/18h）', async ({ page }) => {
  await gotoJuly2026(page)
  await page.locator('.cal-cell', { has: page.locator('.cal-num', { hasText: /^8$/ }) }).click()
  const card = page.locator('.detail-card')
  await expect(card.locator('.detail-date-header')).toHaveText('2026-07-08 · 严重不足')
  await expect(card.locator('.dwh-value')).toHaveText('3.1')
  await expect(card.locator('.detail-bar-labels')).toContainText('目标 16h')
  await expect(card.locator('.detail-hint-warn')).toContainText('低于医生建议的 16h 目标')
  // 选中态高亮
  await expect(page.locator('.cal-cell.cal-sel .cal-num')).toHaveText('8')
})

test('压力分段：点日期展示异常事件列表', async ({ page }) => {
  await gotoJuly2026(page)
  await page.locator('.segmented .seg-btn', { hasText: '压力异常' }).click()
  await expect(page.locator('.segmented .seg-btn', { hasText: '压力异常' })).toHaveClass(/seg-active/)
  // 默认选中今天 → 无压力异常事件
  await expect(page.locator('.detail-empty', { hasText: '该日期无压力异常事件' })).toBeVisible()
  // 点 07-12 → 3 条异常
  await page.locator('.cal-cell', { has: page.locator('.cal-num', { hasText: /^12$/ }) }).click()
  const card = page.locator('.detail-card')
  await expect(card.locator('.detail-date-header')).toContainText('2026-07-12')
  await expect(card.locator('.detail-date-header')).toContainText('3条异常')
  await expect(card.locator('.ap-item')).toHaveCount(3)
  await expect(card.locator('.ap-item-point').first()).toHaveText('P10')
  await expect(card.locator('.ap-point-error').first()).toBeVisible()
})

test('分段切换后详情随选中日期联动', async ({ page }) => {
  await gotoJuly2026(page)
  // 佩戴分段：点 07-08 → 3.1h
  await page.locator('.cal-cell', { has: page.locator('.cal-num', { hasText: /^8$/ }) }).click()
  await expect(page.locator('.dwh-value')).toHaveText('3.1')
  // 切压力分段：同一日期 → 2 条 error 事件
  await page.locator('.segmented .seg-btn', { hasText: '压力异常' }).click()
  await expect(page.locator('.detail-date-header')).toContainText('2026-07-08')
  await expect(page.locator('.ap-item')).toHaveCount(2)
})
