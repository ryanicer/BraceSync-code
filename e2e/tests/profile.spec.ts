import { test, expect } from '@playwright/test'
import { setupPatientE2E, ok } from '../helpers'

/**
 * T444 P-2 我的页编辑弹层：组标题跟稿面 profile.html:133「联系方式」，
 * 去掉「修前把整句当组标题 + 组标题与字段标签都写『紧急联系人』重字」两处缺陷。
 *
 * profile 不在 helpers.routes（历史只覆盖 tab 主链路）；setupPatientE2E 给
 * GET /api/v1/patient/profile 挂的是 T579 时长桩（只有 dailyWearTargetHours），
 * 本 spec 在其之后注册自己的路由把它盖掉（LIFO：后注册先咨询）。
 * 只做 H5 渲染实测，小程序端不冒充已验。
 */
const PROFILE_PATH = '/#/pages/profile/index'
/**
 * T646 后的患者侧载荷形状（model.PatientSelfDTO）：没有医护姓名那两枚键。
 * 下面第一条用例喂的就是这一枚，第二条用例故意喂一枚「旧形状 + 姓名」的回退载荷，
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
 * T646（依 T637 设计稿 §八）：这一条故意喂「旧形状」载荷 —— 服务端若回退成带医护姓名两枚键，
 * 页面也不许把它们渲染出来。正对照是同一次渲染里患者本人的姓名照常出现，
 * 证明「页面上查不到那两枚字串」不是页面整块没出数据。
 */
test('患者侧载荷即使带回医护姓名两枚键，「我的」页也不渲染医护姓名', async ({ page }) => {
  // LIFO：后注册的路由先命中，所以这一枚盖掉 beforeEach 里那份干净载荷。
  // 计数器是这条用例自己的在场尺：served 为 0 就说明页面压根没取走脏载荷，
  // 下面两枚负断言扫的是空面（假绿），必须先红在这里。
  let served = 0
  await page.route(/\/api\/v1\/patient\/profile$/, (route) => {
    served += 1
    // FULL_PROFILE 本身已是网关信封，这里只剥 data 再重新包一层（直接 ok(FULL_PROFILE) 是双层信封）
    return route.fulfill({ json: ok({ ...FULL_PROFILE.data, teamName: '脊柱侧弯矫正一组', doctorName: '王医生' }) })
  })
  // goto 同一枚 hash URL 不会重启动文档，也就不会再发那一发请求 ⇒ 用 reload 强制重跑 onShow(loadProfile)
  await page.reload()
  await expect(page.locator('.profile-card')).toBeVisible()
  expect(served, '脏载荷必须真被页面取走过至少一次').toBeGreaterThan(0)

  await expect(page.locator('.profile-name'), '正对照：本人姓名照常渲染，页面不是空数据').toHaveText('张晓宇')
  const body = await page.locator('body').innerText()
  expect(body).not.toContain('王医生')
  expect(body).not.toContain('脊柱侧弯矫正一组')

  // 副文案只说绑定状态（teamId 在场 ⇒ 已绑定）
  await expect(page.locator('.menu-sub').first()).toHaveText('已绑定')
})
