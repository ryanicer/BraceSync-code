import { test, expect, type Page, type Locator } from '@playwright/test'
import { realLogin, getAuthToken, uniqueName, E2E_PATIENT_NAME_PREFIX } from '../real-helpers'
import { resolveH5Origin } from '../h5-origin'

/**
 * T462 S4 · 链 C 患者自助资料编辑（真实模式 / staging）
 *
 * 链路口径取自 T462 设计稿 §四「链 C」与 §七 S4：
 *   admin 建档（自建患者，T053 前缀）→ admin 设登录口令（T477）→ 患者口令登录拿 JWT
 *   → 注入患者端 H5 存储 → 真 UI 打开「我的」页 → 点编辑 → 改昵称/年龄/身高 → 保存
 *   → 患者侧读端点值级回读 → 域侧删掉自建患者（T467）并核对行数守恒
 *
 * 覆盖边界（设计稿 §十三 第 1 条要求用例名自证）：
 *   本用例覆盖的是「持患者身份的档案编辑」这条真实业务链，不声称覆盖患者登录 UI。
 *   患者端登录页只有 uni.login（微信授权），CI 拿不到 wx code，所以会话只能由
 *   POST /api/v1/patient/login + 存储注入建立；登录页本身的渲染归 e2e-miniapp 真机窗口。
 *   为什么只让「资料编辑」那一腿走真 UI：写入口（PUT /api/v1/patients/:id 的白名单 +
 *   限本人）只有从前端点出来才算串到；而登录腿在 CI 里没有可用通道，硬造就是造假绿。
 *
 * 可逆性：写段产生的行只有「本用例自建的患者」，收尾用 T467 的 DELETE 整行删除，
 *   比设计稿 C3 原写的「改回原值」更彻底（不留任何 T053 残留行）。删除能力先由
 *   preflightGeneration() 在任何写之前实测确认；端点不在架就直接抛红，不会跑到一半留下脏行。
 *
 * 同源约束（本用例为什么不沿用 baseURL）：全仓没有 CORS 放行，而患者端产物把 API 基址
 *   在构建期写死（apps/patient-miniapp/.env.staging:1 = http://hbksd.com.cn:81，注入点
 *   vite.config.ts:80 的 __API_BASE_URL__）。页面从别的源打开 ⇒ 应用自己的请求跨源 ⇒ 必被
 *   浏览器拦掉。所以患者端这一腿固定从产物内写死的源打开，并在末尾显式断言「请求源 == 页面源」。
 *
 * 不动既有面：不改 real-helpers.ts、不改 playwright.real.config.ts、不新增 CI 触发路径
 *   （real 配置的 testMatch 用 glob 收集 tests 下所有 spec 文件，会自动把本文件收进每日 20:00
 *   那一轮 STRICT）。
 */

/** 患者端产物内写死的源（见文件头「同源约束」）；T614 起由 ../h5-origin 统一解析并点名缺失变量 */
const H5_ORIGIN = resolveH5Origin('E2E_PATIENT_H5_URL', 'T462 链 C 患者端')
const H5_PROFILE_PATH = '/patient-h5/#/pages/profile/index'

/** 患者端存储键（apps/patient-miniapp/src/utils/token.ts 的 TOKEN_KEY / PATIENT_ID_KEY） */
const LS_PATIENT_TOKEN_KEY = 'bracesync_token'
const LS_PATIENT_ID_KEY = 'bracesync_patient_id'

/**
 * newPatientID 的发号形状（repo/pg.go:1262-1268）：P + 四位年 + hex.EncodeToString(6 字节) = 12 位 hex。
 * 首跑实测 P20268bdcfd4628e5（P 后 16 位）—— 我最初写的 /^P\d{16}$/ 把 hex 当纯数字，恒红。
 */
const PATIENT_ID_RE = /^P\d{4}[0-9a-f]{12}$/

/**
 * 不存在的患者号，只用来探「路由在不在架」。按实测形状拼出，末尾再自证一次它匹配
 * PATIENT_ID_RE —— 手写定长零串会数错位数，探针就打到了别的形状上。
 */
const GHOST_PATIENT_ID = `P${'0'.repeat(16)}`
/** 全仓未注册的路径：负对照用 */
const NOT_REGISTERED_PATH = '/api/v1/zzz-t462-chain-c-not-registered-9f3c'
if (!PATIENT_ID_RE.test(GHOST_PATIENT_ID)) {
  throw new Error(`T462 链 C 探针患者号形状不符发号规则，代次探针打的不是真实形状：${GHOST_PATIENT_ID}`)
}

/** GET /api/v1/patient/profile 与 PUT /api/v1/patients/:id 的响应体（model.AdminPatientDTO，无 omitempty ⇒ 空值出 null） */
interface PatientProfile {
  patientId: string
  name: string
  gender: string | null
  age: number | null
  diagnosis: string | null
  cobbAngle: number | null
  heightCm: number | null
  weightKg: number | null
  emergencyContactName: string | null
  phone: string
  status: string
  teamId: string | null
}

interface Envelope {
  status: number
  contentType: string
  /** null = 响应体不是 JSON（gin 自己的 404 就是这一形） */
  code: number | null
  message: string
  data: unknown
}

/**
 * node 侧接口调用。绝对地址与 baseURL 相对地址都收；不吞错误码，原样回给调用方判。
 * 这些请求不走浏览器，所以不会（也不该）出现在下面的页面网络采集里。
 */
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
    /* 非 JSON：code 留 null，正是负对照要的形态 */
  }
  return {
    status: res.status(),
    contentType: (res.headers()['content-type'] ?? '').split(';')[0],
    code,
    message,
    data,
  }
}

/** 成功信封断言：code 必须是 0 —— 「不看 200 就算过」是这条链的红线 */
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
 * 代次前置探针（只读，零写）。
 *
 * 三步：① 未注册路径必须回非 JSON 的 gin 404 —— 这是负对照，缺了它，
 * ②③ 的「JSON 信封 code=10404」就分不清「路由在架且资源不存在」与「路由没在架」。
 * ② T477 设口令端点在架（链 C 的患者会话靠它）；③ T467 删除端点在架（收尾还原靠它）。
 * 任一不成立就 throw，不 skip：skip 会让「链 C 其实没串成」在夜巡里报绿。
 */
async function preflightGeneration(page: Page, token: string): Promise<void> {
  for (const method of ['POST', 'PUT', 'DELETE'] as const) {
    const r = await callApi(page, method, NOT_REGISTERED_PATH, { token })
    if (r.code !== null) {
      throw new Error(
        `S4 前置探针失效：未注册路径 ${method} ${NOT_REGISTERED_PATH} 应回非 JSON 的 gin 404，实得 status=${r.status} code=${r.code} ct=${r.contentType} —— 此时「JSON code=10404 ⇒ 路由在架」这条判据不成立，不能判 T477/T467`,
      )
    }
  }
  const t477 = await callApi(page, 'POST', `/api/v1/admin/patients/${GHOST_PATIENT_ID}/password`, {
    token,
  })
  const t467 = await callApi(page, 'DELETE', `/api/v1/admin/patients/${GHOST_PATIENT_ID}`, { token })
  console.log(
    `[t462-s4][代次锚] 未注册路径=非JSON404(3 法) | T477 POST …/password status=${t477.status} code=${t477.code} ct=${t477.contentType} | T467 DELETE …/patients status=${t467.status} code=${t467.code} ct=${t467.contentType}`,
  )
  if (t477.code !== 10404) {
    throw new Error(
      `staging 上 T477 设口令端点不可用（期望 JSON code=10404「患者不存在」，实得 status=${t477.status} code=${t477.code} message=${t477.message}）。链 C 的患者会话要先由 admin 给自建患者设口令，端点不在架就别往下写，否则留下删不掉的脏行`,
    )
  }
  if (t467.code !== 10404) {
    throw new Error(
      `staging 上 T467 患者删除端点不可用（期望 JSON code=10404，实得 status=${t467.status} code=${t467.code} message=${t467.message}）。没有它就还原不了自建患者，本用例不许在 staging 留行`,
    )
  }
}

/**
 * 患者端 H5 输入填充。
 *
 * uni-app 把 <input class="form-input"> 渲染成（staging 产物实测 DOM，2026-09-29 抓自
 * 弹层 outerHTML）：
 *   <uni-view class="form-row"><uni-text class="form-label"><span>昵称</span></uni-text>
 *     <uni-input class="form-input"><div class="uni-input-wrapper">
 *       <div class="uni-input-placeholder" style="display:none">请输入昵称</div>
 *       <input class="uni-input-input" type="text">
 *     </div></uni-input></uni-view>
 * 两条由此得出的口径：placeholder 是覆盖层 div、原生 input 上没有该属性（不能用
 * [placeholder=...] 定位）；内层 input 高度≈0，必须 force（同 e2e/helpers.ts:fillUniInput）。
 *
 * 定位口径：先按 .form-label 文案锚出**唯一**标签，再取其同排的兄弟 uni-input 里的原生
 * input（原生 input 在 uni-input 的 div.uni-input-wrapper **里面**，是孙节点，所以下面用
 * descendant 轴 —— 写成 ../uni-input/input 会因单步 child 轴而恒 0，第 3 跑就是这么红的）。
 * 不用 .form-row.filter({ has: sheet.locator(...) }) —— 那样 Playwright 会把内层
 * 相对外层重根，变成「在 .form-row 里再找一个 .bottom-sheet」，实测恒 0 个（首跑就是这么红的）。
 * 也不靠 nth 序号：弹层加一个字段就会串格。标签不唯一时 toHaveCount(1) 直接判红，
 * 而不是悄悄填进别的格子。
 */
async function fillUniInput(sheet: Locator, label: string, value: string): Promise<void> {
  const labelEl = sheet.locator('.form-label', { hasText: new RegExp(`^\\s*${label}`) })
  await expect(labelEl, `弹层内「${label}」标签应唯一`).toHaveCount(1)
  const input = labelEl.locator('xpath=../uni-input/descendant::input')
  await expect(input, `「${label}」应与输入框同排（标签的兄弟节点）`).toHaveCount(1)
  await input.fill(value, { force: true })
  await expect(input, `「${label}」应真的写进了控件`).toHaveValue(value)
}

/**
 * 11 位、1 开头的测试号。
 *
 * 不许写死：建档按 phone_hash 查重（repo/pg.go:1275-1284），而 13900000001 是 T041 播种技师
 * 账号的手机号（e2e/tech-helpers.ts:12）—— 复用会撞查重、或和技师链互踩。1e8 号段 + 本用例
 * 收尾删行，撞号可忽略；真撞上就是 code=10409，建档那步的 why 里已写明怎么辨。
 */
function uniqueTestPhone(): string {
  return `135${String(Math.floor(Math.random() * 1e8)).padStart(8, '0')}`
}

test.describe('21-链 C 患者自助资料编辑（T462 S4）', () => {
  /** 本用例自建、收尾必须删掉的患者号（afterAll 读它做还原） */
  let createdPatientId: string | null = null
  /** 建档前的患者总行数，删行后必须回到这个值 */
  let baselineTotal: number | null = null

  test('21.1 自建患者 → 口令登录 → 真 UI 改资料 → 值级回读：患者端页面只发出声明的那一条 PUT', async ({
    page,
    browser,
  }) => {
    // 浏览器（admin-web 那一腿真发的）请求采集。node 侧 callApi 不在这里，两腿分开记，
    // 「UI 写了什么」这个判据才不会被我自己的 API 调用污染。
    const adminNet: string[] = []
    page.on('request', (req) => {
      const u = new URL(req.url())
      if (u.pathname.startsWith('/api/v1/')) adminNet.push(`${req.method()} ${u.pathname}`)
    })

    /** 患者端页面自己发出的 /api/v1 请求（有序）与非 GET 明细 */
    const h5Net: string[] = []
    const h5Writes: Array<{ method: string; path: string; origin: string; body: unknown }> = []

    // ── 1) admin 登录（真实表单），拿 JWT ──────────────────────
    await realLogin(page)
    await expect(page).toHaveURL(/\/dashboard/, { timeout: 20_000 })
    const adminToken = await getAuthToken(page)
    expect(adminToken, '真实模式登录应把 admin JWT 写进 localStorage').toBeTruthy()
    expect(
      adminNet,
      'admin 腿应是浏览器真发出的表单登录（BASE_URL 打空也会让下面的断言全绿，这条先自证）',
    ).toContain('POST /api/v1/auth/login')

    // ── 2) 代次前置探针（只读，任何写之前）────────────────────
    await preflightGeneration(page, adminToken!)

    // ── 3) 快照患者总行数（行数守恒基准）──────────────────────
    const before = await callOk<{ total: number }>(
      page,
      'GET',
      '/api/v1/admin/patients?page=1&pageSize=10',
      { token: adminToken!, why: '读患者列表基线' },
    )
    expect(
      typeof before.total,
      `列表基线应带数值 total，实得 ${JSON.stringify(before.total)}`,
    ).toBe('number')
    baselineTotal = before.total

    // ── 4) 自建患者（A2 建档段：T467 之后建了能删）────────────
    const name = uniqueName(E2E_PATIENT_NAME_PREFIX)
    const phone = uniqueTestPhone()
    const created = await callOk<PatientProfile>(page, 'POST', '/api/v1/admin/patients', {
      token: adminToken!,
      body: { name, phone, gender: 'female', age: 14, diagnosis: '胸段侧弯', cobbAngle: 25 },
      why: '建档失败（message=patient already exists 即随机号撞了 phone_hash 查重，重跑一轮）',
    })
    createdPatientId = created.patientId
    expect(createdPatientId, '建档应回符合发号形状的患者号').toMatch(PATIENT_ID_RE)
    console.log(`[t462-s4][造数] patientId=${createdPatientId} name=${name}`)

    // ── 5) admin 给该患者设登录口令（T477：服务端发号，明文一次性返回）
    const pwd = await callOk<{ patientId: string; password: string }>(
      page,
      'POST',
      `${H5_ORIGIN}/api/v1/admin/patients/${createdPatientId}/password`,
      { token: adminToken!, why: '设口令失败 ⇒ 链 C 的患者会话建不起来' },
    )
    expect(pwd.patientId, '口令端点应回同一患者号').toBe(createdPatientId)
    // 口令值不落日志、不进卡面，只量长度（服务端 genDoctorPassword：Br + 12 随机位 + #7 = 16）
    expect(pwd.password.length, '一次性口令长度应为 16（口令值不打印）').toBe(16)

    // ── 6) 患者口令登录 → 患者 JWT ────────────────────────────
    const login = await callOk<{ token: string; patientId: string; role: string }>(
      page,
      'POST',
      `${H5_ORIGIN}/api/v1/patient/login`,
      { body: { phone, password: pwd.password }, why: '患者口令登录失败' },
    )
    expect(login.patientId, '登录回的患者号应等于自建患者').toBe(createdPatientId)
    expect(login.role, '登录态角色应为 patient').toBe('patient')
    const patientToken = login.token

    // ── 7) 会话注入患者端 H5，只让「资料编辑」走真 UI ──────────
    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 } })
    try {
      const h5 = await ctx.newPage()
      h5.on('request', (req) => {
        const u = new URL(req.url())
        if (!u.pathname.startsWith('/api/v1/')) return
        h5Net.push(`${req.method()} ${u.pathname}`)
        if (req.method() !== 'GET') {
          let body: unknown = null
          try {
            const raw = req.postData()
            body = raw === null ? null : JSON.parse(raw)
          } catch {
            body = null
          }
          h5Writes.push({ method: req.method(), path: u.pathname, origin: u.origin, body })
        }
      })

      await h5.addInitScript(
        ({ tokenKey, pidKey, token, pid }) => {
          // H5 端 uni.setStorageSync 底层就是 localStorage（同 e2e/helpers.ts setupPatientE2E 口径）
          window.localStorage.setItem(tokenKey, token)
          window.localStorage.setItem(pidKey, pid)
        },
        {
          tokenKey: LS_PATIENT_TOKEN_KEY,
          pidKey: LS_PATIENT_ID_KEY,
          token: patientToken,
          pid: createdPatientId,
        },
      )
      await h5.goto(`${H5_ORIGIN}${H5_PROFILE_PATH}`, { waitUntil: 'domcontentloaded' })

      await expect(
        h5.locator('.profile-card'),
        '注入患者会话后「我的」页应渲染档案卡（渲染不出＝登录态没被接住）',
      ).toBeVisible({ timeout: 25_000 })
      await expect(h5.locator('.profile-name')).toHaveText(name, { timeout: 15_000 })
      await expect(h5.locator('.profile-meta')).toContainText(createdPatientId)

      // ── 8) 编辑前基线：患者侧读端点与页面同值 ────────────────
      const pre = await callOk<PatientProfile>(h5, 'GET', `${H5_ORIGIN}/api/v1/patient/profile`, {
        token: patientToken,
        why: '患者侧档案读端点应放行并回本人',
      })
      expect(pre.patientId, 'profile 端点应回本人').toBe(createdPatientId)
      expect(pre.name, '编辑前姓名应等于建档值').toBe(name)
      expect(pre.age, '编辑前年龄应等于建档值').toBe(14)
      expect(pre.heightCm, '新建患者身高应为 null').toBeNull()
      expect(pre.cobbAngle, '建档 Cobb 角应读回').toBe(25)

      // ── 9) 真 UI：点编辑 → 弹层 → 改三格 → 保存 ──────────────
      await h5.locator('.profile-edit').click()
      const sheet = h5.locator('.bottom-sheet').filter({ hasText: '编辑个人信息' })
      await expect(sheet, '点「编辑」应展开个人信息弹层').toHaveClass(/bottom-sheet-show/, {
        timeout: 10_000,
      })
      // 前端代次锚：组标题文案是 T444 才有的口径。读到旧文案说明 staging 上的患者端产物
      // 早于我读源码的那一代，此后所有选择器判据都不再对应任何一份在库代码。
      await expect(
        sheet.locator('.form-section-label').first(),
        '弹层组标题应为「联系方式」，读到旧文案即 staging 患者端产物早于 T444',
      ).toHaveText('联系方式', { timeout: 5_000 })

      const newName = `${name}-改`
      await fillUniInput(sheet, '昵称', newName)
      await fillUniInput(sheet, '年龄', '15')
      await fillUniInput(sheet, '身高', '158')
      await sheet.locator('.sheet-confirm-text').click()

      await expect(sheet, '保存后弹层应收起').not.toHaveClass(/bottom-sheet-show/, { timeout: 15_000 })

      // ── 10) 值级回读：患者侧端点 + 页面 DOM + admin 侧端点三点对平
      const post = await callOk<PatientProfile>(h5, 'GET', `${H5_ORIGIN}/api/v1/patient/profile`, {
        token: patientToken,
        why: '保存后回读本人档案',
      })
      expect(post.name, 'PUT 后姓名应落库').toBe(newName)
      expect(post.age, 'PUT 后年龄应落库').toBe(15)
      expect(post.heightCm, 'PUT 后身高应落库').toBe(158)
      expect(post.gender, '性别未被弹层改动，应保持建档值').toBe('female')
      expect(post.diagnosis, '诊断不在白名单内，不该被前端空值清掉').toBe('胸段侧弯')
      expect(post.cobbAngle, 'Cobb 角由临床端写，患者保存不得清掉（T230 裁定 B）').toBe(25)

      await expect(h5.locator('.profile-name'), '页面应回显新昵称').toHaveText(newName, {
        timeout: 15_000,
      })
      const meta = await h5.locator('.profile-meta').innerText()
      expect(meta, `档案副行应含新年龄与患者号，实得 ${meta}`).toContain('15岁')
      expect(meta).toContain(createdPatientId)

      const adminView = await callOk<PatientProfile>(
        page,
        'GET',
        `/api/v1/admin/patients/${createdPatientId}`,
        { token: adminToken!, why: 'admin 侧读回同一患者' },
      )
      expect(adminView.name, 'admin 侧与患者侧读到的姓名应同值').toBe(post.name)
      expect(adminView.age, 'admin 侧与患者侧读到的年龄应同值').toBe(post.age)
      expect(adminView.heightCm, 'admin 侧与患者侧读到的身高应同值').toBe(post.heightCm)

      // ── 11) 反证一：患者端页面发出的写只有声明的那一条 PUT ───
      const wrote = h5Writes.map((w) => `${w.method} ${w.path}`)
      expect(
        wrote,
        `患者端页面只应发出 1 条写请求（PUT 本人档案），实得 ${JSON.stringify(wrote)}`,
      ).toEqual([`PUT /api/v1/patients/${createdPatientId}`])
      // 同源自证：全仓没有 CORS 放行，产物里的 API 基址又是构建期写死的 ⇒
      // 「请求源 == 页面源」不是当然成立，而是这条链跑得通的前提（跑红＝源改配没同步本用例）。
      expect(h5Writes[0].origin, '患者端应用发出的请求应与页面同源').toBe(H5_ORIGIN)

      const putBody = h5Writes[0].body as Record<string, unknown> | null
      expect(putBody, 'PUT 应带 JSON 体').not.toBeNull()
      const keys = Object.keys(putBody!).sort()
      expect(
        keys,
        `弹层只应提交白名单内的非空字段，实得 ${keys.join('|')}`,
      ).toEqual(['age', 'gender', 'heightCm', 'name'])
      expect(putBody!.name, 'PUT 体 name 应等于新昵称').toBe(newName)
      expect(putBody!.age, 'PUT 体 age 应为数值 15（不是字符串）').toBe(15)
      expect(putBody!.heightCm, 'PUT 体 heightCm 应为数值 158').toBe(158)
      // 白名单外字段一个都不许出现在请求里。服务端 DisallowUnknownFields 会 400，
      // 但「前端根本不发」才是这条链要守的口径，不能靠服务端兜。
      for (const forbidden of ['phone', 'cobbAngle', 'patientId', 'id', 'status', 'teamId']) {
        expect(putBody!, `PUT 体不得含白名单外字段 ${forbidden}`).not.toHaveProperty(forbidden)
      }

      // ── 12) 反证二：串联顺序（先读 → 再写 → 再读回显）────────
      const firstRead = h5Net.indexOf('GET /api/v1/patient/profile')
      const putIdx = h5Net.indexOf(`PUT /api/v1/patients/${createdPatientId}`)
      const lastRead = h5Net.lastIndexOf('GET /api/v1/patient/profile')
      expect(
        firstRead,
        `未捕获到患者页的档案读请求（说明没真落到「我的」页），页面请求序列=${h5Net.join(' → ')}`,
      ).toBeGreaterThanOrEqual(0)
      expect(putIdx, `未捕获到 PUT 写请求（保存那一步没真发出去），序列=${h5Net.join(' → ')}`).toBeGreaterThan(
        firstRead,
      )
      expect(
        lastRead,
        `保存后页面应再读一次档案回显，顺序应为 读=${firstRead} 写=${putIdx} 回读=${lastRead}`,
      ).toBeGreaterThan(putIdx)
    } finally {
      await ctx.close()
    }
  })

  /**
   * 还原 + 报备。
   *
   * 用例主体半路红也要跑（afterAll 不看用例状态）：删掉自建患者，并核对总行数回到建档前
   * 的快照值。删不动就把患者号打进日志、冠以「残留报备」标记，然后把失败抛出去 ——
   * staging 上任何一行 T053 数据都得让 PM 看得见。
   */
  test.afterAll(async ({ browser }) => {
    if (!createdPatientId) return
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const token = await getAuthToken(page)
      if (!token) throw new Error('afterAll 取不到 admin JWT，无法还原')

      const del = await callApi(page, 'DELETE', `/api/v1/admin/patients/${createdPatientId}`, {
        token,
      })
      console.log(
        `[t462-s4][还原] DELETE /api/v1/admin/patients/${createdPatientId} → status=${del.status} code=${del.code}`,
      )
      expect(del.code, `还原删除应回 code=0，实得 status=${del.status} code=${del.code} message=${del.message}`).toBe(
        0,
      )

      // 删干净的反证：详情端点必须转为「不存在」，而不是「行还在、只是状态变了」
      const gone = await callApi(page, 'GET', `/api/v1/admin/patients/${createdPatientId}`, { token })
      expect(gone.code, `删后详情应回 code=10404（硬删），实得 ${gone.code}`).toBe(10404)

      const after = await callOk<{ total: number }>(
        page,
        'GET',
        '/api/v1/admin/patients?page=1&pageSize=10',
        { token, why: '删行后读患者总数' },
      )
      if (baselineTotal !== null) {
        expect(
          after.total,
          `行数守恒：删后 total=${after.total} 应回到建档前快照 total=${baselineTotal}`,
        ).toBe(baselineTotal)
      } else {
        // 建档前就红了 ⇒ 没有快照可对。仍要删行（上面已删），这里只把读数留档。
        console.log(`[t462-s4][还原] 无建档前快照可比对，删后 total=${after.total}`)
      }
    } catch (err) {
      console.log(
        `[t462-s4][残留报备] 自建患者未确认删除成功，patientId=${createdPatientId}，原因=${
          (err as Error).message
        }`,
      )
      throw err
    } finally {
      await ctx.close()
    }
  })
})
