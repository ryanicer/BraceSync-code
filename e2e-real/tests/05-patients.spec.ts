import { test, expect, type Page, type Locator } from '@playwright/test'
import {
  realLogin,
  gotoMenuAndWaitTable,
  pickSelectOption,
  tableRows,
  adminMessage,
  E2E_PATIENT_NAME_PREFIX,
  uniqueName,
  getAuthToken,
} from '../real-helpers'

/**
 * T053 - 05 患者管理（真实模式）
 * T051 seed：5 名患者。断言用 ≥5 行 / 动态读取首行姓名（避免硬编码）。
 * 覆盖：列表 / 搜索 / 团队筛选 / 添加患者（写） / 分配团队（写） + 末尾清理本任务创建的患者。
 */
/**
 * 列表断言一律钉在「患者列表」卡内。
 *
 * T289 4.2 让本页多了一张「批量患者-团队绑定」独立卡片，它自带 el-table。
 * 旧写法按全局 .el-table__* 取行取表头，于是：
 *  - 5.1 的 textContent() 撞到 strict mode violation（两个 body-wrapper）；
 *  - 5.3 / 5.6 的表头文案列表变成两张表拼接（…|绑定团队|状态||患者ID|姓名|当前团队|分配至），
 *    indexOf('团队') 取不到列；
 *  - 5.2 搜索结果里混进批量卡的未分配患者行。
 */
function patientTable(page: Page): Locator {
  return page.locator('.patient-list-card')
}

/** 表头文案 → 列下标（首列是 selection 复选框，故绝不按写死序号取列） */
async function headerIndex(page: Page, title: string): Promise<number> {
  const texts = await patientTable(page)
    .locator('.el-table__header-wrapper thead th')
    .evaluateAll((ths) => ths.map((th) => (th.textContent ?? '').trim()))
  const idx = texts.indexOf(title)
  expect(idx, `表头应含「${title}」列，实得 ${texts.join('|')}`).toBeGreaterThan(-1)
  return idx
}

/** 一行的各单元格文本（逐格取，避免整行文本拼接后无法定位具体列） */
async function cellTexts(row: Locator): Promise<string[]> {
  return row.evaluate((tr) =>
    Array.from(tr.querySelectorAll('td')).map((td) => (td.textContent ?? '').trim()),
  )
}

/**
 * 弹窗内按 form-item 文案锚出那一项（T462 S3 随 5.4 放开一起补）。
 *
 * 为什么不再按 input 序号填：本页新建表单自 T248/T353 起是
 * 姓名/手机号/年龄/诊断/团队(select)/性别(radio)/医生(select)/Cobb角 八项
 * （apps/admin-web/src/pages/patients/index.vue:159-191），而 5.4 旧写法是
 * `allInputs.nth(1)`/`nth(2)` 位置填值 —— nth(2) 现在是「年龄」框，把中文诊断塞进去
 * 必被后端值域校验判 400（services/user-service/internal/handler/handler.go createPatient
 * 「invalid age range」），于是那条用例每次都没建成患者、却因为整段断言包在
 * 「文案含成功」的 if 里而照常报绿。锚文案填、且成功文案改成硬断言，两处一起收。
 */
function formItem(scope: Locator, label: string): Locator {
  return scope.locator('.el-form-item').filter({ hasText: label }).first()
}

test.describe('05-患者管理', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    // T279：裸等表格行会命中上一页（数据概览）的排行表 → 本页 0 行就开断言（5.1/5.2 实跑失效根因）
    // T358：本页自 T289 起有第二张会出数的表（批量患者-团队绑定），全局等待会被它先满足，
    //        5.1 因此间歇读到 0 行 ⇒ 等待作用域收到患者列表卡自己身上，与 5.1 断言的是同一张表
    await gotoMenuAndWaitTable(page, '患者管理', 'patients', patientTable(page))
  })

  // 记录本文件新建的患者名，末尾清理
  let createdPatients: string[] = []

  test.afterAll(async ({ browser }) => {
    // 清理 5.4 自建的患者（T462 S3 甲案，PM 2589 裁「跑，跑完删除」）。
    //
    // 🔴 T462 订正「为什么以前这段等于没清」：旧实现打的是 GET /api/v1/patients 与
    //   DELETE /api/v1/patients/:id —— 这条根路径全仓没有注册（网关 userServiceRoutes 只登记
    //   /admin/patients 一族，见 services/gateway/cmd/server/proxy_admin.go:105-124 与
    //   services/user-service/internal/handler/handler.go:236-245 的路由表），所以每次都是 404，
    //   再被下面三层吞错静掉：`if (!list.ok()) continue` + 删除的 `.catch(() => {})` +
    //   外层 `catch { /* ignore */ }`。「清干净了」与「一行都没删」在报告里同形，
    //   staging 因此留下 P20264360f30837c4 / T053测试-672410 这类永久脏行
    //   —— 也正是 5.4 当年被停跑的原因。
    //   现改为走登记在架的 /admin/patients 两端点，并且**不吞错**：删不动就抛红，
    //   让「没清干净」在 CI 里看得见，而不是留给下一轮或攒成脏数据。
    if (createdPatients.length === 0) return
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const token = await getAuthToken(page)
      if (!token) throw new Error('05 收尾清理取不到 admin JWT，无法删除自建患者')
      const headers = { Authorization: `Bearer ${token}` }
      for (const name of createdPatients) {
        const listRes = await page.request.get('/api/v1/admin/patients', {
          headers,
          params: { page: '1', pageSize: '10', keyword: name },
        })
        const listBody = (await listRes.json().catch(() => null)) as {
          code?: number
          message?: string
          data?: { list?: Array<{ patientId: string; name: string }> }
        } | null
        if (!listRes.ok() || listBody?.code !== 0) {
          throw new Error(
            `清理前置查询失败：GET /api/v1/admin/patients?keyword=${name} → status=${listRes.status()} code=${listBody?.code} message=${listBody?.message}`,
          )
        }
        const hits = (listBody.data?.list ?? []).filter((p) => p.name === name)
        // 「≥1 而不是恰好 1」：清理段的职责是把同名行全删掉，行数多于 1 只说明唯一命名撞过号
        // （uniqueName 取毫秒后 6 位，约 11.6 天一轮）—— 那是 5.4 的判据要管的事，不该在这里
        // 变成「清不动」的理由。
        expect(hits.length, `按唯一姓名应至少查回自建患者 ${name} 的一行，实得 ${hits.length} 行`).toBeGreaterThanOrEqual(1)
        for (const p of hits) {
          const delRes = await page.request.delete(`/api/v1/admin/patients/${p.patientId}`, { headers })
          const delBody = (await delRes.json().catch(() => null)) as { code?: number; message?: string } | null
          console.log(
            `[t053-cleanup] DELETE /api/v1/admin/patients/${p.patientId} (${name}) → status=${delRes.status()} code=${delBody?.code}`,
          )
          if (!delRes.ok() || delBody?.code !== 0) {
            // 409 = 该患者被别处引用（逐表计数只进技术日志，响应体 message 恒为中文短句，T464 双通道）
            // ⇒ 停手回报，不绕库、不删别人的关联行。
            throw new Error(
              `自建患者未删净：${p.patientId}（${name}）→ status=${delRes.status()} code=${delBody?.code} message=${delBody?.message}` +
                (delBody?.code === 10409 ? ' ⇒ 被引用，请拿患者号查 user-service 技术日志的 per-table 计数后停手回报' : ''),
            )
          }
        }
      }
      createdPatients = []
    } finally {
      await ctx.close()
    }
  })

  test.describe('列表渲染', () => {
    test('5.1 ≥5 行患者，列信息含 ID（PT-）/姓名/团队/设备/状态', async ({ page }) => {
      const rows = tableRows(page, patientTable(page))
      const count = await rows.count()
      expect(count).toBeGreaterThanOrEqual(5)

      // 列信息存在性验证（整体表格文本中包含预期关键词簇）
      const wrapperText = await patientTable(page).locator('.el-table__body-wrapper').textContent()
      // 患者 ID：staging seed 是 P20260003 等 P+数字 格式（PT- 前缀也兼容）
      expect(wrapperText).toMatch(/P\d{4,}|PT-/)
      // T270 收尾假绿#1（D1）订正：旧写法 `/TEAM\d+|中文…/` 把「团队列显示成原始编号」
      // 也当合格 —— 那正是 D1 的症状本身，等于给缺陷放行。值级判据见下方 5.6。
      expect(wrapperText).not.toMatch(/\bTEAM\d+\b/)
      // 状态：PM 09-22 01:03 裁定 ① 收口为「可登录 / 不可登录」两态（旧文案「活跃」已作废）
      expect(wrapperText).toMatch(/可登录|不可登录/)
      // 设备：D+数字（如 D0002）或 DEV- 或「未绑定」
      expect(wrapperText).toMatch(/D\d{3,}|DEV-|未绑定/)
    })
  })

  test.describe('组织名字典契约（T270 收尾假绿#1：D1 值级判据）', () => {
    test('5.6 团队/主治医生列＝后端字典名，不得回落成原始 ID', async ({ page }) => {
      const token = await getAuthToken(page)
      expect(token, 'beforeEach 已 realLogin，应拿到 JWT').toBeTruthy()
      const headers = { Authorization: `Bearer ${token}` }

      /** 后端统一信封 { code, message, data }；code≠0 或 HTTP 非 2xx 直接判红 */
      async function apiData(path: string): Promise<Record<string, unknown>[]> {
        const res = await page.request.get(path, { headers })
        expect(res.ok(), `GET ${path} 应 2xx`).toBe(true)
        const body = await res.json()
        expect(body.code, `GET ${path} 信封 code 应为 0`).toBe(0)
        const data = body.data
        return Array.isArray(data) ? data : ((data?.list ?? []) as Record<string, unknown>[])
      }

      const teamById = new Map(
        (await apiData('/api/v1/teams')).map(
          (t) => [String(t.teamId), String(t.name)] as [string, string],
        ),
      )
      const doctorById = new Map(
        (await apiData('/api/v1/doctors')).map(
          (d) => [String(d.doctorId), String(d.name)] as [string, string],
        ),
      )
      // 与页面首屏完全同参（patients/index.vue: pageSize 默认 10、page 1），DOM 才可比
      const apiRows = await apiData('/api/v1/admin/patients?page=1&pageSize=10')
      expect(apiRows.length, 'staging seed 患者应 ≥1 行').toBeGreaterThanOrEqual(1)

      // T289 4.2 后本列的表头文案是「绑定团队」（设计稿 患者管理.html:88 口径）
      const teamIdx = await headerIndex(page, '绑定团队')
      const docIdx = await headerIndex(page, '主治医生')
      await headerIndex(page, '患者ID') // 列存在性也在契约内（定位行靠它，取不到行即计入跳过）

      async function cellsOfRow(patientId: string): Promise<string[] | null> {
        const row = patientTable(page)
          .locator('.el-table__body-wrapper tbody tr')
          .filter({ hasText: patientId })
          .first()
        if ((await row.count()) === 0) return null // 该行不在当前 DOM 页（分页），跳过计数
        return cellTexts(row)
      }

      let teamChecked = 0
      let docChecked = 0
      let unassignedChecked = 0
      for (const p of apiRows) {
        const pid = String(p.patientId)
        const cells = await cellsOfRow(pid)
        if (!cells) continue
        const teamId = p.teamId ? String(p.teamId) : null
        const doctorId = p.doctorId ? String(p.doctorId) : null

        if (teamId) {
          teamChecked++
          const dictName = teamById.get(teamId)
          // 判据力自检：字典必须能把该 ID 解析成「与编号不同」的名字，否则下面的相等断言是恒等式
          expect(dictName, `字典 /api/v1/teams 应能解析 ${teamId}`).toBeTruthy()
          expect(dictName, '字典名不得与原始编号同名，否则本条无判据力').not.toBe(teamId)
          // 1) 值级：等于 /api/v1/teams 字典里该 teamId 的中文名（D1 修前这里是 mock 查表 → TEAM01）
          expect(cells[teamIdx], `${pid} 团队列应等于字典值`).toBe(dictName)
          // 2) 后端 join 出参非空时，页面必须用 join 值（患者页优先 row.teamName）
          if (p.teamName) expect(cells[teamIdx], `${pid} 团队列应等于后端 join 值`).toBe(String(p.teamName))
          // 3) 症状级：不得显示成原始编号
          expect(cells[teamIdx], `${pid} 团队列回落成了编号`).not.toBe(teamId)
        } else {
          unassignedChecked++
          expect(cells[teamIdx], `${pid} 无团队时应显示 -`).toBe('-')
        }

        if (doctorId) {
          docChecked++
          expect(cells[docIdx], `${pid} 主治医生列应等于字典值`).toBe(doctorById.get(doctorId))
          if (p.doctorName) expect(cells[docIdx], `${pid} 主治医生列应等于后端 join 值`).toBe(String(p.doctorName))
          expect(cells[docIdx], `${pid} 主治医生列回落成了编号`).not.toBe(doctorId)
        } else {
          unassignedChecked++
          expect(cells[docIdx], `${pid} 无医生时应显示 -`).toBe('-')
        }
        // 兜底：任何一格都不该把 undefined / null 直接印出来
        for (const c of cells) expect(c).not.toMatch(/undefined|null|\[object/)
      }

      // 防「零行也通过」：staging seed 至少各命中一次两类分支
      expect(teamChecked, '应至少命中 1 行有团队的患者').toBeGreaterThanOrEqual(1)
      expect(docChecked, '应至少命中 1 行有主治医生的患者').toBeGreaterThanOrEqual(1)
      expect(unassignedChecked, '应至少命中 1 个未分配字段（否则「-」分支未被守）').toBeGreaterThanOrEqual(1)
    })
  })

  test.describe('搜索与筛选', () => {
    test('5.2 关键词搜索：读取第一个存在的姓名 → 搜索 → 仅匹配行', async ({ page }) => {
      const rows = tableRows(page, patientTable(page))
      // 姓名列按表头定位后逐格取值（旧写法用正则从整行文本里「猜」中文段，
      // 会把「未分配」「选择团队」这类列文案当成姓名，也可能猜不到 T053测试-xxx 这种含数字的名字）
      const nameIdx = await headerIndex(page, '姓名')
      const keyword = (await cellTexts(rows.first()))[nameIdx]
      expect(keyword, '首行姓名列不应为空').not.toBe('')
      // 填入搜索框
      const search = page.locator('.search-input input')
      await expect(search).toBeVisible({ timeout: 5_000 })
      await search.fill(keyword)
      // 点「查询」
      const queryBtn = page.locator('.page-toolbar').getByRole('button', { name: '查询' })
      if ((await queryBtn.count()) > 0) {
        await queryBtn.first().click()
      } else {
        // 如果没有「查询」按钮，按回车
        await search.press('Enter')
      }
      await page.waitForTimeout(2_000)
      // 结果每行都包含 keyword
      const filteredRows = tableRows(page, patientTable(page))
      const count = await filteredRows.count()
      expect(count).toBeGreaterThanOrEqual(1)
      for (let i = 0; i < count; i++) {
        const t = await filteredRows.nth(i).textContent()
        expect(t).toContain(keyword)
      }
      // 清空恢复
      await search.fill('')
      if ((await queryBtn.count()) > 0) {
        await queryBtn.first().click()
      } else {
        await search.press('Enter')
      }
      await page.waitForTimeout(2_000)
      expect(await tableRows(page, patientTable(page)).count()).toBeGreaterThanOrEqual(5)
    })

    test('5.3 团队筛选：按表头列取首行「团队」原值 → 筛后每行该列都等于它', async ({ page }) => {
      /**
       * T270 收尾假绿#1（D1）订正两处旧写法：
       *  1) 旧代码用 /(TEAM\d+|中文…)/ 从整行文本里「猜」团队名 —— 页面把编号印出来也算命中，
       *     正是 D1 的症状；现按表头文案定位列、取该格原值。
       *  2) 旧收尾是「每行至少有内容」的零断言 + pickSelectOption 的静默 catch；现要求逐行等值。
       */
      const teamIdx = await headerIndex(page, '绑定团队')
      const rows = tableRows(page, patientTable(page))
      await expect(rows.first()).toBeVisible({ timeout: 15_000 })
      const totalBefore = await rows.count()

      // 取第一行「有团队」的值（seed 里有未分配患者的行显示 -）
      let teamName = ''
      for (let i = 0; i < totalBefore; i++) {
        const v = (await cellTexts(rows.nth(i)))[teamIdx]
        if (v && v !== '-') {
          teamName = v
          break
        }
      }
      expect(teamName, '首页应至少有一行带团队').not.toBe('')

      const teamSelect = page.locator('.team-select, .filter-select.team')
      await expect(teamSelect.first()).toBeVisible({ timeout: 8_000 })
      await pickSelectOption(page, teamSelect.first(), teamName)

      const filtered = tableRows(page, patientTable(page))
      await expect
        .poll(
          async () => {
            const n = await filtered.count()
            if (n === 0) return false
            for (let i = 0; i < n; i++) {
              if ((await cellTexts(filtered.nth(i)))[teamIdx] !== teamName) return false
            }
            return true
          },
          { timeout: 20_000, message: `筛选「${teamName}」后每行团队列都应等于该值` },
        )
        .toBe(true)

      const count = await filtered.count()
      expect(count, '按存在的团队名筛选不应清零').toBeGreaterThanOrEqual(1)
      expect(count, '筛选结果不应多于筛选前').toBeLessThanOrEqual(totalBefore)
    })
  })

  test.describe('添加患者（写操作，可重放唯一命名）', () => {
    test('5.4 添加患者 → 唯一姓名（T053测试-xxx）+ 最小必填 → 提交成功 → 搜索可找到', async ({ page }) => {
      // T462 S3 放开（PM 2589 裁甲案「跑，跑完删除」）。T279 当年停跑的理由是「后端没有
      // DELETE /admin/patients/:id ⇒ afterAll 是结构性空转，跑一次永久留一条脏数据」，
      // 该前提自 T467 起不再成立：端点在架（handler.go:245 + gateway proxy_admin.go:118），
      // 本文件上方 afterAll 也已改走它做真删除并去掉三层吞错。原停跑注释全文留在 git 历史。
      const patientName = uniqueName(E2E_PATIENT_NAME_PREFIX)
      // 手机号每次唯一：建档按 phone_hash 查重（repo/pg.go:1275-1284），写死一个号时
      // 只要有一轮清理没跑成，下一轮就必 409 ⇒ 又落回「没建成也算过」。
      const phone = `135${String(Math.floor(Math.random() * 1e8)).padStart(8, '0')}`

      // 找"添加患者"/"新建患者"按钮
      const addBtn = page
        .locator('.page-toolbar')
        .getByRole('button', { name: /添加患者|新建患者|新增患者/ })
      await expect(addBtn.first()).toBeVisible({ timeout: 8_000 })
      await addBtn.first().click()

      const dialog = page.locator('.el-dialog:visible').filter({ hasText: /新建患者/ }).first()
      await expect(dialog, '点「添加患者」应开标题为「新建患者」的弹窗').toBeVisible({ timeout: 10_000 })

      // 最小必填集＝姓名 + 手机号（createRules 只给这两项挂 prop，index.vue:159-191）。
      // 年龄/诊断/Cobb/团队/性别/医生一律留空 —— 这一格验的正是「只带必填也能建档」。
      await formItem(dialog, '姓名').locator('input').first().fill(patientName)
      await formItem(dialog, '手机号').locator('input').first().fill(phone)

      const saveBtn = dialog.getByRole('button', { name: '确定' }).first()
      await expect(saveBtn).toBeVisible()
      await saveBtn.click()

      // 成功文案是硬断言（旧写法「文案含成功才往下断言」＝ 建失败也判绿）
      await expect(adminMessage(page), '最小必填建档应回「创建成功」').toHaveText('创建成功', {
        timeout: 15_000,
      })
      createdPatients.push(patientName)
      await expect(dialog).toBeHidden({ timeout: 5_000 })

      // 回到列表搜索姓名，验「建出来的行查得到」
      const search = page.locator('.search-input input')
      await expect(search, '本页应有姓名搜索框').toBeVisible({ timeout: 8_000 })
      await search.fill(patientName)
      const qBtn = page.locator('.page-toolbar').getByRole('button', { name: '查询' }).first()
      await qBtn.click()
      const rows = tableRows(page, patientTable(page))
      await expect
        .poll(
          async () => {
            const n = await rows.count()
            if (n === 0) return false
            const t = await rows.first().textContent()
            return (t ?? '').includes(patientName)
          },
          { timeout: 20_000, message: `按唯一姓名搜索后，列表首行应为刚建的 ${patientName}` },
        )
        .toBe(true)
      expect(await rows.count()).toBeGreaterThanOrEqual(1)
      expect(await rows.first().textContent()).toContain(patientName)

      // 清空搜索：不把筛选态留给同文件的下一条用例
      await search.fill('')
      await qBtn.click()
    })
  })

  test.describe('分配团队（写操作）', () => {
    test('5.5 行 → 抽屉 → 分配团队 → 选团队 → 确定 → ElMessage 成功', async ({ page }) => {
      // T279 停跑（PM 口径：只跑自建自删的写用例）：本用例改的是 seed 患者的团队归属。
      //   PUT /admin/patients/:id/team 虽可改回，但用例中途失败就把 seed 患者留在错误团队，
      //   连带影响 5.3 团队筛选与 06 团队管理对成员数/患者数的 seed 断言。
      test.skip(true, '改 seed 患者的团队归属，失败即污染 5.3/06 的 seed 断言，按 PM 口径停跑')
      const rows = tableRows(page, patientTable(page))
      expect(await rows.count()).toBeGreaterThanOrEqual(1)

      // patients 页 @row-click=viewDetail：点第 2 个 td（避开患者ID列的文本选中），行点击事件与点哪格无关
      const firstRow = rows.first()
      await firstRow.locator('td').nth(1).click()

      const drawer = page.locator('.el-drawer')
      await expect(drawer).toBeVisible({ timeout: 8_000 })
      // 抽屉标题形如「姓名（ID）」
      await expect(drawer.locator('.el-drawer__title')).toContainText(/（|\(/, { timeout: 5_000 })

      // .drawer-actions 内「分配团队」按钮
      const assignBtn = drawer.locator('.drawer-actions').getByRole('button', { name: '分配团队' })
      await expect(assignBtn).toBeVisible({ timeout: 5_000 })
      await assignBtn.click()

      // 分配团队 dialog（title=分配团队）
      const dialog = page.locator('.el-dialog').filter({ hasText: '分配团队' })
      await expect(dialog).toBeVisible({ timeout: 8_000 })

      // 选目标团队：dialog 内第一个 el-select 的第 1 个选项
      const teamSelect = dialog.locator('.el-select').first()
      await expect(teamSelect).toBeVisible({ timeout: 8_000 })
      await teamSelect.click({ timeout: 5_000 })
      const dropdown = page.locator('.el-select-dropdown:visible')
      await expect(dropdown).toBeVisible({ timeout: 5_000 })
      const opts = dropdown.locator('.el-select-dropdown__item')
      await expect(opts.first()).toBeVisible({ timeout: 8_000 })
      await opts.first().click()

      // 点「确定」
      const confirmBtn = dialog.getByRole('button', { name: '确定' }).first()
      await expect(confirmBtn).toBeVisible({ timeout: 5_000 })
      await confirmBtn.click()

      // ElMessage 成功（不能含 失败/错误/error）
      const msg = adminMessage(page)
      const visible = await msg.isVisible({ timeout: 15_000 }).catch(() => false)
      if (visible) {
        const t = await msg.textContent()
        expect(t).not.toMatch(/失败|错误|error|500|404/)
      }
    })
  })
})

/**
 * T358 回归守卫：本页「已就绪」的信号必须锚在患者列表卡自己身上。
 *
 * 成因（CI run 35884021247 实跑判红 + 本地注入 4/4 复现）：本页自 T289 起有两张各发各请求的表
 * （列表 pageSize=10 / 批量绑定卡 pageSize=100），而等待条件是「页面里任意一行表格可见」，
 * 批量卡先返回时条件即被它满足，紧跟其后的一次性 rows.count() 读到 0。
 *
 * 本用例把 CI 里的那次响应顺序固定下来：只给列表那一枪加 1.2s 延迟，批量卡不加。
 * 修好了（等待带卡作用域）⇒ 照旧出数；作用域被人摘回去 ⇒ 同一条注入立刻判红。
 * 只读：route 只加延迟后 continue，不改响应体、不写数据。
 */
test.describe('05b-列表就绪等待作用域（T358 竞态回归）', () => {
  test('批量卡先出数时，等待仍锚在患者列表卡：一次性读行数不得为 0', async ({ page }) => {
    let delayed = 0
    await page.route('**/api/v1/admin/patients*', async (route) => {
      // 只延迟列表那一枪（pageSize=10）；批量卡的 pageSize=100 原样放行
      if (/[?&]pageSize=10(&|$)/.test(route.request().url())) {
        delayed++
        await new Promise((r) => setTimeout(r, 1_200))
      }
      await route.continue()
    })

    await realLogin(page)
    await gotoMenuAndWaitTable(page, '患者管理', 'patients', patientTable(page))

    const rows = tableRows(page, patientTable(page))
    expect(
      await rows.count(),
      '等待放行时患者列表卡就该已经出数；读到 0 说明等待又被同页另一张表满足了（T358 回归）',
    ).toBeGreaterThanOrEqual(5)
    // 注入没命中就等于本用例什么都没守（列表请求形状变了）⇒ 判红并要求同步更新
    expect(delayed, '延迟注入未命中 pageSize=10 那一枪，判据已失效').toBeGreaterThanOrEqual(1)
  })
})
