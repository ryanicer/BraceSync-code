import { test, expect, type Page, type Locator } from '@playwright/test'
import {
  realLogin,
  adminLogout,
  gotoMenu,
  menuItems,
  adminMessage,
  tableRows,
  topBarUserName,
  getAuthToken,
  realRoutes,
} from '../real-helpers'
import { requireDeployedBuild } from '../deploy-guard'

/**
 * T324 · e2e-real 部署守卫的落位示例：T315「医护账号」页（admin-web 第 15 页）
 *
 * 为什么是这一条：T315 已于 2026-09-22 19:21 合入 main（#172），但 staging 上部署的前端包是
 * 当天 13:58 构建的那版，侧栏还只有 14 页、入口文字「医护账号」在已部署包里根本不存在。
 * 这正是本卡要解的那一类断言 —— 换在 T324 之前，它会把【任何】改前端行为的 PR 的门禁钉成假红
 * （同形状的事故见 T322：3 failed / 29 passed）。
 *
 * 现在的口径：先探已部署包有没有该入口；没有 ⇒ 显式 post-deploy 跳过（报告里能按标签反查），
 * 有 ⇒ 下面这些断言照常真跑，此后该页任何回归一样判红。部署后回归阶段（定时 / 手动）
 * 仍探不到入口就直接判红，不允许长期挂在「跳过」上蒙混。
 *
 * ── T632 追加（2026-10-09）：段1「医护账号管理」全链路 ──────────────────────
 * 派发单要求补的三段是「补填手机号 → 重置密码 → 用新密码登录运营后台」。口径三条：
 *   1) 零 seed 写入：靶子是本用例自建的测试医护（前缀 DOCTOR_PREFIX，复用不重建），
 *      seed 的 D0001/doctor_li 一行不碰 —— 红线「staging seed 只读」。
 *   2) 一次性口令的值只从【接口】取，不从弹层文本里抠：弹层渲染口令是应用自己的行为（本用例只断言它的
 *      形状），把明文抠进变量会让它有机会落进失败留档的 trace/screenshot（CI 第 413 行上传
 *      e2e-real/test-results/）。对齐 21-chain-c 的既有姿势：口令经 POST …/password 那一腿拿，
 *      日志只打长度。
 *   3) 收尾能撤的都撤：手机号用 PUT 的空串语义清空（服务端 Phone 指空串＝清空，前端永不发空串，
 *      所以这一腿只能走接口）。医护账号行本身撤不掉 —— POST /admin/doctors 之外没有删除端点
 *      （路由面见 services/gateway/cmd/server/rbac.go:107-110 与 handler/handler.go:263-268），
 *      故采用「复用不新建」把残留钉死在 1 行，并在 afterAll 打一行残留报备。
 */

const MENU_TITLE = '医护账号'

/** 自建测试医护的姓名前缀（复用键：按此前缀在列表里找，找到就用，找不到才新建） */
const DOCTOR_PREFIX = 'T632账号测试'
/** 职称取值用 seed 在册的那枚（doctors.title 无 CHECK，但取在册值可排除「职称表口径」这一枚干扰） */
const DOCTOR_TITLE = '主治医师'
const DOCTOR_DEPARTMENT = 'T632测试科室'

/** phone.Mask：11 位号码留前 3 后 4，中间四颗星（services/user-service/internal/phone/phone.go:120-131） */
const PHONE_MASK_RE = /^\d{3}\*{4}\d{4}$/
/** genDoctorPassword：Br + 12 位字母数字 + #7（repo/doctor_accounts_t314.go:44-49,72-85） */
const DOCTOR_PWD_RE = /^Br[0-9a-zA-Z]{12}#7$/
/**
 * 弹层整段文本里的「初始密码：」那一行的形状（不锚行首行尾——整段文本里它前后还有别的字）。
 * 上一枚 DOCTOR_PWD_RE 是整串判据，别把它的 .source 拼进大正则：^...$ 一嵌就恒不匹配（首跑实测）。
 */
const DOCTOR_PWD_LINE_RE = /初始密码：Br[0-9a-zA-Z]{12}#7/
/** 断言消息专用的脱敏视图：口令中段换成星，形状不变但读不出值 */
function maskDoctorPassword(text: string): string {
  return text.replace(/Br[0-9a-zA-Z]{12}#7/g, 'Br************#7')
}
/** 10409 在 admin 侧的文案（packages/shared-utils/src/errorCopy.ts:71 基础表；派发单说的「状态冲突」是 Go 注释里的码语义，UI 面上落的是这句） */
const CONFLICT_TOAST = '当前状态与该操作冲突，请刷新后重试'
/** 登录失败在 admin 侧的文案（errorCopy.ts 的 ADMIN_ERROR_CODE_COPY[10401]） */
const LOGIN_FAIL_TOAST = '用户名或密码错误'

interface DoctorDTO {
  doctorId: string
  name: string
  title: string | null
  department: string | null
  teamId: string | null
  phoneMasked: string | null
  phoneState: string
  patientCount: number
  status: string
  username: string | null
  accountStatus: string | null
  createdAt: string | null
}

interface TeamDTO {
  teamId: string
  name: string
  patientCount: number
}

interface Envelope {
  status: number
  code: number | null
  message: string
  data: unknown
}

/** 只读的接口调用：绝对/相对地址都收，错误码原样回给调用方判（同 21-chain-c 的姿势） */
async function callApi(
  p: Page,
  method: string,
  urlPath: string,
  opts: { token?: string; body?: unknown } = {},
): Promise<Envelope> {
  const headers: Record<string, string> = {}
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await p.request.fetch(urlPath, {
    method,
    headers,
    data: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  })
  const text = await res.text().catch(() => '')
  let code: number | null = null
  let message = ''
  let data: unknown = null
  try {
    const env = JSON.parse(text) as { code?: unknown; message?: unknown; data?: unknown }
    if (typeof env.code === 'number') code = env.code
    if (typeof env.message === 'string') message = env.message
    data = env.data ?? null
  } catch {
    /* 非 JSON：code 留 null */
  }
  return { status: res.status(), code, message, data }
}

async function callOk<T>(
  p: Page,
  method: string,
  urlPath: string,
  opts: { token?: string; body?: unknown; why: string },
): Promise<T> {
  const r = await callApi(p, method, urlPath, opts)
  expect(
    r.code,
    `${opts.why}：${method} ${urlPath} 应回 code=0，实得 status=${r.status} code=${r.code} message=${r.message}`,
  ).toBe(0)
  return r.data as T
}

/**
 * 新建医护账号要落进一枚真实团队。取「患者最少」的那枚而不是写死 TEAM01：
 * 本用例后面要在患者下拉首面（pageSize=100）里够到自己建的患者，团队越空越够得到。
 */
async function pickSparseTeam(p: Page, token: string): Promise<TeamDTO> {
  const teams = await callOk<TeamDTO[]>(p, 'GET', '/api/v1/teams?page=1&pageSize=100', {
    token,
    why: '读团队列表以挑选测试医护的归属团队',
  })
  expect(teams.length, '团队列表为空 ⇒ 没有任何可用团队，测试医护无处安放').toBeGreaterThan(0)
  const sorted = [...teams].sort((a, b) => a.patientCount - b.patientCount || a.teamId.localeCompare(b.teamId))
  const chosen = sorted[0]
  expect(
    chosen.patientCount,
    `最空的团队 ${chosen.teamId} 也有 ${chosen.patientCount} 名患者，接近下拉首面 100 上限 ⇒ 后面的患者选择腿不稳`,
  ).toBeLessThan(90)
  console.log(`[t632-账号段][选团队] ${chosen.teamId} 患者数=${chosen.patientCount}（本用例取最空的那枚）`)
  return chosen
}

/**
 * 复用优先地拿到本用例的测试医护：列表里已有前缀行就沿用（按 doctorId 升序取第一枚，
 * 排序键来自服务端 `ORDER BY d.doctor_id`，pg.go:679 —— 不依赖本用例的局部变量，跨 run 稳定），
 * 没有才新建。新建走接口而不是「+ 新建医护账号」弹层：弹层的团队/职称两格是 el-select，
 * 段1 的判据在「补手机号 / 重置密码 / 登录」这三腿上，建档这腿用接口最稳且少一层脆性。
 */
async function ensureFixtureDoctor(p: Page, token: string): Promise<DoctorDTO> {
  const all = await callOk<DoctorDTO[]>(p, 'GET', '/api/v1/doctors', {
    token,
    why: '读医护列表以复用/新建测试医护',
  })
  const mine = all
    .filter((d) => (d.name || '').startsWith(DOCTOR_PREFIX))
    .sort((a, b) => a.doctorId.localeCompare(b.doctorId))
  if (mine.length > 0) {
    const hit = mine[0]
    expect(
      hit.username,
      `复用的测试医护 ${hit.doctorId} 没有登录账号（username=${String(hit.username)}）—— 段1 的「重置密码」需要已绑账号的行`,
    ).toBeTruthy()
    console.log(
      `[t632-账号段][复用医护] doctorId=${hit.doctorId} username=${hit.username} phoneState=${hit.phoneState}（前缀命中 ${mine.length} 枚，取 doctorId 最小那枚）`,
    )
    return hit
  }

  const team = await pickSparseTeam(p, token)
  // 后缀只取 6 位毫秒尾（同 real-helpers.uniqueName 的口径），姓名宽度受 maxlength=20 约束
  const name = `${DOCTOR_PREFIX}-${Date.now().toString().slice(-6)}`
  expect(
    name.length,
    `姓名「${name}」长度 ${name.length} 超过新建弹层的 maxlength=20 ⇒ 接口能过但 UI 口径不一致`,
  ).toBeLessThanOrEqual(20)
  const created = await callOk<DoctorDTO>(p, 'POST', '/api/v1/admin/doctors', {
    token,
    body: { name, title: DOCTOR_TITLE, department: DOCTOR_DEPARTMENT, teamId: team.teamId },
    why: '新建测试医护失败（这是本用例唯一的建档动作，复用逻辑见 ensureFixtureDoctor）',
  })
  expect(created.username, '建档应回系统生成的登录账号').toMatch(/^doc\d{5}$/)
  console.log(
    `[t632-账号段][新建医护] doctorId=${created.doctorId} username=${created.username} teamId=${team.teamId}（初始口令长度 ${String((created as DoctorDTO & { initialPassword?: string }).initialPassword).length}，值不落日志）`,
  )
  return created
}

/** 医护账号页里按姓名锚出唯一一行 */
function rowOfDoctor(page: Page, name: string): Locator {
  return tableRows(page, page.locator('.medical-accounts')).filter({ hasText: name })
}

/** 对话框（编辑弹层）/ 消息盒（确认与一次性凭据）的作用域锚 */
function visibleDialog(page: Page): Locator {
  return page.locator('.el-dialog:visible').last()
}
function visibleMessageBox(page: Page): Locator {
  return page.locator('.el-message-box:visible').last()
}

/**
 * 弹层里按 el-form-item 的标签文案锚出那一格的 input。
 * 为什么不用 placeholder：手机号格的 placeholder 会随 phoneState 变三套文案，
 * 而「标签唯一」这件事本来就是要守的口径（同名标签长出第二枚就是回归）。
 */
async function fillDialogField(dialog: Locator, label: string, value: string): Promise<void> {
  const labelEl = dialog.locator('.el-form-item__label', { hasText: new RegExp(`^\\s*${label}`) })
  await expect(labelEl, `弹层内「${label}」标签应唯一`).toHaveCount(1)
  const input = labelEl.locator('xpath=../div[contains(@class,"el-form-item__content")]//input')
  await expect(input, `「${label}」应与输入框同格（标签的兄弟 content 里）`).toHaveCount(1)
  await input.fill('')
  await input.fill(value)
  await expect(input, `「${label}」应真的写进了控件`).toHaveValue(value)
}

/**
 * 负腿专用的表单登录（错口令必然被拒）。
 * 为什么不复用 realLogin：那一腿把「登录接口非 200」当场定性成「凭据漂移」并抛错
 * （real-helpers.ts:309-326，T506 的设计意图 —— 真回归与口令被改要一眼分开）。
 * 用它跑一条【必然失败】的登录，拿到的会是误诊文案而不是登录页的拒绝表现，
 * 所以这里自己填表、自己断言 toast。
 */
async function submitLoginAndExpectRejection(page: Page, username: string, password: string): Promise<void> {
  await page.goto(realRoutes.login, { waitUntil: 'domcontentloaded' })
  await expect(page.locator('.login-card')).toBeVisible({ timeout: 15_000 })
  await page.locator('.login-form input:not([type="password"])').first().fill(username)
  await page.locator('.login-form input[type="password"]').fill(password)
  await page.locator('.login-form').getByRole('button', { name: '登 录' }).click()
  await expect(adminMessage(page), '错口令应被当场拒下').toContainText(LOGIN_FAIL_TOAST, { timeout: 20_000 })
  await expect(page.url(), '被拒的登录不该把会话放进后台').toMatch(/login$/)
}

test.describe('09-医护账号（T315，第 15 页）', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    await expect(menuItems(page).first()).toBeVisible({ timeout: 20_000 })
  })

  test('9.1 侧栏入口可进入，列表按设计稿渲染 10 列', async ({ page }) => {
    await requireDeployedBuild(page, {
      marker: 'T315-doctor-accounts',
      why: 'T315 合入 main 后 staging 尚未部署该前端包',
      probe: async (p) => (await menuItems(p).filter({ hasText: MENU_TITLE }).count()) > 0,
    })

    await gotoMenu(page, MENU_TITLE)
    // 登录后浏览器落在根路径（无 /admin 前缀，见 real-helpers 顶部 T279 实测说明），故按 router 路径断
    await expect(page).toHaveURL(/\/doctor-accounts$/, { timeout: 15_000 })

    // 表头 10 列，顺序与文案按 apps/admin-web/src/pages/doctors/index.vue（设计稿 医护账号.html:141）
    await expect(page.locator('.medical-accounts .el-table__header-wrapper thead th')).toHaveText(
      ['姓名', '登录账号', '手机号', '科室', '所属团队', '职称', '管理患者数', '状态', '创建时间', '操作'],
      { timeout: 20_000 },
    )

    // 页面骨架：搜索框 + 两个筛选 + 新建按钮 + 计数提示（条数由真实数据决定，只断格式）
    await expect(page.locator('.medical-accounts .search-input input')).toBeVisible()
    await expect(page.getByRole('button', { name: '+ 新建医护账号' })).toBeVisible()
    await expect(page.locator('.medical-accounts .count-hint')).toContainText(/共 \d+ 个账号/)
    await expect(page.locator('.medical-accounts .count-hint')).toContainText(/启用 \d+ \/ 禁用 \d+/)

    // 无渲染事故（脱敏/占位逻辑漏网时会冒出 undefined/NaN）
    await expect(page.locator('.medical-accounts')).not.toContainText(/undefined|NaN/)
  })
})

test.describe('09b-T632 段1 医护账号管理链（自建测试医护，零 seed 写入）', () => {
  /** 本用例的靶子医护；afterAll 的清空腿读它 */
  let fixture: DoctorDTO | null = null
  /** 段1 期间给该医护补上的手机号，收尾要清空（不复用上一轮的号：掩码不可逆，读不回明文就没法断「新号可登录」） */
  let fixturePhone = ''

  test('9.2 补填手机号 → 重置密码 → 新密码与手机号都能登录', async ({ page }) => {
    await realLogin(page)
    const adminToken = await getAuthToken(page)
    expect(adminToken, '运营账号登录应把 JWT 写进 localStorage').toBeTruthy()

    const doctor = await ensureFixtureDoctor(page, adminToken!)
    fixture = doctor
    const doctorName = doctor.name
    fixturePhone = `135${String(Math.floor(Math.random() * 1e8)).padStart(8, '0')}`

    await gotoMenu(page, MENU_TITLE)
    await expect(page).toHaveURL(/doctor-accounts$/, { timeout: 15_000 })

    // 搜索框是本页的客户端过滤（doctors/index.vue:186-191 的 computed list），按姓名收窄到唯一一行
    const search = page.locator('.medical-accounts .search-input input')
    await search.fill(doctorName)
    const row = rowOfDoctor(page, doctorName)
    await expect(row, `搜索「${doctorName}」应只剩本用例那一行`).toHaveCount(1)

    // ── 起点自证：手机号格是「—」（phoneState=absent）；上一轮收尾没清就是红，不是跳过
    const phoneCell = row.locator('td').nth(2)
    await expect(
      phoneCell,
      `测试医护的手机号格起点应是「—」（phoneState=${doctor.phoneState}）—— 带着上一轮的号开跑说明 afterAll 清空腿没落地`,
    ).toHaveText('—')

    // ── 1) 补填手机号（本页没有「补填手机号」按钮，这条链就是 编辑 → 手机号格 → 保存修改）
    await row.getByRole('button', { name: '编辑' }).click()
    const editDialog = visibleDialog(page)
    await expect(editDialog.getByText('编辑医护账号')).toBeVisible()
    await expect(
      editDialog.locator('.el-form-item__label', { hasText: '手机号' }).locator('xpath=../div[contains(@class,"el-form-item__content")]//input'),
      '未录手机号时该格 placeholder 应是「选填，11位手机号」（utils/phoneField.ts:36-40 的 absent 那一支）',
    ).toHaveAttribute('placeholder', '选填，11位手机号')

    await fillDialogField(editDialog, '手机号', fixturePhone)
    await editDialog.getByRole('button', { name: '保存修改' }).click()
    await expect(adminMessage(page)).toContainText('修改成功', { timeout: 20_000 })

    // 列表面：手机号列从「—」变成脱敏号（T487 起 phoneState 走 masked）
    await expect(
      phoneCell,
      `保存后手机号格应是脱敏号而不是「—」，实得「${await phoneCell.innerText()}」`,
    ).toHaveText(PHONE_MASK_RE)

    // 值级回读：接口面与列表面同值，且 phoneState=masked（「页面显示对了但库里没落」是这一腿管的）
    const afterUpdate = await callOk<DoctorDTO[]>(page, 'GET', '/api/v1/doctors', {
      token: adminToken!,
      why: '补号后回读医护列表',
    })
    const dbRow = afterUpdate.find((d) => d.doctorId === doctor.doctorId)
    expect(dbRow, '接口面应还能找到本用例的测试医护').toBeTruthy()
    expect(dbRow!.phoneState, '补号后接口面 phoneState 应为 masked').toBe('masked')
    expect(dbRow!.phoneMasked, '接口面脱敏号应与列表面同一枚').toBe(await phoneCell.innerText())

    // 再开一次编辑：已绑号时该格不回显原号（T487 的脱敏口径），placeholder 换成第二支
    await row.getByRole('button', { name: '编辑' }).click()
    const reopen = visibleDialog(page)
    const reopenPhoneInput = reopen
      .locator('.el-form-item__label', { hasText: '手机号' })
      .locator('xpath=../div[contains(@class,"el-form-item__content")]//input')
    await expect(reopenPhoneInput).toHaveAttribute('placeholder', '已绑定手机号，留空即不修改')
    // 作用域必须锚到那一格：弹层里 7 枚 input（姓名/手机号/登录账号/科室/三枚 select），
    // 写成 dialog.locator('input').not.toHaveValue(...) 会撞 strict mode（首跑实测 7 元素命中）。
    await expect(reopenPhoneInput, '已绑号时手机号格不回显明文').not.toHaveValue(fixturePhone)
    await expect(reopenPhoneInput).toHaveValue('')
    await reopen.getByRole('button', { name: '取消' }).click()

    // ── 2) 重置密码：按钮 → 确认盒 → 一次性凭据弹层（只断形状，明文不抠进变量，见文件头口径 2）
    await row.getByRole('button', { name: '重置密码' }).click()
    const confirmBox = visibleMessageBox(page)
    await expect(confirmBox.locator('.el-message-box__title')).toHaveText('确认重置密码')
    await expect(confirmBox).toContainText('旧密码即时失效')
    await confirmBox.getByRole('button', { name: '确认' }).click()

    const credBox = visibleMessageBox(page)
    await expect(credBox.locator('.el-message-box__title')).toHaveText('重置成功')
    await expect(credBox).toContainText(`登录账号：${doctor.username}`)
    const credText = await credBox.innerText()
    // 形状判据写成 test + 布尔断言：expect(text).toMatch(re) 失败时 Playwright 会把 received
    // 整段打进日志与 error-context（本轮首跑就是这么把一枚明文口令印进本地 run 日志的）。
    // 这里断言消息只给脱敏视图；且弹层里那枚随即被下面步 3 的接口重置覆盖失效。
    expect(
      DOCTOR_PWD_LINE_RE.test(credText),
      `一次性凭据弹层应有「初始密码：Br+12位+#7」那一行，实得（已脱敏）${maskDoctorPassword(credText)}`,
    ).toBe(true)
    await expect(credBox).toContainText('仅此一次展示，关闭后不可再看')
    await credBox.getByRole('button', { name: '我已转交本人' }).click()
    await expect(page.locator('.el-message-box:visible'), '点「我已转交本人」后消息盒应收起').toHaveCount(0)

    // ── 3) 拿到可用于登录腿的口令：再重置一次，这发走接口、明文只进变量不进日志
    //     （上一发在弹层里显示的口令随即失效——同一账号同一时刻只有一枚有效口令，这正是重置的语义）
    const reset = await callOk<{ doctorId: string; username: string; password: string }>(
      page,
      'POST',
      `/api/v1/admin/doctors/${doctor.doctorId}/reset-password`,
      { token: adminToken!, why: '接口腿重置测试医护口令（供登录三腿使用）' },
    )
    expect(reset.doctorId, '重置应回本用例的医护号').toBe(doctor.doctorId)
    expect(reset.username, '重置回的登录账号应与建档时同枚').toBe(doctor.username)
    // 形状判据只打布尔，消息不带值（口令明文一旦进断言消息就会随失败留档上传）
    expect(DOCTOR_PWD_RE.test(reset.password), '一次性口令应是 Br+12 位+#7 形状（值不打印）').toBe(true)

    // ── 4) 用新密码登录运营后台（用户名通道）
    await adminLogout(page)
    await realLogin(page, doctor.username!, reset.password)
    await expect(page).toHaveURL(/dashboard$/, { timeout: 20_000 })
    await expect(
      topBarUserName(page),
      '顶栏用户名应是该医护姓名（admins.name 与建档姓名同值，repo/doctor_accounts_t314.go:143-149）',
    ).toHaveText(doctorName)

    // ── 5) 反证：口令面能判「不」。用一枚错口令（末位多一字符）走同一条表单腿，
    //     必须被拒并停在登录页 —— 缺了这格，步 4/6 的「登录成功」就分不清是判据还是常绿装饰。
    await adminLogout(page)
    await submitLoginAndExpectRejection(page, doctor.username!, `${reset.password}x`)

    // ── 6) 手机号通道登录（T487 双凭据：登录页同一个框里填 11 位手机号）
    await realLogin(page, fixturePhone, reset.password)
    await expect(page).toHaveURL(/dashboard$/, { timeout: 20_000 })
    await expect(topBarUserName(page)).toHaveText(doctorName)
    console.log(
      `[t632-账号段][双凭据] username=${doctor.username} 与手机号(尾 4=${fixturePhone.slice(-4)}) 都能登录；口令值全程未落日志`,
    )
  })

  /**
   * T631 关联档（PM 2026-10-09 11:28 裁定：改为可选，可做成「登记现状」类断言或直接删档）。
   *
   * 取舍：留档，按现状断言。理由是 Boss 那句「按种子垃圾数据处理」否掉的是【代码要不要修】，
   * 而页面把「无账号医生」这行的重置密码按钮照旧摆出来（doctors/index.vue:68-77 的操作列三条按钮
   * 全无 v-if）—— 这一格「能不能点、点了给什么」是用例覆盖得到的真实行为，删档等于把它从视野里抹掉。
   *
   * 断言只押两件事：① 该行的重置被服务端判成 10409 且文案落定（repo 侧 ErrDoctorNoAccount，
   * doctor_accounts_t314.go:35-38 → handler:150-168）；② 没有出现一次性凭据弹层。
   * 这条腿零写入：拒绝路径在 UPDATE 之前，库内不变更（同文件 313-324 的判序）。
   *
   * 现针对象是 seed 的无账号医生行（POST /admin/doctors 建的行必然带账号，建不出「无账号」形态），
   * 只读地按行筛选、点一次注定被拒的按钮，seed 数据本身一行不改。
   * 若 Boss 后续把这枚垃圾行从种子里清掉，这里没有「无账号医生」可点 ⇒ 本条把现状报成 0 行并跳过，
   * 判据的正面（有账号能重置成功）在同批次的 9.2 步 2 里，不靠这条腿证绿。
   */
  test('9.3 无账号医生的「重置密码」现状登记（T631 关联档）', async ({ page }) => {
    await realLogin(page)
    await gotoMenu(page, MENU_TITLE)
    await expect(page).toHaveURL(/doctor-accounts$/, { timeout: 15_000 })

    // 「登录账号」格（第 2 列）渲染 —— 即 username 为空（doctors/index.vue:41 的 DASH）
    const allRows = tableRows(page, page.locator('.medical-accounts'))
    await expect(allRows.first()).toBeVisible({ timeout: 20_000 })
    const total = await allRows.count()
    const noAccountNames: string[] = []
    for (let i = 0; i < total; i++) {
      const r = allRows.nth(i)
      if ((await r.locator('td').nth(1).innerText()).trim() === '—') {
        noAccountNames.push((await r.locator('td').nth(0).innerText()).trim())
      }
    }
    console.log(
      `[t632-账号段][T631现状] 本页可见 ${total} 行，其中「登录账号=—」的无账号医生 ${noAccountNames.length} 行：${noAccountNames.join('、') || '（无）'}`,
    )

    test.skip(
      noAccountNames.length === 0,
      'staging 种子里已无「无账号医生」行 ⇒ T631 关联档自然退役（现状登记见上一行日志；正面判据在 9.2）',
    )

    const targetName = noAccountNames[0]
    const target = rowOfDoctor(page, targetName)
    await expect(target, `按姓名「${targetName}」应收窄到唯一一行`).toHaveCount(1)

    await target.getByRole('button', { name: '重置密码' }).click()
    const confirmBox = visibleMessageBox(page)
    await expect(confirmBox.locator('.el-message-box__title')).toHaveText('确认重置密码')
    await confirmBox.getByRole('button', { name: '确认' }).click()

    await expect(adminMessage(page), '无账号医生的重置应落成 10409 的冲突文案').toContainText(CONFLICT_TOAST)
    await expect(page.locator('.el-message-box:visible'), '被拒后不该出现一次性凭据弹层').toHaveCount(0)
    await expect(target.locator('td').nth(1), '被拒的这行仍应是无账号形态（登录账号列没变）').toHaveText('—')
  })

  /**
   * 收尾：只清本用例自建的那枚手机号（服务端 Phone 指空串＝清空，
   * repo/doctor_accounts_t314.go 的 DoctorAccountUpdate 注释；前端 phonePatch 永不发空串，
   * utils/phoneField.ts，所以这一腿只能走接口）。清号的动机见 9.2 起点自证那一格：
   * 号留在库里，下一轮既读不回明文（只剩掩码），「手机号能登录」那条就退化成断言上一轮的号。
   *
   * 医护账号行本身清不掉（无删除端点），每轮复用同一枚 ⇒ 残留恒为 1 行，这里把它报出来。
   */
  test.afterAll(async ({ browser }) => {
    if (!fixture) return
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const token = await getAuthToken(page)
      if (!token) throw new Error('afterAll 取不到 admin JWT，无法清空测试医护的手机号')

      const r = await callApi(page, 'PUT', `/api/v1/admin/doctors/${fixture.doctorId}`, {
        token,
        body: { phone: '' },
      })
      console.log(
        `[t632-账号段][还原] PUT /admin/doctors/${fixture.doctorId} {phone:""} → status=${r.status} code=${r.code}`,
      )
      expect(r.code, `清空手机号应回 code=0，实得 status=${r.status} code=${r.code} message=${r.message}`).toBe(0)

      const back = await callOk<DoctorDTO[]>(page, 'GET', '/api/v1/doctors', { token, why: '清号后回读' })
      const row = back.find((d) => d.doctorId === fixture!.doctorId)
      expect(row, '还原后该医护应仍在册（只清号不删行）').toBeTruthy()
      expect(row!.phoneState, '清号后 phoneState 应回到 absent').toBe('absent')

      const leftover = back.filter((d) => (d.name || '').startsWith(DOCTOR_PREFIX))
      console.log(
        `[t632-账号段][残留报备] 自建医护 ${leftover.length} 行无删除端点可撤，本轮沿用 doctorId=${fixture.doctorId} username=${fixture.username}`,
      )
      expect(leftover.length, `前缀「${DOCTOR_PREFIX}」的行应恒为 1 枚（复用不新建），实得 ${leftover.length}`).toBe(1)
    } catch (err) {
      console.log(
        `[t632-账号段][残留报备] 手机号未确认清空，doctorId=${fixture.doctorId}，原因=${(err as Error).message}`,
      )
      throw err
    } finally {
      await ctx.close()
    }
  })
})
