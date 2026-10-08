import { test, expect, type Page, type Locator } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * T625 盲区用例（admin-web / mock 模式）：类 3「复查报告多格式上传 + 记录可读 + 下载入口」
 * 与类 5「技师手机号可见可编辑 → 保存回显 → 其它字段一项都不动」。
 *
 * 与既有 spec 的分工（不重复、只补空位）：
 * - admin-review.spec.ts（T301/T307）只用一种格式（application/pdf）走上传链路，
 *   且从未点过「下载」——图片格式（image/jpeg、image/png）、扩展名与 MIME 的**两道**白名单、
 *   以及「每条记录各自的 fileId 是否串行」全是空位；
 * - admin-tech.spec.ts 的 A-TECH-05（T624）核了「留空不改号 / 非法号被拦 / 改号落库」，
 *   但改号后只对比了「团队」一列 —— 换号会不会顺手把安装次数、认证状态、启停、创建时间
 *   带跑，以及「新号的明文是否滞留在页面上」「重开弹窗是否把新号回填成原值」都没钉。
 *
 * 🔴 为什么每条用例前有一个「当前是不是 admin-web」的探针（probeAdminApp）：
 *   本文件名是 t625-blindspot-admin.spec.ts，既不被 e2e/admin-playwright.config.ts 的
 *   testMatch（形如「以 admin- 开头」的 glob）收集，也不在根 playwright.config.ts 的 testIgnore
 *   （同一条以 admin- 开头的 glob）里 ⇒ 根 config（CI 的 `npx playwright test`，e2e.yml:128）会把
 *   本文件收进**患者端 H5 dev server**（端口 5173）去跑。T625 红线禁止改任何 Playwright
 *   config，所以只能在用例内自检：baseURL 指向的不是 admin-web 时整文件自跳过（灰），
 *   而不是把 admin 路由打到患者端页面上判红。
 *   要在本机真正跑这些用例：
 *     npx playwright test e2e/tests/t625-blindspot-admin.spec.ts \
 *       --config=<一个 testMatch 覆盖本文件的 admin 侧 config> 
 *   或 `E2E_LOCAL_BASE_URL=http://localhost:5175 npx playwright test e2e/tests/t625-blindspot-admin.spec.ts`
 *   （后者会带 Pixel 5 移动视口，后台是桌面布局，只作冒烟看，不作 CI 判据）。
 *
 * 🔴 mock 边界：绿只说明「前端消费 + mock 契约」一致。真实链路上有三处 mock 观测不到的判据，
 *   本轮**不写断言**（写了必红），已在用例里以 test.skip 显式登记：
 *   1) 魔数指纹：后端 services/file-service/internal/service/presigner.go 以文件头 8 字节为权威，
 *      而 api/index.ts:715-726 的 mock presign 分支根本不读 params.fileHeader ⇒
 *      「扩展名/MIME 都合法但内容是 exe」在 mock 下必然一路通过；
 *   2) 手机号重复的 409：org.ts:197-208 的 mockUpdateTechnician 没有查重，真实服务
 *      user-service/internal/handler/handler.go:1232 才回 TechPhoneHashTaken（该格由
 *      services/user-service/internal/handler/technician_t625_test.go 在 Go 侧钉住）；
 *   3) 技师行的 phoneState=unreadable：org.ts:39-44 四行技师全是 masked，
 *      unreadable 只在医护侧有种子（DOC-102，org.ts:34）⇒ 技师页那一格没有数据源。
 *
 * 数据隔离：mock 的 TECHNICIANS / RECORDS 是页面 JS 上下文里的模块级数组，Playwright 每条
 * 用例独立 context ⇒ 用例之间不共享写入；本文件的写入只影响自己那一份。
 */

/** admin-web 的静态标题（apps/admin-web/index.html:6「矫智通运营平台」），患者端 H5 是「矫智通」 */
const ADMIN_TITLE_MARK = '运营平台'

const ADMIN_APP_SKIP_REASON =
  '当前 baseURL 指向的不是 admin-web（根 playwright.config.ts 把本文件收进患者端 H5 server，' +
  '而 e2e/admin-playwright.config.ts 的 testMatch 只收 admin-*.spec.ts）：' +
  '需以覆盖本文件的 admin 侧 config 运行；T625 禁止改 config，故此处自跳过'

/** null = 未探测；true/false 在同一 worker 内复用结论，避免每条用例都付一次导航成本 */
let adminAppReady: boolean | null = null

/** 探针：能否在 baseURL 上打开 admin-web 登录页（标题 + 登录表单都要在） */
async function probeAdminApp(page: Page): Promise<boolean> {
  if (adminAppReady !== null) return adminAppReady
  try {
    await page.goto(adminRoutes.login, { waitUntil: 'domcontentloaded', timeout: 20_000 })
    await page.waitForFunction((mark: string) => document.title.includes(mark), ADMIN_TITLE_MARK, { timeout: 8_000 })
    await page.locator('.login-form').waitFor({ state: 'visible', timeout: 8_000 })
    adminAppReady = true
  } catch {
    adminAppReady = false
  }
  return adminAppReady
}

/**
 * 记录 window.open 的目标地址。
 * downloadReport（pages/review-records/index.vue:205-209）是 `window.open(row.reportDownloadUrl, '_blank')`，
 * 而 mock 的下载域名 https://mock-cos.example.com 并不存在 ⇒ 用 popup 事件拿不到可靠判据，
 * 就在页面里把 open 换成记录器（只作用于本测试上下文，不改产品代码）。
 */
async function installPopupRecorder(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const w = window as unknown as { __openedUrls?: string[]; open: (url?: string | null) => unknown }
    w.__openedUrls = []
    w.open = (url?: string | null) => {
      w.__openedUrls = w.__openedUrls || []
      w.__openedUrls.push(String(url ?? ''))
      return null
    }
  })
}

async function openedUrls(page: Page): Promise<string[]> {
  return page.evaluate(() => ((window as unknown as { __openedUrls?: string[] }).__openedUrls ?? []))
}

async function resetOpenedUrls(page: Page): Promise<void> {
  await page.evaluate(() => {
    ;(window as unknown as { __openedUrls?: string[] }).__openedUrls = []
  })
}

/** 复查页列表卡片（两页同构：第 1 张 page-card 是上传区，第 2 张才是列表） */
const listCard = (page: Page): Locator => page.locator('.page-card').nth(1)
const listTitle = (page: Page): Locator => listCard(page).locator('.page-card-title')
const rowsIn = (page: Page): Locator => tableRows(page, listCard(page))
const uploadInput = (page: Page): Locator => page.locator('input.el-upload__input')
const uploadedTag = (page: Page): Locator => page.locator('.el-tag', { hasText: '已上传' })

async function pickPatient(page: Page, name: string): Promise<void> {
  await pickSelectOption(page, page.locator('.patient-select'), name)
}

/** 复查日期输入框（表单第一个 input，与 admin-review.spec.ts 同一入口） */
async function setReviewDate(page: Page, date: string): Promise<void> {
  const dateInput = page.locator('.review-form input').first()
  await dateInput.fill(date)
  await dateInput.press('Enter')
}

/** 夹具里 PT-001（林小雨）已有两条记录：2026-06-12 / 2026-09-15，日期都避开这两格 */
const PATIENT = '林小雨'

/** 三种格式各带真魔数头：mock 目前不看 fileHeader，写成真头是为了 mock 补上校验后不用返工 */
const FORMATS: { ext: string; mime: string; fileName: string; date: string; buffer: Buffer }[] = [
  {
    ext: 'pdf',
    mime: 'application/pdf',
    fileName: 't625-report.pdf',
    date: '2026-10-01',
    buffer: Buffer.from('%PDF-1.4\n%T625 e2e fixture\n'),
  },
  {
    ext: 'jpg',
    mime: 'image/jpeg',
    fileName: 't625-report.jpg',
    date: '2026-10-02',
    buffer: Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01]),
  },
  {
    ext: 'png',
    mime: 'image/png',
    fileName: 't625-report.png',
    date: '2026-10-03',
    buffer: Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d]),
  },
]

async function uploadFixtureFile(page: Page, fileName: string, mimeType: string, buffer: Buffer): Promise<void> {
  await uploadInput(page).setInputFiles({ name: fileName, mimeType, buffer })
  await expect(adminMessage(page)).toContainText('文件上传成功')
  await expect(uploadedTag(page)).toHaveCount(1)
}

test.describe('T625 类 3 · 复查报告多格式上传与下载入口（review-records）', () => {
  test.beforeEach(async ({ page }) => {
    test.skip(!(await probeAdminApp(page)), ADMIN_APP_SKIP_REASON)
    await installPopupRecorder(page)
    await adminLogin(page, 'admin')
    await page.goto(`${adminRoutes.reviewRecords}?mock_review=data`)
  })

  test('PDF / JPG / PNG 三种格式各自上传、各自成记录、下载入口各自指向自己的 fileId', async ({ page }) => {
    await pickPatient(page, PATIENT)
    await expect(listTitle(page)).toHaveText('历史复查记录（2）')
    const baselineRows = await rowsIn(page).count()
    expect(baselineRows).toBe(2)

    const downloadUrls: string[] = []
    for (const f of FORMATS) {
      await setReviewDate(page, f.date)
      await uploadFixtureFile(page, f.fileName, f.mime, f.buffer)

      // 按钮文字取代文件列表（:show-file-list="false"，index.vue:33-41）⇒ 选中后必须回显真文件名
      await expect(page.locator('.review-form .el-upload button')).toContainText(f.fileName)

      await page.getByRole('button', { name: '提交复查记录' }).click()
      await expect(adminMessage(page)).toContainText('复查记录已提交')

      const row = rowsIn(page).filter({ hasText: f.date })
      await expect(row).toHaveCount(1)
      // 提交后表单清空：不能残留上一次的「已上传」状态（残留会让下一条记录挂到前一个文件）
      await expect(uploadedTag(page)).toHaveCount(0)

      // 记录可读：文件列就是刚上传的那个文件名，且渲染出下载入口
      await expect(row.locator('td').nth(4)).toHaveText(f.fileName)
      await expect(row.getByRole('button', { name: '下载' })).toHaveCount(1)

      await resetOpenedUrls(page)
      await row.getByRole('button', { name: '下载' }).click()
      const urls = await openedUrls(page)
      expect(urls, `第 ${f.ext} 那条记录的下载入口必须真的触发一次打开动作`).toHaveLength(1)
      expect(urls[0]).toMatch(new RegExp(`^https://mock-cos\\.example\\.com/download/FILE-M\\d+$`))
      downloadUrls.push(urls[0])
    }

    // 三条新记录 + 两条夹具记录；「每条各挂各的文件」只有在条数对得上时才算数
    await expect(listTitle(page)).toHaveText(`历史复查记录（${baselineRows + FORMATS.length}）`)
    // 串行判据：三次上传拿到三个互不相同的 fileId ⇒ 记录不会被挂到同一个文件上
    expect(new Set(downloadUrls).size, `三条记录的下载地址必须互不相同：${downloadUrls.join(' , ')}`).toBe(FORMATS.length)
    // 夹具里那条刻意无报告的记录（2026-09-15）不能被新上传「借到」下载入口
    const noFileRow = rowsIn(page).filter({ hasText: '2026-09-15' })
    await expect(noFileRow.locator('td').nth(4)).toHaveText('无')
    await expect(noFileRow.getByRole('button', { name: '下载' })).toHaveCount(0)
  })

  test('.jpeg 与 .jpg 是同一种东西：别名扩展名不得被白名单漏掉', async ({ page }) => {
    await pickPatient(page, PATIENT)
    await setReviewDate(page, '2026-10-04')
    await uploadFixtureFile(page, 't625-report.jpeg', 'image/jpeg', FORMATS[1].buffer)
    await page.getByRole('button', { name: '提交复查记录' }).click()
    await expect(adminMessage(page)).toContainText('复查记录已提交')
    const row = rowsIn(page).filter({ hasText: '2026-10-04' })
    await expect(row.locator('td').nth(4)).toHaveText('t625-report.jpeg')
    await expect(row.getByRole('button', { name: '下载' })).toHaveCount(1)
  })

  test('白名单是两道：扩展名合法但 MIME 不在名单内也要被拒，且不打上传、不写列表', async ({ page }) => {
    await pickPatient(page, PATIENT)
    const before = await rowsIn(page).count()
    await uploadInput(page).setInputFiles({
      name: 'forged.pdf',
      mimeType: 'text/plain',
      buffer: Buffer.from('not a pdf at all'),
    })
    // 第一道（扩展名）过了，第二道（MIME）必须响 —— 只查扩展名的实现这里会被绕过
    await expect(adminMessage(page)).toContainText('不支持的 MIME 类型：text/plain')
    await expect(uploadedTag(page)).toHaveCount(0)
    await expect(rowsIn(page)).toHaveCount(before)
    // 被拒的文件不能挂到记录上：提交按钮仍只由「复查日期」门控，但列表没有新增行
    await expect(listTitle(page)).toHaveText(`历史复查记录（${before}）`)
  })

  test('上传成功但没提交记录：列表行数与下载入口都不许提前出现', async ({ page }) => {
    await pickPatient(page, PATIENT)
    const before = await rowsIn(page).count()
    await setReviewDate(page, '2026-10-05')
    await uploadFixtureFile(page, 't625-never-submitted.png', 'image/png', FORMATS[2].buffer)
    await expect(rowsIn(page)).toHaveCount(before)
    await expect(rowsIn(page).filter({ hasText: '2026-10-05' })).toHaveCount(0)
    await expect(rowsIn(page).getByRole('button', { name: '下载' })).toHaveCount(1) // 只有夹具 FILE-R0001 那条
  })

  test('双扩展名 .exe.pdf 走的是「最后一位扩展名」判据：mock 下会被放行（真实兜底是后端魔数）', async ({ page }) => {
    // 这条钉的是**前端现有口径**（review-report-whitelist.ts:40 取 lastIndexOf('.')）：
    // '.exe.pdf' 的 ext 是 '.pdf' ⇒ 前端放行。产品意图是「后端魔数为权威」，所以这里只把
    // 前端的宽松呈出来，不写「必须被拒」那种会红的期望。
    await pickPatient(page, PATIENT)
    await setReviewDate(page, '2026-10-06')
    await uploadFixtureFile(page, 'payload.exe.pdf', 'application/pdf', Buffer.from('%PDF-1.4 fake'))
    await page.getByRole('button', { name: '提交复查记录' }).click()
    await expect(adminMessage(page)).toContainText('复查记录已提交')
    await expect(rowsIn(page).filter({ hasText: '2026-10-06' }).locator('td').nth(4)).toHaveText('payload.exe.pdf')
  })

  test('mock 侧魔数兜底缺席：伪造内容的文件在 mock 下一路通过（本轮不下断言）', async () => {
    test.skip(true, 'T307 未合入：mock 的 presign 分支不读 fileHeader（apps/admin-web/src/api/index.ts:715-726），' +
      '「扩展名 + MIME 都合法、内容是 exe」这类伪造在 mock 下必然通过，魔数权威只在后端 presigner.go。' +
      '需新卡把 mock 侧 presign 接到文件头指纹校验；合入后去掉本行即转绿')
  })
})

test.describe('T625 类 5 · 技师手机号可见可编辑与「只动号码」', () => {
  const TECH_NAME = '郑师傅' // TECH-003：未认证 + 启用，与 A-TECH-05 用的周师傅错开
  const PHONE_BEFORE_MASKED = '137****7890'
  const PHONE_AFTER = '13500007777'
  const PHONE_AFTER_MASKED = '135****7777'

  /** 一行 7 列的文本快照（姓名 0 / 手机号 1 / 团队 2 / 安装次数 3 / 认证 4 / 状态 5 / 创建时间 6） */
  async function cells(row: Locator): Promise<string[]> {
    return row.evaluate((el) => Array.from(el.querySelectorAll('td')).map((td) => (td.textContent ?? '').trim()))
  }

  const rowOf = (page: Page, name: string): Locator => tableRows(page).filter({ hasText: name })

  // beforeEach 里赋值；声明处只给类型（Playwright 保证钩子先于用例体执行）
  let phoneInput!: Locator

  test.beforeEach(async ({ page }) => {
    test.skip(!(await probeAdminApp(page)), ADMIN_APP_SKIP_REASON)
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.technicians)
    await expect(tableRows(page).first()).toBeVisible({ timeout: 15_000 })
    await expect(rowOf(page, TECH_NAME)).toHaveCount(1)
    // T624 口径：按 label 定位（占位文案随 phoneState 变，不能当选择器）
    phoneInput = page.locator('.el-dialog:visible .el-form-item').filter({ hasText: '手机号' }).locator('input')
  })

  test('换号只动号码：其余六列逐项与换号前一致，且明文号码不得留在页面上任何位置', async ({ page }) => {
    const row = rowOf(page, TECH_NAME)
    const before = await cells(row)
    expect(before[1], '基线行的手机号列应是脱敏值').toBe(PHONE_BEFORE_MASKED)

    await row.getByRole('button', { name: '编辑' }).click()
    const dialog = page.locator('.el-dialog:visible')
    // 可见可编辑（T624）：不禁用、不预填脱敏串
    await expect(phoneInput).toBeEnabled()
    await expect(phoneInput).toHaveValue('')
    await expect(phoneInput).toHaveAttribute('placeholder', '已绑定手机号，留空即不修改')

    await phoneInput.fill(PHONE_AFTER)
    await dialog.getByRole('button', { name: '保存修改' }).click()
    await expect(adminMessage(page)).toContainText('修改成功')
    await expect(page.locator('.el-dialog:visible')).toHaveCount(0)

    const edited = rowOf(page, TECH_NAME)
    // loadData() 未 await（pages/technicians/index.vue:202）⇒ 轮询等新号回显
    await expect(edited.locator('td').nth(1), '号码列按脱敏形态回显新号').toHaveText(PHONE_AFTER_MASKED)
    const after = await cells(edited)
    // 除手机号那一列，其余逐列相等：换号不得牵动姓名/团队/安装次数/认证/启停/创建时间
    expect(after.map((v, i) => (i === 1 ? before[i] : v)), '换号后其它列必须一项不动').toEqual(before)

    // 明文外泄判据：整页文本里搜不到刚写入的 11 位明文（列表脱敏、弹窗不回填）
    await expect(page.locator('.technicians')).not.toContainText(PHONE_AFTER)
    expect(await page.locator('body').innerText(), '明文手机号不得出现在页面任何角落').not.toContain(PHONE_AFTER)
  })

  test('换号后重开编辑弹窗：号码仍不回填、占位文案仍是「留空即不修改」（phoneState 仍是 masked）', async ({ page }) => {
    await rowOf(page, TECH_NAME).getByRole('button', { name: '编辑' }).click()
    const dialog = page.locator('.el-dialog:visible')
    await phoneInput.fill(PHONE_AFTER)
    await dialog.getByRole('button', { name: '保存修改' }).click()
    await expect(adminMessage(page)).toContainText('修改成功')

    const edited = rowOf(page, TECH_NAME)
    await expect(edited.locator('td').nth(1)).toHaveText(PHONE_AFTER_MASKED)
    await edited.getByRole('button', { name: '编辑' }).click()
    // 脱敏串不是原值 ⇒ 编辑框永不预填（phoneField.ts:36-40 + index.vue:177）
    await expect(phoneInput).toHaveValue('')
    await expect(phoneInput).toHaveAttribute('placeholder', '已绑定手机号，留空即不修改')
    await dialog.getByRole('button', { name: '取消' }).click()
    await expect(page.locator('.el-dialog:visible')).toHaveCount(0)
    // 取消不写：号仍是刚保存的那个
    await expect(rowOf(page, TECH_NAME).locator('td').nth(1)).toHaveText(PHONE_AFTER_MASKED)
  })

  test('纯空白号 = 非法输入被前端拦下，绝不走「留空即清空」那条写路径', async ({ page }) => {
    // Go 侧 technician_t625_test.go 钉的是服务端「len==0 才叫不改号，空白串按非法处理」；
    // 前端这一格是它的镜像：phonePatch 只对 trim 后为空的输入返回 undefined（phoneField.ts:47-50），
    // 空格串会先被 PHONE_RE 拦下 ⇒ 既不会改号，更不会把号清成 NULL。
    const row = rowOf(page, TECH_NAME)
    await row.getByRole('button', { name: '编辑' }).click()
    const dialog = page.locator('.el-dialog:visible')
    await phoneInput.fill('   ')
    await dialog.getByRole('button', { name: '保存修改' }).click()
    await expect(adminMessage(page)).toContainText('手机号需为 11 位号码，或留空')
    await expect(dialog).toBeVisible()
    expect((await cells(rowOf(page, TECH_NAME)))[1], '被拦下后号码列不变').toBe(PHONE_BEFORE_MASKED)
  })

  test('只改姓名不动号：输入框留空时该键根本不下发，列表列仍是原脱敏值', async ({ page }) => {
    const marker = `改名${test.info().testId.replace(/[^0-9a-zA-Z]/g, '').slice(-6)}`
    expect(marker.length, '姓名输入框 maxlength=20').toBeLessThanOrEqual(20)
    const row = rowOf(page, TECH_NAME)
    const before = await cells(row)
    expect(before[1], '基线行的手机号列应是脱敏值').toBe(PHONE_BEFORE_MASKED)

    await row.getByRole('button', { name: '编辑' }).click()
    const dialog = page.locator('.el-dialog:visible')
    await dialog.locator('input[placeholder="技师姓名"]').fill(marker)
    await phoneInput.fill('')
    await dialog.getByRole('button', { name: '保存修改' }).click()
    await expect(adminMessage(page)).toContainText('修改成功')

    const renamed = rowOf(page, marker)
    await expect(renamed).toHaveCount(1)
    const after = await cells(renamed)
    expect(after[1], '留空即不改号').toBe(PHONE_BEFORE_MASKED)
    expect(after.slice(2), '改名不动号码列以外的任何列').toEqual(before.slice(2))
  })

  test('技师页 phoneState=unreadable 那一格无数据源（本轮不下断言）', async () => {
    test.skip(true, 'T361 未合入（技师侧数据源）：mock 四行技师的 phoneState 全是 masked（apps/admin-web/src/mock/org.ts:39-44），' +
      'unreadable（列表出 \'***\'、占位文案「号码读取失败（***）…填 11 位新号可覆盖」，utils/phoneField.ts:26-40）' +
      '这一格在技师页没有可行解的输入数据；需新卡补一条 phoneState=unreadable 的技师种子（医护侧已有 DOC-102，org.ts:34）；合入后去掉本行即转绿')
  })

  test('换号撞上他人号码的 409 在 mock 下不可观测（本轮不下断言）', async () => {
    test.skip(true, 'T247 未合入（mock 写侧查重）：org.ts:197-208 的 mockUpdateTechnician 不校验号码是否已被他人占用，' +
      '真实服务的 409（TechPhoneHashTaken，services/user-service/internal/handler/handler.go:1232/1237）在 mock 下不可观测；' +
      '该格现由 services/user-service/internal/handler/technician_t625_test.go 在 Go 侧钉住，E2E 侧需新卡把查重搬到 mock；合入后去掉本行即转绿')
  })
})
