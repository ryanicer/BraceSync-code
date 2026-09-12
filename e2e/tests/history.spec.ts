import { test, expect } from '@playwright/test'
import { routes, setupPatientE2E } from '../helpers'

/**
 * history 页：佩戴异常/压力异常双 Tab 切换、列表渲染
 * mock 数据：15 条佩戴记录（7 月 12 条 + 6 月 3 条），7 组压力异常（默认展开前 3 组，共 6 条明细）
 * T074 真实模式基建：beforeEach 顶部注入登录态 + route 拦截
 */

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await page.goto(routes.history)
  await expect(page.getByText('异常监测').first()).toBeVisible()
})

test('默认佩戴异常 Tab 渲染列表', async ({ page }) => {
  const segBtn = (label: string) => page.locator('.segmented .seg-btn', { hasText: label })
  await expect(segBtn('佩戴异常')).toHaveClass(/seg-active/)

  // 按月分组头
  await expect(page.locator('.month-header', { hasText: '2026年7月' })).toBeVisible()
  await expect(page.locator('.month-header', { hasText: '2026年6月' })).toBeVisible()
  // 15 条佩戴记录
  await expect(page.locator('.wearing-row')).toHaveCount(15)
  // 状态标签渲染（达标/不足/严重不足）
  await expect(page.locator('.w-tag', { hasText: '达标' }).first()).toBeVisible()
  await expect(page.locator('.w-tag', { hasText: '严重不足' }).first()).toBeVisible()
})

test('切换到压力异常 Tab 渲染异常分组', async ({ page }) => {
  await page.locator('.segmented .seg-btn', { hasText: '压力异常' }).click()
  await expect(page.locator('.segmented .seg-btn', { hasText: '压力异常' })).toHaveClass(/seg-active/)

  // 7 组按日期分组的异常
  await expect(page.locator('.p-group')).toHaveCount(7)
  await expect(page.locator('.p-group-date', { hasText: '2026-07-12' })).toBeVisible()
  // 异常条数徽标
  await expect(page.locator('.p-group-badge', { hasText: '条异常' }).first()).toBeVisible()
  // 默认展开前 3 组：可见异常明细项 3+2+1=6
  await expect(page.locator('.p-group-body')).toHaveCount(3)
  await expect(page.locator('.p-item')).toHaveCount(6)
  await expect(page.locator('.p-item-detail', { hasText: '持续偏高' }).first()).toBeVisible()
})

test('压力异常分组可折叠/展开', async ({ page }) => {
  await page.locator('.segmented .seg-btn', { hasText: '压力异常' }).click()
  await expect(page.locator('.p-group-body')).toHaveCount(3)
  // 点击第一组头部收起
  await page.locator('.p-group-header').first().click()
  await expect(page.locator('.p-group-body')).toHaveCount(2)
  // 再点一次展开
  await page.locator('.p-group-header').first().click()
  await expect(page.locator('.p-group-body')).toHaveCount(3)
})

test('Tab 来回切换状态保持', async ({ page }) => {
  await page.locator('.segmented .seg-btn', { hasText: '压力异常' }).click()
  await expect(page.locator('.p-group')).toHaveCount(7)
  await page.locator('.segmented .seg-btn', { hasText: '佩戴异常' }).click()
  await expect(page.locator('.wearing-row')).toHaveCount(15)
})
