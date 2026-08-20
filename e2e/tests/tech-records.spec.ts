import { test, expect } from '@playwright/test'
import { techRoutes, uniModal } from '../tech-helpers'

/**
 * tech-records 页：安装记录列表（筛选/详情弹窗）
 * 对齐 T018 技师端 records 页功能（mock 数据，不依赖后端）
 */

test.beforeEach(async ({ page }) => {
  await page.goto(techRoutes.records)
})

test('页面标题与记录总数', async ({ page }) => {
  await expect(page.getByText('安装记录').first()).toBeVisible()
  await expect(page.getByText('共 5 条记录')).toBeVisible()
})

test('默认显示全部记录', async ({ page }) => {
  await expect(page.locator('.record-card')).toHaveCount(5)
  await expect(page.locator('.record-device').first()).toHaveText('PRS-ML05-RC-002')
  await expect(page.locator('.wifi-badge').first()).toContainText('已联网')
})

test('筛选已联网记录', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '已联网' }).click()
  await expect(page.locator('.seg-btn', { hasText: '已联网' })).toHaveClass(/seg-active/)
  await expect(page.locator('.record-card')).toHaveCount(3)
})

test('筛选待配置记录', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '待配置' }).click()
  await expect(page.locator('.seg-btn', { hasText: '待配置' })).toHaveClass(/seg-active/)
  await expect(page.locator('.record-card')).toHaveCount(2)
})

test('切回全部筛选', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '已联网' }).click()
  await expect(page.locator('.record-card')).toHaveCount(3)
  await page.locator('.seg-btn', { hasText: '全部' }).click()
  await expect(page.locator('.record-card')).toHaveCount(5)
})

test('点击记录显示详情弹窗', async ({ page }) => {
  await page.locator('.record-card').first().click()
  const m = uniModal(page)
  await expect(m.root).toBeVisible({ timeout: 5_000 })
  // uni-modal 标题和内容在内部结构中
  await expect(m.root).toContainText('安装详情')
  await expect(m.root).toContainText('PRS-ML05-RC-002')
  await expect(m.root).toContainText('pat-001')
  // 点击确定关闭
  await m.confirm.click()
  await expect(m.root).not.toBeVisible({ timeout: 5_000 })
})

test('新建安装按钮跳转到 bind 页', async ({ page }) => {
  await page.locator('.fab-btn').click()
  // navigateTo 在 H5 中触发页面导航
  await expect(page).not.toHaveURL(/pages\/records/, { timeout: 10_000 })
})
