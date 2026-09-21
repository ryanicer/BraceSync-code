import { test, expect, type Page } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption } from '../admin-helpers'

/**
 * T301 G1 补齐：复查记录页 + 复查模板页（此前只被 admin-permissions 的 goto 判不 403 覆盖）
 *
 * 🔴 范围边界（实测得出，不是偷懒）：
 * - `fetchReviewRecords` / `fetchReviewTemplates` 在 USE_MOCK 下**恒返回空数组**
 *   （apps/admin-web/src/api/index.ts:558-575），所以这两页的「列表有数据」断言在 mock 基座下不可能成立；
 * - `uploadFileDirect`（api/index.ts:504-514）**没有 USE_MOCK 分支**，会对
 *   `https://mock-cos.example.com/...` 发真实 PUT ⇒ mock 下探到合法文件后既无成功也无失败提示
 *   （实测：选 .pdf 等 4s，`.el-message` 为空、「已上传」tag 数 0）。
 * ⇒ 本 spec 只覆盖「空态 / 表单门控 / 客户端白名单」这三类 mock 下真有判据的行为；
 *   列表与上传成功链路的缺口登记在 T301 覆盖矩阵 G2（真实模式）里，不在此伪装覆盖。
 */

const uploadInput = (page: Page) => page.locator('input.el-upload__input')

async function pickPatient(page: Page, name: string): Promise<void> {
  await pickSelectOption(page, page.locator('.patient-select'), name)
}

test.describe('复查记录页（review-records）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.reviewRecords)
  })

  test('未选患者：只有占位空态，不渲染上传入口', async ({ page }) => {
    await expect(page.locator('.el-empty')).toContainText('请选择患者开始上传复查报告')
    await expect(uploadInput(page)).toHaveCount(0)
  })

  test('选中患者后出现表单，历史列表为空态', async ({ page }) => {
    await pickPatient(page, '林小雨')
    await expect(page.getByRole('button', { name: '提交复查记录' })).toBeVisible()
    await expect(page.locator('.page-card-title').nth(1)).toHaveText('历史复查记录（0）')
    await expect(page.locator('.el-empty')).toContainText('暂无复查记录')
  })

  test('提交按钮门控：无复查日期禁用，填日期后可提交', async ({ page }) => {
    await pickPatient(page, '林小雨')
    const submit = page.getByRole('button', { name: '提交复查记录' })
    await expect(submit).toBeDisabled()
    const dateInput = page.locator('.review-form input').first()
    await dateInput.fill('2026-09-22')
    await dateInput.press('Enter')
    await expect(submit).toBeEnabled()
  })

  test('报告文件白名单在前端拦截：.exe 报错且不打上传', async ({ page }) => {
    await pickPatient(page, '林小雨')
    await uploadInput(page).setInputFiles({
      name: 'setup.exe',
      mimeType: 'application/x-msdownload',
      buffer: Buffer.from('MZ'),
    })
    await expect(adminMessage(page)).toContainText('不支持的文件类型：.exe')
    await expect(page.locator('.el-tag', { hasText: '已上传' })).toHaveCount(0)
  })
})

test.describe('复查模板页（review-templates）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.reviewTemplates)
  })

  test('模板列表恒为空态（mock 返回空数组）', async ({ page }) => {
    await expect(page.locator('.page-card-title').nth(1)).toHaveText('复查报告模板列表（0）')
    await expect(page.locator('.el-empty')).toContainText('暂无模板，请先上传')
  })

  test('未填模板名称时上传按钮禁用', async ({ page }) => {
    const submit = page.getByRole('button', { name: '上传模板' })
    await expect(submit).toBeVisible()
    await expect(submit).toBeDisabled()
  })

  test('模板文件同样走白名单：.exe 被拒', async ({ page }) => {
    await uploadInput(page).setInputFiles({
      name: 'evil.exe',
      mimeType: 'application/x-msdownload',
      buffer: Buffer.from('MZ'),
    })
    await expect(adminMessage(page)).toContainText('不支持的文件类型：.exe')
    await expect(page.getByRole('button', { name: '上传模板' })).toBeDisabled()
  })
})
