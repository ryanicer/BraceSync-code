import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption } from '../admin-helpers'
import { REVIEW_REPORT_MAX_BYTES } from '../../apps/admin-web/src/utils/review-report-whitelist'

/**
 * T309：复查记录页的 20MB 前端预校验
 *
 * 修复前该页 validateFile 只判白名单（扩展名 + MIME），超限文件会先跑完 presign 与
 * 文件直传、再由后端 upload-complete 拒（同域模板页早已预拦）⇒ 本页缺的是对称的那一步。
 *
 * 判据取「文案」而非「网络请求」：mock 基座下 uploadFileDirect 的行为由 api 层决定，
 * 用请求计数会随夹具改动失真；而 size 文案只在预拦分支出现，修复前整条链路都不会打出它。
 * 边界值（恰好 20MB 放行）已由 apps/admin-web/test/review-report-whitelist.spec.ts:84 覆盖，
 * 此处不重复。
 *
 * 单独成文（不写进 admin-review.spec.ts）：Ella 的 PR #165 正在重写那个文件，避免撞车。
 */

test.describe('复查记录页（review-records）· 文件大小上限', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.reviewRecords)
  })

  test('报告超过 20MB：前端预拒并给出 R4-b 固定文案，不进入上传链路', async ({ page }) => {
    await pickSelectOption(page, page.locator('.patient-select'), '林小雨')
    const uploadInput = page.locator('input.el-upload__input')
    await expect(uploadInput).toHaveCount(1)

    await uploadInput.setInputFiles({
      name: 'report-oversize.pdf',
      mimeType: 'application/pdf',
      // 扩展名与 MIME 都合法，唯一违例的是体积（PRD 固定文案，故此处不引用常量拼文案）
      buffer: Buffer.alloc(REVIEW_REPORT_MAX_BYTES + 1, 0x25),
    })

    await expect(adminMessage(page)).toContainText('文件大小超过 20MB，请压缩后重试')
    // 未进入上传链路 ⇒ 既没有「已上传」标记，选择按钮也不回显该文件名
    await expect(page.locator('.el-tag', { hasText: '已上传' })).toHaveCount(0)
    await expect(page.locator('.el-upload .el-button')).toContainText('选择文件')
  })
})
