import { test, expect } from '@playwright/test'
import { realLogin, adminMessage, adminRoutes } from '../real-helpers'

/**
 * T053 - 02 Dashboard 数据概览（真实模式）
 * ⚠️ seed 聚合数据不固定（随 feed 写入变化），全部用存在性 + 非空断言，不校验固定数值。
 * 覆盖：6 KPI 卡片渲染 / 图表与排行 / 周期切换不崩溃
 */
test.describe('02-Dashboard 数据概览', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    await page.goto(adminRoutes.dashboard, { waitUntil: 'domcontentloaded' })
    await expect(page.locator('.page-title, h1')).toBeVisible({ timeout: 20_000 }).catch(() => {})
  })

  const KPI_LABELS = [
    '累计患者',
    '今日活跃佩戴',
    '今日告警次数',
    '平均佩戴时长',
    '设备在线率',
    '本月新增患者',
  ] as const

  test.describe('KPI 卡片', () => {
    test('2.1 渲染 6 张 KPI 卡片，label 齐全 + value 含数字（非空）', async ({ page }) => {
      const cards = page.locator('.kpi-card')
      // 允许 ≥6（未来扩展），但至少 6
      await expect(cards).toHaveCount((n) => n >= 6, { timeout: 20_000 })

      // 验证 label 齐全（6 个预期 label 都能找到一张卡片）
      for (const label of KPI_LABELS) {
        const card = cards.filter({ hasText: label })
        await expect(card).toHaveCount(1)
        // 对应 kpi-value 含至少 1 位数字（真实数据可能是 "1234"、"8.2h"、"96.8%" 等）
        const valueText = await card.locator('.kpi-value').textContent({ timeout: 5_000 })
        expect(valueText).toBeTruthy()
        expect(/\d/.test(valueText!)).toBe(true)
      }
    })
  })

  test.describe('图表与排行', () => {
    test('2.2 4 图 canvas 渲染 + 两张排行表各 ≥3 行', async ({ page }) => {
      // 4 图标题存在（文案即使用了 chart.js 也要先显示文字标题）
      const chartTitles = [
        '近7天日均佩戴时长',
        '近7天告警趋势',
        '各团队管理患者数',
        '佩戴时长分布',
      ]
      for (const t of chartTitles) {
        await expect(page.getByText(t)).toBeVisible({ timeout: 10_000 })
      }
      // canvas 数量至少 4
      await expect(page.locator('.dashboard canvas, .page-card canvas, canvas')).toHaveCount(
        (n) => n >= 4,
        { timeout: 25_000 },
      )
      // 团队佩戴达标排行表 ≥3 行（seed 3 团队）
      const teamRankCard = page.locator('.page-card').filter({ hasText: '团队佩戴达标排行' })
      const teamRows = teamRankCard.locator('.el-table__body-wrapper tbody tr')
      await expect(teamRows).toHaveCount((n) => n >= 3, { timeout: 10_000 })
      // 医生管理患者排行表 ≥3 行（seed 3 医生）
      const docRankCard = page.locator('.page-card').filter({ hasText: '医生管理患者排行' })
      const docRows = docRankCard.locator('.el-table__body-wrapper tbody tr')
      await expect(docRows).toHaveCount((n) => n >= 3, { timeout: 10_000 })
    })
  })

  test.describe('周期切换', () => {
    test('2.3 切换 今日/本周/本月 后 ElMessage 无错误 + 页面仍在 dashboard', async ({ page }) => {
      const toolbar = page.locator('.page-toolbar')
      // 切换到「本周」
      await toolbar.getByText('本周').click()
      await page.waitForTimeout(2_000)
      // 不应出现 error 型 ElMessage（.el-message--error 可见即失败）
      const errMsg = page.locator('.el-message--error')
      const errVisible = await errMsg.isVisible().catch(() => false)
      expect(errVisible).toBe(false)
      expect(new URL(page.url()).pathname).toContain('/dashboard')

      // 切换到「本月」
      await toolbar.getByText('本月').click()
      await page.waitForTimeout(2_000)
      const errVisible2 = await errMsg.isVisible().catch(() => false)
      expect(errVisible2).toBe(false)
      expect(new URL(page.url()).pathname).toContain('/dashboard')

      // 回到「今日」，确保页面仍可正常渲染
      const todayBtns = toolbar.getByText('今日')
      if ((await todayBtns.count()) > 0) {
        await todayBtns.first().click()
        await page.waitForTimeout(1_000)
      }
      // 检查没有 error 提示
      const msg = adminMessage(page)
      const msgVisible = await msg.isVisible().catch(() => false)
      if (msgVisible) {
        const msgType = await msg.getAttribute('class')
        expect(msgType).not.toContain('error')
      }
    })
  })
})
