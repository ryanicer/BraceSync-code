import { test, expect } from '@playwright/test'
import { techRoutes, forceTechLoginMock, fillTechInput, uniToast, MOCK_TECH_TOKEN } from '../tech-helpers'

/**
 * T486 技师自助改密页（H5 + mock 通道）
 *
 * 判据分工（为什么这一腿要存在，而不是只靠 vitest）：
 *  - 后端「旧密码错必拒 / 强度规则」在 Go 侧（technician_self_password_t486_test.go）；
 *  - 「填错原密码不许被踢回登录页」在传输层（apps/tech-miniapp/test/change-password.spec.ts）；
 *  - 本腿只看浏览器里的真形状：入口可达、三格默认掩码、非法口令拦在页内、
 *    成功之后清凭据并落回登录页。这几格只有真渲染才看得到。
 * mock 通道下 changeTechPassword 必定成功，所以「旧密码错」这一格不在此处，别把它当验过了。
 */

const PASSWORD_URL = '/#/pages/password/index'
const RULE_HINT = '口令需为 6-16 位，包含字母和数字，不支持中文或空格'

test.describe('已登录（forceTechLoginMock 注入 token）', () => {
  test.beforeEach(async ({ page }) => {
    await forceTechLoginMock(page)
  })

  test('首页有「修改密码」入口，点进去是三格口令输入 + 规则提示', async ({ page }) => {
    await page.goto(techRoutes.home)
    await page.locator('.account-row').click()
    await expect(page).toHaveURL(/pages\/password\/index/)
    await expect(page.locator('.intro-title')).toHaveText('修改登录密码')
    await expect(page.locator('.input-group')).toHaveCount(3)
    await expect(page.locator('.rule-hint')).toHaveText(RULE_HINT)
  })

  test('三格口令默认掩码，点眼睛才转明文（真机上旁人看得见输的是什么）', async ({ page }) => {
    await page.goto(PASSWORD_URL)
    const inputs = page.locator('.input-group input')
    await expect(inputs).toHaveCount(3)
    for (let i = 0; i < 3; i++) {
      await expect(inputs.nth(i)).toHaveAttribute('type', 'password')
    }
    await page.locator('.pwd-toggle').first().click()
    await expect(inputs.first()).toHaveAttribute('type', 'text')
    await page.locator('.pwd-toggle').first().click()
    await expect(inputs.first()).toHaveAttribute('type', 'password')
  })

  test('新密码不合规则时拦在页内：行内出提示句，不提交、不落回登录页', async ({ page }) => {
    await page.goto(PASSWORD_URL)
    const inputs = page.locator('.input-group input')
    await fillTechInput(inputs.nth(0), 'old123456')
    await fillTechInput(inputs.nth(1), 'abcdefg') // 纯字母 ⇒ 缺数字
    await fillTechInput(inputs.nth(2), 'abcdefg')
    await page.locator('.btn-primary').click()

    await expect(page.locator('.field-error')).toHaveText(RULE_HINT)
    await expect(uniToast(page, '密码修改成功')).toHaveCount(0)
    await expect(page).toHaveURL(/pages\/password\/index/)
    // 未提交 ⇒ 会话仍在（旧 token 没被清、页面没被踢走）
    expect(await page.evaluate(() => localStorage.getItem('bracesync_tech_token'))).toBe(MOCK_TECH_TOKEN)
  })

  test('两次输入不一致：提示挂在确认格，新密码那一格不报错', async ({ page }) => {
    await page.goto(PASSWORD_URL)
    const inputs = page.locator('.input-group input')
    await fillTechInput(inputs.nth(0), 'old123456')
    await fillTechInput(inputs.nth(1), 'ab1def')
    await fillTechInput(inputs.nth(2), 'ab1deg')
    await page.locator('.btn-primary').click()

    await expect(page.locator('.field-error')).toHaveCount(1)
    await expect(page.locator('.field-error')).toHaveText('两次输入的新密码不一致')
  })

  test('改密成功：toast 说明要重新登录，凭据即清并落回登录页', async ({ page }) => {
    await page.goto(PASSWORD_URL)
    const inputs = page.locator('.input-group input')
    await fillTechInput(inputs.nth(0), 'old123456')
    await fillTechInput(inputs.nth(1), 'ab1def')
    await fillTechInput(inputs.nth(2), 'ab1def')
    await page.locator('.btn-primary').click()

    await expect(uniToast(page, '密码修改成功，请重新登录')).toBeVisible()
    // 口令已换 ⇒ 旧 token 立即作废，页面 1.5s 后 reLaunch 到登录页
    await expect(page.locator('.agree-row')).toBeVisible({ timeout: 10_000 })
    expect(await page.evaluate(() => localStorage.getItem('bracesync_tech_token'))).toBeNull()
  })
})

test.describe('未登录（不注入 token：addInitScript 会在每次导航前重放，清了也会回来）', () => {
  test('直达改密页被守卫送回登录页（页面自身不假设已登录）', async ({ page }) => {
    await page.goto(PASSWORD_URL)
    await expect(page.locator('.agree-row')).toBeVisible({ timeout: 10_000 })
    expect(await page.evaluate(() => localStorage.getItem('bracesync_tech_token'))).toBeNull()
  })
})
