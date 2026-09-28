import { test, expect } from '@playwright/test'
import { techRoutes, forceTechLoginMock, uniModal } from '../tech-helpers'

/**
 * tech-alerts 页：技师端告警列表与详情弹窗（T433 缺陷一 / 二 / 三 的现场判据）
 *
 * 这条 spec 是 T433 新开的：改前该页在 mock 构建下必然落「加载失败」——
 * 页面直接调 utils/request，而 request 在 USE_MOCK 下抛错（utils/request.ts:35-37），
 * 所以这三格此前**没有任何可跑绿的 e2e**，Alice 只能手敲 hash 在现网走查（T428 第 10 项）。
 * 补齐 api/alert.ts 的 mock 分支后，这些判据才第一次能在门禁里执行。
 *
 * mock 集 60 条 > 单页 50，现网形态是 86 条（同样跨页）。
 */

test.beforeEach(async ({ page }) => {
  await forceTechLoginMock(page)
  await page.goto(techRoutes.alerts)
})

test('页头取接口总数，不再是本页条数（缺陷一）', async ({ page }) => {
  await expect(page.getByText('告警通知').first()).toBeVisible()
  await expect(page.getByText('共 60 条告警')).toBeVisible()
  await expect(page.locator('.alert-card')).toHaveCount(60)
})

test('第二页的告警也在列表里（缺陷一「取数只发一页」）', async ({ page }) => {
  // pageSize=50 ⇒ 第 51 条起属第二页，改前永不出现在页面上。
  // 按 detail 文案认行（alertId 只在弹窗标题里渲染，列表卡片上没有）。
  await expect(page.locator('.alert-card').nth(50)).toContainText('T433 mock 告警 51')
})

test('实际值为 0 时该行仍渲染（缺陷二）', async ({ page }) => {
  const first = page.locator('.alert-card').first()
  await expect(first.locator('.type-badge')).toHaveText('佩戴时长不足')
  await expect(first.locator('.meta-item').filter({ hasText: '实际值' })).toBeVisible()
  await expect(first.locator('.meta-item').filter({ hasText: '实际值' })).toContainText('0h')
})

test('详情弹窗阈值按小时口径，且 0 值行不消失（缺陷二 + 缺陷三）', async ({ page }) => {
  await page.locator('.alert-card').first().click()
  const modal = uniModal(page)
  await expect(modal.root).toBeVisible()
  const text = await modal.root.innerText()
  expect(text).toContain('实际值: 0h')
  // 改前弹窗写「阈值: 540h」，同弹窗详情句写「低于目标 9.0 小时」
  expect(text).toContain('阈值: 9h')
  expect(text).not.toContain('540h')
  await modal.confirm.click()
})

test('T443 裁定⑤：患者位显姓名，无姓名的行回落 ID', async ({ page }) => {
  // mock 第 1 条（i=0）带姓名；第 6 条（i=5）刻意 null ⇒ 门禁里两分支都被执行
  const first = page.locator('.alert-card').first()
  await expect(first.locator('.meta-item').filter({ hasText: '患者' })).toContainText('张明远')
  const nameless = page.locator('.alert-card').nth(5)
  await expect(nameless.locator('.meta-item').filter({ hasText: '患者' })).toContainText('P20260006')
  await expect(nameless.locator('.meta-item').filter({ hasText: '患者' })).not.toContainText('undefined')
})

test('T443 裁定①：详情弹窗末尾有「处置建议」固定模板，编号连续', async ({ page }) => {
  await page.locator('.alert-card').first().click()
  const modal = uniModal(page)
  await expect(modal.root).toBeVisible()
  const text = await modal.root.innerText()
  expect(text).toContain('患者: 张明远')
  expect(text).toContain('处置建议:')
  expect(text).toContain('1. ')
  expect(text).toContain('4. ')
  await modal.confirm.click()
})

test('T443 裁定⑤：详情弹窗的患者行同样回落 ID（弹窗与列表同源）', async ({ page }) => {
  await page.locator('.alert-card').nth(5).click()
  const modal = uniModal(page)
  await expect(modal.root).toBeVisible()
  const text = await modal.root.innerText()
  expect(text).toContain('患者: P20260006')
  expect(text).not.toContain('患者: null')
  expect(text).not.toContain('患者: undefined')
  await modal.confirm.click()
})

test('筛选分档按全量而非首页（缺陷一的筛选面）', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '待处理' }).click()
  await expect(page.locator('.alert-card')).toHaveCount(59)
  await page.locator('.seg-btn', { hasText: '已处理' }).click()
  await expect(page.locator('.alert-card')).toHaveCount(1)
  await page.locator('.seg-btn', { hasText: '全部' }).click()
  await expect(page.locator('.alert-card')).toHaveCount(60)
})
