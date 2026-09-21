import { test, expect, type Page, type Locator } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * T301 G1 补齐、T307 G11 收口：复查记录页 + 复查模板页
 *
 * 两态由 URL query 控制（T307 建的 mock 夹具，缺省有数据，empty 为强制空）：
 * - ?mock_review=data  ⇒ src/mock/review.ts 的预置记录/模板（PT-001 两条、PT-002 一条、模板两条）
 * - ?mock_review=empty ⇒ 两个列表接口恒回空数组，用于验空态占位
 * 每条用例都显式声明所依赖的状态，不依赖夹具默认值。
 *
 * 🔴 边界：本 spec 全在 mock 模式下，绿只说明「前端消费与 mock 契约一致」，
 *   不构成真实模式（VITE_USE_MOCK=false）证据 —— 真实链路缺口见 T301 覆盖矩阵 G2 与 T304。
 */

type MockState = 'data' | 'empty'

const goPage = (page: Page, path: string, state: MockState) => page.goto(`${path}?mock_review=${state}`)

const uploadInput = (page: Page) => page.locator('input.el-upload__input')

/** 列表卡片（两个页面都是第 2 张 page-card：第 1 张是上传区） */
const listCard = (page: Page): Locator => page.locator('.page-card').nth(1)

const listTitle = (page: Page): Locator => listCard(page).locator('.page-card-title')

const rowsIn = (page: Page): Locator => tableRows(page, listCard(page))

const uploadedTag = (page: Page): Locator => page.locator('.el-tag', { hasText: '已上传' })

async function pickPatient(page: Page, name: string): Promise<void> {
  await pickSelectOption(page, page.locator('.patient-select'), name)
}

/** 选一个白名单内文件并等 mock 直传链路走完（presign ⇒ 直传 ⇒ complete） */
async function uploadReportFile(page: Page, fileName: string): Promise<void> {
  await uploadInput(page).setInputFiles({
    name: fileName,
    mimeType: 'application/pdf',
    buffer: Buffer.from('%PDF-1.4 e2e fixture'),
  })
  await expect(adminMessage(page)).toContainText('文件上传成功')
  await expect(uploadedTag(page)).toHaveCount(1)
}

test.describe('复查记录页（review-records）· 有数据态', () => {
  // 本页 validateFile 只调 validateReviewReportFile（扩展名 + MIME），没有模板页那句
  // checkReviewReportFileSize ⇒ 20MB 前端预校验在本页缺失，只能靠后端 upload-complete 兜底。
  // 属产品代码不对称（T307 红线禁改 pages 下文件），已按缺陷在卡内登记，故此处不补对应用例。
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await goPage(page, adminRoutes.reviewRecords, 'data')
  })

  test('选中患者：按 patientId 过滤渲染夹具记录，计数标题与行数一致', async ({ page }) => {
    await pickPatient(page, '林小雨')
    await expect(listTitle(page)).toHaveText('历史复查记录（2）')
    await expect(rowsIn(page)).toHaveCount(2)
    // 夹具字段逐列核到页面：日期 / 类型中文映射 / 检查所见 / 下次复查 / 报告文件名
    const first = rowsIn(page).filter({ hasText: '2026-06-12' })
    await expect(first).toHaveCount(1)
    await expect(first).toContainText('初诊')
    await expect(first).toContainText('Cobb 28°')
    await expect(first).toContainText('2026-09-10')
    await expect(first.locator('td').nth(4)).toHaveText('林小雨-初诊报告.pdf')
    await expect(first.getByRole('button', { name: '下载' })).toHaveCount(1)
  })

  test('无报告文件的记录：文件列显示「无」且不渲染下载入口', async ({ page }) => {
    await pickPatient(page, '林小雨')
    const row = rowsIn(page).filter({ hasText: '2026-09-15' })
    await expect(row).toHaveCount(1)
    await expect(row.locator('td').nth(4)).toHaveText('无')
    await expect(row.getByRole('button', { name: '下载' })).toHaveCount(0)
  })

  test('夹具内无记录的患者：同页回落空态（证明列表按患者过滤，不是全局常量）', async ({ page }) => {
    await pickPatient(page, '王梓萌')
    await expect(listTitle(page)).toHaveText('历史复查记录（0）')
    await expect(rowsIn(page)).toHaveCount(0)
    await expect(listCard(page).locator('.el-empty')).toContainText('暂无复查记录')
  })

  test('上传报告成功后回填列表：新记录带文件名并出现下载入口', async ({ page }) => {
    await pickPatient(page, '陈子航')
    await expect(listTitle(page)).toHaveText('历史复查记录（1）')
    const dateInput = page.locator('.review-form input').first()
    await dateInput.fill('2026-09-20')
    await dateInput.press('Enter')
    await uploadReportFile(page, '陈子航-复诊报告.pdf')
    await page.getByRole('button', { name: '提交复查记录' }).click()
    await expect(adminMessage(page)).toContainText('复查记录已提交')
    await expect(listTitle(page)).toHaveText('历史复查记录（2）')
    const row = rowsIn(page).filter({ hasText: '2026-09-20' })
    await expect(row).toHaveCount(1)
    await expect(row.locator('td').nth(4)).toHaveText('陈子航-复诊报告.pdf')
    await expect(row.getByRole('button', { name: '下载' })).toHaveCount(1)
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
    const before = await rowsIn(page).count()
    await uploadInput(page).setInputFiles({
      name: 'setup.exe',
      mimeType: 'application/x-msdownload',
      buffer: Buffer.from('MZ'),
    })
    await expect(adminMessage(page)).toContainText('不支持的文件类型：.exe')
    await expect(uploadedTag(page)).toHaveCount(0)
    // 拦截必须真的没打上传：列表不被写入
    await expect(rowsIn(page)).toHaveCount(before)
  })
})

test.describe('复查记录页（review-records）· 空态', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await goPage(page, adminRoutes.reviewRecords, 'empty')
  })

  test('未选患者：只有占位空态，不渲染上传入口', async ({ page }) => {
    await expect(page.locator('.el-empty')).toContainText('请选择患者开始上传复查报告')
    await expect(uploadInput(page)).toHaveCount(0)
  })

  test('夹具置空后选中患者：出现表单，历史列表为空态', async ({ page }) => {
    await pickPatient(page, '林小雨')
    await expect(page.getByRole('button', { name: '提交复查记录' })).toBeVisible()
    await expect(listTitle(page)).toHaveText('历史复查记录（0）')
    await expect(rowsIn(page)).toHaveCount(0)
    await expect(listCard(page).locator('.el-empty')).toContainText('暂无复查记录')
  })
})

test.describe('复查模板页（review-templates）· 有数据态', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await goPage(page, adminRoutes.reviewTemplates, 'data')
  })

  test('模板列表渲染夹具：名称 / 版本 / 上传时间 / 上传人（PRD §7A.11.y:734 四要素）', async ({ page }) => {
    await expect(listTitle(page)).toHaveText('复查报告模板列表（2）')
    await expect(rowsIn(page)).toHaveCount(2)
    const v3 = rowsIn(page).filter({ hasText: '脊柱侧弯复查报告模板（成人）' })
    await expect(v3.locator('td').nth(1)).toHaveText('v3')
    await expect(v3.locator('td').nth(2)).toHaveText('2026-09-01')
    await expect(v3.locator('td').nth(3)).toHaveText('ADMIN')
    await expect(v3.locator('td').nth(4)).toHaveText('脊柱侧弯复查报告模板-v3.docx')
    await expect(v3.getByRole('button', { name: '下载' })).toHaveCount(1)
    // TPL-0002 夹具刻意 fileName 为 null ⇒ 页面回落到 fileId，且无 downloadUrl 时不渲染下载
    const v1 = rowsIn(page).filter({ hasText: '儿童矫形复查记录表' })
    await expect(v1.locator('td').nth(4)).toHaveText('FILE-T0002')
    await expect(v1.getByRole('button', { name: '下载' })).toHaveCount(0)
    await expect(rowsIn(page).getByRole('button', { name: '版本替换' })).toHaveCount(2)
  })

  test('上传新模板成功后回填列表：新增一行 v1 且带下载入口', async ({ page }) => {
    await page.locator('.upload-form input').first().fill('E2E 新模板')
    await uploadReportFile(page, 'e2e-template-v1.pdf')
    await page.getByRole('button', { name: '上传模板' }).click()
    await expect(adminMessage(page)).toContainText('模板「E2E 新模板」上传成功')
    await expect(listTitle(page)).toHaveText('复查报告模板列表（3）')
    const row = rowsIn(page).filter({ hasText: 'E2E 新模板' })
    await expect(row.locator('td').nth(1)).toHaveText('v1')
    await expect(row.locator('td').nth(4)).toHaveText('e2e-template-v1.pdf')
    await expect(row.getByRole('button', { name: '下载' })).toHaveCount(1)
  })

  test('版本替换后回填：旧版 retired 不显示，同组行版本递增、列表条数不变', async ({ page }) => {
    await rowsIn(page).filter({ hasText: '脊柱侧弯复查报告模板（成人）' })
      .getByRole('button', { name: '版本替换' }).click()
    await expect(page.locator('.page-card-title').first()).toHaveText('版本替换：脊柱侧弯复查报告模板（成人）')
    await uploadReportFile(page, 'e2e-template-v4.pdf')
    await page.getByRole('button', { name: '确认替换（旧版本将标记 retired）' }).click()
    await expect(adminMessage(page)).toContainText('模板版本替换成功，旧版本已标记 retired')
    await expect(listTitle(page)).toHaveText('复查报告模板列表（2）')
    const row = rowsIn(page).filter({ hasText: '脊柱侧弯复查报告模板（成人）' })
    await expect(row).toHaveCount(1)
    await expect(row.locator('td').nth(1)).toHaveText('v4')
    await expect(row.locator('td').nth(4)).toHaveText('e2e-template-v4.pdf')
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

  test('模板超过 20MB 上限：前端预拒、不进入上传链路（PRD §7A.11.y:742 R4-b）', async ({ page }) => {
    await page.locator('.upload-form input').first().fill('超大模板')
    await uploadInput(page).setInputFiles({
      name: 'oversize.pdf',
      mimeType: 'application/pdf',
      buffer: Buffer.alloc(21 * 1024 * 1024, 0x25),
    })
    await expect(adminMessage(page)).toContainText('文件大小超过 20MB，请压缩后重试')
    await expect(uploadedTag(page)).toHaveCount(0)
    await expect(page.getByRole('button', { name: '上传模板' })).toBeDisabled()
    await expect(rowsIn(page)).toHaveCount(2)
  })
})

test.describe('复查模板页（review-templates）· 空态', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await goPage(page, adminRoutes.reviewTemplates, 'empty')
  })

  test('夹具置空：模板列表为空态占位', async ({ page }) => {
    await expect(listTitle(page)).toHaveText('复查报告模板列表（0）')
    await expect(rowsIn(page)).toHaveCount(0)
    await expect(listCard(page).locator('.el-empty')).toContainText('暂无模板，请先上传')
  })
})
