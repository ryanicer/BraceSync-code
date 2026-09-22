import { test, expect } from '@playwright/test'
import { realLogin, adminMessage, pickSelectOption, realRoutes } from '../real-helpers'

/**
 * T053 - 02 Dashboard 数据概览（真实模式）
 * ⚠️ seed 聚合数据不固定（随 feed 写入变化），全部用存在性 + 非空断言，不校验固定数值。
 * 覆盖：6 KPI 卡片渲染 / 图表与排行 / 周期切换不崩溃
 */
test.describe('02-Dashboard 数据概览', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    await page.goto(realRoutes.dashboard, { waitUntil: 'domcontentloaded' })
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
      await expect(cards.first()).toBeVisible({ timeout: 20_000 })
      const cardCount = await cards.count()
      expect(cardCount).toBeGreaterThanOrEqual(6)

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
      const canvas = page.locator('.dashboard canvas, .page-card canvas, canvas')
      await expect(canvas.first()).toBeVisible({ timeout: 25_000 })
      const canvasCount = await canvas.count()
      expect(canvasCount).toBeGreaterThanOrEqual(4)
      // 团队佩戴达标排行表 ≥3 行（seed 3 团队）
      const teamRankCard = page.locator('.page-card').filter({ hasText: '团队佩戴达标排行' })
      const teamRows = teamRankCard.locator('.el-table__body-wrapper tbody tr')
      await expect(teamRows.first()).toBeVisible({ timeout: 10_000 })
      const teamRowCount = await teamRows.count()
      expect(teamRowCount).toBeGreaterThanOrEqual(3)
      // 医生管理患者排行表 ≥3 行（seed 3 医生）
      const docRankCard = page.locator('.page-card').filter({ hasText: '医生管理患者排行' })
      const docRows = docRankCard.locator('.el-table__body-wrapper tbody tr')
      await expect(docRows.first()).toBeVisible({ timeout: 10_000 })
      const docRowCount = await docRows.count()
      expect(docRowCount).toBeGreaterThanOrEqual(3)
    })
  })

  test.describe('周期切换', () => {
    test('2.3 切换 今日/本周/本月 后 ElMessage 无错误 + 页面仍在 dashboard', async ({ page }) => {
      // T289 起周期控件是 el-select（.period-select），不再是三个文字按钮 ——
      // 旧写法 toolbar.getByText('本周').click() 等不到元素直接超时。
      const periodSelect = page.locator('.page-toolbar .period-select')
      await expect(periodSelect).toBeVisible({ timeout: 15_000 })

      // 每切换一次都要求 KPI 接口真的按新 period 发过一次请求（不是只换了个高亮样式）
      const periods: { label: string; query: string }[] = [
        { label: '本周', query: 'week' },
        { label: '本月', query: 'month' },
        { label: '今日', query: 'today' },
      ]
      for (const p of periods) {
        const kpiRequested = page
          .waitForResponse(
            (r) => r.url().includes(`/admin/dashboard/kpi?period=${p.query}`) && r.ok(),
            { timeout: 20_000 },
          )
          .catch(() => null)
        await pickSelectOption(page, periodSelect, p.label)
        const resp = await kpiRequested
        expect(resp, `切到「${p.label}」后应发出 GET /admin/dashboard/kpi?period=${p.query}`).not.toBeNull()

        const errMsg = page.locator('.el-message--error')
        expect(await errMsg.isVisible().catch(() => false), `切到「${p.label}」不应报错`).toBe(false)
        expect(new URL(page.url()).pathname).toContain('/dashboard')
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
