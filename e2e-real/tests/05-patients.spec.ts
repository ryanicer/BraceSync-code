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
/** 表头文案 → 列下标（首列是 selection 复选框，故绝不按写死序号取列） */
async function headerIndex(page: Page, title: string): Promise<number> {
  const texts = await page
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

test.describe('05-患者管理', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    // T279：裸等表格行会命中上一页（数据概览）的排行表 → 本页 0 行就开断言（5.1/5.2 实跑失效根因）
    await gotoMenuAndWaitTable(page, '患者管理', 'patients')
  })

  // 记录本文件新建的患者名，末尾清理
  let createdPatients: string[] = []

  test.afterAll(async ({ browser }) => {
    // 清理：通过 API 批量删除本任务创建的患者（UI 删除入口未实现时兜底）
    if (createdPatients.length === 0) return
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const token = await getAuthToken(page)
      if (!token) return
      for (const name of createdPatients) {
        const list = await page.request.get('/api/v1/patients', {
          headers: { Authorization: `Bearer ${token}` },
          params: { keyword: name },
        })
        if (!list.ok()) continue
        const data: any = await list.json().catch(() => null)
        const items = data?.data?.items || data?.items || data?.data || []
        for (const p of items) {
          if (p.name === name || String(p.name || '').includes(name)) {
            await page.request.delete(`/api/v1/patients/${p.id || p.patientId}`, {
              headers: { Authorization: `Bearer ${token}` },
            }).catch(() => {})
          }
        }
      }
    } catch { /* ignore */ } finally {
      await ctx.close()
    }
  })

  test.describe('列表渲染', () => {
    test('5.1 ≥5 行患者，列信息含 ID（PT-）/姓名/团队/设备/状态', async ({ page }) => {
      const rows = tableRows(page)
      const count = await rows.count()
      expect(count).toBeGreaterThanOrEqual(5)

      // 列信息存在性验证（整体表格文本中包含预期关键词簇）
      const wrapperText = await page.locator('.el-table__body-wrapper').textContent()
      // 患者 ID：staging seed 是 P20260003 等 P+数字 格式（PT- 前缀也兼容）
      expect(wrapperText).toMatch(/P\d{4,}|PT-/)
      // T270 收尾假绿#1（D1）订正：旧写法 `/TEAM\d+|中文…/` 把「团队列显示成原始编号」
      // 也当合格 —— 那正是 D1 的症状本身，等于给缺陷放行。值级判据见下方 5.6。
      expect(wrapperText).not.toMatch(/\bTEAM\d+\b/)
      // 状态：活跃 / 未绑定 / 待分配 任一
      expect(wrapperText).toMatch(/活跃|未绑定|待分配|佩戴/)
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

      const teamIdx = await headerIndex(page, '团队')
      const docIdx = await headerIndex(page, '主治医生')
      await headerIndex(page, '患者ID') // 列存在性也在契约内（定位行靠它，取不到行即计入跳过）

      async function cellsOfRow(patientId: string): Promise<string[] | null> {
        const row = page
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
      const rows = tableRows(page)
      // 读取第一行的患者姓名（列位置：在 PT-xxx 之后，团队名之前，用正则从行文本中抓中文姓名段）
      const firstRowText = await rows.first().textContent() ?? ''
      // 匹配 2-4 字中文（典型姓名长度）
      const m = firstRowText.match(/([\u4e00-\u9fa5]{2,4})/)
      expect(m).toBeTruthy()
      const keyword = m![1]
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
      const filteredRows = tableRows(page)
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
      expect(await tableRows(page).count()).toBeGreaterThanOrEqual(5)
    })

    test('5.3 团队筛选：按表头列取首行「团队」原值 → 筛后每行该列都等于它', async ({ page }) => {
      /**
       * T270 收尾假绿#1（D1）订正两处旧写法：
       *  1) 旧代码用 /(TEAM\d+|中文…)/ 从整行文本里「猜」团队名 —— 页面把编号印出来也算命中，
       *     正是 D1 的症状；现按表头文案定位列、取该格原值。
       *  2) 旧收尾是「每行至少有内容」的零断言 + pickSelectOption 的静默 catch；现要求逐行等值。
       */
      const teamIdx = await headerIndex(page, '团队')
      const rows = tableRows(page)
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

      const filtered = tableRows(page)
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
      // T279 停跑：后端无 DELETE /admin/patients/{id}（user-service 路由表与 proxy_admin.go 均无），
      //   患者页也无删除入口 ⇒ afterAll 的 API 清理是结构性空转，跑一次永久留一条脏数据
      //   （staging 遗留 P20264360f30837c4 / T053测试-672410 即证）。补删除端点后再恢复。
      //   ⚠️ PM 裁定（本卡 2026-09-21 13:00）把 5.4 列入「跑（自建唯一命名数据、跑完删除）」，
      //      但「跑完删除」这一步在 staging 没有端点可做 ⇒ 前提不成立，已回报 PM 待重裁；
      //      PM 若确认「可留一条脏患者」，删掉下面这行 test.skip 即可放开，不需其他改动。
      test.skip(true, '无 DELETE /admin/patients 端点，afterAll 清理失效 ⇒ 「跑完删除」前提不成立，会永久留脏数据；已报 PM 待重裁')
      const patientName = uniqueName(E2E_PATIENT_NAME_PREFIX)

      // 找"添加患者"/"新建患者"按钮
      const addBtn = page
        .locator('.page-toolbar')
        .getByRole('button', { name: /添加患者|新建患者|新增患者/ })
      await expect(addBtn.first()).toBeVisible({ timeout: 8_000 })
      await addBtn.first().click()

      const dialog = page.locator('.el-dialog').filter({ hasText: /患者|新建|添加/ })
      await expect(dialog).toBeVisible({ timeout: 10_000 })

      // 填最小必填集（姓名 + 手机号/诊断 + 性别/生日等）：找到所有非 password input 逐个填
      // 1) 姓名：第一个 input 或有 "姓名" 标签的字段
      const nameInput = dialog
        .locator('input, .el-input__inner')
        .filter({ hasNot: page.locator('[type="password"]') })
        .first()
      await nameInput.fill(patientName)

      // 2) 手机号（如果表单有）：11 位数字
      const allInputs = dialog.locator('input:not([type="password"])')
      const inputCount = await allInputs.count()
      if (inputCount >= 2) {
        await allInputs.nth(1).fill('13900000001')
      }
      if (inputCount >= 3) {
        // 诊断 / 备注：兜底文字
        await allInputs.nth(2).fill('测试诊断 T053 真实模式')
      }

      // 3) 如果有 select（性别/团队），选第一个非空值
      const selects = dialog.locator('.el-select')
      for (let i = 0; i < Math.min(await selects.count(), 2); i++) {
        const s = selects.nth(i)
        try {
          await s.click({ timeout: 3_000 })
          const opt = page.locator('.el-select-dropdown:visible .el-select-dropdown__item').nth(1)
          if (await opt.isVisible({ timeout: 3_000 })) {
            await opt.click()
          } else {
            // 第一个选项
            await page.locator('.el-select-dropdown:visible .el-select-dropdown__item').first().click()
          }
        } catch { /* ignore */ }
        await page.locator('.el-select-dropdown:visible')
          .waitFor({ state: 'hidden', timeout: 3_000 })
          .catch(() => {})
      }

      // 4) 保存 / 提交按钮（staging 应用文案是「确定」，兼容 保存/确认/提交/添加）
      const saveBtn = dialog.getByRole('button', { name: /保存|确定|确认|提交|添加/ }).first()
      await expect(saveBtn).toBeVisible()
      await saveBtn.click()

      // ElMessage 成功（或错误：如必填未齐）
      const msg = adminMessage(page)
      const msgVisible = await msg.isVisible({ timeout: 15_000 }).catch(() => false)
      if (msgVisible) {
        const msgText = await msg.textContent()
        if (/成功|完成|已添加/.test(msgText ?? '')) {
          createdPatients.push(patientName)
          // 对话框关闭
          await expect(dialog).toBeHidden({ timeout: 5_000 }).catch(() => {})
          // 回到列表搜索姓名
          await page.waitForTimeout(1_500)
          const search = page.locator('.search-input input')
          if ((await search.count()) > 0) {
            await search.fill(patientName)
            const qBtn = page.locator('.page-toolbar').getByRole('button', { name: '查询' })
            if ((await qBtn.count()) > 0) await qBtn.first().click()
            else await search.press('Enter')
            await page.waitForTimeout(2_000)
            const rows = tableRows(page)
            expect(await rows.count()).toBeGreaterThanOrEqual(1)
            expect(await rows.first().textContent()).toContain(patientName)
            // 清空搜索
            await search.fill('')
            if ((await qBtn.count()) > 0) await qBtn.first().click()
            else await search.press('Enter')
          }
        }
        // 如果是必填校验错误，不算 bug（只是我们没填全），记录即可
      }
    })
  })

  test.describe('分配团队（写操作）', () => {
    test('5.5 行 → 抽屉 → 分配团队 → 选团队 → 确定 → ElMessage 成功', async ({ page }) => {
      // T279 停跑（PM 口径：只跑自建自删的写用例）：本用例改的是 seed 患者的团队归属。
      //   PUT /admin/patients/:id/team 虽可改回，但用例中途失败就把 seed 患者留在错误团队，
      //   连带影响 5.3 团队筛选与 06 团队管理对成员数/患者数的 seed 断言。
      test.skip(true, '改 seed 患者的团队归属，失败即污染 5.3/06 的 seed 断言，按 PM 口径停跑')
      const rows = tableRows(page)
      expect(await rows.count()).toBeGreaterThanOrEqual(1)

      // patients 页 @row-click=viewDetail：点第 2 个 td（第 1 个是 selection 列，避开）触发行点击打开抽屉
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
