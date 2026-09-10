import { test, expect } from '@playwright/test'
import { techRoutes, forceTechLoginMock } from '../tech-helpers'

/**
 * tech-records 页：安装记录列表（WiFi 状态 + 可达性双筛选）
 * 对齐 T089 V2.1 records 页：6 条 mock 记录、双筛选、无详情弹窗、无 FAB
 */

test.beforeEach(async ({ page }) => {
  await forceTechLoginMock(page)
  await page.goto(techRoutes.records)
})

test('页面标题与记录总数', async ({ page }) => {
  await expect(page.getByText('安装记录').first()).toBeVisible()
  await expect(page.getByText('共 6 条记录')).toBeVisible()
})

test('默认显示全部记录', async ({ page }) => {
  await expect(page.locator('.record-card')).toHaveCount(6)
  await expect(page.locator('.record-device').first()).toHaveText('PRS-ML05-RC-001')
  await expect(page.locator('.wifi-badge').first()).toContainText('已联网')
})

test('WiFi 筛选：已联网', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '已联网' }).click()
  await expect(page.locator('.seg-btn', { hasText: '已联网' })).toHaveClass(/seg-active/)
  await expect(page.locator('.record-card')).toHaveCount(3)
})

test('WiFi 筛选：待配置', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '待配置' }).click()
  await expect(page.locator('.seg-btn', { hasText: '待配置' })).toHaveClass(/seg-active/)
  await expect(page.locator('.record-card')).toHaveCount(3)
})

test('WiFi 切回全部', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '已联网' }).click()
  await expect(page.locator('.record-card')).toHaveCount(3)
  await page.locator('.seg-btn', { hasText: '全部 WiFi' }).click()
  await expect(page.locator('.record-card')).toHaveCount(6)
})

test('可达性筛选：已验证', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '已验证' }).click()
  await expect(page.locator('.seg-btn', { hasText: '已验证' })).toHaveClass(/seg-active/)
  await expect(page.locator('.record-card')).toHaveCount(2)
})

test('可达性筛选：待验证', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '待验证' }).click()
  await expect(page.locator('.seg-btn', { hasText: '待验证' })).toHaveClass(/seg-active/)
  await expect(page.locator('.record-card')).toHaveCount(2)
})

test('可达性筛选：已跳过', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '已跳过' }).click()
  await expect(page.locator('.seg-btn', { hasText: '已跳过' })).toHaveClass(/seg-active/)
  await expect(page.locator('.record-card')).toHaveCount(2)
})
