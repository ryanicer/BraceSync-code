import { test, expect, type Locator, type Page } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption } from '../admin-helpers'

/**
 * T276 2.4 拖拽式流程设计器（USE_MOCK=true，模板 CRUD 走 src/mock/flow.ts 的可写存储）
 * 逐条对应卡片 ①~⑥：
 * ① 左栏 DndPanel 8 类节点 + 搜索 ② 形状与尺寸（触发圆、判断菱、其余矩形，按类别着色）
 * ③ 拖拽落位/连线 + 缩放/适应画布/撤销重做/缩略图 ④ 声明式连接规则
 * ⑤ 右栏属性面板按类别动态渲染并写回 properties ⑥ 模板新建/保存/切换回显/删除
 *
 * 连线一律走「锚点真实拖拽」：LogicFlow 的连接规则只绑在 view/Anchor 的 pointer 事件上，
 * lf.addEdge() 不经过规则，用 API 造线会验出假绿。
 */

/** LogicFlow 的缩略图会在主画布 .lf-graph 内再嵌一个 LogicFlow 实例，自带一份
 * .lf-graph / .lf-design-node / .lf-edge / .lf-element-text —— 画布查询必须从主 svg 起，否则计数翻倍。
 * 链路 .fd-canvas-inner > div > .lf-graph > svg 只有主画布满足（缩略图那份挂在 .lf-tool-overlay 下）。 */
const mainSvg = '.fd-canvas-inner > div > .lf-graph > svg'
const canvas = (page: Page) => page.locator('.fd-canvas-inner')
const graph = (page: Page) => page.locator('.fd-canvas-inner > div > .lf-graph')
const nodes = (page: Page) => page.locator(`${mainSvg} .lf-design-node`)
const edges = (page: Page) => page.locator(`${mainSvg} .lf-edge`)
const palette = (page: Page, label: string) => page.locator('.lf-dndpanel .lf-dnd-item').filter({ hasText: label })
const propsPanel = (page: Page) => page.locator('.fd-props')
/** 按 el-form-item 的中文标题取一项（面板里同时有多个 textarea/input，必须按标题限定才不会撞 strict mode） */
const formItem = (page: Page, label: string) => propsPanel(page).locator('.el-form-item').filter({ hasText: label })
const tplPopper = (page: Page) => page.locator('.fd-tpl-popper')

async function openDesigner(page: Page) {
  await adminLogin(page, 'admin')
  await page.goto(adminRoutes.alerts)
  await page.getByRole('tab', { name: '流程配置' }).click()
  await expect(page.locator('.fd-body')).toBeVisible()
  await expect(graph(page)).toBeVisible()
}

/** 从左侧面板把节点拖到画布指定位置（DndPanel 用 onpointerdown + lf.dnd.startDrag）。
 * 面板 8 项在 720p 视口下超出可视高度，必须先滚到可见再取坐标，否则 mouse.down 落在视口外。 */
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
  await expect(canvas(page).locator('.lf-dnd-mask')).toHaveCount(0)
  // 落位必须逐次断言：LogicFlow 的 Dnd 在 pointerup 时用 elementFromPoint 判断「落点在不在画布上」，
  // 任何东西（例如 v-loading 淡出中的遮罩）盖在落点上都会静默丢掉这次拖放，不报错也不产生节点。
  await expect.poll(() => nodes(page).count(), {
    timeout: 5_000,
    message: `从面板拖「${label}」到画布未生成节点`,
  }).toBe(before + 1)
}

/** 节点锚点的屏幕坐标（锚点常驻 DOM，只在 hover 时可见） */
async function anchorPoints(node: Locator): Promise<{ x: number; y: number }[]> {
  return node.locator('.lf-anchor').evaluateAll((els) => els.map((el) => {
    const b = el.getBoundingClientRect()
    return { x: b.x + b.width / 2, y: b.y + b.height / 2 }
  }))
}

/** 从源节点最靠近目标的锚点拖一条线到目标节点（落点用节点体内，LogicFlow 自动吸附最近锚点） */
async function dragConnect(page: Page, fromLabel: string, toLabel: string): Promise<void> {
  const from = nodes(page).filter({ hasText: fromLabel }).first()
  const to = nodes(page).filter({ hasText: toLabel }).first()
  // 锚点只在 hover / 选中时渲染，而上一步的落点常常就在源节点里 —— 指针不挪开就不会再触发
  // mouseenter，hover 出来的锚点是空的。先停在画布左上角的空白处再 hover。
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
  // LogicFlow 的锚点拖拽用 raf 节流更新落点，合成事件之间必须留帧间隙，
  // 否则 mouseup 时落点还停在起点（真人拖拽天然有帧），连线创建不出来
  await page.mouse.move((start.x + target.x) / 2, (start.y + target.y) / 2)
  await page.waitForTimeout(60)
  await page.mouse.move(target.x, target.y)
  await page.waitForTimeout(120)
  await page.mouse.up()
  await page.waitForTimeout(120)
}

/** 节点外形选择器：LogicFlow 的锚点也是 circle.lf-basic-shape（带 lf-node-anchor 类），必须排掉 */
const shapeSel = (kind?: string) =>
  `${mainSvg} ${kind ? `.lf-design-node.is-${kind} ` : ''}.lf-basic-shape:not(.lf-node-anchor):not(.lf-node-anchor-hover)`

/** 形状描边色（按类别） */
function nodeStroke(page: Page, kind: string): Promise<string> {
  return page.locator(shapeSel(kind)).evaluate((el) => getComputedStyle(el).stroke)
}

/** 形状标签名 + 包围盒：用 SVG 用户坐标，不受画布缩放影响，能真正验到落库的几何属性 */
function nodeShape(page: Page, kind: string): Promise<{ tag: string; w: number; h: number }> {
  return page.locator(shapeSel(kind)).evaluate((el) => {
    const b = (el as unknown as SVGGraphicsElement).getBBox()
    return { tag: el.tagName.toLowerCase(), w: Math.round(b.width), h: Math.round(b.height) }
  })
}

/** 打开「模板管理」下拉（已开则不重复点，避免点击切换成关闭） */
async function openTplMenu(page: Page) {
  const popper = tplPopper(page)
  if (!(await popper.count()) || !(await popper.first().isVisible())) {
    await page.getByRole('button', { name: '模板管理' }).click()
  }
  await expect(popper).toBeVisible()
}

async function closeTplMenu(page: Page) {
  const popper = tplPopper(page)
  if (await popper.count()) {
    await page.keyboard.press('Escape')
    if (await popper.first().isVisible().catch(() => false)) {
      await page.getByRole('button', { name: '模板管理' }).click()
    }
    await expect(popper.first()).toBeHidden({ timeout: 3_000 }).catch(() => { /* 收起动画慢不阻塞 */ })
  }
}

const tplItem = (page: Page, name: string) => tplPopper(page).locator('.fd-tpl-item').filter({ hasText: name })

/** 走顶栏按钮新建模板；弹层遮罩淡出前会吃掉指针事件，必须等它卸载再拖拽 */
async function createTemplate(page: Page, name: string) {
  await page.getByRole('button', { name: '新建流程模板' }).click()
  const box = page.locator('.el-message-box')
  await box.getByRole('textbox').fill(name)
  await box.getByRole('button', { name: '新建' }).click()
  await expect(adminMessage(page)).toContainText('已创建空模板')
  // 弹层容器会留在 DOM 里（隐藏），只有可见 overlay 会吃掉指针事件
  await expect.poll(async () => page.locator('.el-overlay:visible').count()).toBe(0)
}

/** 进设计器后先新建一张空模板：预置模板自带 7 个节点，不清零没法核对拖拽后的数量与形状 */
async function openBlankDesigner(page: Page, tag: string): Promise<string> {
  await openDesigner(page)
  const name = `T276 ${tag} ${Date.now()}`
  await createTemplate(page, name)
  await expect(nodes(page)).toHaveCount(0)
  return name
}

test.describe('流程设计器', () => {
  test('① 左栏节点面板 8 类节点（带图标），搜索可过滤', async ({ page }) => {
    await openDesigner(page)
    await expect(page.locator('.lf-dndpanel .lf-dnd-item')).toHaveCount(8)
    for (const label of ['触发节点', '通知节点', '处理节点', '判断节点', '并行节点', '汇聚节点', '延时节点', '归档节点']) {
      await expect(palette(page, label)).toBeVisible()
    }
    // 图标是 data-uri SVG 背景图（DndPanel 会自己包 url()，这里验最终计算样式）
    const icon = await palette(page, '触发节点').locator('.lf-dnd-shape')
      .evaluate((el) => getComputedStyle(el).backgroundImage)
    expect(icon).toContain('data:image/svg+xml')

    await page.locator('.fd-palette-head input').fill('判断')
    await expect(page.locator('.lf-dndpanel .lf-dnd-item')).toHaveCount(1)
    await expect(palette(page, '判断节点')).toBeVisible()
    // 类别键也能搜到
    await page.locator('.fd-palette-head input').fill('delay')
    await expect(page.locator('.lf-dndpanel .lf-dnd-item')).toHaveCount(1)
    await expect(palette(page, '延时节点')).toBeVisible()
    await page.locator('.fd-palette-head input').fill('不存在')
    await expect(page.locator('.lf-dndpanel .lf-dnd-item')).toHaveCount(0)
  })

  test('② 拖拽落节点：形状、尺寸、类别色都按设计稿', async ({ page }) => {
    await openBlankDesigner(page, '形状')
    await dragNode(page, '触发节点', 260, 90)
    await dragNode(page, '判断节点', 260, 230)
    await dragNode(page, '处理节点', 260, 370)
    await expect(nodes(page)).toHaveCount(3)
    await expect(page.locator('.fd-count')).toContainText('节点 3 · 连线 0')

    // 设计稿形状与尺寸：触发 58 圆（r=29）、判断 100×56 菱（rx/ry）、处理 140×48 矩
    expect(await nodeShape(page, 'trigger')).toEqual({ tag: 'circle', w: 58, h: 58 })
    expect(await nodeShape(page, 'condition')).toEqual({ tag: 'polygon', w: 100, h: 56 })
    expect(await nodeShape(page, 'process')).toEqual({ tag: 'rect', w: 140, h: 48 })

    // 触发 #F59E0B、判断 #3B82F6、处理 #10B981
    expect(await nodeStroke(page, 'trigger')).toBe('rgb(245, 158, 11)')
    expect(await nodeStroke(page, 'condition')).toBe('rgb(59, 130, 246)')
    expect(await nodeStroke(page, 'process')).toBe('rgb(16, 185, 129)')
  })

  test('③④ 拖拽连线成功；触发不能当终点、归档不能当起点', async ({ page }) => {
    await openBlankDesigner(page, '连线')
    await dragNode(page, '触发节点', 200, 90)
    await dragNode(page, '处理节点', 200, 250)
    await dragNode(page, '归档节点', 420, 250)
    await expect(nodes(page)).toHaveCount(3)

    await dragConnect(page, '触发节点', '处理节点')
    await expect(edges(page)).toHaveCount(1)
    await expect(page.locator('.fd-count')).toContainText('连线 1')

    // 归档节点只能连出→不能连出：从它拖出不产生新边
    await dragConnect(page, '归档节点', '处理节点')
    await expect(edges(page)).toHaveCount(1)
    // 触发节点是入口：连到它也不产生新边
    await dragConnect(page, '处理节点', '触发节点')
    await expect(edges(page)).toHaveCount(1)
  })

  test('③ 悬浮工具栏：撤销重做、缩放百分比、适应画布；缩略图已挂', async ({ page }) => {
    await openBlankDesigner(page, '工具栏')
    await dragNode(page, '处理节点', 240, 200)
    await expect(nodes(page)).toHaveCount(1)
    await expect(page.locator('.lf-mini-map')).toBeVisible()

    const bar = (title: string) => page.locator(`.fd-float-bar button[title="${title}"]`)
    // §6.3 (d)：S3 实测 lf.isEnableUndo() 在 2.2.5 不存在，可用态取 history.undoAble()/redoAble()。
    // 刚 render 完栈里只有基线，按钮必须是禁用态（不是「点了没反应」）
    await expect(bar('上一步')).toBeDisabled()
    await expect(bar('下一步')).toBeDisabled()
    // LogicFlow 的 History 是 debounce 100ms 才落栈的，栈里只有基线时 undo() 直接 return；
    // 拖完立刻按「上一步」会点成空操作
    await page.waitForTimeout(200)
    await expect(bar('上一步')).toBeEnabled()
    await bar('上一步').click()
    await expect(nodes(page)).toHaveCount(0)
    await expect(bar('上一步')).toBeDisabled()
    await bar('下一步').click()
    await expect(nodes(page)).toHaveCount(1)

    const zoom = page.locator('.fd-zoom')
    const before = (await zoom.textContent()) ?? ''
    await bar('放大').click()
    await expect(zoom).not.toHaveText(before)
    await bar('缩小').click()
    await bar('适应画布').click()
    await expect(graph(page)).toBeVisible()
    await expect(nodes(page)).toHaveCount(1)
  })

  test('⑤ 右栏属性面板按类别动态渲染并写回节点', async ({ page }) => {
    await openBlankDesigner(page, '属性')
    await expect(propsPanel(page)).toContainText('请选择一个节点以编辑属性')

    await dragNode(page, '处理节点', 220, 120)
    await nodes(page).filter({ hasText: '处理节点' }).first().click({ force: true })
    await expect(propsPanel(page).getByText('节点类型')).toBeVisible()
    await expect(propsPanel(page).locator('.fd-kind-tag')).toHaveText('处理节点')
    // 处理节点：角色 / 时限 / 通知方式 / 超时升级 / 备注，但没有条件表达式
    await expect(formItem(page, '处理角色')).toBeVisible()
    await expect(formItem(page, '通知方式')).toBeVisible()
    // 开关的 active-text「启用超时升级」含同名子串，必须 exact
    await expect(propsPanel(page).getByText('超时升级', { exact: true })).toBeVisible()
    await expect(propsPanel(page).getByText('条件配置')).toHaveCount(0)
    // §9.3：通知方式/超时升级后端本期没有消费者，不标注就是骗 admin
    await expect(formItem(page, '通知方式').locator('.fd-declared')).toHaveText('本期仅配置，不自动执行')
    await expect(formItem(page, '处理角色').locator('.fd-declared')).toHaveCount(0)

    await propsPanel(page).locator('input').first().fill('医生确认')
    await page.keyboard.press('Tab')
    await expect(page.locator(`${mainSvg} .lf-element-text`).first()).toContainText('医生确认')
    await pickSelectOption(page, formItem(page, '处理角色').locator('.el-select'), '主治医生')
    await pickSelectOption(page, formItem(page, '处理时限').locator('.el-select'), '小时')
    await propsPanel(page).locator('.el-switch').click()
    await expect(propsPanel(page).getByText('升级目标角色')).toBeVisible()

    // 判断节点：换成条件表达式 + 角色/时限，没有通知方式与超时升级
    await dragNode(page, '判断节点', 220, 320)
    await nodes(page).filter({ hasText: '判断节点' }).first().click({ force: true })
    await expect(propsPanel(page).locator('.fd-kind-tag')).toHaveText('判断节点')
    await expect(formItem(page, '条件配置').locator('textarea')).toBeVisible()
    await expect(propsPanel(page).getByText('通知方式')).toHaveCount(0)
    await expect(propsPanel(page).getByText('超时升级')).toHaveCount(0)
    await formItem(page, '条件配置').locator('textarea').fill('压力值 > 100')
    await page.keyboard.press('Tab')

    // 触发节点：只剩备注 + 说明文案
    await dragNode(page, '触发节点', 460, 120)
    await nodes(page).filter({ hasText: '触发节点' }).first().click({ force: true })
    await expect(propsPanel(page).locator('.fd-kind-tag')).toHaveText('触发节点')
    await expect(propsPanel(page).locator('.fd-hint')).toHaveText('流程入口，只能连出，不需要额外属性。')
    await expect(propsPanel(page).getByText('处理角色')).toHaveCount(0)
    await expect(formItem(page, '备注')).toBeVisible()

    // 删除节点：先确认（§4.1 要求点明「在途实例该节点名回空」），确认后画布少一个、面板回空态
    const before = await nodes(page).count()
    await propsPanel(page).getByRole('button', { name: '删除节点' }).click()
    const delBox = page.locator('.el-message-box')
    await expect(delBox).toContainText('在途实例')
    await delBox.getByRole('button', { name: '取消' }).click()
    await expect(nodes(page)).toHaveCount(before)
    await propsPanel(page).getByRole('button', { name: '删除节点' }).click()
    await delBox.getByRole('button', { name: '删除' }).click()
    await expect(nodes(page)).toHaveCount(before - 1)
    await expect(propsPanel(page)).toContainText('请选择一个节点以编辑属性')
  })

  test('⑥ 模板：新建 → 编排 → 保存 → 切换清空 → 回显 → 删除', async ({ page }) => {
    await openDesigner(page)
    const firstName = `T276 e2e 模板 ${Date.now()}`
    await createTemplate(page, firstName)
    await expect(page.locator('.fd-topbar-meta')).toContainText(firstName)
    await expect(nodes(page)).toHaveCount(0)

    await dragNode(page, '触发节点', 200, 90)
    await dragNode(page, '处理节点', 200, 250)
    await dragNode(page, '归档节点', 200, 400)
    await dragConnect(page, '触发节点', '处理节点')
    await dragConnect(page, '处理节点', '归档节点')
    await expect(nodes(page)).toHaveCount(3)
    await expect(edges(page)).toHaveCount(2)

    await page.getByRole('button', { name: '保存', exact: true }).click()
    // 保存成功提示带版本号 + 回读数量（§4.3 第 5 步：后端回读比对就是「字段没被吞」的最小证据）
    await expect(adminMessage(page)).toContainText('流程模板已保存（v2）。节点 3 / 连线 2，回读 3 / 2。')
    await expect(page.locator('.fd-dirty')).toHaveCount(0)
    await expect(page.locator('.fd-topbar-meta')).toContainText('版本：v2')

    // 模板管理下拉里能看到自己的模板与节点数。列表接口的 nodes 恒为空（契约 :1082），
    // 所以「3 节点」只能来自保存后那次详情回读的回填 —— 这条断言同时钉住 mock 与真实后端同口径。
    await openTplMenu(page)
    await expect(tplItem(page, firstName)).toContainText('3 节点')

    // 切到别的模板：画布整体换成对方形状，本模板不残留
    await tplItem(page, '默认告警流程').click()
    await expect(nodes(page)).toHaveCount(7)
    await expect(page.locator('.fd-topbar-meta')).toContainText('默认告警流程')
    await closeTplMenu(page)

    await openTplMenu(page)
    await tplItem(page, firstName).click()
    await expect(page.locator('.fd-topbar-meta')).toContainText(firstName)
    await expect(nodes(page)).toHaveCount(3)
    await expect(edges(page)).toHaveCount(2)
    // 形状与几何按落库结果回显（触发仍是 58 圆、归档是灰矩形）
    expect(await nodeShape(page, 'trigger')).toEqual({ tag: 'circle', w: 58, h: 58 })
    expect(await nodeShape(page, 'archive')).toEqual({ tag: 'rect', w: 140, h: 48 })
    expect(await nodeStroke(page, 'archive')).toBe('rgb(148, 163, 184)')
    await closeTplMenu(page)

    // 模板搜索（下拉里的搜索框走列表接口的 keyword）
    await openTplMenu(page)
    await page.locator('.fd-tpl-search input').fill('不存在的模板名')
    await expect(tplPopper(page).locator('.fd-tpl-empty')).toBeVisible()
    await page.locator('.fd-tpl-search input').fill(firstName)
    await expect(tplPopper(page).locator('.fd-tpl-item')).toHaveCount(1)
    // 删掉搜索词：删除后的兜底是「列表第一个模板」，关键词还挂着的话列表本身就是空的，
    // 兜底逻辑（切到默认告警流程）就验不到了
    await page.locator('.fd-tpl-search input').fill('')
    await expect(tplPopper(page).locator('.fd-tpl-item')).not.toHaveCount(1)
    await closeTplMenu(page)

    // 删除自建模板（真实环境同理：有实例的模板后端 409 拒删）
    await page.getByRole('button', { name: '删除模板' }).click()
    await page.locator('.el-message-box').getByRole('button', { name: '删除' }).click()
    await expect(adminMessage(page)).toContainText('已删除模板')
    await expect(page.locator('.fd-topbar-meta')).toContainText('默认告警流程')
    await expect(nodes(page)).toHaveCount(7)
  })

  test('⑥ 重名新建被拒（契约 409 等价），不会静默覆盖已有模板', async ({ page }) => {
    await openDesigner(page)
    await page.getByRole('button', { name: '新建流程模板' }).click()
    await page.locator('.el-message-box').getByRole('textbox').fill('默认告警流程')
    await page.locator('.el-message-box').getByRole('button', { name: '新建' }).click()
    await expect(adminMessage(page)).toContainText('已存在')
    // 仍停在原模板，画布没被清空
    await expect(page.locator('.fd-topbar-meta')).toContainText('默认告警流程')
    await expect(nodes(page)).toHaveCount(7)
  })

  test('⑥ 已有实例的模板拒绝删除（契约 409）', async ({ page }) => {
    await openDesigner(page)
    await expect(page.locator('.fd-topbar-meta')).toContainText('默认告警流程')
    await page.getByRole('button', { name: '删除模板' }).click()
    await page.locator('.el-message-box').getByRole('button', { name: '删除' }).click()
    await expect(adminMessage(page)).toContainText('流程实例')
    await expect(page.locator('.fd-topbar-meta')).toContainText('默认告警流程')
    await expect(nodes(page)).toHaveCount(7)
  })

  test('⑥ 未保存改动：dirty 角标 + 丢弃确认，取消时不动画布（T285 §6.3 a / §6.4）', async ({ page }) => {
    const mine = await openBlankDesigner(page, 'dirty')
    await expect(page.locator('.fd-dirty')).toHaveCount(0)

    await dragNode(page, '处理节点', 220, 160)
    await expect(page.locator('.fd-count')).toContainText('未保存')

    // 新建路径：先问「丢弃？」，选「留在当前模板」就停在原处，也不会弹出改名 prompt
    await page.getByRole('button', { name: '新建流程模板' }).click()
    const box = page.locator('.el-message-box')
    await expect(box).toContainText('有未保存的改动')
    await box.getByRole('button', { name: '留在当前模板' }).click()
    await expect(nodes(page)).toHaveCount(1)
    await expect(page.locator('.fd-topbar-meta')).toContainText(mine)
    await expect(box).toHaveCount(0)

    // 切模板路径：同样先问；这次选「丢弃并继续」，画布整体换成目标模板
    await openTplMenu(page)
    await tplItem(page, '默认告警流程').click()
    await expect(box).toContainText('有未保存的改动')
    await box.getByRole('button', { name: '丢弃并继续' }).click()
    await expect(nodes(page)).toHaveCount(7)
    await expect(page.locator('.fd-topbar-meta')).toContainText('默认告警流程')
    await closeTplMenu(page)
    // 丢掉的是未保存改动，不是别人的模板：自己的模板还在下拉里
    await openTplMenu(page)
    await expect(tplItem(page, mine)).toBeVisible()
    await closeTplMenu(page)
  })

  test('⑥ 结构校验：有错就拦下保存、画布标红、右栏清单可定位（T285 §6.2）', async ({ page }) => {
    await openBlankDesigner(page, '校验')
    await dragNode(page, '触发节点', 200, 80)
    await dragNode(page, '处理节点', 200, 260)
    // 没连线：V5 不可达 + V6 没有出边（error）、V9 没有归档（warning）

    await page.getByRole('button', { name: '保存', exact: true }).click()
    await expect(adminMessage(page)).toContainText('结构校验未通过')
    const issues = page.locator('.fd-issues li')
    await expect(issues.filter({ hasText: '走不到' })).toHaveCount(1)
    await expect(issues.filter({ hasText: '没有连出归档节点' })).toHaveCount(1)
    await expect(issues.filter({ hasText: '没有归档节点' })).toHaveCount(1)
    await expect(page.locator('.fd-issues .is-error')).toHaveCount(2)
    await expect(page.locator('.fd-issues-count')).toHaveText('2 错误 / 1 提示')
    // 命中的节点标红（is-invalid 只进视图，保存时会被剥掉）
    await expect(page.locator(`${mainSvg} g.lf-design-node.is-invalid`)).toHaveCount(2)
    // 没保存成：版本号仍是 v1，未保存角标还在
    await expect(page.locator('.fd-topbar-meta')).toContainText('版本：v1')
    await expect(page.locator('.fd-dirty')).toHaveCount(1)

    // 点清单里的错误项 → 选中对应节点，右栏跟着切过去
    await issues.filter({ hasText: '走不到' }).click()
    await expect(propsPanel(page).locator('.fd-kind-tag')).toHaveText('处理节点')

    // 补齐出口后再保存：清单清空、标红退色
    await dragNode(page, '归档节点', 420, 260)
    await dragConnect(page, '触发节点', '处理节点')
    await dragConnect(page, '处理节点', '归档节点')
    await page.getByRole('button', { name: '保存', exact: true }).click()
    await expect(adminMessage(page)).toContainText('流程模板已保存（v2）')
    await expect(page.locator('.fd-issues')).toHaveCount(0)
    await expect(page.locator(`${mainSvg} g.lf-design-node.is-invalid`)).toHaveCount(0)

    await page.getByRole('button', { name: '删除模板' }).click()
    await page.locator('.el-message-box').getByRole('button', { name: '删除' }).click()
    await expect(adminMessage(page)).toContainText('已删除模板')
  })

  test('设计器属性存进模板图：运行态与后端消费方读到同一套字段', async ({ page }) => {
    const name = `T276 属性透传 ${Date.now()}`
    await openDesigner(page)
    await createTemplate(page, name)
    await dragNode(page, '处理节点', 200, 120)
    await dragNode(page, '归档节点', 200, 300)
    await dragConnect(page, '处理节点', '归档节点')
    await nodes(page).filter({ hasText: '处理节点' }).first().click({ force: true })
    await propsPanel(page).locator('input').first().fill('医生确认')
    await page.keyboard.press('Tab')
    await pickSelectOption(page, formItem(page, '处理角色').locator('.el-select'), '主治医生')
    // 校验器要入口：没有触发节点的图 V2 就拦下了，透传用例关心的是属性而不是拓扑
    await dragNode(page, '触发节点', 200, 40)
    await dragConnect(page, '触发节点', '医生确认')
    await page.getByRole('button', { name: '保存', exact: true }).click()
    await expect(adminMessage(page)).toContainText('流程模板已保存')

    const stored = await page.evaluate(async (tplName) => {
      const mod = await import('/src/api/flow.ts')
      // 列表接口不带图数据（契约 :1082），属性透传只能查详情
      const row = (await mod.fetchFlowTemplates(tplName)).find((t: { name: string }) => t.name === tplName)
      const tpl = row ? await mod.fetchFlowTemplate(row.templateId) : null
      const handle = tpl?.nodes.find((n: { text?: { value?: string } }) => n.text?.value === '医生确认')
      return { name: tpl?.name, nodes: tpl?.nodes.length ?? 0, props: handle?.properties ?? null }
    }, name)
    expect(stored.name).toBe(name)
    expect(stored.nodes).toBe(3)
    // design-* 已收成内置形状名（type 在 serializeGraph 里）；属性键按 T285 §2.3，
    // 且运行态键（status/assigneeName）与标红用的 invalid 都不落库
    expect(stored.props).toMatchObject({
      kind: 'process', assigneeRole: '主治医生', timeLimit: 30, timeUnit: 'minutes',
      channels: ['system', 'sms'], width: 140, height: 48,
    })
    expect(stored.props.invalid).toBeUndefined()
    expect(stored.props.assigneeName).toBeUndefined()
    // 自建模板用完即删，不留残留
    await page.getByRole('button', { name: '删除模板' }).click()
    await page.locator('.el-message-box').getByRole('button', { name: '删除' }).click()
    await expect(adminMessage(page)).toContainText('已删除模板')
  })
})
