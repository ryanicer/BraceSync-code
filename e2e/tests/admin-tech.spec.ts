import { test, expect, type Locator, type Page } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, tableRows } from '../admin-helpers'

/**
 * admin-web 技师管理：T270 补齐 README §5 第一类缺口 A-TECH-02 / 03 / 04 / 05 / 07 / 08
 *
 * 需求原文＝ T267 docs/tests/acceptance/admin/技师管理.md 对应条目；本文件把其中
 * 「对应 E2E：无」的六条落成断言。用例定义里的**真实数据值**（staging 4 个团队、
 * 3 名技师、TEAM01 命名空间）不进断言——那是点击 Agent 在 staging 的判据；
 * 这里断言的是**契约与联动关系**（总数↔行数、下拉↔列表页、校验↔不落库），
 * 换一批数据依然成立，也不会「把现状当期望」。
 *
 * 每条用例都是独立 browser context（Playwright 默认），mock 的模块级数组写在页面
 * JS 上下文里 ⇒ 用例之间不共享技师数据；但新增/改名仍带唯一标记名，避免与基线数据
 * （周/吴/郑/冯 4 人）撞名，并在用例内自行回滚断言口径。
 * 列取值统一按 td 下标读（勿用 toContainText(/\\t…\\t/) —— 文本断言会折叠空白）。
 */

const MARK = (testId: string) => `E2E-T270-${testId}`

/** 一行按下标取单元格文本（姓名 0 / 手机号 1 / 所属团队 2 / 安装次数 3 / 认证 4 / 状态 5 / 创建时间 6） */
function cellTexts(row: Locator): Promise<string[]> {
  return row.evaluate((el) => Array.from(el.querySelectorAll('td')).map((td) => (td.textContent ?? '').trim()))
}

/** 分页条「共 N 条」的数字 */
async function paginationTotal(page: Page): Promise<number> {
  const text = await page.locator('.pagination').innerText()
  const n = Number(text.match(/共\s*(\d+)\s*条/)?.[1])
  expect(Number.isNaN(n), `分页条未显示「共 N 条」：${text}`).toBe(false)
  return n
}

/** 按姓名精确取某一行（等列表刷新到恰好 1 行） */
function rowByName(page: Page, name: string) {
  return tableRows(page).filter({ hasText: name })
}

/** 打开弹窗内「所属团队」下拉，等选项真正渲染完（teleport + 异步数据，click 后立刻读会拿到 0 项） */
async function openTeamOptions(page: Page, dialog: Locator): Promise<Locator> {
  await dialog.locator('.el-select').click()
  const items = page.locator('.el-select-dropdown:visible .el-select-dropdown__item')
  await expect(items.first()).toBeVisible({ timeout: 5_000 })
  return items
}

test.describe('技师管理（T270 A-TECH-02/03/04/05/07/08）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.technicians)
    await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
  })

  test('A-TECH-02 搜索框按姓名过滤：命中唯一行 → 清空恢复 → 无命中走空态', async ({ page }) => {
    const baseline = await tableRows(page).count()
    expect(baseline).toBeGreaterThan(1) // 前置自检：列表非空且不止一行，否则过滤无意义

    const search = page.locator('.search-input input')
    await search.fill('周师傅')
    await search.press('Enter') // 实现的过滤挂在回车/清空上，不是边打字边过滤
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('周师傅')
    // 非匹配行不得残留
    await expect(rows).not.toContainText('吴师傅')

    // 清空（@clear 触发重新加载）→ 恢复原行数
    await page.locator('.search-input .el-input__clear').click()
    await expect(tableRows(page)).toHaveCount(baseline)

    // 无命中关键词 → 空态，且不弹错误
    await search.fill('查无此人XYZ')
    await search.press('Enter')
    await expect(tableRows(page)).toHaveCount(0)
    await expect(page.locator('.el-table__empty-text')).toContainText('暂无数据')
    await expect(page.locator('.el-message--error')).toHaveCount(0)
  })

  test('A-TECH-04 新建表单校验：四轮警告文案 + 弹窗不关 + 不落库 + 手机号限 11 位', async ({ page }) => {
    const before = await tableRows(page).count()
    await page.getByRole('button', { name: '新建技师' }).click()
    const dialog = page.locator('.el-dialog:visible')
    await expect(dialog).toContainText('新建技师')
    // 字段标签与底部按钮文案（设计稿 技师管理.html:112-142 的落地口径）
    await expect(dialog.locator('.el-form-item__label')).toHaveText(['姓名', '手机号', '所属团队'])
    await expect(dialog.locator('.el-form-item.is-required')).toHaveCount(3)
    const name = dialog.locator('input[placeholder="技师姓名"]')
    const phone = dialog.locator('input[placeholder="11 位手机号"]')
    expect(await phone.getAttribute('maxlength')).toBe('11')

    // 手机号输入框最多留住 11 位
    await phone.fill('138000002671234')
    await expect(phone).toHaveValue('13800000267')

    const submit = dialog.getByRole('button', { name: '确认创建' })
    // 1) 全空 → 请填写姓名
    await phone.fill('')
    await submit.click()
    await expect(adminMessage(page)).toContainText('请填写姓名')
    // 2) 只有姓名 → 手机号校验
    await name.fill('校验测试')
    await submit.click()
    await expect(adminMessage(page)).toContainText('请填写正确的 11 位手机号')
    // 3) 姓名 + 非法手机号 → 仍是手机号校验
    await phone.fill('123')
    await expect(phone).toHaveValue('123')
    await submit.click()
    await expect(adminMessage(page)).toContainText('请填写正确的 11 位手机号')
    // 4) 姓名 + 合法手机号、团队未选 → 团队校验
    await phone.fill('13800000267')
    await submit.click()
    await expect(adminMessage(page)).toContainText('请选择所属团队')

    // 四轮均被前端拦下：弹窗仍在、列表行数不变、无错误提示
    await expect(dialog).toBeVisible()
    await expect(rowByName(page, '校验测试')).toHaveCount(0)
    await expect(tableRows(page)).toHaveCount(before)
    await expect(page.locator('.el-message--error')).toHaveCount(0)

    await dialog.getByRole('button', { name: '取消' }).click()
    await expect(page.locator('.el-dialog:visible')).toHaveCount(0)
  })

  test('A-TECH-03 新建技师：成功提示 + 弹窗关闭 + 列表刷新含新行 + 总数 +1，随后禁用新增行', async ({ page }) => {
    const marker = MARK(test.info()!.testId.replace(/[^0-9a-zA-Z]/g, '').slice(-8))
    const totalBefore = await paginationTotal(page)
    const rowsBefore = await tableRows(page).count()

    await page.getByRole('button', { name: '新建技师' }).click()
    const dialog = page.locator('.el-dialog:visible')
    await dialog.locator('input[placeholder="技师姓名"]').fill(marker)
    await dialog.locator('input[placeholder="11 位手机号"]').fill('13800000270')
    // 团队取下拉首项（不写死 mock 团队名，避免「把现状当期望」）
    const items = await openTeamOptions(page, dialog)
    const chosenTeam = (await items.allInnerTexts()).map((s) => s.trim())[0]
    await items.first().click()
    await expect(dialog.locator('.el-select')).toContainText(chosenTeam)
    await dialog.getByRole('button', { name: '确认创建' }).click()

    await expect(adminMessage(page)).toContainText('创建成功')
    await expect(page.locator('.el-dialog:visible')).toHaveCount(0)

    // 列表自动刷新：新行在位，且各列按新建语义渲染
    const row = rowByName(page, marker)
    await expect(row).toHaveCount(1)
    const cells = await cellTexts(row)
    expect(cells.slice(0, 4), '姓名/脱敏手机号/团队中文名/安装次数').toEqual([marker, '138****0270', chosenTeam, '0'])
    expect(cells[4], '新建技师默认未认证').toBe('未认证')
    expect(cells[5], '新建技师默认启用').toBe('启用')
    expect(cells[6], '创建时间列取日期（非原始 ISO）').toMatch(/^\d{4}-\d{2}-\d{2}$|^\-$/)
    // 分页总数 +1，且与表格行数同步
    await expect.poll(() => paginationTotal(page), { timeout: 10_000 }).toBe(totalBefore + 1)
    await expect(tableRows(page)).toHaveCount(rowsBefore + 1)

    // 本条在 T267 里标 [需确认]：真实库没有删除接口、禁用是唯一可回退动作，
    // 这里顺带验一次禁用链路（确认框 → 提示 → 状态列翻面）。
    await row.getByRole('button', { name: '禁用' }).click()
    await page.locator('.el-popconfirm').getByRole('button', { name: '确定' }).click()
    await expect(adminMessage(page)).toContainText('已禁用')
    await expect(row.locator('.el-tag--info')).toContainText('禁用')
  })

  test('A-TECH-05 编辑技师：标题/回填/手机号锁定 → 改名保存 → 强制还原', async ({ page }) => {
    const row = rowByName(page, '周师傅')
    await expect(row).toHaveCount(1)
    const teamBefore = (await cellTexts(row))[2]
    expect(teamBefore, '目标行应有可辨识的所属团队').toBeTruthy()

    await row.getByRole('button', { name: '编辑' }).click()
    const dialog = page.locator('.el-dialog:visible')
    await expect(dialog).toContainText('编辑技师') // 标题不是「新建技师」
    const name = dialog.locator('input[placeholder="技师姓名"]')
    const phone = dialog.locator('input[placeholder="11 位手机号"]')
    await expect(name).toHaveValue('周师傅') // 姓名回填
    await expect(phone).toBeDisabled() // 编辑态手机号锁定（实现口径：改号不在本期）
    await expect(dialog.locator('.el-select')).toContainText(teamBefore) // 团队回填=列表同列值
    await expect(dialog.getByRole('button', { name: '保存修改' })).toBeVisible()
    await expect(dialog.getByRole('button', { name: '确认创建' })).toHaveCount(0)

    // 改名保存。姓名输入框 maxlength=20 ⇒ 标记要短；且新名不能含「周师傅」，
    // 否则 hasText 子串匹配会把改名后的行也算进原名行，「确已改写」那条断言就废了。
    const marker = `改名回滚${test.info().testId.replace(/[^0-9a-zA-Z]/g, '').slice(-6)}`
    expect(marker.length, '标记需容纳在 maxlength=20 内').toBeLessThanOrEqual(20)
    await name.fill(marker)
    await dialog.getByRole('button', { name: '保存修改' }).click()
    await expect(adminMessage(page)).toContainText('修改成功')
    await expect(page.locator('.el-dialog:visible')).toHaveCount(0)
    const renamed = rowByName(page, marker)
    await expect(renamed).toHaveCount(1)
    expect((await cellTexts(renamed))[2], '仅改名，团队保持不变').toBe(teamBefore)
    await expect(rowByName(page, '周师傅')).toHaveCount(0) // 同名行确已被改写，而非新增了一份

    // 🔴 强制还原
    await renamed.getByRole('button', { name: '编辑' }).click()
    await expect(dialog.locator('input[placeholder="技师姓名"]')).toHaveValue(marker)
    await dialog.locator('input[placeholder="技师姓名"]').fill('周师傅')
    await dialog.getByRole('button', { name: '保存修改' }).click()
    await expect(adminMessage(page)).toContainText('修改成功')
    await expect(rowByName(page, marker)).toHaveCount(0)
    await expect(rowByName(page, '周师傅')).toHaveCount(1)
  })

  test('A-TECH-07 团队下拉数据来源：占位文案 + 选项与「团队管理」页名单一致 + 列表列取自同一来源', async ({ page }) => {
    await page.getByRole('button', { name: '新建技师' }).click()
    const dialog = page.locator('.el-dialog:visible')
    // 未选择时显示占位文案
    await expect(dialog.locator('.el-select .el-select__placeholder')).toHaveText('请选择团队')

    const items = await openTeamOptions(page, dialog)
    const dropdownNames = (await items.allInnerTexts()).map((s) => s.trim())
    expect(dropdownNames.length).toBeGreaterThan(0)
    // 不得出现设计稿里写死的示例团队（技师管理.html:163 硬编码三项）
    for (const hardcoded of ['矫形团队A组', '矫形团队B组', '西南区域组']) {
      expect(dropdownNames, '团队下拉不应是设计稿写死名单').not.toContain(hardcoded)
    }
    await dialog.locator('.el-select').click() // 再点一次收起下拉（勿用 Escape：会连带关掉弹窗）
    await dialog.getByRole('button', { name: '取消' }).click()

    // 与团队管理页列表同源：两侧团队名集合必须一致
    await page.goto(adminRoutes.teams)
    const teamRows = tableRows(page)
    await expect(teamRows.first()).toBeVisible({ timeout: 15_000 })
    const pageNames = await teamRows.evaluateAll((rows) =>
      rows.map((r) => (r as HTMLElement).querySelectorAll('td')[1]?.textContent?.trim() ?? ''),
    )
    expect(pageNames.length, '团队管理页应至少有一个团队').toBeGreaterThan(0)
    expect([...dropdownNames].sort()).toEqual([...pageNames.filter(Boolean)].sort())

    // 技师列表「所属团队」列的取值必须落在同一份团队名单里（D1 类展示错配的守卫）
    await page.goto(adminRoutes.technicians)
    await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
    const columnNames = await tableRows(page).evaluateAll((rows) =>
      rows.map((r) => (r as HTMLElement).querySelectorAll('td')[2]?.textContent?.trim() ?? ''),
    )
    expect(columnNames.length).toBeGreaterThan(0)
    for (const shown of columnNames) {
      expect(pageNames, `技师列显示的「${shown}」不在团队名单内`).toContain(shown)
    }
  })

  test('A-TECH-08 分页与总数：总数与行数一致 + 控件齐全 + 不足一页时下一页禁用', async ({ page }) => {
    const total = await paginationTotal(page)
    const rows = await tableRows(page).count()
    expect(rows, '单页时行数须等于总数').toBe(total)
    expect(total).toBeGreaterThan(0)

    // 控件齐全：总数文案 + 上一页 + 页码 + 下一页
    const pager = page.locator('.pagination')
    await expect(pager.locator('.el-pagination__total')).toBeVisible()
    await expect(pager.locator('button.btn-prev')).toBeVisible()
    await expect(pager.locator('.el-pager li').first()).toBeVisible()
    await expect(pager.locator('button.btn-next')).toBeVisible()
    // 数据不足一页 → 上一页/下一页均禁用，页码只有 1
    await expect(pager.locator('button.btn-next')).toBeDisabled()
    await expect(pager.locator('button.btn-prev')).toBeDisabled()
    await expect(pager.locator('.el-pager li')).toHaveCount(1)
    await expect(pager.locator('.el-pager li.is-active')).toHaveText('1')

    // 搜索过滤后：表格只剩命中行，但总数仍回显后端总量 —— 记录该口径（前端当前页过滤）
    const search = page.locator('.search-input input')
    await search.fill('周师傅')
    await search.press('Enter')
    await expect(tableRows(page)).toHaveCount(1)
    await expect(pager.locator('.el-pagination__total')).toHaveText(`共 ${total} 条`)
  })
})
