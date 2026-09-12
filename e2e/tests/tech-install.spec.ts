import { test, expect } from '@playwright/test'
import {
  techRoutes, fillTechInput,
  MOCK_DEVICE_ID,
  forceTechLoginMock, mockTechBLE,
} from '../tech-helpers'

/**
 * tech-install 页：安装流程 3 阶段（患者确认 → 空载校准归零 → WiFi 配网）
 * 对齐 T089 V2.1 install 页核心 UX：校准后静态压力已归零主视图
 *
 * 前置：通过 bind 流程进入 install（installId + patient + bleConnected 均已就绪）
 */

/** 走 bind 流程进入 install 页（phase 1） */
async function goToInstallViaBind(page) {
  await mockTechBLE(page)
  await forceTechLoginMock(page)
  await page.goto(techRoutes.bind)
  const deviceInput = page.locator('.section').nth(1).locator('.form-input').first()
  await fillTechInput(deviceInput, MOCK_DEVICE_ID)
  const patientInput = page.locator('.section').nth(1).locator('.form-input').nth(1)
  await fillTechInput(patientInput, 'pat-001')
  await page.locator('.btn-primary', { hasText: '绑定设备' }).click()
  await page.waitForURL('**/pages/install/**', { timeout: 15_000 })
}

/** 从 phase 1 推进到 phase 2（点击确认下一步） */
async function goToPhase2(page) {
  await page.locator('.btn-primary', { hasText: '确认，下一步' }).click()
  await expect(page.locator('.card-title', { hasText: '设备校准' })).toBeVisible({ timeout: 5_000 })
}

/** 从 phase 2 执行校准并推进到 phase 3 */
async function calibrateAndGoToPhase3(page) {
  await page.locator('.btn-primary', { hasText: '开始校准' }).click()
  // 校准完成主视图
  await expect(page.getByText('校准后静态压力已归零')).toBeVisible({ timeout: 20_000 })
  await page.locator('.btn-primary', { hasText: '校准完成，下一步' }).click()
  await expect(page.locator('.card-title', { hasText: 'WiFi 网络配置' })).toBeVisible({ timeout: 5_000 })
}

test.describe('安装流程 3 阶段', () => {
  test.beforeEach(async ({ page }) => {
    await goToInstallViaBind(page)
  })

  test('步骤条渲染 3 个阶段', async ({ page }) => {
    await expect(page.locator('.step')).toHaveCount(3)
    await expect(page.locator('.step-label').first()).toHaveText('患者确认')
    await expect(page.locator('.step-label').nth(1)).toHaveText('空载校准')
    await expect(page.locator('.step-label').last()).toHaveText('WiFi配网')
  })

  test('阶段一：患者信息确认并推进', async ({ page }) => {
    await expect(page.locator('.card-title', { hasText: '患者信息确认' })).toBeVisible()
    await expect(page.getByText('张明远')).toBeVisible()
    await expect(page.getByText('pat-001')).toBeVisible()
    await expect(page.getByText('14 岁')).toBeVisible()
    await expect(page.getByText('特发性脊柱侧弯')).toBeVisible()
    await page.locator('.btn-primary', { hasText: '确认，下一步' }).click()
    await expect(page.locator('.card-title', { hasText: '设备校准' })).toBeVisible({ timeout: 5_000 })
  })

  test('阶段二：空载校准归零（校准后静态压力已归零）', async ({ page }) => {
    await goToPhase2(page)
    // BLE 已连接（mockTechBLE），显示空载采集确认 + 开始校准按钮
    await expect(page.getByText('空载采集确认')).toBeVisible()
    await expect(page.locator('.btn-primary', { hasText: '开始校准' })).toBeVisible()

    await page.locator('.btn-primary', { hasText: '开始校准' }).click()

    // R3-2 归零矩阵主视图
    await expect(page.getByText('校准后静态压力已归零')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText('基线已保存')).toBeVisible()
    // 20 格全 0.00
    const cells = page.locator('.pressure-cell')
    await expect(cells).toHaveCount(20)
    for (let i = 0; i < 20; i++) {
      await expect(cells.nth(i)).toContainText('0.00')
    }
    // 三项校验通过
    await expect(page.getByText('数据点数：20/20')).toBeVisible()
    await expect(page.getByText('范围校验：通过')).toBeVisible()
    await expect(page.getByText('稳定性：通过')).toBeVisible()
  })

  test('阶段三：WiFi 配网入口', async ({ page }) => {
    await goToPhase2(page)
    await calibrateAndGoToPhase3(page)
    // WiFi 未配置状态
    await expect(page.getByText('配网状态')).toBeVisible()
    await expect(page.locator('.status-badge', { hasText: '未配置' })).toBeVisible()
    await expect(page.locator('.btn-primary', { hasText: '配置 WiFi' })).toBeVisible()
  })
})
