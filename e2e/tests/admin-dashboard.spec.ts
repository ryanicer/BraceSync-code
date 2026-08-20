import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin } from '../admin-helpers'

/**
 * admin-web Dashboard（数据概览）：6 KPI + 4 图 + 2 排行 + 周期切换
 * mock 数据对齐 mock/dashboard.ts（today: 1256/892/47/8.2/96.8/38）
 */

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.dashboard)
})

test.describe('KPI 卡片', () => {
  test('渲染 6 张 KPI 卡片且数值对齐 mock', async ({ page }) => {
    await expect(page.locator('.kpi-card')).toHaveCount(6)
    const expected: [string, string][] = [
      ['累计患者', '1256'],
      ['今日活跃佩戴', '892'],
      ['今日告警次数', '47'],
      ['平均佩戴时长', '8.2h'],
      ['设备在线率', '96.8%'],
      ['本月新增患者', '38'],
    ]
    for (const [label, value] of expected) {
      const card = page.locator('.kpi-card').filter({ hasText: label })
      await expect(card).toHaveCount(1)
      await expect(card.locator('.kpi-value')).toHaveText(value)
    }
  })
})

test.describe('图表与排行', () => {
  test('4 张图表渲染 canvas（佩戴趋势/告警趋势/团队患者数/佩戴分布）', async ({ page }) => {
    await expect(page.getByText('近7天日均佩戴时长')).toBeVisible()
    await expect(page.getByText('近7天告警趋势')).toBeVisible()
    await expect(page.getByText('各团队管理患者数')).toBeVisible()
    await expect(page.getByText('佩戴时长分布')).toBeVisible()
    // chart.js 渲染为 canvas，4 图各一个
    await expect(page.locator('.dashboard canvas')).toHaveCount(4, { timeout: 15_000 })
  })

  test('团队佩戴达标排行：5 行且首名为脊柱侧弯一组', async ({ page }) => {
    const card = page.locator('.page-card').filter({ hasText: '团队佩戴达标排行' })
    await expect(card.locator('.el-table__body-wrapper tbody tr')).toHaveCount(5)
    const firstRow = card.locator('.el-table__body-wrapper tbody tr').first()
    await expect(firstRow).toContainText('脊柱侧弯一组')
    await expect(firstRow).toContainText('9.2h')
    await expect(firstRow).toContainText('94.6%')
  })

  test('医生管理患者排行：5 行且首名为张建国', async ({ page }) => {
    const card = page.locator('.page-card').filter({ hasText: '医生管理患者排行' })
    await expect(card.locator('.el-table__body-wrapper tbody tr')).toHaveCount(5)
    await expect(card.locator('.el-table__body-wrapper tbody tr').first()).toContainText('张建国')
  })

  test('达标率 tag 颜色分级（≥90 success / 80-90 primary / <80 warning）', async ({ page }) => {
    const card = page.locator('.page-card').filter({ hasText: '团队佩戴达标排行' })
    const rows = card.locator('.el-table__body-wrapper tbody tr')
    // 94.6% → success；88.0% → primary；76.4% → warning
    await expect(rows.nth(0).locator('.el-tag--success')).toHaveCount(1)
    await expect(rows.nth(2).locator('.el-tag--primary')).toHaveCount(1)
    await expect(rows.nth(4).locator('.el-tag--warning')).toHaveCount(1)
  })
})

test.describe('周期切换', () => {
  test('切换到本周后 KPI 数值变化（告警 47 → 312）', async ({ page }) => {
    await page.locator('.page-toolbar').getByText('本周').click()
    const card = page.locator('.kpi-card').filter({ hasText: '今日告警次数' })
    await expect(card.locator('.kpi-value')).toHaveText('312')
    await expect(page.locator('.kpi-card').filter({ hasText: '今日活跃佩戴' }).locator('.kpi-value')).toHaveText('905')
  })

  test('切换到本月后 KPI 数值变化（告警 → 1287）', async ({ page }) => {
    await page.locator('.page-toolbar').getByText('本月').click()
    const card = page.locator('.kpi-card').filter({ hasText: '今日告警次数' })
    await expect(card.locator('.kpi-value')).toHaveText('1287')
  })
})
