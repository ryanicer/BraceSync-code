import { expect, test, type Page } from '@playwright/test'
import { ok, setupPatientE2E, toast } from '../helpers'
import type { FeelingLog } from '@bracesync/shared-types'

/**
 * T505 患者端矫形日志页 e2e（Playwright H5，route mock）
 *
 * feeling-logs 不在 helpers.setupPatientE2E 的历史路由表内（那批只覆盖 tab 主链路），
 * 按 profile.spec.ts 的先例在本 spec 自行注册：Playwright LIFO ⇒ 本 spec 的路由先被咨询。
 * GET/POST 同路径（后端按 (patient_id, log_date) upsert ⇒ 编辑即同日覆盖），故按 method 分流。
 *
 * 档位单选的回填/默认不靠 radio 的 DOM 选中态断言（uni-app H5 的原生 input 被组件包裹，
 * 可见性与 checked 画法不稳），一律走「保存后 POST 请求体」这条真实写通道取证。
 *
 * 选择器：业务代码不加 data-testid（红线），全部 class + 文案定位（同 e2e/helpers.ts 约定）。
 */
const FEELINGS_PATH = '/#/pages/feelings/index'

function log(over: Partial<FeelingLog>): FeelingLog {
  return {
    logId: '101',
    patientId: 'pat-e2e-001',
    logDate: '2026-07-12',
    comfortScore: null,
    feeling: 'fitted',
    discomfortAreas: ['右侧腰'],
    notes: '佩戴感觉良好，右侧腰段有轻微压迫感但可忍受。',
    replyContent: null,
    replyTime: null,
    ...over,
  }
}

/** 注册 feeling-logs 路由（GET 回 list / POST 捕获请求体并回落库行） */
async function mockFeelingLogs(page: Page, list: FeelingLog[]) {
  const posted: Array<Record<string, unknown>> = []
  await page.route(/\/api\/v1\/patients\/[^/]+\/feeling-logs$/, async (route) => {
    const req = route.request()
    if (req.method().toUpperCase() === 'POST') {
      const body = req.postDataJSON() as Record<string, unknown>
      posted.push(body)
      return route.fulfill({
        json: ok(log({
          logId: '900',
          logDate: String(body.logDate ?? '2026-07-12'),
          feeling: body.feeling === 'discomfort' ? 'discomfort' : 'fitted',
          discomfortAreas: Array.isArray(body.discomfortAreas) ? (body.discomfortAreas as string[]) : [],
          notes: String(body.notes ?? ''),
        })),
      })
    }
    return route.fulfill({ json: ok(list) })
  })
  return posted
}

test('列表态：两档标签按稿面配色与文案呈现，医生回复气泡含内容与时刻', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await mockFeelingLogs(page, [
    log({ replyContent: '束带再放松一格，两周后复评。', replyTime: '2026-09-24T02:15:00Z' }),
    log({ logId: '102', logDate: '2026-07-11', feeling: 'discomfort', notes: '下午左侧肩胛区域有压痛感。' }),
  ])
  await page.goto(FEELINGS_PATH)

  const cards = page.locator('.log-card')
  await expect(cards).toHaveCount(2)

  // 稿面 :64 日期画法 = 日期 + 周几
  await expect(cards.nth(0).locator('.log-date')).toHaveText('2026-07-12 周日')
  await expect(cards.nth(1).locator('.log-date')).toHaveText('2026-07-11 周六')

  await expect(cards.nth(0).locator('.log-tag-ok')).toHaveText('贴合')
  await expect(cards.nth(1).locator('.log-tag-warn')).toHaveText('不适')

  const bubble = cards.nth(0).locator('.reply-bubble')
  await expect(bubble).toContainText('束带再放松一格，两周后复评。')
  await expect(bubble.locator('.reply-time')).toHaveText(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/)

  // 无回复的第二条不得凭空长出气泡
  await expect(cards.nth(1).locator('.reply-bubble')).toHaveCount(0)
})

test('空态：接口回空数组时呈现空态而非伪造日志', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await mockFeelingLogs(page, [])
  await page.goto(FEELINGS_PATH)
  await expect(page.locator('.state-text')).toHaveText('暂无矫形日志')
  await expect(page.locator('.log-card')).toHaveCount(0)
})

test('新增：不动档位直接保存，写通道收到稿面默认档位 fitted 与 YYYY-MM-DD 日期', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  const posted = await mockFeelingLogs(page, [])
  await page.goto(FEELINGS_PATH)

  await page.locator('.add-btn').click()
  await expect(page.locator('.editor-card')).toBeVisible()
  await page.locator('.form-actions .btn-primary').click()

  await expect(toast(page, '日志已保存')).toBeVisible()
  await expect.poll(() => posted.length).toBe(1)
  expect(posted[0]).toEqual({
    feeling: 'fitted',
    discomfortAreas: [],
    notes: '',
    logDate: expect.any(String),
  })
  expect(String(posted[0].logDate)).toMatch(/^\d{4}-\d{2}-\d{2}$/)
})

test('新增：8 区点选与描述写进写通道（区名按稿面中文原词、顺序按点选序）', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  const posted = await mockFeelingLogs(page, [])
  await page.goto(FEELINGS_PATH)

  await page.locator('.add-btn').click()
  await expect(page.locator('.body-zone')).toHaveCount(8)
  await page.locator('.body-zone').filter({ hasText: '胸椎' }).click()
  await page.locator('.body-zone').filter({ hasText: '右侧腰' }).click()
  await expect(page.locator('.body-zone-active')).toHaveCount(2)
  // 再点一次取消选中（稿面 toggleZone）
  await page.locator('.body-zone').filter({ hasText: '右侧腰' }).click()
  await expect(page.locator('.body-zone-active')).toHaveCount(1)

  await page.locator('uni-textarea textarea').fill('胸椎处有压痛，已调整束带。')
  await page.locator('.form-actions .btn-primary').click()

  await expect.poll(() => posted.length).toBe(1)
  expect(posted[0].discomfortAreas).toEqual(['胸椎'])
  expect(posted[0].notes).toBe('胸椎处有压痛，已调整束带。')
})

test('查看弹层 → 编辑：该条日志的档位与部位回填后进写通道（同日覆盖即编辑语义）', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  const posted = await mockFeelingLogs(page, [
    log({ feeling: 'discomfort', discomfortAreas: ['胸椎', '左侧腰'], notes: '两点位压痛。' }),
  ])
  await page.goto(FEELINGS_PATH)

  await page.locator('.log-card').click()
  const sheet = page.locator('.bottom-sheet')
  await expect(sheet).toBeVisible()
  await expect(sheet).toContainText('2026-07-12 周日')
  await expect(sheet.locator('.view-value').first()).toHaveText('不适')
  await expect(sheet.locator('.view-value').nth(1)).toHaveText('胸椎、左侧腰')
  await expect(sheet.locator('.view-desc')).toHaveText('两点位压痛。')

  await sheet.locator('.sheet-actions .btn-primary').click()
  await expect(sheet).toHaveCount(0)
  await expect(page.locator('.editor-card')).toBeVisible()
  await expect(page.locator('.body-zone-active')).toHaveCount(2)
  await expect(page.locator('uni-textarea textarea')).toHaveValue('两点位压痛。')

  await page.locator('.form-actions .btn-primary').click()
  await expect.poll(() => posted.length).toBe(1)
  expect(posted[0]).toEqual({
    feeling: 'discomfort',
    discomfortAreas: ['胸椎', '左侧腰'],
    notes: '两点位压痛。',
    logDate: '2026-07-12',
  })
})

test('取消不进写通道：编辑器「取消」只回到列表，不发 POST', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  const posted = await mockFeelingLogs(page, [log({})])
  await page.goto(FEELINGS_PATH)

  await page.locator('.add-btn').click()
  await page.locator('.form-actions .btn-outline').click()
  await expect(page.locator('.editor-card')).toHaveCount(0)
  await expect(page.locator('.log-card')).toHaveCount(1)
  expect(posted).toHaveLength(0)
})
