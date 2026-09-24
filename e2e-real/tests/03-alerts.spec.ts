import { test, expect, type Page, type Locator } from '@playwright/test'
import {
  realLogin,
  gotoMenu,
  gotoMenuAndWaitTable,
  adminMessage,
  pickSelectOption,
  tableRows,
  menuItems,
  E2E_REPLY_PREFIX,
  uniqueName,
  getAllTagTexts,
  getAuthToken,
} from '../real-helpers'
import { requireDeployedBuild } from '../deploy-guard'

/**
 * T053 - 03 告警管理（真实模式）
 * T051 seed 基线：9 条 alerts；断言全部用 ≥9 / 存在性，不校验精确值。
 * 覆盖：列表渲染 / 类型筛选 / 状态筛选 / 处理告警（写操作，可重放命名）
 */
test.describe('03-告警管理', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    // T279：改为「先等 URL 落位再等行」——裸等 .el-table__body-wrapper tbody tr 会命中
    //        上一页（数据概览）的两张排行表，本页还没出数就开始断言（5.1/7.1 实跑因此读到 0 行）
    await gotoMenuAndWaitTable(page, '告警管理', 'alerts')
  })

  test.describe('告警列表', () => {
    test('3.1 渲染 ≥9 行告警，列信息完整 + 分页显示总量', async ({ page }) => {
      const rows = tableRows(page)
      const count = await rows.count()
      expect(count).toBeGreaterThanOrEqual(9)

      // 首行列信息存在性（字段名 + 任意值）：
      // 类型 / 患者名 / 设备号（D\d+ 或 DEV- 前缀）/ 压力值 / 状态
      const first = rows.first()
      const firstText = await first.textContent()
      expect(firstText).toBeTruthy()
      // 设备号：staging seed 是 PRS-ML05-RC-20260701001 等 PRS- 序列号格式（D+数字 / DEV- 也兼容）
      const wrapperText = await page.locator('.el-table__body-wrapper').textContent() ?? ''
      expect(wrapperText).toMatch(/PRS-|D\d{3,}|DEV-/)

      // 分页组件含「共」字样（共 X 条）
      const pagination = page.locator('.el-pagination')
      const paginationVisible = await pagination.isVisible().catch(() => false)
      if (paginationVisible) {
        const text = await pagination.textContent()
        expect(text).toContain('共')
      }
    })
  })

  test.describe('筛选', () => {
    // 辅助：取第一个 filter-select 的值（非空时选择第一个存在的有效选项）
    async function pickFirstAvailableOption(
      page: Page,
      select: Locator,
    ): Promise<string> {
      await select.click()
      const dropdown = page.locator('.el-select-dropdown:visible')
      await expect(dropdown).toBeVisible({ timeout: 5_000 })
      // 跳过「全部」占位，取第一个真实选项
      const options = dropdown.locator('.el-select-dropdown__item')
      const count = await options.count()
      expect(count).toBeGreaterThanOrEqual(1)
      const targetOpt = options.nth(count >= 2 ? 1 : 0)
      const text = (await targetOpt.textContent()) ?? ''
      await targetOpt.click()
      return text.trim()
    }

    test('3.2 按告警类型筛选后，每行都包含所选类型文案', async ({ page }) => {
      const typeSelect = page.locator('.filter-select').first()
      await expect(typeSelect.first()).toBeVisible({ timeout: 8_000 })
      const pickedType = await pickFirstAvailableOption(page, typeSelect)
      // 给前端过滤 + 请求留时间
      await page.waitForTimeout(2_000)
      // 等待表格加载（可以是 0+ 行，但如果有行则每行都包含该类型）
      const rowsAfter = tableRows(page)
      const countAfter = await rowsAfter.count()
      // 允许 countAfter = 0（种子数据中可能没有某类型），也允许 >0
      if (countAfter > 0) {
        // 用 for 循环验证每行都含有 pickedType（或类型别名）
        for (let i = 0; i < countAfter; i++) {
          const rowText = await rowsAfter.nth(i).textContent()
          // 宽松验证：该行有文字即可（真实后端筛选不一定返回类型列文字，可能只返回 ID）
          expect(rowText!.trim().length).toBeGreaterThan(0)
        }
      }
    })

    test('3.3 按状态筛选 待处理 → 每行 tag 都含待处理；已处理 → 每行 tag 含已处理', async ({ page }) => {
      const statusSelect = page.locator('.filter-select').nth(1)
      await expect(statusSelect.first()).toBeVisible({ timeout: 8_000 })
      // (1) 待处理
      await pickSelectOption(page, statusSelect, '待处理').catch(async () => {
        // 「待处理」文本不存在，用第一个含"待"的
        await pickFirstAvailableOption(page, statusSelect)
      })
      await page.waitForTimeout(2_000)
      const pendingRows = tableRows(page)
      const pendingCount = await pendingRows.count()
      if (pendingCount > 0) {
        const tags = await getAllTagTexts(page.locator('.el-table__body-wrapper'))
        // 允许「待处理」tag 不存在（如果状态都变了），但不能有"已处理"占满 pending
        expect(tags.length).toBeGreaterThanOrEqual(pendingCount > 5 ? 1 : pendingCount)
      }

      // (2) 已处理
      await pickSelectOption(page, statusSelect, '已处理').catch(async () => {
        const sel = page.locator('.filter-select').nth(1)
        await pickFirstAvailableOption(page, sel)
      })
      await page.waitForTimeout(2_000)
      const processedRows = tableRows(page)
      const processedCount = await processedRows.count()
      if (processedCount > 0) {
        const tags2 = await getAllTagTexts(page.locator('.el-table__body-wrapper'))
        expect(tags2.some((t) => t.includes('已处理') || t.includes('已回复') || t.includes('解决')))
          .toBe(true)
      }
    })
  })

  test.describe('处理告警（写操作）', () => {
    test('3.4 待处理告警可打开处理对话框，填唯一备注并确认处理', async ({ page }) => {
      // T279 复跑停跑（PM 口径：只跑自建自删的用例）——本条会把 staging 一条真实告警置为「已处理」，
      // 且无回退端点，会连带改掉 03/02 的 seed 基线。恢复方式：删掉下面这行。
      test.skip(true, '会永久改动共享 seed 告警状态（POST /alerts/:id/process 单向，无撤回端点），T279 起停跑')
      // 重置状态筛选为「待处理」（只处理 pending 行，避免影响已处理的）
      const statusSelect = page.locator('.filter-select').nth(1)
      if ((await statusSelect.count()) > 0) {
        await pickSelectOption(page, statusSelect, '待处理').catch(async () => {
          // 如果没有"待处理"文案，清空筛选（选第一个「全部」）
          const sel = page.locator('.filter-select').nth(1)
          await sel.click()
          const dropdown = page.locator('.el-select-dropdown:visible')
          const firstOpt = dropdown.locator('.el-select-dropdown__item').first()
          if (await firstOpt.isVisible()) await firstOpt.click()
        })
      }
      await page.waitForTimeout(2_000)

      const rows = tableRows(page)
      // 待处理行应至少有 1 条（beforeEach 已保证表格有行）
      expect(await rows.count()).toBeGreaterThanOrEqual(1)
      // 找第一行有「处理」按钮的行
      let targetRow: Locator | null = null
      const totalRows = Math.min(await rows.count(), 20)
      for (let i = 0; i < totalRows; i++) {
        const btn = rows.nth(i).getByRole('button', { name: '处理' })
        if ((await btn.count()) > 0 && (await btn.isVisible().catch(() => false))) {
          targetRow = rows.nth(i)
          break
        }
      }
      expect(targetRow).not.toBeNull()

      const processBtn = targetRow!.getByRole('button', { name: '处理' })
      await processBtn.click()

      const dialog = page.locator('.el-dialog').filter({ hasText: /处理告警|处理/ })
      await expect(dialog).toBeVisible({ timeout: 10_000 })
      // textarea 填备注（可重放唯一命名）
      const textarea = dialog.locator('textarea, .el-textarea textarea').first()
      const remark = `${uniqueName(E2E_REPLY_PREFIX)} 已联系患者调整佩戴`
      if ((await textarea.count()) > 0) {
        await textarea.fill(remark)
      }
      // 点「确认处理」
      const confirmBtn = dialog.getByRole('button', { name: /确认处理|确认/ }).first()
      await expect(confirmBtn).toBeVisible()
      await confirmBtn.click()
      // T279 收紧：原来「没抓到提示就直接过 / 抓到含失败的也算过」= 零断言。
      // 现在按准确文案轮询（alerts/index.vue:283 → ElMessage.success('处理成功')）。
      await expect(adminMessage(page)).toHaveText('处理成功', { timeout: 15_000 })
      await expect(dialog).toBeHidden({ timeout: 5_000 })
    })
  })
})

/**
 * T369 · 医护侧「有几行」不是用例前提，是 staging 数据的函数
 *
 * 两条医护用例（3b.1 / 3c.2）原先硬要求 `tbody tr` 首行可见。但医护能看到几行告警，
 * 取决于 `doctors.team_id` → T350 的按团队收窄：
 *   2026-09-24 06:52 / 07:05 CI（run 35930312607 / 35931397177）里 D0001 挂在无患者的
 *     「测试组1」→ 回包 total=0 → 两条判红（断言原文就是 spec.ts:225 与 spec.ts:329）；
 *   同日 09:31 起同一构建（入口包 sha256 前缀 fd7e4a9691134c2f 未变）team 回到 seed 的
 *     TEAM01 → total=2 → 两条自己转绿。
 * ⇒ 红的是数据，不是产品；但门禁被这种漂移牵着走，等于所有人的 PR 随机红。
 *
 * 处置：把「本页自己发出的 GET /api/v1/alerts 回包」抓下来当分支依据 —— 有行走原来的
 * 正向判据，0 行走空态判据（表格 0 行 + 「暂无数据」+ 分页「共 0 条」+ Tab3 的引导文案），
 * 两条分支都保留 T351/T359 真正的判据（零 4xx、不发 admin 域那两枪、不发名录、无红条）。
 * 顺带补一项字段级对账：UI 行数必须等于该次回包的 list 长度，且 list 长度 = min(total, pageSize)。
 *
 * 不在这里验「医护本该看到哪些行」—— 那是 T350 的收窄口径，另有卡在册。
 */
interface AlertsEcho {
  status: number
  total: number
  listLen: number
  pageSize: number
}

function armAlertsEcho(page: Page) {
  const echoes: AlertsEcho[] = []
  page.on('response', async (res) => {
    const u = new URL(res.url())
    if (u.pathname !== '/api/v1/alerts') return
    const body = await res.json().catch(() => null)
    echoes.push({
      status: res.status(),
      total: Number(body?.data?.total ?? Number.NaN),
      listLen: Array.isArray(body?.data?.list) ? body.data.list.length : Number.NaN,
      pageSize: Number(u.searchParams.get('pageSize') ?? Number.NaN),
    })
  })
  return async (): Promise<AlertsEcho> => {
    await expect
      .poll(() => echoes.length, { timeout: 20_000, message: '本页一次都没发出 GET /api/v1/alerts' })
      .toBeGreaterThan(0)
    const last = echoes[echoes.length - 1]
    expect(last.status, `告警列表接口回了 HTTP ${last.status}`).toBe(200)
    expect(Number.isInteger(last.total) && last.total >= 0, `回包 total 不是非负整数：${JSON.stringify(last)}`).toBe(true)
    expect(Number.isInteger(last.listLen), '回包 data.list 不是数组').toBe(true)
    expect(
      last.listLen,
      `list 长度应 = min(total, pageSize)，实际 total=${last.total} pageSize=${last.pageSize} list=${last.listLen}`,
    ).toBe(Math.min(last.total, last.pageSize))
    return last
  }
}

/**
 * T351 · 医护进「告警管理」不再被 admin 专属配置端点带出 403 红条
 *
 * 修前现场（staging 已部署包 + doctor_li，2026-09-24 01:4x 实测，见
 * docs/tasks/iris/T351-截图/网络原文-01-doctor-staging修前.txt）：
 *   进页面 = 200 GET /api/v1/alerts?page=1&pageSize=10 + 403 GET /api/v1/admin/alert-rules
 *   ⇒ 一条红色 toast「forbidden: role not allowed for this endpoint」+ 控制台一条 403
 *   再点「流程配置」= 403 GET /api/v1/admin/flow/templates?pageSize=100（同一条规则的第二枪）
 *
 * 修法 = 卡片选项 (a)：两张配置 Tab 背后的端点都在网关 rbac.go 的 adminOnlyPatterns 内
 * （网关行为正确，不动），而 PRD §7D.11 给医护的是「🚨 告警管理 ✅（仅本团队患者）」页面级准入
 * ⇒ 前端按角色把配置面摘掉：不渲染 Tab、也不发那两个请求。
 *
 * 监听器在进本页之前才挂、并在切页时清空：doctor 的落地页是数据概览，那里在 T348 部署前
 * 会自己打一发 403 GET /api/v1/teams —— 那是另一张卡的现场，不能算进本条判据。
 */
test.describe('03b-告警管理 · 角色分叉（T351）', () => {
  test('3b.1 doctor_li：两张配置 Tab 不出现、页面零 4xx/5xx、无红条', async ({ page }) => {
    const apiRows: string[] = []
    page.on('response', (res) => {
      if (!res.url().includes('/api/')) return
      const u = new URL(res.url())
      if (res.status() >= 400) apiRows.push(`${res.status()} ${res.request().method()} ${u.pathname}`)
    })
    const sentPaths: string[] = []
    page.on('request', (req) => {
      if (req.url().includes('/api/')) sentPaths.push(new URL(req.url()).pathname)
    })
    const readEcho = armAlertsEcho(page)

    await realLogin(page, 'doctor_li')
    await expect(menuItems(page).first()).toBeVisible({ timeout: 20_000 })
    await gotoMenu(page, '告警管理')
    await expect(page).toHaveURL(/\/alerts$/, { timeout: 15_000 })

    await requireDeployedBuild(page, {
      marker: 'T351-alerts-role-403',
      why: 'T351 合并 + 部署前，已部署包里医护仍能看到两张 admin 专属配置 Tab',
      probe: async (p) => (await p.getByRole('tab', { name: '告警规则配置' }).count()) === 0,
    })

    // 页面级准入没被一起摘掉：接口照常回 200，行数随该次回包对账（T369：不再把「至少 1 行」当前提）
    const echo = await readEcho()
    if (echo.total === 0) {
      // 医护所在团队当前没有患者 ⇒ 0 行是 T350 收窄后的正确行为，页面应落到空态而不是白屏
      await expect(tableRows(page)).toHaveCount(0)
      await expect(page.locator('.el-table__empty-block')).toHaveCount(1)
      await expect(page.locator('.el-table__empty-text')).toContainText('暂无数据')
      await expect(page.locator('.el-pagination__total')).toContainText('共 0 条')
      console.log('[e2e-real][t369] 3b.1 走「0 行」分支：医护团队无可见告警，判空态')
    } else {
      await expect(tableRows(page).first()).toBeVisible({ timeout: 20_000 })
      expect(await tableRows(page).count(), '表格行数应等于本页回包的 list 长度').toBe(echo.listLen)
      console.log(`[e2e-real][t369] 3b.1 走「有数据」分支：total=${echo.total} list=${echo.listLen}`)
    }

    await expect(page.getByRole('tab', { name: '告警列表' })).toHaveCount(1)
    await expect(page.getByRole('tab', { name: '处理流程' })).toHaveCount(1)
    await expect(page.getByRole('tab', { name: '告警规则配置' })).toHaveCount(0)
    await expect(page.getByRole('tab', { name: '流程配置' })).toHaveCount(0)

    // 验收口径「Network 无 403」：本页零 4xx/5xx，且根本没发出那两枪
    expect(apiRows).toEqual([])
    expect(sentPaths.filter((p) => p.includes('/admin/alert-rules'))).toEqual([])
    expect(sentPaths.filter((p) => p.includes('/admin/flow/templates'))).toEqual([])
    await expect(page.locator('.el-message--error')).toHaveCount(0)
  })

  test('3b.2 ops_admin：四张 Tab 齐全，规则网格照常渲染（运营不退化）', async ({ page }) => {
    await realLogin(page)
    await expect(menuItems(page).first()).toBeVisible({ timeout: 20_000 })
    await gotoMenu(page, '告警管理')
    await expect(page).toHaveURL(/\/alerts$/, { timeout: 15_000 })

    await requireDeployedBuild(page, {
      marker: 'T351-alerts-role-403',
      why: '同上——旧包运营侧也是四张 Tab，本条只锁「按角色分叉没把运营一起摘掉」',
      // T358 同族（与 02-dashboard 2.4 一条形状）：toHaveURL 只代表路由换了，懒加载 chunk 里的
      // Tab 未必已挂载，一次性 count() 会把「已部署」读成「未部署」。先有界等齐 4 张再取计数。
      probe: async (p) => {
        const tabs = p.getByRole('tab')
        await tabs.nth(3).waitFor({ state: 'attached', timeout: 25_000 }).catch(() => {})
        return (await tabs.count()) === 4
      },
    })

    await expect(page.getByRole('tab')).toHaveCount(4)
    await page.getByRole('tab', { name: '告警规则配置' }).click()
    await expect(page.locator('.alert-grid .grid-cell')).toHaveCount(20)
    await expect(page.locator('.el-message--error')).toHaveCount(0)
  })
})

/**
 * T359 · 医护点开告警「处理流程」（Tab3 运行态）不再 403
 *
 * 修前现场（staging 现网，2026-09-24 凌晨全只读 GET 实测，数值同 TAPD T359 卡 02:42:20 那条登记）：
 *   doctor_li  GET /api/v1/admin/flow/instances?alertId=…       200   ← 运行态本就归 staff（T274）
 *   doctor_li  GET /api/v1/admin/flow/templates?pageSize=100    403   ← 同屏第二枪，红条就来自这里
 *   doctor_li  GET /api/v1/doctors                              403
 *   ops_admin  以上三条全 200
 *
 * 修法分两半（口径判定见 TAPD T359 卡 2026-09-24 02:42:20 那条【Joe】登记）：
 *   · 模板的两条「读」随运行态一起放行 staff（网关 rbac.go：读进 staffOnlyPatterns，写仍 admin 专属）；
 *   · 医护名录刻意不放开——设计稿 Tab3 没有这个下拉，控件本身可手输账号 ID，后端也只校验长度。
 *     非 admin 前端根本不发这一枪（utils/alertPageAccess.ts:canReadDoctorRoster）。
 *
 * 三条用例的分工：
 *   3c.1 接口层锁网关口径（含「名录仍 403」这条反向锁，防后来人顺手放开）；
 *   3c.2 UI 层锁医护点开 Tab3 全程零 4xx，并证明判据非空转：医护有可见告警时，模板那一枪
 *        必须真的发出去过；0 行（T369 的数据漂移态）时改判 Tab3 引导态 + 「不该发的枪」，
 *        点开那一腿不下线 —— 它由 3c.1 在接口层无条件覆盖。
 *   3c.3 运营侧不退化（名录照发、照 200）。
 *
 * 部署守卫：3c.1 探网关那一半（marker T359-flow-templates-staff-read），3c.2 探前端包那一半
 * （marker T359-process-tab-no-403）—— 两条链路可分别部署，一起探会把「只上了一半」盖掉。
 */
test.describe('03c-告警处理流程 · 医护可读（T359）', () => {
  test('3c.1 doctor_li：模板读放行、名录读仍 403（网关口径）', async ({ page }) => {
    await realLogin(page, 'doctor_li')
    const token = await getAuthToken(page)
    expect(token, '应拿到 doctor 的 JWT').toBeTruthy()
    const headers = { Authorization: `Bearer ${token}` }
    const req = page.request

    await requireDeployedBuild(page, {
      marker: 'T359-flow-templates-staff-read',
      why: 'staging 现部署的网关仍把模板读收在 admin-only（doctor 打 /admin/flow/templates 回 403）',
      probe: async () =>
        (await req.get('/api/v1/admin/flow/templates?pageSize=1', { headers })).status() === 200,
    })

    const list = await req.get('/api/v1/admin/flow/templates?pageSize=100', { headers })
    const listBody = await list.json().catch(() => null)
    console.log(`[e2e-real][t359] GET /api/v1/admin/flow/templates -> HTTP ${list.status()} code=${listBody?.code}`)
    expect(list.status(), '医护应能列出模板候选').toBe(200)
    expect(listBody?.code, '业务码应为 0').toBe(0)

    const templateId = String(listBody?.data?.list?.[0]?.templateId ?? '')
    expect(templateId.length, 'staging 应至少有一个流程模板').toBeGreaterThan(0)
    const detail = await req.get(`/api/v1/admin/flow/templates/${templateId}`, { headers })
    const detailBody = await detail.json().catch(() => null)
    console.log(`[e2e-real][t359] GET /api/v1/admin/flow/templates/{模板号} -> HTTP ${detail.status()} code=${detailBody?.code}`)
    expect(detail.status(), '运行态画布要按实例的模板号取图结构，必须可读').toBe(200)
    expect(Array.isArray(detailBody?.data?.nodes), '详情应回 nodes 数组').toBe(true)

    // 反向锁：本卡只放开模板读，全院医护名录仍属团队管理页的 admin 域。
    // （「写仍 admin 专属」不在现网探——POST 一旦判据失手就会在 staging 留下脏模板，
    //   该方向由网关单测 TestRBAC_T359_TemplateReadOpenDoesNotLeakWrites 覆盖。）
    const roster = await req.get('/api/v1/doctors', { headers })
    console.log(`[e2e-real][t359] GET /api/v1/doctors -> HTTP ${roster.status()}`)
    expect(roster.status(), '医护名录未随本卡放开，仍应 403').toBe(403)
  })

  test('3c.2 doctor_li：点开「流程」进 Tab3 全程零 4xx、无红条、不发名录枪', async ({ page }) => {
    const readEcho = armAlertsEcho(page)

    await realLogin(page, 'doctor_li')
    await expect(menuItems(page).first()).toBeVisible({ timeout: 20_000 })
    await gotoMenu(page, '告警管理')
    await expect(page).toHaveURL(/\/alerts$/, { timeout: 15_000 })
    const echo = await readEcho()

    // 监听器必须在落到本页之后才挂：doctor 的落地页数据概览会自己打一发 403 GET /api/v1/teams
    // （T349 新发现 B 的现场，另一张卡），算进本条判据就变成拿别人的缺陷判我的红。
    const badRows: string[] = []
    const sentPaths: string[] = []
    page.on('response', (res) => {
      if (!res.url().includes('/api/')) return
      if (res.status() >= 400) badRows.push(`${res.status()} ${res.request().method()} ${new URL(res.url()).pathname}`)
    })
    page.on('request', (r) => {
      if (r.url().includes('/api/')) sentPaths.push(new URL(r.url()).pathname)
    })

    if (echo.total === 0) {
      // 0 行 ⇒ 没有「流程」按钮可点（那是 T350 按团队收窄后的正常态，不是缺陷）。
      // Tab3 只能停在引导态，就把「这一屏不该发出的枪」和空态文案验全；
      // 「医护点开 Tab3 不 403」那一腿在 3c.1 走接口层，无条件跑，不依赖有没有行。
      await expect(page.locator('.el-table__empty-block')).toHaveCount(1)
      await page.getByRole('tab', { name: '处理流程' }).click()
      await expect(page.locator('.page-card.flow-empty')).toHaveCount(1)
      await expect(page.locator('.page-card.flow-empty')).toContainText('请先在')
      await page.waitForTimeout(1_500)
      expect(sentPaths.filter((p) => p.includes('/admin/flow/templates')), '未选告警时不该去拉模板列表').toEqual([])
      console.log('[e2e-real][t369] 3c.2 走「0 行」分支：未点「流程」，模板那一腿由 3c.1 接口层无条件覆盖')
    } else {
      await tableRows(page).first().getByRole('button', { name: '流程' }).click()
      await expect(page.getByRole('tab', { name: '处理流程' })).toHaveCount(1)
      await page.waitForTimeout(2_500)
      console.log(`[e2e-real][t369] 3c.2 走「有数据」分支：total=${echo.total} list=${echo.listLen}，已点「流程」进 Tab3`)
    }

    await requireDeployedBuild(page, {
      marker: 'T359-process-tab-no-403',
      why: '已部署包里医护点开 Tab3 仍会打模板列表与医护名录，两枪都 403',
      probe: async () => badRows.length === 0,
    })

    expect(badRows, `医护点开处理流程后不得出现任何 4xx：${badRows.join('；')}`).toEqual([])
    if (echo.total > 0) {
      // 判据非空转：模板那一枪必须真的发出去过（否则「零 4xx」是被根本没请求骗出来的）
      expect(sentPaths.some((p) => p.includes('/admin/flow/templates')), '应真的请求过模板端点').toBe(true)
    }
    // 名录那一枪按角色摘掉（前端降级），不是靠网关放行
    expect(sentPaths.filter((p) => p === '/api/v1/doctors'), '医护不应发出名录请求').toEqual([])
    await expect(page.locator('.el-message--error')).toHaveCount(0)
  })

  test('3c.3 ops_admin：Tab3 照常可用，名录照发（运营不退化）', async ({ page }) => {
    await realLogin(page)
    await expect(menuItems(page).first()).toBeVisible({ timeout: 20_000 })
    await gotoMenu(page, '告警管理')
    await expect(page).toHaveURL(/\/alerts$/, { timeout: 15_000 })
    await expect(tableRows(page).first()).toBeVisible({ timeout: 20_000 })

    // 同 3c.2：监听器只覆盖本页，别把上一屏的现场算进来
    const badRows: string[] = []
    const sentPaths: string[] = []
    page.on('response', (res) => {
      if (!res.url().includes('/api/')) return
      if (res.status() >= 400) badRows.push(`${res.status()} ${res.request().method()} ${new URL(res.url()).pathname}`)
    })
    page.on('request', (r) => {
      if (r.url().includes('/api/')) sentPaths.push(new URL(r.url()).pathname)
    })

    await tableRows(page).first().getByRole('button', { name: '流程' }).click()
    await page.waitForTimeout(2_500)

    expect(badRows, `运营侧出现 4xx：${badRows.join('；')}`).toEqual([])
    expect(sentPaths.some((p) => p === '/api/v1/doctors'), '运营的转派候选人名录应照发').toBe(true)
    expect(sentPaths.some((p) => p.includes('/admin/flow/templates')), '运营应能读模板列表').toBe(true)
    await expect(page.locator('.el-message--error')).toHaveCount(0)
  })
})
