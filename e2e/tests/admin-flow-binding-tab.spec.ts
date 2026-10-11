import { expect, test, type Page, type Locator } from '@playwright/test'
import { adminLogin, adminMessage, adminPath, pickSelectOption } from '../admin-helpers'

/**
 * T666：告警↔流程绑定独立 Tab
 *  - 绑定区从「流程配置」Tab 移出，独立成「流程绑定」Tab（仅 admin）
 *  - 切到该 Tab 组件挂载即 fetch；切走销毁、切回重新挂载 ⇒ 新建模板后无需刷新页面就能选到
 *  - 无模板空态给指引文案
 */

const tab = (page: Page, label: string) =>
  page.locator('.alerts-tabs .el-tabs__item', { hasText: label }).first()

async function openAlerts(page: Page) {
  await adminLogin(page, 'admin')
  await page.goto(adminPath('/alerts'))
  await expect(tab(page, '告警列表')).toBeVisible()
}

// ── 设计器最小操作集（与 admin-flow-designer.spec 同口径的精简版：三节点线性链） ──

function palette(page: Page, label: string): Locator {
  return page.locator('.lf-dndpanel .lf-dnd-item', { hasText: label }).first()
}
function canvas(page: Page): Locator {
  return page.locator('.fd-canvas-inner').first()
}
function nodes(page: Page): Locator {
  return page.locator('.lf-graph .lf-design-node')
}

async function dragNode(page: Page, label: string, x: number, y: number) {
  const item = palette(page, label)
  await item.scrollIntoViewIfNeeded()
  const box = await item.boundingBox()
  const area = await canvas(page).boundingBox()
  if (!box || !area) throw new Error('找不到节点面板或画布容器')
  const before = await nodes(page).count()
  await page.mouse.move(box.x + box.width / 2, box.y + 12)
  await page.mouse.down()
  await page.mouse.move(area.x + x, area.y + y, { steps: 12 })
  await page.mouse.up()
  await expect.poll(() => nodes(page).count(), { timeout: 5_000 }).toBe(before + 1)
}

async function anchorPoints(node: Locator) {
  return node.locator('.lf-anchor').evaluateAll((els) => els.map((el) => {
    const b = el.getBoundingClientRect()
    return { x: b.x + b.width / 2, y: b.y + b.height / 2 }
  }))
}

async function dragConnect(page: Page, fromLabel: string, toLabel: string) {
  const from = nodes(page).filter({ hasText: fromLabel }).first()
  const to = nodes(page).filter({ hasText: toLabel }).first()
  const area = await canvas(page).boundingBox()
  if (!area) throw new Error('找不到画布容器')
  await page.mouse.move(area.x + 6, area.y + 6)
  await from.hover()
  const tb = await to.boundingBox()
  const anchors = await anchorPoints(from)
  if (!tb || !anchors.length) throw new Error('锚点或目标节点不可定位')
  const target = { x: tb.x + tb.width / 2, y: tb.y + tb.height / 2 }
  const start = anchors.reduce((best, a) => (Math.hypot(a.x - target.x, a.y - target.y) < Math.hypot(best.x - target.x, best.y - target.y) ? a : best), anchors[0])
  await page.mouse.move(start.x, start.y)
  await page.mouse.down()
  await page.mouse.move((start.x + target.x) / 2, (start.y + target.y) / 2)
  await page.waitForTimeout(60)
  await page.mouse.move(target.x, target.y)
  await page.waitForTimeout(120)
  await page.mouse.up()
  await page.waitForTimeout(120)
}

async function createMinimalTemplate(page: Page, name: string) {
  await page.getByRole('button', { name: '新建流程模板' }).click()
  const box = page.locator('.el-message-box')
  await box.getByRole('textbox').fill(name)
  await box.getByRole('button', { name: '新建' }).click()
  await expect(adminMessage(page)).toContainText('已创建空模板')
  await expect.poll(async () => page.locator('.el-overlay:visible').count()).toBe(0)
  await dragNode(page, '触发节点', 120, 80)
  await dragNode(page, '处理节点', 300, 80)
  await dragNode(page, '归档节点', 120, 240)
  await dragConnect(page, '触发节点', '处理节点')
  await dragConnect(page, '处理节点', '归档节点')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(adminMessage(page)).toContainText('流程模板已保存')
}

async function selectTemplate(page: Page, name: string) {
  await page.getByRole('button', { name: '模板管理' }).click()
  const popper = page.locator('.fd-tpl-popper')
  await expect(popper).toBeVisible()
  await popper.locator('.fd-tpl-item').filter({ hasText: name }).click()
  await expect(page.locator('.fd-topbar-meta')).toContainText(name)
}

async function deleteCurrentTemplate(page: Page) {
  await page.getByRole('button', { name: '删除模板' }).click()
  await page.locator('.el-message-box').getByRole('button', { name: '删除' }).click()
  await expect(adminMessage(page)).toContainText('已删除模板')
}

test.describe('T666 流程绑定独立 Tab', () => {
  test('绑定区移出流程配置，独立 Tab 内可见（空态分支由组件单测覆盖，mock 基线预置模板不可删）', async ({ page }) => {
    await openAlerts(page)
    // 流程配置 Tab 里不再有绑定区
    await tab(page, '流程配置').click()
    await expect(page.locator('.ftb-card')).toHaveCount(0)
    // 独立 Tab 存在且可进
    await tab(page, '流程绑定').click()
    const card = page.locator('.ftb-card')
    await expect(card).toBeVisible()
    await expect(card).toContainText('告警类型 ↔ 流程模板绑定')
    // 四类告警行齐（后端固定四行）
    await expect(card.locator('.ftb-row')).toHaveCount(4)
  })

  test('切 Tab 重拉：新建模板后切到绑定 Tab 即可选到，无需刷新页面（Boss 痛点闭环）', async ({ page }) => {
    await openAlerts(page)
    const name = `T666 绑定 Tab ${Date.now()}`

    // 首次进绑定 Tab：四行绑定配置在（mock 基线自带预置模板，空态由组件单测覆盖）
    await tab(page, '流程绑定').click()
    await expect(page.locator('.ftb-card .ftb-row')).toHaveCount(4)

    // 去流程配置建一张三节点链模板并保存
    await tab(page, '流程配置').click()
    await createMinimalTemplate(page, name)

    // 切回绑定 Tab：v-if 让组件先销毁后重建（卡片先消失再出现）⇒ onMounted 重跑、列表重新拉取，
    // 新建模板无需刷新页面就出现在下拉里（Boss 原痛点：新建模板必须刷新后才出现）
    await tab(page, '流程绑定').click()
    await expect(page.locator('.ftb-card')).toBeVisible()
    const firstSelect = page.locator('.ftb-row .ftb-select .el-select').first()
    await firstSelect.click()
    await expect(page.locator('.el-select-dropdown:visible .el-select-dropdown__item', { hasText: name })).toBeVisible()
    await page.keyboard.press('Escape')

    // 再切走切回：卡片再次经历销毁→重建（防回退成常驻缓存单例）
    await tab(page, '告警列表').click()
    await expect(page.locator('.ftb-card')).toHaveCount(0)
    await tab(page, '流程绑定').click()
    await expect(page.locator('.ftb-card .ftb-row')).toHaveCount(4)

    // 清理：回流程配置，设计器已 remount 无当前模板，先从模板管理选中再删
    await tab(page, '流程配置').click()
    await selectTemplate(page, name)
    await deleteCurrentTemplate(page)
  })

  test('非 admin 看不到流程绑定 Tab（权限不回退）', async ({ page }) => {
    // doctor 角色：mock 登录后进告警页
    await adminLogin(page, 'doctor')
    await page.goto(adminPath('/alerts'))
    await expect(tab(page, '告警列表')).toBeVisible()
    await expect(tab(page, '流程绑定')).toHaveCount(0)
    await expect(tab(page, '流程配置')).toHaveCount(0)
    await expect(page.locator('.ftb-card')).toHaveCount(0)
  })
})
