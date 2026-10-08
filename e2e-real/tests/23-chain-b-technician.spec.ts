import { test, expect, type Page, type Response } from '@playwright/test'
import { isHiddenAlertType } from '@bracesync/shared-utils'
import { resolveH5Origin } from '../h5-origin'

/**
 * T462 S5 · 链 B 技师端（真实模式 / staging）
 *
 * 链路口径取自 T462 设计稿 §四「链 B」与 §七 S5（PM 2598 裁的丙案）：
 *   B1 真 UI 手机号+口令登录 → 首页顶栏出现该技师的姓名与工号
 *   B2 安装记录浏览（只读）→ 页头计数 = 接口 total、逐行值级对平、筛选态行数 = 接口该档行数
 *   B4 告警跨页身份（只读）→ 详情弹层的 alertId 与所点行同一，且 records 来回后仍是同一条
 *
 * 为什么这三段值得单独一条链（而不是沿用 admin 侧 03-alerts 那 6 条）：
 *   admin 侧读的是同一批后端数据，但走的是后台前端的展示层。技师端是另一套产物
 *   （apps/tech-miniapp，uni-app H5，hash 路由），页头计数、筛选 chip、详情弹层都是它自己的实现，
 *   T433 那三条缺陷（页头把「本页条数」当总数、取数只发一页、告警页在首页无入口）就长在这一侧。
 *   「跨页跳转后仍锚在同一条告警」这一段在两个前端里都没有先例，是链 B 的新覆盖。
 *
 * 三态现况（本轮实测，2026-09-29 只读预检 .tmp-verify/t462-s5-shape-probe.txt）：
 *   告警 total=93，其中 processStatus=pending 89 / processed 3 / processing 1，另有 1 行
 *   type=pressure_fluctuation 属展示侧隐藏（判据真源 packages/shared-utils 的 isHiddenAlertType，界面不展示、数据不删）。
 *   于是「待处理 + 已处理 = 全部」这个二态直觉在现网不成立：processing 那行落在两个 chip 之外，
 *   而卡片脚注又按 `processStatus === 'pending' ? '待处理' : '已处理'` 把它显示成「已处理」。
 *   这是 T433 已登记的三态映射遗留（pages/alerts/index.vue:57 的三元式 + utils/alertDisplay.ts:51 的注），
 *   本用例不修它、也不假装它不存在：判据写成「全部 = 待处理 + 已处理 + processing 行数」，
 *   并显式钉住「脚注显示为已处理的行数 = 已处理 chip 行数 + processing 行数」——
 *   摘掉隐藏过滤、改掉 chip 判据或改掉三态映射都会判红，而不是悄悄换口径。
 *
 * 凭据纪律（丙案）：账号与口令只从环境变量 TECHE2E_TECH_ACCOUNT / TECHE2E_TECH_PASSWORD 读，
 *   缺任一值即抛红，绝不 test.skip —— 静默跳过的绿不是证据，这一条与链 C 的「三锚不成立即抛」同一律。
 *   值不落日志、不落卡、不落仓：本文件只打印长度。B1 的失败腿用硬编码假号（现网无此技师），
 *   不需要凭据也能跑，所以缺凭据时这一腿仍会先把「表单真的驱动了部署产物」证一遍再撞凭据门。
 *
 * 零写：全链只发 GET 与 POST /api/v1/tech/login。登录成功只签令牌、失败统一 401，
 *   两条出口在 handler.go:596-640 techLogin 里都排在任何库表写之前（行号按合并头 3792f79 实测；技师域没有删除路由；B3 采集与 B5 处理备注
 *   属不可逆段，本轮不做）。每条 test 收尾都跑 assertZeroBusinessWrites() 自证，而不是靠注释声明。
 *
 * 同源约束：技师端产物把 API 基址在构建期写死（apps/tech-miniapp/.env.staging = http://hbksd.com.cn:81，
 *   经 define 注入为 __API_BASE_URL__），全仓没有 CORS 放行 —— 页面从别的源打开，应用自己的请求就跨源。
 *   所以链 B 固定从产物内写死的那个源打开，并在收尾断言「请求源 == 页面源」。
 *   另外「产物在架」不能只看 HTTP 码：瞎编的深链 /tech-h5/zzz-not-a-real-deep-path 也回 200、
 *   正文与首页同长（SPA fallback，本轮实测），所以这里断的是渲染出来的页面本身（uni-page 的 data-page）。
 *
 * 不动既有面：不改 real-helpers.ts、不改 playwright.real.config.ts、不碰 apps/tech-miniapp 的任何源码，
 *   既有 e2e-real 用例的断言语义一条没动（链 B 独立成这一个新文件，实测 --list 只 +3 条）。CI 侧只往
 *   .github/workflows/e2e.yml 的「Run e2e-real against staging」env 块补两个 secret 注入；
 *   缺了那两行，值到不了用例，登录腿必红 —— 那是设计意图，不是偶发。
 */

/** 技师端产物内写死的源（见文件头「同源约束」）；T614 起由 ../h5-origin 统一解析并点名缺失变量 */
const H5_ORIGIN = resolveH5Origin('E2E_TECH_H5_URL', 'T462 链 B 技师端')

/** uni-app H5 把 pages.json 首页（登录页）映射成 '#/'，深链走 hash（本轮实测） */
const PAGE_LOGIN = `${H5_ORIGIN}/tech-h5/#/`
const PAGE_RECORDS = `${H5_ORIGIN}/tech-h5/#/pages/records/index`
/** 告警页在首页没有入口（T433 缺陷四；e2e/tech-helpers.ts 的 techRoutes.alerts 注释同一口径），hash 直达是现网唯一通道 */
const PAGE_ALERTS = `${H5_ORIGIN}/tech-h5/#/pages/alerts/index`

/** 技师端存储键（apps/tech-miniapp/src/utils/token.ts:1-2；H5 端 uni.setStorageSync 底层即 localStorage，键原样） */
const LS_TOKEN_KEY = 'bracesync_tech_token'
const LS_TECH_ID_KEY = 'bracesync_tech_id'

const ACCOUNT_ENV = 'TECHE2E_TECH_ACCOUNT'
const PASSWORD_ENV = 'TECHE2E_TECH_PASSWORD'

/** 硬编码假凭据：只用来证「失败分支也在」，它不需要真值，也就不该成为「缺凭据就跳过」的借口 */
const BOGUS_PHONE = '13900000009'
const BOGUS_PASSWORD = 'zzProbe1'
/** 失败腿的唯一合法出口（本轮本机真跑：http=401 code=10401，页面 toast 同句，storage 不写） */
const LOGIN_FAIL_TOAST = '手机号或密码错误'

/** 两页各自写死的单页大小（pages/records/index.vue:89、pages/alerts/index.vue:91） */
const RECORDS_PAGE_SIZE = 20
const ALERTS_PAGE_SIZE = 50

/** 链 B 允许的唯一非 GET 请求 */
const ALLOWED_WRITE = 'POST /api/v1/tech/login'

interface TechLoginData {
  token: string
  techId: string
  name: string
  teamId: string
  role: string
}

interface InstallRow {
  installId: string
  deviceId: string
  patientId: string
  patientName: string | null
  wifiStatus: string
  baselineId: string | null
}

interface AlertRow {
  alertId: string
  type: string
  detail: string
  deviceId: string
  patientId: string
  processStatus: string
}

/**
 * 凭据门（丙案）。缺失即抛，不 skip；只打印长度。
 *
 * 顺带校形状：登录页按 /^1\d{10}$/ 校验手机号、按 6-16 位校验口令
 * （pages/login/index.vue:83-91）。若 secret 存的东西过不了页面自己的校验，接口根本不会被调到，
 * 那时用例红的是「夹具不对」却报成「登录失败」—— 所以这一格在这里分开判。
 */
function requireTechCredentials(why: string): { account: string; password: string } {
  const account = (process.env[ACCOUNT_ENV] ?? '').trim()
  const password = process.env[PASSWORD_ENV] ?? ''
  const missing = [account ? null : ACCOUNT_ENV, password ? null : PASSWORD_ENV].filter(Boolean)
  if (missing.length > 0) {
    throw new Error(
      `T462 链 B 缺技师凭据（${why}）：环境变量 ${missing.join(' / ')} 未设置或为空。` +
        `按 PM 2598 裁的丙案，这里必须判红而不是跳过 —— 静默跳过的绿不是证据。` +
        `CI 侧应在 .github/workflows/e2e.yml 的 env 块注入 secrets.TECHE2E_TECH_ACCOUNT / secrets.TECHE2E_TECH_PASSWORD。`,
    )
  }
  if (!/^1\d{10}$/.test(account)) {
    throw new Error(
      `T462 链 B 的 ${ACCOUNT_ENV} 形状不是 11 位手机号（登录页 isValidPhone 会先于接口拦下，` +
        `用例红了也测不到登录逻辑）。实得长度 ${account.length}。`,
    )
  }
  if (password.length < 6 || password.length > 16) {
    throw new Error(
      `T462 链 B 的 ${PASSWORD_ENV} 长度 ${password.length} 落在登录页校验窗口 6-16 之外` +
        `（pages/login/index.vue:87），接口永远不会被调到。`,
    )
  }
  console.log(
    `[t462-s5][凭据门] ${why} | ${ACCOUNT_ENV} len=${account.length} | ${PASSWORD_ENV} len=${password.length}（值不落日志）`,
  )
  return { account, password }
}

/**
 * 页面自己发出的 /api/v1 请求采集。
 *
 * requests 带 query（用来看单页大小与页数）、writes 记 method + pathname、
 * responses 存 GET 的 Response 对象，等页面 settle 后统一取体 —— 在事件回调里 await 会和处理顺序打架。
 * 对平基准用「页面自己收到的响应」而不是用例另发一次同样的 GET：后者与页面渲染不在同一时刻，
 * staging 上有别的轮次在写时就会行行错位，把「逐行值级对平」变成随机红。
 */
interface NetLog {
  requests: string[]
  writes: { line: string; origin: string }[]
  responses: Map<string, Response[]>
  origins: Set<string>
}

function attachNet(page: Page): NetLog {
  const net: NetLog = { requests: [], writes: [], responses: new Map(), origins: new Set() }
  page.on('request', (req) => {
    const u = new URL(req.url())
    if (!u.pathname.startsWith('/api/v1/')) return
    net.requests.push(`${req.method()} ${u.pathname}${u.search}`)
    net.origins.add(u.origin)
    if (req.method() !== 'GET') net.writes.push({ line: `${req.method()} ${u.pathname}`, origin: u.origin })
  })
  page.on('response', (res) => {
    const u = new URL(res.url())
    if (!u.pathname.startsWith('/api/v1/') || res.request().method() !== 'GET') return
    const bucket = net.responses.get(u.pathname) ?? []
    bucket.push(res)
    net.responses.set(u.pathname, bucket)
  })
  return net
}

/** 每次整页导航前调用：上一轮的响应留着会和这一轮混成重复行 */
function resetCapturedResponses(net: NetLog): void {
  net.responses.clear()
}

interface Captured<T> {
  total: number
  rows: T[]
  pages: number
  pageSizes: number[]
}

async function capturePaged<T>(net: NetLog, pathname: string, why: string): Promise<Captured<T>> {
  const list = net.responses.get(pathname) ?? []
  if (list.length === 0) {
    throw new Error(`${why}：页面没有发出 GET ${pathname}，采集不到接口侧的行 —— 断言无从对平`)
  }
  let total = -1
  const rows: T[] = []
  const pageSizes: number[] = []
  for (const res of list) {
    const body = (await res.json().catch(() => null)) as {
      code?: number
      message?: string
      data?: { total?: number; list?: T[] }
    } | null
    if (!body || body.code !== 0 || !body.data) {
      throw new Error(`${why}：GET ${pathname} 响应体不是 code=0 的分页信封，实得 ${JSON.stringify(body)?.slice(0, 160)}`)
    }
    if (typeof body.data.total !== 'number') {
      throw new Error(`${why}：GET ${pathname} 没回 total，页头计数与接口无从对平`)
    }
    pageSizes.push(Number(new URL(res.url()).searchParams.get('pageSize') ?? '0'))
    total = body.data.total
    rows.push(...(body.data.list ?? []))
  }
  return { total, rows, pages: list.length, pageSizes }
}

/**
 * 页头计数文案解析。两形：`共 N 条记录` / `共 N 条告警，已加载 M 条`
 * 后者是 fetchAllPages 触到 maxPages 闸门的 truncated 态（pages/records/index.vue:117-118、pages/alerts/index.vue:109-114）。
 */
function parseCountSubtitle(text: string): { header: number; loaded: number | null } {
  const m = /^共 (\d+) 条(?:记录|告警)(?:，已加载 (\d+) 条)?$/.exec(text.trim())
  if (!m) {
    throw new Error(`页头计数文案不是「共 N 条记录/告警」这一形，实得 ${JSON.stringify(text)}`)
  }
  return { header: Number(m[1]), loaded: m[2] === undefined ? null : Number(m[2]) }
}

/**
 * 导航并等页面渲染出来 —— 按 uni-page 的 data-page 认页面，不按 HTTP 码（见文件头 SPA fallback）。
 * anchor 再给一个页内元素，证明这一页的数据腿真的跑到了（空列表会落到 empty-card 而不是卡片）。
 */
async function gotoTechPage(page: Page, url: string, dataPage: string, anchor: string, net: NetLog): Promise<void> {
  resetCapturedResponses(net)
  await page.goto(url, { waitUntil: 'domcontentloaded' })
  await expect(page.locator(`uni-page[data-page="${dataPage}"]`), `应渲染出 ${dataPage} 这一页`).toHaveCount(1, { timeout: 25_000 })
  await expect(page.locator(anchor).first(), `${dataPage} 的锚点 ${anchor} 应出现（空列表会落成 empty-card）`).toBeVisible({
    timeout: 25_000,
  })
}

/**
 * 真 UI 登录：填手机号 / 口令、勾协议、点登录，返回接口响应。
 *
 * 选择器全部按 staging 已部署产物实测（.tmp-verify/t462-s5-login-dom.txt、t462-s5-toast-home-probe.txt）：
 * 两个 .input-field 是 uni-input 宿主，原生 input 在其 div.uni-input-wrapper 里（内层高度≈0 ⇒ 必须 force）；
 * 协议框 .agree-row .checkbox 勾上后 class 追加 checkbox-checked；按钮是 .btn-primary（没有 form 提交）。
 */
async function driveLoginForm(page: Page, net: NetLog, account: string, password: string): Promise<Response> {
  await gotoTechPage(page, PAGE_LOGIN, 'pages/login/index', '.input-field', net)
  const inputs = page.locator('.input-field input')
  await expect(inputs, '登录页应有两个 uni-input 原生输入框').toHaveCount(2)
  await inputs.nth(0).fill(account, { force: true })
  await inputs.nth(1).fill(password, { force: true })
  await expect(inputs.nth(0), '手机号应真的写进了控件').toHaveValue(account)
  await expect(inputs.nth(1), '口令应真的写进了控件').toHaveValue(password)
  const checkbox = page.locator('.agree-row .checkbox')
  // 勾选态存在应用 store 里且登录 401 后不重置：第二次进登录页它可能已是勾上的，再点一次会把它 toggle 回未勾（CI 23.1 实红）
  const agreeClassBefore = (await checkbox.getAttribute('class')) ?? ''
  if (!agreeClassBefore.includes('checkbox-checked')) await checkbox.click()
  await expect(
    checkbox,
    `勾协议后 .checkbox 应带上 checkbox-checked（进门时 class="${agreeClassBefore}"）`,
  ).toHaveClass(/checkbox-checked/)
  const waiting = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v1/tech/login', { timeout: 25_000 })
  await page.locator('.btn-primary').click()
  return waiting
}

/** 读回应用自己写入的会话（不自己 setItem —— 那等于把「应用真的存下了」这一格跳过） */
async function readTechStorage(page: Page): Promise<{ token: string | null; techId: string | null }> {
  return page.evaluate((keys: string[]) => {
    const t = localStorage.getItem(keys[0])
    const i = localStorage.getItem(keys[1])
    return { token: t === null ? null : t, techId: i === null ? null : i }
  }, [LS_TOKEN_KEY, LS_TECH_ID_KEY])
}

/**
 * 完整技师会话：凭据门 → 真 UI 登录 → 首页顶栏渲染出姓名与工号。
 * 返回接口侧登录结果，供各段做值级对平（token 不外传，页面自己带着）。
 */
async function loginAsTechnician(page: Page, net: NetLog, why: string): Promise<TechLoginData> {
  const { account, password } = requireTechCredentials(why)
  const res = await driveLoginForm(page, net, account, password)
  expect(res.status(), '技师登录应回 200').toBe(200)
  const body = (await res.json()) as { code?: number; message?: string; data?: TechLoginData }
  expect(body.code, `技师登录信封 code 应为 0，实得 ${body.code} message=${body.message}`).toBe(0)
  const data = body.data
  if (!data || !data.token || !data.techId) {
    throw new Error(`技师登录响应缺 token/techId（契约 model.go:391-398），实得键=${Object.keys(data ?? {}).join(',')}`)
  }
  expect(data.role, '技师登录签的角色应是 technician').toBe('technician')

  const stored = await readTechStorage(page)
  expect(stored.token, '登录成功后应用应把接口回的 token 写进 bracesync_tech_token').toBe(data.token)
  expect(stored.techId, '登录成功后应用应把接口回的 techId 写进 bracesync_tech_id').toBe(data.techId)

  // 成功后页面 reLaunch 到首页前有 1500ms 延时（pages/login/index.vue:128-136），用断言重试而不是 sleep
  await expect(page.locator('uni-page[data-page="pages/home/index"]'), '登录后应落在首页，且是渲染出来的页面').toHaveCount(
    1,
    { timeout: 25_000 },
  )
  await expect(page.locator('.user-role'), '首页顶栏工号应是接口回的 techId').toContainText(data.techId)
  await expect(page.locator('.user-name'), '首页顶栏姓名应是接口回的 name（name 不落存储，只活在登录后这一跳的 store 里）').toHaveText(
    data.name,
  )
  console.log(
    `[t462-s5][登录腿] ${why} | techId=${data.techId} | nameLen=${data.name.length} | teamIdLen=${data.teamId.length} | tokenLen=${data.token.length}（值不落日志）`,
  )
  return data
}

/** 零写自证：除技师登录外页面不得发出任何非 GET；并显式钉住请求源与页面源同源 */
function assertZeroBusinessWrites(net: NetLog, testId: string): void {
  const offenders = net.writes.filter((w) => w.line !== ALLOWED_WRITE)
  expect(
    offenders,
    `${testId} 零写自查：链 B 只允许 ${ALLOWED_WRITE}（成功只签令牌、失败统一 401，两条出口都在任何库表写之前，handler.go:596-640），实得非 GET 明细=${JSON.stringify(net.writes)}`,
  ).toEqual([])
  expect([...net.origins], `${testId} 同源自查：技师端应用发出的请求应全部来自产物内写死的源`).toEqual([H5_ORIGIN])
  console.log(
    `[${testId}][零写自查] 非 GET = ${JSON.stringify(net.writes.map((w) => w.line))} | /api/v1 请求 ${net.requests.length} 条 = ${JSON.stringify(net.requests)}`,
  )
}

/** 卡片内结构化取值：一次 evaluate 拿全行，比逐格 locator 稳（行是 v-for 出来的，模板加字段不会串格） */
async function readRecordCards(page: Page): Promise<{ deviceId: string; fields: Record<string, string> }[]> {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll('.record-card')).map((el) => {
      const fields: Record<string, string> = {}
      el.querySelectorAll('.info-item').forEach((item) => {
        const k = (item.querySelector('.info-label')?.textContent ?? '').trim()
        const v = (item.querySelector('.info-value')?.textContent ?? '').trim()
        if (k) fields[k] = v
      })
      return { deviceId: (el.querySelector('.record-device')?.textContent ?? '').trim(), fields }
    }),
  )
}

async function readAlertCards(page: Page): Promise<{ detail: string; statusTag: string }[]> {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll('.alert-card')).map((el) => ({
      detail: (el.querySelector('.alert-detail')?.textContent ?? '').trim(),
      statusTag: (el.querySelector('.status-tag')?.textContent ?? '').trim(),
    })),
  )
}

test.describe('23-链 B 技师端（T462 S5）', () => {
  test.use({ viewport: { width: 390, height: 900 } })

  test('23.1 B1 真 UI 登录：假凭据必 401 且不落会话，真凭据才签出 technician 令牌', async ({ page }) => {
    const net = attachNet(page)

    // 失败腿（不需要凭据，所以缺凭据时这一腿先把「表单真的驱动了部署产物」证一遍，再撞凭据门）
    const bad = await driveLoginForm(page, net, BOGUS_PHONE, BOGUS_PASSWORD)
    expect(bad.status(), '不存在的技师号应回 401（统一防枚举口径，不给「号是否存在」留差异）').toBe(401)
    const badBody = (await bad.json().catch(() => null)) as { code?: number } | null
    expect(badBody?.code, '失败腿信封码应是 10401').toBe(10401)
    await expect(page.locator('uni-toast .uni-simple-toast__text'), '失败腿应落到页面自撰的那句提示').toHaveText(LOGIN_FAIL_TOAST)
    const afterBad = await readTechStorage(page)
    expect(afterBad.token, '失败腿不得写入会话').toBeNull()
    expect(afterBad.techId, '失败腿不得写入会话').toBeNull()
    await expect(page.locator('uni-page[data-page="pages/login/index"]'), '失败腿应仍停在登录页').toHaveCount(1)

    // 成功腿
    const data = await loginAsTechnician(page, net, '23.1 成功腿')
    expect(data.techId, '签出的 techId 应非空（与首页顶栏工号同源那一格已在 loginAsTechnician 里断过）').toBeTruthy()
    assertZeroBusinessWrites(net, '23.1')
    expect(net.writes.filter((w) => w.line === ALLOWED_WRITE).length, '非 GET 应恰为两条登录 POST：失败腿与成功腿各一条').toBe(2)
  })

  test('23.2 B2 安装记录浏览（只读）：页头数 = 接口 total、逐行值级对平、筛选态行数 = 接口该档行数', async ({ page }) => {
    const net = attachNet(page)
    await loginAsTechnician(page, net, '23.2 前置会话')

    await gotoTechPage(page, PAGE_RECORDS, 'pages/records/index', '.record-card', net)
    const captured = await capturePaged<InstallRow>(net, '/api/v1/install-records', '23.2')
    const counts = parseCountSubtitle(await page.locator('.page-subtitle').innerText())
    expect(
      counts.loaded,
      `页头出现「已加载 M 条」= fetchAllPages 触到 maxPages 闸门（20 页 × 单页 ${RECORDS_PAGE_SIZE}），本轮判据按取满写；现网 ${captured.total} 条远不到顶`,
    ).toBeNull()

    const domCards = await readRecordCards(page)
    expect(domCards.length, '渲染行数应与页头计数一致（T433 缺陷一的现行判据）').toBe(counts.header)
    expect(domCards.length, '渲染行数应与接口 total 对平').toBe(captured.total)
    expect(captured.rows.length, '页面自己收到的行数应等于 total（逐页取满，改前只发一页）').toBe(captured.total)
    expect(
      domCards.map((c) => c.deviceId),
      '设备位应逐行等于接口回的行序（数组序＝展示序）',
    ).toEqual(captured.rows.map((r) => r.deviceId))
    expect(
      domCards.map((c) => c.fields['患者']),
      '患者位应是 patientName 回落 patientId（后端空值语义是空串）',
    ).toEqual(captured.rows.map((r) => r.patientName || r.patientId))
    expect(
      domCards.map((c) => c.fields['基线']),
      '基线位应是 baselineId 回落「未保存」',
    ).toEqual(captured.rows.map((r) => r.baselineId || '未保存'))

    const pageRequests = net.requests.filter((r) => r.startsWith('GET /api/v1/install-records'))
    expect(pageRequests.every((r) => r.includes(`pageSize=${RECORDS_PAGE_SIZE}`)), '安装记录请求应带页面写死的单页大小').toBe(true)
    expect(captured.pageSizes.every((n) => n === RECORDS_PAGE_SIZE), '实际发出的单页大小应与常量一致').toBe(true)
    expect(
      captured.pages,
      `取数页数不得少于 total=${captured.total} 在单页 ${RECORDS_PAGE_SIZE} 下所需的页数（改前只发 1 页，正是 T433 缺陷一）`,
    ).toBeGreaterThanOrEqual(Math.ceil(captured.total / RECORDS_PAGE_SIZE))

    // 筛选态：本地过滤的行数必须等于接口侧该档的行数（现网只有两档有行）
    const wifiCount = (status: string) => captured.rows.filter((r) => r.wifiStatus === status).length
    const rowsUnder = async (chipLabel: string): Promise<number> => {
      await page.locator('.seg-btn').filter({ hasText: new RegExp(`^${chipLabel}$`) }).click()
      await page.waitForTimeout(300)
      return page.locator('.record-card').count()
    }
    const connected = await rowsUnder('已连接')
    const unconfigured = await rowsUnder('未配置')
    const all = await rowsUnder('全部 WiFi')
    expect(connected, '「已连接」档行数应等于接口该档行数').toBe(wifiCount('connected'))
    expect(unconfigured, '「未配置」档行数应等于接口该档行数').toBe(wifiCount('unconfigured'))
    expect(all, '「全部 WiFi」档行数应等于取满的行数').toBe(captured.rows.length)
    console.log(
      `[23.2][安装记录对平] total=${captured.total} 渲染=${all} 已连接=${connected} 未配置=${unconfigured} 其余档（failed/skipped）=${all - connected - unconfigured}`,
    )

    assertZeroBusinessWrites(net, '23.2')
  })

  test('23.3 B4 告警跨页身份（只读）：详情弹层的 alertId 与所点行同一，records 来回后仍是同一条', async ({ page }) => {
    const net = attachNet(page)
    const session = await loginAsTechnician(page, net, '23.3 前置会话')

    await gotoTechPage(page, PAGE_ALERTS, 'pages/alerts/index', '.alert-card', net)
    const captured = await capturePaged<AlertRow>(net, '/api/v1/alerts', '23.3')
    const counts = parseCountSubtitle(await page.locator('.page-subtitle').innerText())
    expect(counts.loaded, '页头出现「已加载」= 取数触到 maxPages 闸门，本轮判据按取满写').toBeNull()

    const visible = captured.rows.filter((r) => !isHiddenAlertType(r.type))
    const hiddenRows = captured.rows.length - visible.length
    const pending = visible.filter((r) => r.processStatus === 'pending')
    const processed = visible.filter((r) => r.processStatus === 'processed')
    const processing = visible.filter((r) => r.processStatus === 'processing')

    const cardsAll = await readAlertCards(page)
    expect(cardsAll.map((c) => c.detail), '渲染行的详情位应逐行等于接口回的可见行（隐藏 ${hiddenRows} 行属展示侧过滤，数据不删）').toEqual(
      visible.map((r) => r.detail),
    )
    expect(cardsAll.length, `可见行数应等于「接口行数 - 展示侧隐藏行数」（隐藏 ${hiddenRows} 行）`).toBe(visible.length)
    expect(cardsAll.length, '渲染行数应与页头计数一致').toBe(counts.header)

    const rowsUnder = async (chipLabel: string): Promise<number> => {
      await page.locator('.seg-btn').filter({ hasText: new RegExp(`^${chipLabel}$`) }).click()
      await page.waitForTimeout(300)
      return page.locator('.alert-card').count()
    }
    const pendingRows = await rowsUnder('待处理')
    const processedRows = await rowsUnder('已处理')
    const allRows = await rowsUnder('全部')
    expect(pendingRows, '「待处理」chip 行数应等于接口 pending 的可见行数').toBe(pending.length)
    expect(processedRows, '「已处理」chip 行数应等于接口 processed 的可见行数').toBe(processed.length)
    expect(
      allRows,
      `三态守恒：全部 = 待处理 + 已处理 + processing（现网 processing=${processing.length} 行落在两个 chip 之外，二态相加会漏它）`,
    ).toBe(pending.length + processed.length + processing.length)
    // 脚注按 `processStatus === 'pending' ? '待处理' : '已处理'` 渲染，于是 processing 行显示成「已处理」，
    // 「脚注说已处理」比「已处理 chip」正好多出 processing 那一格 —— 这就是 T433 登记的三态映射遗留。
    // 用断言钉住现网实况：改 chip 判据或改三态映射都会在这儿判红，而不是悄悄换口径。
    const tagProcessed = (await readAlertCards(page)).filter((c) => c.statusTag === '已处理').length
    expect(tagProcessed, '卡片脚注「已处理」的行数应等于 chip 已处理行数 + processing 行数').toBe(processedRows + processing.length)
    if (processing.length > 0) {
      console.log(
        `[23.3][三态缺口实况] processing 行 alertId=${processing.map((r) => r.alertId).join(',')} | 全部=${allRows} 待处理=${pendingRows} 已处理=${processedRows} 脚注已处理=${tagProcessed}`,
      )
    }

    // 跨页身份锚点：取一条详情文案在可见行里唯一、且不在首位的行（首位会被别的轮次新增顶掉）
    const detailCount = new Map<string, number>()
    for (const r of visible) detailCount.set(r.detail, (detailCount.get(r.detail) ?? 0) + 1)
    const uniq = visible.filter((r) => detailCount.get(r.detail) === 1)
    if (uniq.length < 3) {
      throw new Error(`23.3：详情文案唯一的可见告警不足 3 条（实得 ${uniq.length}），无法稳定锚定跨页身份`)
    }
    const anchor = uniq[Math.floor(uniq.length / 2)]
    const anchorIndex = visible.findIndex((r) => r.alertId === anchor.alertId)
    // 先开一条别的告警再开锚点：证明弹层标题是跟着所点行走的，不是残留值也不是常量
    const neighbourIndex = anchorIndex > 0 ? anchorIndex - 1 : anchorIndex + 1
    const openDetail = async (index: number): Promise<string> => {
      const expected = `告警 ${visible[index].alertId}`
      await page.locator('.alert-card').nth(index).click()
      const title = page.locator('uni-modal .uni-modal__title')
      await expect(title, `弹层标题应是所点那行的「告警 ${visible[index].alertId}」`).toHaveText(expected, { timeout: 10_000 })
      const body = (await page.locator('uni-modal').innerText()).replace(/\s+/g, ' ')
      expect(body, '弹层设备位应与接口同源').toContain(`设备: ${visible[index].deviceId}`)
      expect(body, '弹层详情位应与接口同源').toContain(`详情: ${visible[index].detail}`)
      expect(body, '弹层状态位应落三态映射（processing 也显示「已处理」，已随 T433 登记）').toContain('状态:')
      await page.locator('uni-modal .uni-modal__btn_primary').click()
      await page.waitForTimeout(300)
      return expected
    }
    const neighbourTitle = await openDetail(neighbourIndex)
    const firstTitle = await openDetail(anchorIndex)
    expect(neighbourTitle, '邻格与锚点必须是两条不同告警，否则「标题跟着行走」这一格没被证到').not.toBe(firstTitle)

    // 跨页：去安装记录再回告警页（告警页在首页无入口，回它只能 hash 直达，正是 T433 缺陷四的现网实况）
    await gotoTechPage(page, PAGE_RECORDS, 'pages/records/index', '.record-card', net)
    await gotoTechPage(page, PAGE_ALERTS, 'pages/alerts/index', '.alert-card', net)
    // uni-app H5 保留页面实例：hash 直达一个已访问过的页面不会再发 GET（CI 23.3 实红：回页采不到接口侧）。
    // 整页 reload 逼一次真请求，判据反而更严 —— 断的是「刷新回来仍是同一会话里的同一份数据」。
    resetCapturedResponses(net)
    const refetch = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v1/alerts', { timeout: 25_000 })
    await page.reload({ waitUntil: 'domcontentloaded' })
    await refetch
    await expect(page.locator('uni-page[data-page="pages/alerts/index"]'), '刷新后应仍落在告警页').toHaveCount(1, {
      timeout: 25_000,
    })
    await expect(page.locator('.alert-card').first(), '刷新后告警列表应重新渲染出行').toBeVisible({ timeout: 25_000 })
    // 页面按 pageSize=50 逐页取满（现网 93 条 ⇒ 两跳）。第一跳落地就往下走会让「接口侧行数」少一页，
    // 于是下面那句逐行对平变成随机红，所以先等它把 total 取满。
    await expect
      .poll(
        async () => {
          const c = await capturePaged<AlertRow>(net, '/api/v1/alerts', '23.3 回页取满中')
          return c.rows.length >= c.total ? c.rows.length : 0
        },
        { message: '23.3 回页：刷新后页面应把整份告警重新取满（采集到的行数达到接口 total）', timeout: 25_000, intervals: [500, 1000] },
      )
      .toBeGreaterThan(0)
    const captured2 = await capturePaged<AlertRow>(net, '/api/v1/alerts', '23.3 回页')
    const visible2 = captured2.rows.filter((r) => !isHiddenAlertType(r.type))
    const counts2 = parseCountSubtitle(await page.locator('.page-subtitle').innerText())
    const cardsBack = await readAlertCards(page)
    expect(cardsBack.map((c) => c.detail), '重新进入告警页后应仍是同一份取满数据（页头数与列表不许分叉）').toEqual(
      visible2.map((r) => r.detail),
    )
    expect(cardsBack.length, '回页后的渲染行数应与页头计数一致').toBe(counts2.header)
    const indexBack = visible2.findIndex((r) => r.alertId === anchor.alertId)
    expect(indexBack, '回到告警页后应仍能找到锚点那条告警').toBeGreaterThanOrEqual(0)
    const secondTitle = await openDetailAt(indexBack, visible2)
    expect(secondTitle, '跨页回来后弹层仍是同一条告警（这一段是设计稿 §四 B4 声明的新覆盖）').toBe(firstTitle)

    const stored = await readTechStorage(page)
    expect(stored.techId, '回页后仍在同一技师会话里').toBe(session.techId)

    assertZeroBusinessWrites(net, '23.3')

    /** 回页后的 visible 是另一个数组，故这里带上来源；判据与 openDetail 完全一致 */
    async function openDetailAt(index: number, rows: AlertRow[]): Promise<string> {
      const expected = `告警 ${rows[index].alertId}`
      await page.locator('.alert-card').nth(index).click()
      const title = page.locator('uni-modal .uni-modal__title')
      await expect(title, `回页后弹层标题应是所点那行的「告警 ${rows[index].alertId}」`).toHaveText(expected, { timeout: 10_000 })
      const body = (await page.locator('uni-modal').innerText()).replace(/\s+/g, ' ')
      expect(body, '回页后弹层详情位应与接口同源').toContain(`详情: ${rows[index].detail}`)
      await page.locator('uni-modal .uni-modal__btn_primary').click()
      await page.waitForTimeout(300)
      return expected
    }
  })
})
