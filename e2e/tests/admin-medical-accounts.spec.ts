import { test, expect, type Locator, type Page } from '@playwright/test'
import { adminLogin, adminMessage, adminRoutes, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * 医护账号页 · 职称下拉词表（T360）
 *
 * 依据：设计稿 docs/design/admin/医护账号.html :133（筛选下拉）/ :248（编辑下拉）/
 * :284「预置 4 项、可扩展」，且 :306 与 :308 两处都读同一个 titles 数组 —— 同源是稿面结构。
 *
 * 缺陷实况：实现只把预置 4 项当词表，而库里既有值不止这 4 个（mock 档案 DOC-002 陈小芳 /
 * DOC-005 赵敏 = 副主任医师，与 code 仓 scripts/db/seed/seed.sql:43 的 D0002 同形）
 * ⇒ 那一行筛不到；编辑态职称下拉里选不到原职称，运营只能改选 4 项之一，一次保存就改写真实职称。
 *
 * 本文件三件事：① 词表 = 预置 4 项（顺序不变）∪ 库内既有值；② 两处下拉逐项相等（同源）；
 * ③ 卡面回归「不改职称直接保存 ⇒ 一字未变」。
 */
const PRESETS = ['主任医师', '主治医师', '康复师', '护士']
const OUT_OF_VOCAB = '副主任医师'
const TITLE_UNION = [...PRESETS, OUT_OF_VOCAB]

/** 工具栏两个筛选下拉：[0] 团队、[1] 职称 */
function titleFilterSelect(page: Page): Locator {
  return page.locator('.toolbar .filter-select').nth(1)
}

function editTitleSelect(page: Page): Locator {
  const item = page.locator('.el-dialog .el-form-item')
    .filter({ has: page.locator('.el-form-item__label:text-is("职称")') })
  return item.locator('.el-select')
}

/** 打开下拉读全部选项文本，读完收起（避免面板挡住后续点击） */
async function optionsOf(page: Page, select: Locator): Promise<string[]> {
  await select.click()
  const panel = page.locator('.el-select-dropdown:visible')
  await expect(panel).toHaveCount(1, { timeout: 5_000 })
  const texts = (await panel.locator('.el-select-dropdown__item').allTextContents()).map((t) => t.trim())
  await select.click()
  await expect(page.locator('.el-select-dropdown:visible')).toHaveCount(0, { timeout: 5_000 })
  return texts
}

function titleCell(row: Locator): Locator {
  return row.locator('td').nth(5)
}

test.describe('医护账号页 · 职称词表与两处下拉同源（T360）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.doctorAccounts)
    await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
  })

  test('筛选下拉 = 预置 4 项 + 库内既有职称，选「副主任医师」能筛出对应行', async ({ page }) => {
    const items = await optionsOf(page, titleFilterSelect(page))
    expect(items).toEqual(['全部职称', ...TITLE_UNION])

    await pickSelectOption(page, titleFilterSelect(page), OUT_OF_VOCAB)
    const rows = tableRows(page)
    await expect(rows).toHaveCount(2)
    for (const row of await rows.all()) {
      await expect(titleCell(row)).toHaveText(OUT_OF_VOCAB)
    }
  })

  test('编辑下拉与筛选下拉逐项相等（同源），不改职称直接保存后一字未变', async ({ page }) => {
    expect(await optionsOf(page, titleFilterSelect(page))).toEqual(['全部职称', ...TITLE_UNION])

    const chen = tableRows(page).filter({ hasText: '陈小芳' })
    await expect(chen).toHaveCount(1)
    await expect(titleCell(chen)).toHaveText(OUT_OF_VOCAB)
    await chen.getByRole('button', { name: '编辑' }).click()
    const dlg = page.locator('.el-dialog')
    await expect(dlg).toBeVisible()

    // 打开弹窗即回显原职称，且词表里选得到它 —— 修前这里只有预置 4 项
    await expect(editTitleSelect(page).locator('.el-select__wrapper')).toContainText(OUT_OF_VOCAB)
    expect(await optionsOf(page, editTitleSelect(page))).toEqual(TITLE_UNION)

    await dlg.getByRole('button', { name: '保存修改' }).click()
    await expect(adminMessage(page)).toContainText('修改成功', { timeout: 10_000 })
    await expect(titleCell(tableRows(page).filter({ hasText: '陈小芳' }))).toHaveText(OUT_OF_VOCAB)
  })
})
