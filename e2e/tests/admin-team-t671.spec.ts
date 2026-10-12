import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * T671 团队管理两修正（Boss 2026-10-11 亲测）：
 *  A. 添加成员候选下拉显示真实职称（Doctor.title），不再全员硬编码「（医生）」；职称空只显名字。
 *  B. 新建团队选了负责人，建团后成员管理列表必须立即可见负责人（后端同事务写 doctors.team_id）。
 *
 * 基座：mock e2e（mockCreateTeam/mockTeamMembers 已在 T671 对齐真实后端契约）。
 */

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.teams)
})

test.describe('T671-A 添加成员下拉显示真实职称', () => {
  test('候选标签带 doctors.title 真实职称，不再出现硬编码「（医生）」', async ({ page }) => {
    // 康复组（TEAM-102）成员为王康复师；无团医生刘医生（主治医师）必在候选里
    await tableRows(page).filter({ hasText: '康复组' }).getByRole('button', { name: '成员' }).click()
    const memberPanel = page.locator('.member-panel').filter({ hasText: '成员管理' })
    await expect(memberPanel).toBeVisible()

    await memberPanel.getByRole('button', { name: '添加成员' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '添加成员' })
    await expect(dialog).toBeVisible()
    await dialog.locator('.el-select').first().click()

    const dropdown = page.locator('.el-select-dropdown:visible')
    // 真实职称后缀（mock doctors 的 title）
    await expect(dropdown.getByText('刘医生（主治医师）', { exact: true })).toBeVisible()
    await expect(dropdown.getByText('王护士（护士）', { exact: true })).toBeVisible()
    // 旧假后缀一枚都不许再有
    await expect(dropdown.locator('.el-select-dropdown__item', { hasText: '（医生）' })).toHaveCount(0)
  })
})

test.describe('T671-B 建团后负责人自动入成员表', () => {
  test('新建团队选刘医生为负责人，成员面板立即含负责人且候选下拉不再列出他', async ({ page }) => {
    const teamName = `T671验证组-${Date.now().toString().slice(-6)}`

    await page.locator('.page-toolbar').getByRole('button', { name: '新建团队' }).click()
    const dialog = page.locator('.el-dialog').filter({ hasText: '新建团队' })
    await expect(dialog).toBeVisible()
    await dialog.locator('input[placeholder*="团队名称"]').fill(teamName)
    // 负责人：无团医生刘医生（DOC-104）
    await pickSelectOption(page, dialog.locator('.el-select').first(), '刘医生')
    await dialog.getByRole('button', { name: '保存' }).click()

    await expect(adminMessage(page)).toContainText('成功')
    await expect(dialog).toBeHidden()
    const newRow = tableRows(page).filter({ hasText: teamName })
    await expect(newRow).toHaveCount(1)

    // 进成员管理：负责人必须已经在成员表里（修复前此处为空）
    await newRow.getByRole('button', { name: '成员' }).click()
    const memberPanel = page.locator('.member-panel').filter({ hasText: '成员管理' })
    await expect(memberPanel).toBeVisible()
    await expect(tableRows(page, memberPanel).filter({ hasText: '刘医生' })).toHaveCount(1)

    // 候选人按 memberId 去重 ⇒ 负责人不再出现在「添加成员」候选里
    await memberPanel.getByRole('button', { name: '添加成员' }).click()
    const addDialog = page.locator('.el-dialog').filter({ hasText: '添加成员' })
    await addDialog.locator('.el-select').first().click()
    await expect(
      page.locator('.el-select-dropdown:visible').getByText('刘医生（主治医师）', { exact: true }),
    ).toHaveCount(0)
  })
})
