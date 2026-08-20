import { test, expect } from '@playwright/test'
import { techRoutes } from '../tech-helpers'

/**
 * tech-matrix 页：6 阶段安装流程（患者确认→安装定位→校准→基线→WiFi→签名）
 * 对齐 T018 技师端 matrix 页安装流程（mock 数据，不依赖后端）
 *
 * 注意：matrix 页 currentStage 为组件级 ref，navigateTo save-baseline 后
 * navigateBack 回到 matrix 时组件重建（stage 重置为 1），因此阶段 5/6
 * 的完整交互在独立 spec 中覆盖。
 */

/** 快速推进 matrix 阶段（从当前阶段到目标阶段前一步） */
async function advanceToStage(page: any, target: number) {
  if (target >= 2) await page.locator('.btn-primary', { hasText: '确认，下一步' }).click()
  if (target >= 3) await page.locator('.btn-primary', { hasText: '确认安装，下一步' }).click()
  if (target >= 4) {
    await page.locator('.btn-outline', { hasText: '开始校准' }).click()
    await expect(page.getByText('校准完成')).toBeVisible({ timeout: 15_000 })
    await page.locator('.btn-primary', { hasText: '校准完成，下一步' }).click()
  }
}

test.describe('安装流程 6 阶段', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(techRoutes.matrix)
  })

  test('步骤条渲染 6 个阶段', async ({ page }) => {
    await expect(page.locator('.stage')).toHaveCount(6)
    await expect(page.locator('.stage-label').first()).toHaveText('患者确认')
    await expect(page.locator('.stage-label').last()).toHaveText('签名完成')
  })

  test('阶段 1：患者信息确认并推进', async ({ page }) => {
    // 卡片标题可能和步骤标签重复，用 .card-title 限定
    await expect(page.locator('.card-title', { hasText: '患者信息确认' })).toBeVisible()
    await expect(page.getByText('张小明')).toBeVisible()
    await expect(page.getByText('pat-001')).toBeVisible()
    await expect(page.getByText('14 岁')).toBeVisible()
    await expect(page.getByText('胸腰段脊柱侧弯')).toBeVisible()
    await expect(page.getByText('28°')).toBeVisible()
    await page.locator('.btn-primary', { hasText: '确认，下一步' }).click()
    await expect(page.locator('.card-title', { hasText: '传感器安装定位' })).toBeVisible({ timeout: 5_000 })
  })

  test('阶段 2：传感器安装定位（20 点网格）', async ({ page }) => {
    await advanceToStage(page, 2)
    await expect(page.locator('.card-title', { hasText: '传感器安装定位' })).toBeVisible()
    await expect(page.locator('.sensor-cell-mini')).toHaveCount(20)
    await expect(page.getByText('确认 20 个传感器全部贴合到位后继续')).toBeVisible()
    await page.locator('.btn-primary', { hasText: '确认安装，下一步' }).click()
    await expect(page.locator('.card-title', { hasText: '设备校准' })).toBeVisible({ timeout: 5_000 })
  })

  test('阶段 3：设备校准（进度条 + mock 采集）', async ({ page }) => {
    await advanceToStage(page, 3)
    await expect(page.locator('.card-title', { hasText: '设备校准' })).toBeVisible()
    await page.locator('.btn-outline', { hasText: '开始校准' }).click()
    await expect(page.locator('.progress-fill')).toBeVisible()
    await expect(page.getByText('校准完成')).toBeVisible({ timeout: 15_000 })
    await page.locator('.btn-primary', { hasText: '校准完成，下一步' }).click()
    await expect(page.locator('.card-title', { hasText: '保存基线数据' })).toBeVisible({ timeout: 5_000 })
  })

  test('阶段 4：保存基线入口跳转', async ({ page }) => {
    await advanceToStage(page, 4)
    await expect(page.locator('.card-title', { hasText: '保存基线数据' })).toBeVisible()
    await expect(page.getByText('20 点 offset_values')).toBeVisible()
    await page.locator('.btn-primary', { hasText: '保存基线' }).click()
    await page.waitForURL('**/pages/save-baseline/**', { timeout: 10_000 })
  })
})

test.describe('签名与完成安装', () => {
  test.setTimeout(60_000)

  test('阶段 6：从 matrix 到 save-baseline 再返回', async ({ page }) => {
    await page.goto(techRoutes.matrix)
    await advanceToStage(page, 4)
    await page.locator('.btn-primary', { hasText: '保存基线' }).click()
    await page.waitForURL('**/pages/save-baseline/**', { timeout: 10_000 })
    await page.locator('.btn-primary', { hasText: '确认保存基线' }).click()
    // navigateBack 触发返回（URL 变化即可验证）
    await expect(page).not.toHaveURL(/pages\/save-baseline/, { timeout: 15_000 })
  })

  test('初始阶段不显示完成安装按钮', async ({ page }) => {
    await page.goto(techRoutes.matrix)
    await expect(page.locator('.btn-primary', { hasText: '完成安装' })).not.toBeVisible()
  })
})
