import { test, expect } from '@playwright/test'
import { techRoutes, forceTechLoginMock } from '../tech-helpers'

/**
 * tech-save-baseline 页：20 点 offset_values 基线校验（计数/范围/稳定性）+ 统计摘要
 * 对齐 T018 技师端 save-baseline 页功能（mock 数据，不依赖后端）
 */

test.beforeEach(async ({ page }) => {
  await forceTechLoginMock(page)
  // save-baseline 页从 installStore 或 mockBaseline() 获取数据
  // 直接导航即可（mock 数据自动填充）
  await page.goto(techRoutes.saveBaseline)
})

test('页面标题显示', async ({ page }) => {
  await expect(page.getByText('保存基线').first()).toBeVisible()
  await expect(page.getByText('基线数据校验')).toBeVisible()
})

test('三项校验状态全部通过', async ({ page }) => {
  // 数据点数: 20/20
  await expect(page.locator('.val-pass').filter({ hasText: '数据点数' })).toBeVisible()
  await expect(page.getByText('20/20')).toBeVisible()
  // 范围校验: 通过
  await expect(page.locator('.val-pass').filter({ hasText: '范围校验' })).toBeVisible()
  // 稳定性: 通过
  await expect(page.locator('.val-pass').filter({ hasText: '稳定性' })).toBeVisible()
})

test('20 点 offset 数据网格展示', async ({ page }) => {
  await expect(page.locator('.offset-cell')).toHaveCount(20)
  // 第一格索引 P01
  await expect(page.locator('.offset-idx').first()).toHaveText('P01')
  // 最后一格索引 P20
  await expect(page.locator('.offset-idx').last()).toHaveText('P20')
})

test('统计摘要显示四项指标', async ({ page }) => {
  await expect(page.getByText('平均值')).toBeVisible()
  await expect(page.getByText('最大值')).toBeVisible()
  await expect(page.getByText('最小值')).toBeVisible()
  await expect(page.getByText('标准差')).toBeVisible()
  // 统计值应为数字格式（非 '--'）
  const statValues = page.locator('.stat-value')
  await expect(statValues).toHaveCount(4)
  for (let i = 0; i < 4; i++) {
    const text = await statValues.nth(i).textContent()
    expect(text).not.toBe('--')
    expect(text).toMatch(/^-?\d+\.\d+$/)
  }
})

test('校验通过时可以保存基线', async ({ page }) => {
  // 确认保存按钮可用（非 btn-disabled）
  await expect(page.locator('.btn-primary', { hasText: '确认保存基线' })).toBeVisible()
  await expect(page.locator('.btn-disabled')).not.toBeVisible()
  // 点击保存
  await page.locator('.btn-primary', { hasText: '确认保存基线' }).click()
  // 显示 toast "基线已保存"
  await expect(page.locator('.toast-text')).toContainText('基线已保存', { timeout: 5_000 })
})

test('返回按钮可用', async ({ page }) => {
  await expect(page.locator('.back-link')).toBeVisible()
  await expect(page.locator('.back-link')).toContainText('返回')
})
