import { expect, test, type Page } from '@playwright/test'
import { ok, setupPatientE2E } from '../helpers'
import type { Advice, CareTeamMember } from '@bracesync/shared-types'

/**
 * T641 患者端「康复建议」页 e2e（Playwright H5，route mock）
 *
 * 两枚读口都不在 helpers.setupPatientE2E 的历史路由表内（那批只覆盖 tab 主链路），
 * 按 profile.spec.ts / feelings.spec.ts 的先例在本 spec 自行注册（LIFO：后注册先咨询）。
 *
 * 这三格为什么值得开浏览器：单测那层（tests/unit/advice-t641.spec.ts）只能读源码文本，
 * 「患者屏幕上到底有没有出现医护姓名」「这一页有没有发过写请求」是渲染态与网络态的判据，
 * 源码里没那个词不等于屏上没有那个词。
 *
 * 选择器：业务代码不加 data-testid（红线），一律 class + 文案定位（同 e2e/helpers.ts 约定）。
 */
const ADVICE_PATH = '/#/pages/advice/index'

function advice(over: Partial<Advice>): Advice {
  return {
    adviceId: 'ADV-001',
    patientId: 'pat-e2e-001',
    title: '主治医师',
    content: '这两周胸段压力偏高，夜间请保持 22 小时以上；洗澡后皮肤发干，先涂润肤再戴。',
    createdAt: '2026-10-08T06:20:00Z',
    updatedAt: '2026-10-08T06:20:00Z',
    editable: false,
    ...over,
  }
}

const TEAM: CareTeamMember[] = [
  { memberType: 'doctor', title: '主治医师' },
  { memberType: 'doctor', title: '护士' },
  { memberType: 'technician', title: '技术支撑' },
]

/** 注册两枚读口；adviceList 为 GET 返回体 */
async function mockAdviceReads(page: Page, adviceList: Advice[], team: CareTeamMember[] = TEAM) {
  await page.route(/\/api\/v1\/patients\/[^/]+\/advice$/, (route) =>
    route.fulfill({ json: ok(adviceList) }))
  await page.route(/\/api\/v1\/patient\/care-team$/, (route) => route.fulfill({ json: ok(team) }))
}

/** 收本页发出的所有 /advice 与 /care-team 请求的方法与 URL（零写判据取证面） */
function recordAdviceRequests(page: Page): string[] {
  const seen: string[] = []
  page.on('request', (req) => {
    if (/\/(advice|care-team)$/.test(new URL(req.url()).pathname)) {
      seen.push(`${req.method()} ${new URL(req.url()).pathname}`)
    }
  })
  return seen
}

test('T659 按稿：团队横排头像+职称、时间轴日期行头、展开控件恒显与全文切换', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await mockAdviceReads(page, [
    advice({}),
    advice({
      adviceId: 'ADV-002',
      title: '护士',
      content: `复查提醒：10 月 16 日上午门诊复查，请携带支具与近 7 日佩戴记录。${'补充说明用于把正文推过两行摘要截断，从而展开后能看到被收住的后半段文字。'.repeat(3)}`,
    }),
    advice({ adviceId: 'ADV-003', title: '医护团队', content: '调整方案后无不适，继续观察一周。' }),
  ])
  await page.goto(ADVICE_PATH)

  // T659：横排头像 strip（稿面 advice.html:45-58），无角色文字胶囊
  const members = page.locator('.team-strip .team-member')
  await expect(members).toHaveCount(3)
  await expect(members.nth(0).locator('.team-avatar-doctor')).toBeVisible()
  await expect(members.nth(0).locator('.team-title')).toHaveText('主治医师')
  await expect(members.nth(2).locator('.team-avatar-tech')).toBeVisible()
  // 技师职称恒为「技术支撑」，不再折成破折号（Boss 截图回归点）
  await expect(members.nth(2).locator('.team-title')).toHaveText('技术支撑')
  await expect(page.locator('.team-role, .team-title-empty')).toHaveCount(0)

  const items = page.locator('.advice-item')
  await expect(items).toHaveCount(3)
  // 行头只到日期（稿面 :72 形 2026-10-09 · 主治医师），不带时分
  await expect(items.nth(0).locator('.advice-meta')).toHaveText(/^\d{4}-\d{2}-\d{2} · 主治医师$/)
  await expect(items.nth(0).locator('.advice-summary')).toContainText('这两周胸段压力偏高')
  // T659：稿面四条 seed（含短正文）均恒显展开控件
  await expect(items.nth(0).locator('.advice-toggle')).toHaveText('展开全文 ▾')
  await expect(items.nth(2).locator('.advice-toggle')).toHaveText('展开全文 ▾')

  const longItem = items.nth(1)
  await expect(longItem.locator('.advice-toggle')).toHaveText('展开全文 ▾')
  await longItem.click()
  await expect(longItem.locator('.advice-toggle')).toHaveText('收起 ▴')
  await expect(longItem.locator('.advice-full')).toContainText('被收住的后半段')
  await longItem.click()
  await expect(longItem.locator('.advice-full')).toHaveCount(0)
})

test('🔴 页面上不出现医护姓名，且本页一次写请求都不发（留言板 = 患者零写操作）', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  // 夹具故意在响应体里多塞两枚档案里才有的姓名键：前端若照着旧副文案的画法读未知键，
  // 姓名就会上屏——这一格钉的正是「新页不消费姓名字段」，而不只是「后端没下发」
  await page.route(/\/api\/v1\/patients\/[^/]+\/advice$/, (route) =>
    route.fulfill({
      json: {
        code: 0,
        message: 'ok',
        data: [advice({})],
        doctorName: '王建国',
        teamName: '脊柱侧弯矫正一组',
      },
    }))
  await page.route(/\/api\/v1\/patient\/care-team$/, (route) =>
    route.fulfill({
      json: {
        code: 0,
        message: 'ok',
        data: [{ memberType: 'doctor', title: '主治医师', name: '李静', phone: '13900000000' }],
      },
    }))

  const requests = recordAdviceRequests(page)
  await page.goto(ADVICE_PATH)
  await expect(page.locator('.advice-item')).toHaveCount(1)

  const bodyText = await page.locator('body').innerText()
  for (const leak of ['王建国', '脊柱侧弯矫正一组', '李静', '13900000000']) {
    expect(bodyText, `患者屏幕不应出现 ${leak}`).not.toContain(leak)
  }

  // 两枚读口各一发，方法全 GET；写面（POST/PUT/DELETE）与「已读回执」类请求一次都没有
  await expect.poll(() => requests.length).toBe(2)
  expect(requests.every((r) => r.startsWith('GET '))).toBe(true)
  expect(requests.some((r) => /\/api\/v1\/patient\/care-team$/.test(r))).toBe(true)

  // 全页零输入控件、零按钮（稿面「零交互」那一格的渲染态读数；展开切换是文本点击，不是表单元素）
  await expect(page.locator('input, textarea, uni-input, uni-textarea, uni-button, button')).toHaveCount(0)
})

test('空态与错误态各归各位：空数组说「暂无」，500 不把失败画成空列表', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await mockAdviceReads(page, [], [])
  await page.goto(ADVICE_PATH)
  await expect(page.locator('.state-text').first()).toHaveText('尚未绑定医护团队')
  await expect(page.locator('.advice-item')).toHaveCount(0)
  await expect(page.locator('.state-text').nth(1)).toHaveText('暂无医护建议')

  await page.route(/\/api\/v1\/patients\/[^/]+\/advice$/, (route) =>
    route.fulfill({ status: 500, json: { code: 50000, message: 'internal', data: null } }))
  await page.goto(ADVICE_PATH)
  await expect(page.locator('.state-text').filter({ hasText: '加载医护建议失败' })).toHaveCount(1)
  // 「点击重试」全场只有一枚：只有读失败的那一格才挂重试（团队那一格此时是空态卡，没有重试）
  await expect(page.locator('.state-sub').filter({ hasText: '点击重试' })).toHaveCount(1)
})

test('未登录（storage 无患者号）不拿空 patientId 去打接口', async ({ page }) => {
  await setupPatientE2E(page, { withLogin: false })
  const requests = recordAdviceRequests(page)
  await page.goto(ADVICE_PATH)
  await expect(page.locator('.state-text').first()).toHaveText('请先登录')
  expect(requests, '未登录时两枚读口都不该发出去（发了只吃到网关 401/403）').toHaveLength(0)
})
