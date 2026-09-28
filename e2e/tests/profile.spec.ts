import { test, expect } from '@playwright/test'
import { setupPatientE2E, ok } from '../helpers'

/**
 * T444 P-2 我的页编辑弹层：组标题跟稿面 profile.html:133「联系方式」，
 * 去掉「修前把整句当组标题 + 组标题与字段标签都写『紧急联系人』重字」两处缺陷。
 *
 * profile 不在 helpers.routes（历史只覆盖 tab 主链路），且 GET /api/v1/patient/profile
 * 未被 setupPatientE2E mock ⇒ 本 spec 自己补该路由（LIFO：后注册先咨询）。
 * 只做 H5 渲染实测，小程序端不冒充已验。
 */
const PROFILE_PATH = '/#/pages/profile/index'
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
  teamName: '脊柱侧弯矫正一组',
  doctorName: '王医生',
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
