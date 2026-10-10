import { test, expect } from '@playwright/test'
import { realLogin, adminMessage, pickSelectOption, realRoutes } from '../real-helpers'
import { requireDeployedBuild } from '../deploy-guard'

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

  // T636：这两枚是「两态」指标（窗口内无行 / 无已绑定设备时为 null，页面出占位不出 0）；
  // NO_DATA 与 dashboard/index.vue 的常量同名同形，改文案要两处一起改。
  const TWO_STATE_LABELS: string[] = ['平均佩戴时长', '设备在线率']
  const NO_DATA = '—'

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
        const valueText = await card.locator('.kpi-value').textContent({ timeout: 5_000 })
        expect(valueText).toBeTruthy()
        // T636 两态：count 类四项无行时回真 0，必须含数字；
        // 平均佩戴时长 / 设备在线率的合法形状有两种 —— 含数字，或「—」且带 .kpi-note「暂无数据」。
        // 旧写法对六项一律要求含数字，等于把「查不到」也判成缺陷，与卡面「无真实数据显示空/占位」冲突。
        if (TWO_STATE_LABELS.includes(label)) {
          const noteCount = await card.locator('.kpi-note').count()
          const shapeOk = /\d/.test(valueText!) || (valueText!.trim() === NO_DATA && noteCount === 1)
          expect(shapeOk, `${label} 既不是数值也不是「${NO_DATA}+暂无数据」，实际 value=${JSON.stringify(valueText)} note=${noteCount}`).toBe(true)
        } else {
          expect(/\d/.test(valueText!), `${label} 应含数字，实际 ${JSON.stringify(valueText)}`).toBe(true)
        }
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
      // T636 两态：整窗无数据那一格换成 el-empty「暂无数据」，所以合法形状是「canvas 格 + 占位格 = 4」，
      // 不再要求 canvas 恒 ≥4（旧写法会在正确的空态上判红）。至少一张图真渲染出来这条等待保留。
      await expect(page.locator('.dashboard .chart-container canvas').first()).toBeVisible({ timeout: 25_000 })
      const slotCanvases = await page.locator('.dashboard .chart-container canvas').count()
      const slotEmpties = await page.locator('.dashboard .el-empty').count()
      expect(slotCanvases + slotEmpties, `四张图每格要么是 canvas 要么是 el-empty，实际 canvas=${slotCanvases} empty=${slotEmpties}`).toBe(4)
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

/**
 * T348 - 医护角色进数据概览（真实模式）
 *
 * 现场（T341 验收缺陷 D-1）：doctor_li 下 6 个 dashboard 端点全 200，但页面一个数都不渲染，
 * 因为旧实现用 Promise.all 附带了一个 admin 专属的 GET /api/v1/teams（网关 rbac.go
 * adminOnlyPatterns）⇒ 403 让整页数据全弃。修法是本页不再依赖 /teams，
 * 「各团队管理患者数」改用同页 staff 权限内的 team-ranking。
 *
 * 这条用例把验收标准钉成门禁：医生视角下不得再出现 /teams 请求、dashboard/* 必须全 200、
 * KPI/图表/排行必须有内容、不得有红色 toast。staging 未部署该构建前按 post-deploy 显式跳过。
 */
test.describe('02b-Dashboard 医护角色（T348）', () => {
  test('2.4 doctor_li：无 /teams 请求 + dashboard 端点全 200 + 6 KPI/4 canvas/排行有行', async ({ page }) => {
    const teamRequests: string[] = []
    const dashboardResponses: { status: number; path: string }[] = []
    page.on('request', (req) => {
      const url = req.url()
      if (url.includes('/api/v1/teams')) teamRequests.push(`${req.method()} ${url}`)
    })
    page.on('response', (res) => {
      const url = res.url()
      if (url.includes('/api/v1/admin/dashboard/')) {
        dashboardResponses.push({ status: res.status(), path: new URL(url).pathname + new URL(url).search })
      }
    })

    // T506：口令不再写死在这里，经 real-helpers 的凭据门取数（CI 走 E2E_REAL_PASSWORD 注入，
    // 缺注入即显式判红；被 401 拦下时报「凭据漂移」而不是下游的「侧栏元素未找到」）。
    await realLogin(page, 'doctor_li')
    await page.goto(realRoutes.dashboard, { waitUntil: 'domcontentloaded' })

    await requireDeployedBuild(page, {
      marker: 'T348-dashboard-teams-403',
      why: 'T348 修的是「医生进数据概览整页空白」，staging 旧构建下医生拿不到 KPI 卡片',
      // T358：KPI 卡是接口回来才 v-for 出来的，domcontentloaded 那一刻恒为 0 张
      // （2026-09-24 现网实测 5/5 读到 0，101–154ms 后才满 6 张）。一次性 count()
      // 会把「已部署」读成「未部署」⇒ strict 阶段天天判红。与 04-monitor 的探针同姿势：
      // 先有界等到第 6 张出现，再取计数；等不到才返回未检出。
      probe: async (p) => {
        const cards = p.locator('.kpi-card')
        await cards.nth(5).waitFor({ state: 'attached', timeout: 25_000 }).catch(() => {})
        return (await cards.count()) >= 6
      },
    })

    await expect(page.locator('.kpi-card')).toHaveCount(6, { timeout: 20_000 })
    const doctorKpiLabels = [
      '累计患者',
      '今日活跃佩戴',
      '今日告警次数',
      '平均佩戴时长',
      '设备在线率',
      '本月新增患者',
    ]
    for (const label of doctorKpiLabels) {
      await expect(page.locator('.kpi-card').filter({ hasText: label })).toHaveCount(1)
    }
    // T636 两态：整窗无数据的图会换成 el-empty「暂无数据」，四格的合法形状是「canvas 或占位」补齐 4，
    // 不再是恒等 4 枚 canvas（旧写法在医生scope 的空窗下必红，而那恰恰是新契约要求的正确表现）。
    const doctorCanvases = await page.locator('.dashboard canvas').count()
    const doctorEmpties = await page.locator('.dashboard .chart-card .el-empty').count()
    expect(doctorCanvases + doctorEmpties, `四张图每格要么是 canvas 要么是 el-empty，实际 canvas=${doctorCanvases} empty=${doctorEmpties}`).toBe(4)
    // D-1 的鉴别位仍要留：旧包现场是「6 端点全 200 但整页不渲染」，那时 canvas 与占位一起为 0。
    expect(doctorCanvases, '医生 scope 下至少一张图真有数据（TEAM01 有 seed 佩戴行）').toBeGreaterThanOrEqual(1)

    // 两张排行表都得有行：旧包这里是 0 行 + 两个「暂无数据」
    const teamRankRows = page
      .locator('.page-card')
      .filter({ hasText: '团队佩戴达标排行' })
      .locator('.el-table__body-wrapper tbody tr')
    const docRankRows = page
      .locator('.page-card')
      .filter({ hasText: '医生管理患者排行' })
      .locator('.el-table__body-wrapper tbody tr')
    await expect(teamRankRows.first()).toBeVisible({ timeout: 15_000 })
    await expect(docRankRows.first()).toBeVisible({ timeout: 15_000 })
    // 旧包现场就是两张表各一个「暂无数据」（el-table 的空态是 .el-table__empty-block，不是 .el-empty）
    expect(await page.locator('.dashboard .el-table__empty-block').count(), '两张排行表都不得是空态').toBe(0)

    // 403 的源头必须真的从本页消失
    expect(teamRequests, `医生下数据概览不应再请求 /api/v1/teams，实际发了：${teamRequests.join(', ')}`).toHaveLength(0)
    expect(dashboardResponses.length).toBeGreaterThanOrEqual(6)
    const nonOk = dashboardResponses.filter((r) => r.status !== 200)
    expect(nonOk, `dashboard 端点应全 200，实际：${JSON.stringify(nonOk)}`).toHaveLength(0)

    const errMsg = page.locator('.el-message--error')
    expect(await errMsg.isVisible().catch(() => false), '不应有红色错误 toast').toBe(false)
  })
})
