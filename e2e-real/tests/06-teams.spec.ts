import { test, expect, type Page, type Locator } from '@playwright/test'
import {
  realLogin,
  gotoMenuAndWaitTable,
  tableRows,
  adminMessage,
  E2E_TEAM_NAME_PREFIX,
  uniqueName,
  getAuthToken,
} from '../real-helpers'

/**
 * T053 - 06 团队管理（真实模式）
 * T051 seed：3 团队。写操作全部用唯一命名 T053团队-xxxxx。
 * 覆盖：列表 / 新建 / 编辑 / 删除引用团队（拒绝或成功均可）/ 删除自建团队（成功）
 */
test.describe('06-团队管理', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    // T279：先等 URL 落位再等行，避免命中上一页（数据概览）排行表
    await gotoMenuAndWaitTable(page, '团队管理', 'teams')
  })

  // 本 spec 新建的团队名（用于末尾删除清理）
  let createdTeams: string[] = []

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

  test.afterAll(async ({ browser }) => {
    if (createdTeams.length === 0) return
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const token = await getAuthToken(page)
      if (token) {
        for (const name of createdTeams) {
          const list = await page.request.get('/api/v1/teams', {
            headers: { Authorization: `Bearer ${token}` },
          })
          if (!list.ok()) continue
          const data: any = await list.json().catch(() => null)
          const items = data?.data?.items || data?.items || data?.data || []
          for (const t of items) {
            if (t.name === name || String(t.name || '').includes(name)) {
              await page.request.delete(`/api/v1/teams/${t.id || t.teamId}`, {
                headers: { Authorization: `Bearer ${token}` },
              }).catch(() => {})
            }
          }
        }
      }
    } catch { /* ignore */ } finally {
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

    test('6.5 删除自己新建的（唯一命名）团队 → 删除成功，列表消失', async ({ page }) => {
      // 前置：6.2 创建团队成功（6.3 可能改了名但 createdTeams 已同步），否则如实失败
      expect(createdTeams.length).toBeGreaterThan(0)
      const targetName = createdTeams[createdTeams.length - 1]
      const rows = tableRows(page)
      let target: Locator | null = null
      for (let i = 0; i < await rows.count(); i++) {
        if ((await rows.nth(i).textContent())?.includes(targetName)) {
          target = rows.nth(i)
          break
        }
      }
      expect(target).not.toBeNull()
      const delBtn = target!.getByRole('button', { name: /删除|移除/ }).first()
      await expect(delBtn).toBeVisible({ timeout: 5_000 })
      await delBtn.click()
      const msgBox = page.locator('.el-message-box')
      await expect(msgBox).toBeVisible({ timeout: 10_000 })
      await msgBox.getByRole('button', { name: /确定|确认/ }).first().click()
      // T279 收紧：旧写法「无提示 = 静默通过」，现在必须真删成功 + 行真的消失
      await expect(adminMessage(page)).toHaveText('删除成功', { timeout: 15_000 })
      await expect(tableRows(page).filter({ hasText: targetName })).toHaveCount(0, { timeout: 10_000 })
      createdTeams.pop()
    })
  })
})
