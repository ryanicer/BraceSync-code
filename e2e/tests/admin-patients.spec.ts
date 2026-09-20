import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * admin-web 患者管理：列表 / 关键词搜索 / 团队筛选 / 分页 / 详情抽屉
 * mock 数据对齐 mock/patients.ts：6 名患者（PT-001~PT-006，TEAM-001 有 2 名）
 */

test.beforeEach(async ({ page }) => {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.patients)
})

test.describe('列表渲染', () => {
  test('渲染 6 名患者且列信息完整', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows).toHaveCount(6)
    const first = rows.first()
    await expect(first).toContainText('PT-001')
    await expect(first).toContainText('林小雨')
    await expect(first).toContainText('28°')
    await expect(first).toContainText('脊柱侧弯一组')
    await expect(first).toContainText('张建国')
    await expect(first).toContainText('DEV-A3F312')
    await expect(first).toContainText('活跃')
  })

  test('未绑定设备患者显示未绑定与待分配', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: '赵欣然' })
    await expect(row).toContainText('未绑定')
    await expect(row).toContainText('待分配')
  })

  test('分页组件显示共 6 条', async ({ page }) => {
    await expect(page.locator('.el-pagination')).toContainText('共 6 条')
  })
})

test.describe('搜索与筛选', () => {
  test('按姓名搜索：林 → 仅林小雨', async ({ page }) => {
    await page.locator('.search-input input').fill('林')
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).click()
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('林小雨')
  })

  test('按患者ID搜索：PT-002 → 陈子航', async ({ page }) => {
    await page.locator('.search-input input').fill('PT-002')
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).click()
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('陈子航')
  })

  test('回车触发搜索', async ({ page }) => {
    await page.locator('.search-input input').fill('王梓萌')
    await page.locator('.search-input input').press('Enter')
    await expect(tableRows(page)).toHaveCount(1)
  })

  test('按团队筛选：脊柱侧弯一组 → 2 名', async ({ page }) => {
    await pickSelectOption(page, page.locator('.team-select'), '脊柱侧弯一组')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(2)
    await expect(rows.nth(0)).toContainText('林小雨')
    await expect(rows.nth(1)).toContainText('陈子航')
  })

  test('清空搜索恢复 6 名', async ({ page }) => {
    await page.locator('.search-input input').fill('林')
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).click()
    await expect(tableRows(page)).toHaveCount(1)
    await page.locator('.search-input input').clear()
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).click()
    await expect(tableRows(page)).toHaveCount(6)
  })
})

test.describe('详情抽屉', () => {
  test('点击行打开详情抽屉', async ({ page }) => {
    await tableRows(page).filter({ hasText: '林小雨' }).click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible()
    await expect(drawer).toContainText('林小雨（PT-001）')
    await expect(drawer).toContainText('青少年特发性脊柱侧弯')
    await expect(drawer).toContainText('28°')
    await expect(drawer).toContainText('脊柱侧弯一组')
    await expect(drawer).toContainText('建档时间')
  })

  /**
   * T270 A-FLOW-10（核心流程.md）：把「抽屉能打开」升级成抽屉的**字段契约**。
   * 逐格值一律与「所点那一行」的列交叉核对（行↔详情同源），不写死 mock 里的具体患者，
   * 所以换一批数据依然成立，也不是「把现状当期望」。
   *
   * ⚠️ 受 D1 影响的只有「所属团队 / 主治医生」两格的**取值**（真实模式下
   * api/index.ts 的 teamNameOf/doctorNameOf 回落 mock 表 ⇒ 显示 TEAM01/D0001），
   * 那两格的**值级**判据随 T269 一起改，本条只断两侧同源一致 —— 抓不住 D1 本身。
   */
  test('A-FLOW-10 抽屉字段契约：标题=所点行 + 8 项逐格对齐列表 + 底部动作 + 关闭后列表不变', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows.first()).toBeVisible({ timeout: 15_000 })
    const rowCount = await rows.count()
    expect(rowCount, '列表需至少 2 行才能取非首行验证').toBeGreaterThan(1)

    // 取第 2 行（避开上一条用例钉死的首行）
    const target = rows.nth(1)
    const cells = await target.evaluate((el) => Array.from(el.querySelectorAll('td')).map((td) => (td.textContent ?? '').trim()))
    const [, patientId, name, gender, age, diagnosis, cobb, team, doctor, device] = cells

    await target.locator('td').nth(2).click() // 点姓名单元格，避开首列复选框
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible()
    await expect(drawer.locator('.el-drawer__title')).toHaveText(`${name}（${patientId}）`)

    const fields = await drawer.evaluate((el) =>
      Array.from(el.querySelectorAll('.el-descriptions__label')).map((th) => ({
        label: (th.textContent ?? '').trim(),
        value: (th.nextElementSibling?.textContent ?? '').trim(),
      })),
    )
    expect(fields.map((f) => f.label), '抽屉 8 项字段名与顺序').toEqual([
      '性别', '年龄', '诊断', 'Cobb角', '所属团队', '主治医生', '绑定设备', '建档时间',
    ])
    const shown = Object.fromEntries(fields.map((f) => [f.label, f.value]))
    expect(shown['性别'], '性别须中文化（male→男）').toBe(gender)
    expect(shown['年龄']).toBe(age)
    expect(shown['诊断']).toBe(diagnosis)
    expect(shown['Cobb角']).toBe(cobb)
    expect(shown['所属团队']).toBe(team)
    expect(shown['主治医生']).toBe(doctor)
    expect(shown['绑定设备'], '无设备显示「未绑定」').toBe(device)
    expect(shown['建档时间'], '建档时间渲染成日期，不是原始 ISO / undefined').toMatch(/^\d{4}-\d{2}-\d{2}$|^—$|^－$|^-$|^\/$/)
    for (const f of fields) expect(f.value, `字段「${f.label}」不得是 undefined/NaN`).not.toMatch(/undefined|NaN|\[object/)

    await expect(drawer.getByRole('button', { name: '分配团队' })).toBeVisible()

    await drawer.locator('.el-drawer__close-btn').click()
    await expect(drawer).toBeHidden()
    await expect(tableRows(page)).toHaveCount(rowCount) // 关抽屉不改变列表（未触发筛选/翻页）
  })

  test('抽屉可关闭', async ({ page }) => {
    await tableRows(page).filter({ hasText: '陈子航' }).click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible()
    await drawer.locator('.el-drawer__close-btn').click()
    await expect(drawer).toBeHidden()
  })
})
