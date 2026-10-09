import { test, expect } from '@playwright/test'
import { setupPatientE2E, ok } from '../helpers'

/**
 * T444 P-2 我的页编辑弹层：组标题跟稿面 profile.html:133「联系方式」，
 * 去掉「修前把整句当组标题 + 组标题与字段标签都写『紧急联系人』重字」两处缺陷。
 *
 * setupPatientE2E 给 GET /api/v1/patient/profile 挂的是 T579 那枚时长桩（只回 dailyWearTargetHours），
 * 本 spec 在其之后注册自己的路由把它盖掉（LIFO：后注册先咨询）。
 * 只做 H5 渲染实测，小程序端不冒充已验。
 */
const PROFILE_PATH = '/#/pages/profile/index'
/**
 * T646 裁定甲（卡评论 4865，weide-duty 2026-10-09 22:52）后的患者侧载荷形状：
 * teamName / doctorName 两枚键在场、值恒 null —— 不是键不在场（那是乙，已与本卡解耦）。
 * 下面第一条用例喂的就是这一枚甲面载荷；第二条用例故意喂一枚「键在且值非 null」的回退载荷，
 * 用来证明页面不再摸那两枚键（而不是靠服务端不出数据才碰巧不显示）。
 */
const FULL_PROFILE = ok({
  patientId: 'pat-e2e-001',
  name: '张晓宇',
  gender: 'female',
  age: 14,
  diagnosis: null,
  cobbAngle: 28,
  deviceId: 'DEV-E2E',
  teamId: 't1',
  doctorId: 'd1',
  phone: '',
  status: 'active',
  createdAt: '2026-05-01T00:00:00Z',
  updatedAt: '2026-05-01T00:00:00Z',
  teamName: null,
  doctorName: null,
  heightCm: 158,
  weightKg: 46,
  emergencyContactName: '张建国',
  emergencyContactPhone: '13987654321',
  emergencyContactRelation: '父亲',
})

test.beforeEach(async ({ page }) => {
  await setupPatientE2E(page, { withLogin: true })
  await page.route(/\/api\/v1\/patient\/profile$/, (route) => route.fulfill({ json: FULL_PROFILE }))
  await page.goto(PROFILE_PATH)
  await expect(page.locator('.profile-card')).toBeVisible()
})

test('编辑弹层组标题为「联系方式」，且弹层内组标题只剩这一处', async ({ page }) => {
  await page.locator('.profile-edit').click()
  const sheet = page.locator('.bottom-sheet', { hasText: '编辑个人信息' })
  await expect(sheet).toBeVisible()

  const sectionLabels = sheet.locator('.form-section-label')
  await expect(sectionLabels).toHaveCount(1)
  await expect(sectionLabels.first()).toHaveText('联系方式')

  // 修前的两处缺陷写法不得再现
  await expect(sectionLabels.first()).not.toHaveText('紧急联系人')
  await expect(sectionLabels.first()).not.toHaveText(/手机号由微信授权/)

  // 「紧急联系人」只作为字段标签（form-label）出现，不是组标题
  await expect(sheet.locator('.form-label', { hasText: '紧急联系人' }).first()).toBeVisible()
})

/**
 * T646 裁定甲（依 T637 设计稿 八）：这一条故意喂「回退载荷」—— 键在场且医护姓名两枚值非 null
 * （服务端收口失效、或后台侧装配点被误用于患者侧时的形状），页面也不许把它们渲染出来。
 * 正对照押在同一次渲染里：患者本人的姓名照常出现，且 body 整面兜得住这枚字串，
 * 证明「body 里查不到那两枚字串」不是页面整块没出数据、也不是读错了面。
 */
test('患者侧载荷即使带回医护姓名两枚字串，「我的」页也不渲染医护姓名', async ({ page }) => {
  let served = 0
  // LIFO：后注册的路由先命中，所以这一枚盖掉 beforeEach 里那份甲面载荷
  await page.route(/\/api\/v1\/patient\/profile$/, (route) => {
    served += 1
    // FULL_PROFILE 本身已是网关信封，这里只剥 data 再重新包一层（直接 ok(FULL_PROFILE) 是双层信封）
    return route.fulfill({ json: ok({ ...FULL_PROFILE.data, teamName: '脊柱侧弯矫正一组', doctorName: '王医生' }) })
  })
  await page.reload()
  await expect(page.locator('.profile-card')).toBeVisible()
  expect(served, '回退载荷必须真被页面取走过至少一次').toBeGreaterThan(0)

  // 正对照①：本人姓名落在渲染位上；正对照②：同一枚字串在 body 那一面兜得住
  await expect(page.locator('.profile-name'), '正对照：本人姓名照常渲染，页面不是空数据').toHaveText('张晓宇')
  const body = await page.locator('body').innerText()
  expect(body, '正对照：读数面确实含本人姓名，不是空面假绿').toContain('张晓宇')

  expect(body).not.toContain('王医生')
  expect(body).not.toContain('脊柱侧弯矫正一组')

  // 副文案只说绑定状态（teamId 在场 ⇒ 已绑定）
  await expect(page.locator('.menu-sub').first()).toHaveText('已绑定')
})
