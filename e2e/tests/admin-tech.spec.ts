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

/** 一行按下标取单元格文本（姓名 0 / 手机号 1 / 所属团队 2 / 归属类型 3 / 安装次数 4 / 认证 5 / 状态 6 / 创建时间 7）
 *  归属类型 = T627 方案乙新列，插在「所属团队」之后 ⇒ 其后各列下标整体 +1 */
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

  test('A-TECH-03 新建技师：一次性凭据弹窗 + 关窗后列表刷新含新行 + 总数 +1，随后禁用新增行', async ({ page }) => {
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

    // T480：建号即发口令 ⇒ 成功反馈是一次性凭据弹窗，不再是一句无用的「创建成功」toast。
    // 展示的是「技师编号 + 登录手机号 + 初始密码」：技师端登录走手机号，只给编号登不进去。
    const cred = page.locator('.el-message-box:visible').last()
    await expect(cred).toContainText('创建成功')
    await expect(cred.locator('p').nth(0)).toHaveText(/^技师编号：TECH-\d+$/)
    await expect(cred.locator('p').nth(1)).toHaveText('登录手机号：13800000270')
    const pwdLine = await cred.locator('p').nth(2).innerText()
    expect(pwdLine, '口令行必须是「初始密码：<非空明文>」').toMatch(/^初始密码：\S+$/)
    const shownPwd = pwdLine.replace(/^初始密码：/, '')
    expect(shownPwd.length, `一次性口令不该是空串：${pwdLine}`).toBeGreaterThan(6)
    // T486：第四段必须是「怎么登录」——技师端登录页只收手机号＋密码，弹窗里给了编号却不说明
    // 它不能用于登录，管理员就会拿编号去试。段落数一并钉死：谁往中间插句子这里先红。
    await expect(cred.locator('p')).toHaveCount(5)
    await expect(cred.locator('p').nth(3)).toHaveText('技师使用手机号 + 密码登录（技师编号不能用于登录）。')
    // 关窗提示要在场：遗失走重置，不给二次查看入口
    await expect(cred).toContainText('仅此一次展示')
    await cred.getByRole('button', { name: '我已转交本人' }).click()
    await expect(page.locator('.el-message-box:visible')).toHaveCount(0)

    // 口令只活在这一次弹窗里：列表与弹窗都不该再出现它
    await expect(dialog).toHaveCount(0)
    await expect(page.locator('.technicians')).not.toContainText(shownPwd)

    // 列表自动刷新：新行在位，且各列按新建语义渲染
    const row = rowByName(page, marker)
    await expect(row).toHaveCount(1)
    const cells = await cellTexts(row)
    // 归属类型一格读作「维护班组」就是 T627 R1 的端到面：下拉只出维护班组 ⇒ 新建行的侧别不可能是医护
    expect(cells.slice(0, 5), '姓名/脱敏手机号/团队中文名/归属类型/安装次数').toEqual([marker, '138****0270', chosenTeam, '维护班组', '0'])
    expect(cells[5], '新建技师默认未认证').toBe('未认证')
    expect(cells[6], '新建技师默认启用').toBe('启用')
    expect(cells[7], '创建时间列取日期（非原始 ISO）').toMatch(/^\d{4}-\d{2}-\d{2}$|^\-$/)
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

  test('A-TECH-09 重置密码：二次确认才发号，一次性展示新口令', async ({ page }) => {
    // T480：口令遗失的唯一出口。取消必须先于写发生 ⇒ 取消那一腿不能改到任何东西。
    const row = rowByName(page, '周师傅')
    await expect(row).toHaveCount(1)

    await row.getByRole('button', { name: '重置密码' }).click()
    const boxes = page.locator('.el-message-box:visible')
    // 取消那一腿只有一张确认框：出现第二张 = 没等确认就发了号
    await expect(boxes).toHaveCount(1)
    const confirm = boxes.last()
    await expect(confirm).toContainText('确认重置密码')
    await expect(confirm).toContainText('旧密码即时失效')
    await confirm.getByRole('button', { name: '取消' }).click()
    await expect(boxes).toHaveCount(0)

    await row.getByRole('button', { name: '重置密码' }).click()
    // .last()：确认框的退场动画未结束时它会与新弹窗同时 :visible，取最后一张才是弹窗本身
    const cred = boxes.last()
    await boxes.getByRole('button', { name: '确认' }).last().click()

    await expect(cred).toContainText('重置成功')
    await expect(cred.locator('p').nth(0)).toHaveText(/^技师编号：TECH-\d+$/)
    // 重置态拿不到明文号码（读侧即已脱敏，T361）⇒ 展示脱敏号 + 一句「登录号＝建档手机号」
    await expect(cred.locator('p').nth(1)).toContainText('登录手机号：138****5678')
    await expect(cred.locator('p').nth(2)).toHaveText(/^初始密码：\S+$/)
    // T486：重置态与创建态共用同一个弹窗 ⇒ 登录方式说明同样在场（段数与创建态一致）
    await expect(cred.locator('p')).toHaveCount(5)
    await expect(cred.locator('p').nth(3)).toHaveText('技师使用手机号 + 密码登录（技师编号不能用于登录）。')
    await cred.getByRole('button', { name: '我已转交本人' }).click()
    await expect(boxes).toHaveCount(0)

    // 重置不动档案：姓名仍在、状态未翻面
    await expect(row).toHaveCount(1)
    expect((await cellTexts(row))[6], '重置口令不得改动启停状态').toBe('启用')
  })

  test('A-TECH-05 编辑技师：标题/回填/手机号可编辑 → 留空不改号 → 改号落库 → 强制还原', async ({ page }) => {
    const row = rowByName(page, '周师傅')
    await expect(row).toHaveCount(1)
    const teamBefore = (await cellTexts(row))[2]
    expect(teamBefore, '目标行应有可辨识的所属团队').toBeTruthy()
    const phoneBefore = (await cellTexts(row))[1]
    expect(phoneBefore, '基线行的手机号列应为脱敏形态').toBe('138****5678')

    await row.getByRole('button', { name: '编辑' }).click()
    const dialog = page.locator('.el-dialog:visible')
    await expect(dialog).toContainText('编辑技师') // 标题不是「新建技师」
    const name = dialog.locator('input[placeholder="技师姓名"]')
    // T624：编辑态手机号放开为可见可编辑 ⇒ 按表单项 label 定位（占位文案随 phoneState 变，不当选择器用）
    const phone = dialog.locator('.el-form-item').filter({ hasText: '手机号' }).locator('input')
    await expect(name).toHaveValue('周师傅') // 姓名回填
    await expect(phone).toBeEnabled() // T624：不再禁用
    await expect(phone).toHaveValue('') // T361：脱敏串不是原值，编辑框永不预填
    await expect(phone).toHaveAttribute('placeholder', '已绑定手机号，留空即不修改')
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
    // T361 的另一半：输入框留空时该键根本不下发，库内号码不能被动
    expect((await cellTexts(renamed))[1], '手机号留空＝不改号，列表列仍是原脱敏值').toBe(phoneBefore)
    await expect(rowByName(page, '周师傅')).toHaveCount(0) // 同名行确已被改写，而非新增了一份

    // 填非法号 → 前端拦下、弹窗不关、列表没被改写
    await renamed.getByRole('button', { name: '编辑' }).click()
    await phone.fill('123')
    await dialog.getByRole('button', { name: '保存修改' }).click()
    await expect(adminMessage(page)).toContainText('手机号需为 11 位号码，或留空')
    await expect(dialog).toBeVisible()
    expect((await cellTexts(rowByName(page, marker)))[1], '非法号被拦下后号码不变').toBe(phoneBefore)

    // 填 11 位新号 → 保存后列表那一列翻新（确认真实落库，不是前端假改）。
    // submitForm 里 loadData() 未 await ⇒ 这里必须用轮询断言等刷新落地，不能同步读一次单元格。
    await phone.fill('13800002468')
    await dialog.getByRole('button', { name: '保存修改' }).click()
    await expect(page.locator('.el-dialog:visible')).toHaveCount(0)
    const edited = rowByName(page, marker)
    await expect(edited.locator('td').nth(1), '改号后列表按脱敏形态展示新号').toHaveText('138****2468')
    expect((await cellTexts(edited))[2], '改号不动团队列').toBe(teamBefore)

    // 🔴 强制还原：姓名与手机号都回到基线（改号会动技师的登录账号，不留痕）
    await edited.getByRole('button', { name: '编辑' }).click()
    await expect(dialog.locator('input[placeholder="技师姓名"]')).toHaveValue(marker)
    await dialog.locator('input[placeholder="技师姓名"]').fill('周师傅')
    await phone.fill('13800005678')
    await dialog.getByRole('button', { name: '保存修改' }).click()
    await expect(adminMessage(page)).toContainText('修改成功')
    await expect(rowByName(page, marker)).toHaveCount(0)
    const restored = rowByName(page, '周师傅')
    await expect(restored).toHaveCount(1)
    await expect(restored.locator('td').nth(1), '还原后手机号列回到基线脱敏值').toHaveText(phoneBefore)
  })

  test('A-TECH-07 团队下拉数据来源：占位文案 + 选项与「团队管理」页的维护班组一支一致 + 列表列取自同一来源', async ({ page }) => {
    await page.getByRole('button', { name: '新建技师' }).click()
    const dialog = page.locator('.el-dialog:visible')
    // T627 方案乙 R1：占位文案跟着筛后的名单走（下拉里已经没有医护团队，文案再写「请选择团队」就是假面）
    await expect(dialog.locator('.el-select .el-select__placeholder')).toHaveText('请选择维护班组')

    const items = await openTeamOptions(page, dialog)
    const dropdownNames = (await items.allInnerTexts()).map((s) => s.trim())
    expect(dropdownNames.length).toBeGreaterThan(0)
    // 不得出现设计稿里写死的示例团队（技师管理.html:163 硬编码三项）
    for (const hardcoded of ['矫形团队A组', '矫形团队B组', '西南区域组']) {
      expect(dropdownNames, '团队下拉不应是设计稿写死名单').not.toContain(hardcoded)
    }
    await dialog.locator('.el-select').click() // 再点一次收起下拉（勿用 Escape：会连带关掉弹窗）
    await dialog.getByRole('button', { name: '取消' }).click()

    // 与团队管理页同源，但只同源到「维护班组」那一支：两侧的名字+类型逐行取回，
    // 下拉集合 == 类型列为「维护班组」的那一组名字（旧口径「与全量名单一致」已被 T627 R1 取代）
    await page.goto(adminRoutes.teams)
    const teamRows = tableRows(page)
    await expect(teamRows.first()).toBeVisible({ timeout: 15_000 })
    const roster = await teamRows.evaluateAll((rows) =>
      rows.map((r) => {
        const td = (r as HTMLElement).querySelectorAll('td')
        return { name: td[1]?.textContent?.trim() ?? '', type: td[2]?.textContent?.trim() ?? '' }
      }),
    )
    expect(roster.length, '团队管理页应至少有一个团队').toBeGreaterThan(0)
    const pageNames = roster.map((r) => r.name).filter(Boolean)
    const maintNames = roster.filter((r) => r.type === '维护班组').map((r) => r.name)
    expect(maintNames.length, '维护班组一支须非空，否则这一格退化成「下拉恒空」的假绿').toBeGreaterThan(0)
    expect([...dropdownNames].sort(), '技师归属下拉与团队页的维护班组一支不同名').toEqual([...maintNames].sort())
    // 医护团队不得出现在技师下拉里（判据 3 的页面面：不是排在后面，是不在面板里）
    for (const r of roster.filter((x) => x.type === '医护团队')) {
      expect(dropdownNames, `医护团队「${r.name}」不应进技师归属下拉`).not.toContain(r.name)
    }

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
