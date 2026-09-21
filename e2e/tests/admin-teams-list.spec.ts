import { test, expect, type Locator, type Page } from '@playwright/test'
import { adminLogin, gotoMenu, tableRows } from '../admin-helpers'

/**
 * admin-web 团队管理 · 列表只读渲染：T270 补齐 README §5 第一类缺口 A-FLOW-14
 *
 * 需求原文 = T267 docs/tests/acceptance/admin/核心流程.md A-FLOW-14
 * （设计稿 团队管理.html:97-99 八列表头 + :83 新建团队按钮）。
 *
 * 既有 admin-team-writes.spec.ts 只覆盖**写**动作，列表渲染本身无断言。
 * 本文件断言的是**列结构与逐格格式契约**：行数据从页面当场读，不写死
 * 「4 行 / TEAM01 脊柱矫形一组」这类 2026-09-20 快照值（那属于点击 Agent
 * 在 staging 的判据），换一批团队依然成立。
 *
 * 未落地项（A-FLOW-14 明确「不判不通过」，故不写断言，只在此登记）：
 *  - 设计稿 :89-92 的四张统计卡（`5.1` 团队统计前端未交）
 *  - 「负责人」「创建时间」两列真实后端不返回 → 整列 `-`（本文件只要求它们
 *    不出现 undefined/NaN，允许 `-`）
 *
 * 🔴 「删除」按钮只验**存在**，绝不点击（走查机制 §5 禁删除类操作）。
 */

function headers(page: Page): Promise<string[]> {
  return page.locator('.el-table__header-wrapper thead th').evaluateAll((ths) => ths.map((th) => (th.textContent ?? '').trim()))
}

async function rowCells(row: Locator): Promise<string[]> {
  return row.evaluate((el) => Array.from(el.querySelectorAll('td')).map((td) => (td.textContent ?? '').trim()))
}

test.describe('团队管理 · 列表渲染（T270 A-FLOW-14）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await gotoMenu(page, '团队管理') // 走菜单进页，模拟真实路径
    await expect(page).toHaveURL(/\/teams/)
    await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
  })

  test('顶栏标题与工具条', async ({ page }) => {
    // T289 G3：顶栏标题带设计稿 emoji 前缀（👥 团队管理），改用 contains 判定文案本体
    await expect(page.locator('.top-nav-title')).toContainText('团队管理')
    await expect(page.locator('.page-toolbar').getByRole('button', { name: '新建团队' })).toBeVisible()
  })

  test('表头八列（顺序即契约）', async ({ page }) => {
    expect(await headers(page)).toEqual(['团队编号', '团队名称', '负责人', '成员数', '管理患者数', '创建时间', '状态', '操作'])
  })

  test('逐行格式：编号/名称非空、计数为整数、状态文案与 tag 颜色一致、操作三按钮', async ({ page }) => {
    const rows = tableRows(page)
    const count = await rows.count()
    expect(count, '列表须有数据行').toBeGreaterThan(0)

    for (let i = 0; i < count; i++) {
      const row = rows.nth(i)
      const cells = await rowCells(row)
      expect(cells, `第 ${i + 1} 行须有 8 列`).toHaveLength(8)
      const [teamId, name, leader, memberCount, patientCount, createdAt, status] = cells
      const where = `第 ${i + 1} 行（${teamId || '(空)'}）`

      expect(teamId, `${where} 团队编号非空`).not.toBe('')
      expect(name, `${where} 团队名称非空`).not.toBe('')
      expect(memberCount, `${where} 成员数须为整数`).toMatch(/^\d+$/)
      expect(patientCount, `${where} 管理患者数须为整数`).toMatch(/^\d+$/)
      // 未落地列允许 '-'，但不许漏 undefined/NaN
      for (const [label, value] of [['负责人', leader], ['创建时间', createdAt]] as const) {
        expect(value, `${where} ${label} 不得是 undefined/NaN`).not.toMatch(/undefined|NaN/)
      }
      expect(['活跃', '已删除'], `${where} 状态文案`).toContain(status)
      const tagClass = (await row.locator('td').nth(6).locator('.el-tag').getAttribute('class')) ?? ''
      if (status === '活跃') expect(tagClass, '活跃须绿标').toContain('el-tag--success')
      else expect(tagClass, '已删除须灰标').toContain('el-tag--info')

      // 操作列：成员 / 编辑 / 删除 都在（🔴 不点删除）
      const ops = row.locator('td').nth(7)
      for (const btn of ['成员', '编辑', '删除']) {
        await expect(ops.getByRole('button', { name: btn }), `${where} 缺操作按钮「${btn}」`).toBeVisible()
      }
    }
  })
})
