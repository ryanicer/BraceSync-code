import { test, expect, type Page, type Locator } from '@playwright/test'
import {
  realLogin,
  gotoMenuAndWaitTable,
  tableRows,
  adminMessage,
  getAuthToken,
  releaseTeamMembers,
  restoreTeamMembers,
  snapshotDoctorTeams,
  expectDoctorsRestored,
  E2E_TEAM_NAME_PREFIX,
  uniqueName,
} from '../real-helpers'
import { requireDeployedBuild } from '../deploy-guard'

/**
 * T053 - 06 团队管理（真实模式）
 * T051 seed：3 团队。写操作全部用唯一命名 T053团队-xxxxx。
 * 覆盖：列表 / 新建 / 编辑 / 删除引用团队（6.4 停跑，见用例内理由）/
 *       自建团队删除的两面（T682 B案）：带引用被拒 409·10409 + 解除引用后删除成功
 */
test.describe('06-团队管理', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    // T279：先等 URL 落位再等行，避免命中上一页（数据概览）排行表
    await gotoMenuAndWaitTable(page, '团队管理', 'teams')
  })

  // 本 spec 新建的团队名（用于末尾删除清理）
  let createdTeams: string[] = []

  // T682「迁回」基数：建团队**之前**医护的归属快照（doctorId → teamId | null）。
  // 6.2 拍、6.5 与 afterAll 用；见 real-helpers.snapshotDoctorTeams 的因果说明。
  let priorDoctorTeams: Record<string, string | null> = {}

  /**
   * 在团队弹窗里选负责人（6.2 新建、6.3 编辑都要调）。
   * 编辑也必须重选：GET /api/v1/teams 的列表项只有 teamId/name/memberCount/patientCount，
   * 不带 leader，而 teams/index.vue:223 openEdit 用 row.leader ?? '' 回填 ⇒ 弹窗负责人恒空，
   * 不选就点保存会被 confirmSaveTeam 的 `if (!leader) ElMessage.warning('请选择负责人')` 拦下。
   * （已作为缺陷登记：T279 报告 F-9）
   */
  async function pickTeamOwner(page: Page, dialog: Locator): Promise<void> {
    const ownerSelect = dialog
      .locator('.el-form-item')
      .filter({ hasText: '负责人' })
      .locator('.el-select')
      .first()
    await expect(ownerSelect, '团队弹窗应有「负责人」下拉').toBeVisible({ timeout: 5_000 })
    await ownerSelect.click()
    const opts = page.locator('.el-select-dropdown:visible .el-select-dropdown__item')
    await expect(opts.first(), '负责人下拉应至少有一名医生').toBeVisible({ timeout: 5_000 })
    const ownerName = ((await opts.first().innerText()) ?? '').trim()
    expect(ownerName, '负责人选项文案不应为空').not.toBe('')
    await opts.first().click()
    await expect(ownerSelect, '选完负责人后应回显所选医生').toContainText(ownerName, { timeout: 5_000 })
  }

  /**
   * 按团队名解析 teamId（GET /api/v1/teams 列表项只有 teamId/name/memberCount/patientCount）。
   * 6.5 与 afterAll 都要它：解除引用、删除都必须用 ID，UI 行文案里拿不到。
   */
  async function findTeamIdByName(page: Page, token: string, name: string): Promise<string | null> {
    const res = await page.request.get('/api/v1/teams', {
      headers: { Authorization: `Bearer ${token}` },
    })
    if (!res.ok()) return null
    const data: any = await res.json().catch(() => null)
    const items = data?.data?.items || data?.items || data?.data || []
    for (const t of items) {
      if (t.name === name || String(t.name || '').includes(name)) return String(t.id || t.teamId)
    }
    return null
  }

  /**
   * T682 B案：删除前解除引用走产品契约端点，实现见 real-helpers.releaseTeamMembers
   * （根因、端点口径、UI「移除」按钮在真实模式失效的缺陷说明都在那里，勿在本文件复制第二份）。
   */

  test.afterAll(async ({ browser }) => {
    if (createdTeams.length === 0) return
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const token = await getAuthToken(page)
      if (!token) {
        console.log('[06-cleanup] 无 token，跳过清理 ⇒ 以下团队会残留：', createdTeams.join('|'))
        return
      }
      const headers = { Authorization: `Bearer ${token}` }
      // T682：清理必须先解除负责人引用再 DELETE，否则恒 409 ⇒ 静默留孤儿
      //（T279 实跑就是这么在 staging 落下 T053团队-248968 的）
      for (const name of createdTeams) {
        const teamId = await findTeamIdByName(page, token, name)
        if (!teamId) {
          console.log(`[06-cleanup] 未找到团队「${name}」（可能已删）`)
          continue
        }
        const rel = await releaseTeamMembers(page.request, token, teamId)
        if (rel.results.length > 0) {
          console.log(`[06-cleanup] 「${name}」(${teamId}) 解除引用 ${JSON.stringify(rel.results)}`)
        }
        const del = await page.request.delete(`/api/v1/teams/${teamId}`, { headers }).catch(() => null)
        if (!del || del.status() !== 200) {
          console.log(`[06-cleanup] 删除「${name}」(${teamId}) 未 200：status=${del?.status()}`)
        }
        // 迁回（清理路径只打日志不判红，否则一处失败会挡住后面几条的清理）
        const restored = await restoreTeamMembers(page.request, token, priorDoctorTeams, rel.results)
        for (const r of restored) {
          if (r.status !== null && r.status !== 200) {
            console.log(`[06-cleanup] 迁回 ${r.memberId} → ${r.toTeamId} 未 200：status=${r.status}`)
          }
        }
      }
    } catch (e) {
      console.log('[06-cleanup] 清理异常：', String(e))
    } finally {
      await ctx.close()
    }
  })

  test.describe('列表渲染', () => {
    test('6.1 3 行 seed 团队（或更多），列信息含团队名/负责人/成员/患者', async ({ page }) => {
      const rows = tableRows(page)
      const count = await rows.count()
      // seed 3 团队 + 可能历史遗留，≥3 即可
      expect(count).toBeGreaterThanOrEqual(3)

      const allText = await page.locator('.el-table').textContent() ?? ''
      // 团队名含「组/团队/科」
      expect(allText).toMatch(/组|团队|科/)
      // 至少出现"成员"或"人"（成员数/患者数列头）
      expect(allText).toMatch(/成员|患者|负责人|医生/)
    })

    test('6.1b T658：列表只显医护团队，维护班组（TEAM04/TEAM05）不出现在表格', async ({ page }) => {
      const table = page.locator('.page-card .el-table')
      await expect(table.first()).toBeVisible()
      // 部署腿：e2e-real 打 staging 现网包，PR 合入 ≠ 现网包已更新；旧包仍显示维护班组，
      // 走显式跳过（同 08-settings 8.4 口径），Andy 部署追平后自动转验。
      await requireDeployedBuild(page, {
        marker: 'T658-teams-hide-maintenance',
        why: '团队管理页列表隐藏维护班组（T658 PR 合入并部署后生效）',
        probe: async (p) => (await p.locator('.page-card .el-table').filter({ hasText: '维护班组' }).count()) === 0,
      })
      // 维护班组标签一个都不许出现（隐藏是全量的，不是只藏 TEAM04/05 两个编号）
      await expect(table).not.toContainText('维护班组')
      // 口径提示常驻，解释统计卡全量与列表的差异
      await expect(page.locator('.page-toolbar')).toContainText('维护班组')
    })
  })

  test.describe('新建团队（写）', () => {
    test('6.2 新建团队（唯一命名 + 选负责人）→ 保存成功 + 列表出现', async ({ page }) => {
      const teamName = uniqueName(E2E_TEAM_NAME_PREFIX)
      // 新建团队按钮
      const newBtn = page
        .locator('.page-toolbar')
        .getByRole('button', { name: /新建团队|新增团队/ })
      await expect(newBtn.first()).toBeVisible({ timeout: 8_000 })
      await newBtn.first().click()

      const dialog = page.locator('.el-dialog').filter({ hasText: /新建团队|新建/ })
      await expect(dialog).toBeVisible({ timeout: 10_000 })
      // 团队名称输入框（按 form-item 文案定位，避免抓到负责人下拉的内层 input）
      const nameInput = dialog
        .locator('.el-form-item')
        .filter({ hasText: '团队名称' })
        .locator('input')
        .first()
      await nameInput.fill(teamName)

      // 负责人（必填，不选会被前端拦成「请选择负责人」）
      await pickTeamOwner(page, dialog)

      // 保存按钮
      const saveBtn = dialog.getByRole('button', { name: /保存|确认|提交/ }).first()
      await expect(saveBtn).toBeVisible()
      // T682 迁回基数：必须在**点保存之前**拍——POST /teams 会把负责人迁进新队，
      // 保存后再读就没有「他原来属于哪个团队」的信息了。
      const snapToken = await getAuthToken(page)
      expect(snapToken, '6.2 需登录态 token 才能拍医护归属基数').toBeTruthy()
      priorDoctorTeams = await snapshotDoctorTeams(page.request, snapToken!)
      // 先把名字登记再点保存：只要发出过保存请求，afterAll 就必须去删它。
      // T279 实跑教训：旧写法整段包在 if(visible){if(/成功/){…}} 里，一旦 ElMessage 没被抓到，
      // 用例零断言通过、createdTeams 却为空 ⇒ staging 留下孤儿团队 T053团队-248968 无人清理。
      createdTeams.push(teamName)
      await saveBtn.click()

      // 结果提示按「文案」等，不能只等「出现任意 .el-message」：
      // 登录成功那条「欢迎，运营小张」也是 .el-message，3s 内会被先读到（T279 实跑就是这样误判的）。
      // teams/index.vue:259 createTeamApi 成功 → ElMessage.success('创建成功')
      await expect(adminMessage(page)).toHaveText('创建成功', { timeout: 15_000 })

      // 对话框关闭 + 列表出现该唯一命名行
      await expect(dialog).toBeHidden({ timeout: 5_000 })
      await expect(tableRows(page).filter({ hasText: teamName })).toHaveCount(1, { timeout: 10_000 })
    })
  })

  test.describe('编辑团队（写）', () => {
    test('6.3 编辑新建团队 → 名称加「-改」→ 更新成功', async ({ page }) => {
      // 前置：6.2 创建团队成功，否则本用例如实失败并让测试人员知道 6.2 有问题
      expect(createdTeams.length).toBeGreaterThan(0)
      const oldName = createdTeams[createdTeams.length - 1]
      // 找到目标行
      const rows = tableRows(page)
      let target: Locator | null = null
      for (let i = 0; i < await rows.count(); i++) {
        if ((await rows.nth(i).textContent())?.includes(oldName)) {
          target = rows.nth(i)
          break
        }
      }
      expect(target).not.toBeNull()
      // 编辑按钮
      const editBtn = target!.getByRole('button', { name: /编辑|修改/ }).first()
      await expect(editBtn).toBeVisible({ timeout: 5_000 })
      await editBtn.click()
      const dialog = page.locator('.el-dialog').filter({ hasText: '编辑团队' })
      await expect(dialog, '点行内「编辑」应打开标题为「编辑团队」的弹窗').toBeVisible({ timeout: 10_000 })
      const nameInput = dialog
        .locator('.el-form-item')
        .filter({ hasText: '团队名称' })
        .locator('input')
        .first()
      // 在末尾加「-改」
      const newName = `${oldName}-改`
      await nameInput.fill('')
      await nameInput.fill(newName)
      // 负责人：编辑弹窗不回显（列表 DTO 无 leader，见 pickTeamOwner 注释），不重选会被拦成「请选择负责人」
      await pickTeamOwner(page, dialog)
      const save = dialog.getByRole('button', { name: /保存|确认|提交/ }).first()
      await save.click()
      const msg = adminMessage(page)
      // T279 收紧：旧写法「无提示 / 提示对不上 = 静默通过」；按准确文案轮询（见 6.2 注释的欢迎语抢位）
      await expect(msg).toHaveText('更新成功', { timeout: 15_000 })
      createdTeams[createdTeams.length - 1] = newName
      await expect(dialog).toBeHidden({ timeout: 5_000 })
      await expect(tableRows(page).filter({ hasText: newName })).toHaveCount(1, { timeout: 10_000 })
    })
  })

  test.describe('删除团队', () => {
    test('6.4 删除 seed 已有团队（被引用则拒绝或通过都 OK，不崩溃）', async ({ page }) => {
      // T279 复跑停跑（PM 口径）：删掉的是 seed 团队本身，02.2/05/06.1 的 seed 基线会跟着塌。
      test.skip(true, '会删除共享 seed 团队且不可恢复，T279 起停跑（自建团队由 6.5 负责删除）')
      const rows = tableRows(page)
      expect(await rows.count()).toBeGreaterThanOrEqual(3)
      // 找第一个不是我们自己创建的团队（seed 团队）
      let target: Locator | null = null
      for (let i = 0; i < await rows.count(); i++) {
        const t = await rows.nth(i).textContent()
        const isMine = createdTeams.some((n) => t?.includes(n))
        if (!isMine) { target = rows.nth(i); break }
      }
      expect(target).not.toBeNull()
      const delBtn = target!.getByRole('button', { name: /删除|移除/ }).first()
      await expect(delBtn).toBeVisible({ timeout: 5_000 })
      await delBtn.click()
      const msgBox = page.locator('.el-message-box')
      if (await msgBox.isVisible({ timeout: 8_000 }).catch(() => false)) {
        await msgBox.getByRole('button', { name: /确定|确认/ }).first().click()
        // ElMessage 任意内容都可（成功/引用都算不崩溃）
        const flash = adminMessage(page)
        await flash.isVisible({ timeout: 15_000 }).catch(() => false)
        const cls = await flash.getAttribute('class').catch(() => '')
        // 如果是 error 类型且含引用/占用，也算符合策略
        expect(cls).toBeTruthy()
      }
    })

    test('6.5 删除自建团队：带引用被拒（409/10409）→ 解除负责人引用 → 删除成功、列表消失', async ({
      page,
    }) => {
      // 前置：6.2 创建团队成功（6.3 可能改了名但 createdTeams 已同步），否则如实失败
      expect(createdTeams.length).toBeGreaterThan(0)
      const targetName = createdTeams[createdTeams.length - 1]
      const token = await getAuthToken(page)
      expect(token, '6.5 需登录态 token 才能按契约端点解除负责人引用').toBeTruthy()
      const teamId = await findTeamIdByName(page, token!, targetName)
      expect(teamId, `GET /api/v1/teams 应能按名解析出自建团队「${targetName}」`).toBeTruthy()

      const rows = tableRows(page)
      const targetRow = rows.filter({ hasText: targetName })
      await expect(targetRow, '目标自建团队应在列表里且只有 1 行').toHaveCount(1)

      // ── ① 正向覆盖（T682 B案新增）：负责人仍在队时删除，必须被拒 ──
      // 6.2 新建时 pickTeamOwner 选的医生被 CreateTeam 迁进本队 ⇒ 本队天然「被引用」，
      // 旧写法却直接期望「删除成功」⇒ 与本 spec 自身的 6.2 天然互斥，恒红（T279/T682 同因）。
      const delBtn = targetRow.first().getByRole('button', { name: /删除|移除/ }).first()
      await expect(delBtn, '目标行应有「删除」按钮').toBeVisible({ timeout: 5_000 })
      await delBtn.click()
      const msgBox = page.locator('.el-message-box')
      await expect(msgBox, '点删除应弹确认框').toBeVisible({ timeout: 10_000 })
      await msgBox.getByRole('button', { name: /确定|确认/ }).first().click()
      // 10409 ⇒ errorCopy.ts:42 的用户文案（前端确实在拒绝，而不是静默成功）
      await expect(adminMessage(page)).toHaveText('当前状态与该操作冲突，请刷新后重试', { timeout: 15_000 })
      // 同一动作的接口面读数：状态码 409 + 信封码 10409（handler.go:3024 是全线唯一 409 来源）
      const rejected = await page.request.delete(`/api/v1/teams/${teamId}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      expect(rejected.status(), `带引用的 DELETE 应 409（实得 ${rejected.status()}）`).toBe(409)
      const rejectedBody: any = await rejected.json().catch(() => null)
      expect(rejectedBody?.code, `拒绝应回信封码 10409（实得 ${rejectedBody?.code}）`).toBe(10409)
      // 拒绝不能留下副作用：行仍在（confirmDelete 仅在成功时乐观移除）
      await expect(rows.filter({ hasText: targetName }), '删除被拒后目标行应仍在列表').toHaveCount(1)

      // ── ② 按产品契约解除引用（见 real-helpers.releaseTeamMembers 注释）──
      const rel = await releaseTeamMembers(page.request, token!, teamId!)
      expect(rel.listStatus, `GET /teams/${teamId}/members 应 200（实得 ${rel.listStatus}）`).toBe(200)
      for (const r of rel.results) {
        expect(
          r.status,
          `解除${r.memberType}引用 ${r.memberId} 应 200（实得 ${r.status}）`,
        ).toBe(200)
      }
      expect(
        rel.doctors + rel.technicians,
        `解除前至少应有 1 名在册成员（负责人被新建流程迁了队），实得 ${JSON.stringify(rel)}`,
      ).toBeGreaterThanOrEqual(1)

      // ── ③ 无引用后再删：必须真删成功 + 行真的消失 ──
      await targetRow.first().getByRole('button', { name: /删除|移除/ }).first().click()
      await expect(msgBox, '第二次点删除应再次弹确认框').toBeVisible({ timeout: 10_000 })
      await msgBox.getByRole('button', { name: /确定|确认/ }).first().click()
      await expect(adminMessage(page)).toHaveText('删除成功', { timeout: 15_000 })
      await expect(rows.filter({ hasText: targetName })).toHaveCount(0, { timeout: 10_000 })
      // 回读接口面（不采信页面）：该 teamId 应已从字典消失
      const gone = await page.request.get(`/api/v1/teams/${teamId}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      expect(gone.status(), `删除后 GET 该团队应 404（实得 ${gone.status()}）`).toBe(404)

      // ── ④ 迁回（T682 B案的「迁回」半边）：把负责人加回他建队前的团队 ──
      // 只解除不迁回 ⇒ doctor_li 变成「无团队医护」，会连累 2.4（医生 scope 排行 0 行）
      // 与 29.1（用例前置「doctor_li 应有团队归属」）——14:04 全量复跑实测 teamId=null。
      const restored = await restoreTeamMembers(page.request, token!, priorDoctorTeams, rel.results)
      for (const r of restored) {
        expect(
          r.status === null || r.status === 200,
          `迁回${r.memberType} ${r.memberId} → ${r.toTeamId} 应 200 或本无归属（实得 ${r.status}）`,
        ).toBe(true)
      }
      console.log(`[06-迁回] ${JSON.stringify(restored)}`)
      await expectDoctorsRestored(page.request, token!, restored)
      createdTeams.pop()
    })
  })
})
