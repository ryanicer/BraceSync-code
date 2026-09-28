import { test, expect } from '@playwright/test'
import { techRoutes, forceTechLoginMock } from '../tech-helpers'

/**
 * tech-records 页：安装记录列表（WiFi 状态筛选）
 * 对齐 T089 V2.1 records 页：WiFi 单筛选、无详情弹窗、无 FAB
 * T162: 可达性筛选/徽标按 T138 裁定删除（记录不落"装机时刻可达性"静态标记）
 * T433 缺陷一：mock 集扩到 26 条（> 单页 20），页头读接口 total、列表逐页取满。
 *   改前这一页只发一页且页头取已加载数组长度 —— 6 条的 mock 一页装得下，
 *   所以「共 6 条」当时既对又测不出缺陷，现网 31 条才暴露（Alice T428 走查第 9 项）。
 */

test.beforeEach(async ({ page }) => {
  await forceTechLoginMock(page)
  await page.goto(techRoutes.records)
})

test('页面标题与记录总数（页头取接口 total，不是本页条数）', async ({ page }) => {
  await expect(page.getByText('安装记录').first()).toBeVisible()
  await expect(page.getByText('共 26 条记录')).toBeVisible()
})

test('默认显示全部记录（跨页取满：第 21 条起在第二页）', async ({ page }) => {
  await expect(page.locator('.record-card')).toHaveCount(26)
  await expect(page.locator('.record-device').first()).toHaveText('PRS-ML05-RC-19700101001')
  await expect(page.locator('.wifi-badge').first()).toContainText('已连接')
  // 只有真翻页才拿得到：序号 21..26 落在 pageSize=20 的第二页
  await expect(page.locator('.record-device', { hasText: '19700101026' })).toHaveCount(1)
})

test('WiFi 筛选：已连接', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '已连接' }).click()
  await expect(page.locator('.seg-btn', { hasText: '已连接' })).toHaveClass(/seg-active/)
  await expect(page.locator('.record-card')).toHaveCount(7)
})

test('WiFi 筛选：未配置', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '未配置' }).click()
  await expect(page.locator('.seg-btn', { hasText: '未配置' })).toHaveClass(/seg-active/)
  await expect(page.locator('.record-card')).toHaveCount(7)
})

/**
 * T459：库里存 failed／skipped 时展示面要按 T447 唯一词形表出词，红档还得真带上红类。
 * mock 集按四档循环（src/api/install.ts 的 WIFI_MOCK_CYCLE）⇒ 26 条里 failed 6、skipped 6。
 * 筛选栏按 C1 裁定（PM 拍「丙」）刻意保持两档，故这里钉的是「不出第三/第四颗 chip」，
 * 不是漏做——稿面筛选栏是可达性维度，且 repo/query.go 不支持按 WiFi 状态过滤。
 */
test('WiFi 四档徽章：连接失败走红档，已跳过配网不冒充别的档（T447＋C1 丙）', async ({ page }) => {
  const failed = page.locator('.wifi-badge.wifi-fail')
  await expect(failed).toHaveCount(6)
  await expect(failed.first()).toHaveText('连接失败')
  await expect(page.locator('.wifi-badge.wifi-pending', { hasText: '已跳过配网' })).toHaveCount(6)
  await expect(page.locator('.wifi-badge.wifi-ok')).toHaveCount(7)
  await expect(page.locator('.wifi-badge.wifi-pending', { hasText: '未配置' })).toHaveCount(7)
  // 反证方向：新增两档没被顺手做成筛选项（做了就与 C1 裁定相反）
  await expect(page.locator('.seg-btn', { hasText: '连接失败' })).toHaveCount(0)
  await expect(page.locator('.seg-btn', { hasText: '已跳过配网' })).toHaveCount(0)
})

test('WiFi 切回全部', async ({ page }) => {
  await page.locator('.seg-btn', { hasText: '已连接' }).click()
  await expect(page.locator('.record-card')).toHaveCount(7)
  await page.locator('.seg-btn', { hasText: '全部 WiFi' }).click()
  await expect(page.locator('.record-card')).toHaveCount(26)
})
