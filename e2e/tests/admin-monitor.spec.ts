import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, tableRows } from '../admin-helpers'

/**
 * admin-web 实时监控：患者快照列表 + 立即刷新 + 详情抽屉 + 30s 轮询标识
 * mock 对齐 mock/patients.ts mockPatientRealtime：
 * - PT-004 刘俊熙 abnormal（68.5N / 3 次异常事件）
 * - PT-005 赵欣然 无设备 → 不发快照请求，状态列保持「加载中」，详情按钮禁用
 * - 其余 online
 */

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.monitor)
})

test.describe('监控列表', () => {
  test('渲染 6 名患者并显示 30s 轮询标识与更新时间', async ({ page }) => {
    await expect(page.getByText('每 30s 自动刷新')).toBeVisible()
    await expect(tableRows(page)).toHaveCount(6)
    // 快照拉取完成后显示最近更新时间（HH:mm:ss）
    await expect(page.locator('.update-time')).toContainText(/\d{2}:\d{2}:\d{2}/, { timeout: 15_000 })
  })

  test('在线 / 异常 / 离线状态 tag 渲染', async ({ page }) => {
    await expect(page.locator('.update-time')).toContainText(/\d{2}:\d{2}:\d{2}/, { timeout: 15_000 })
    const onlineRow = tableRows(page).filter({ hasText: '林小雨' })
    await expect(onlineRow.locator('.el-tag--success')).toContainText('在线')
    const abnormalRow = tableRows(page).filter({ hasText: '刘俊熙' })
    // 该行有两个 danger tag（状态"异常" + 异常事件"N 次"），取状态列第一个
    await expect(abnormalRow.locator('.el-tag--danger').first()).toContainText('异常')
    // 异常患者压力高亮 + 异常事件次数
    await expect(abnormalRow).toContainText('68.5N')
    await expect(abnormalRow.locator('.pressure-warn')).toBeVisible()
    await expect(abnormalRow).toContainText('3 次')
    const offlineRow = tableRows(page).filter({ hasText: '赵欣然' })
    // 未绑定设备患者不发起实时快照请求，状态列保持「加载中」（应用真实行为）
    await expect(offlineRow).toContainText('加载中')
    await expect(offlineRow).toContainText('未绑定')
  })

  test('未绑定设备患者详情按钮禁用', async ({ page }) => {
    await expect(page.locator('.update-time')).toContainText(/\d{2}:\d{2}:\d{2}/, { timeout: 15_000 })
    const row = tableRows(page).filter({ hasText: '赵欣然' })
    await expect(row.getByRole('button', { name: '详情' })).toBeDisabled()
    await expect(row).toContainText('未绑定')
  })
})

test.describe('刷新与详情', () => {
  test('立即刷新更新时间戳', async ({ page }) => {
    await expect(page.locator('.update-time')).toContainText(/\d{2}:\d{2}:\d{2}/, { timeout: 15_000 })
    const before = await page.locator('.update-time').innerText()
    // 等待至少跨 1 秒，保证刷新后时间字符串变化
    await page.waitForTimeout(1_100)
    await page.getByRole('button', { name: '立即刷新' }).click()
    await expect(page.locator('.update-time')).not.toHaveText(before, { timeout: 15_000 })
  })

  test('在线患者详情抽屉显示实时快照', async ({ page }) => {
    await expect(page.locator('.update-time')).toContainText(/\d{2}:\d{2}:\d{2}/, { timeout: 15_000 })
    await tableRows(page).filter({ hasText: '林小雨' }).getByRole('button', { name: '详情' }).click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible()
    await expect(drawer).toContainText('林小雨 实时快照')
    await expect(drawer).toContainText('今日佩戴时长')
    await expect(drawer).toContainText('最大压力')
    await expect(drawer).toContainText('最新帧上报')
    // 在线患者不出现异常提示
    await expect(drawer.locator('.el-alert')).toHaveCount(0)
  })

  test('异常患者详情抽屉显示异常告警提示', async ({ page }) => {
    await expect(page.locator('.update-time')).toContainText(/\d{2}:\d{2}:\d{2}/, { timeout: 15_000 })
    await tableRows(page).filter({ hasText: '刘俊熙' }).getByRole('button', { name: '详情' }).click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible()
    await expect(drawer.locator('.detail-alert')).toContainText('设备状态异常，请检查设备故障码或联系技师')
    await expect(drawer).toContainText('68.5N')
  })
})
