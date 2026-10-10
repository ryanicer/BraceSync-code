import { test, expect, type Page, type Locator } from '@playwright/test'
import { realLogin, getAuthToken, uniqueName, E2E_PATIENT_NAME_PREFIX } from '../real-helpers'
import { resolveH5Origin } from '../h5-origin'

/**
 * T617 丁类 · 链 C 患者自助资料的其余白名单字段（真实模式，打 TST 与 staging 两源）
 *
 * 与 21-chain-c 的分工（不重复断言）：21.1 走的是 昵称 / 年龄 / 身高 三格，PUT 体键集钉成四颗；
 * 本用例只走 21.1 从未触及的五格 —— 性别单选、体重、紧急联系人姓名 / 电话 / 与本人关系。
 * 两枚用例的键集合起来才是后端白名单全集（services/user-service/internal/handler/patient_profile_write.go:32-41 的八颗）。
 *
 * 为什么另起一颗文件而不并进 21 号：21 号那颗 describe 的收尾只删「一枚」自建患者
 * （createdPatientId 是标量，afterAll 只看最后一次赋值）。把第二枚患者塞进同一 describe，
 * 第一枚就留在池里删不掉，正是要防的池污染形。本用例自带患者号与自删收尾。
 *
 * 可逆性：写段只产生本用例自建的患者，收尾用 T467 的 DELETE 整行删除并核对行数守恒；
 * 删除能力先由 preflightGeneration() 在任何写之前实测确认，端点不在架就抛红，绝不 skip。
 *
 * 同源与患者会话口径与 21 号一致：患者端产物把 API 基址写死在构建期，全仓没有 CORS 放行；
 * 登录页只有 uni.login，CI 拿不到 wx code，所以会话由口令登录 + 存储注入建立，
 * 本用例不声称覆盖患者登录页渲染。
 */

const H5_ORIGIN = resolveH5Origin('E2E_PATIENT_H5_URL', 'T617 链 C 丁类补齐')
const H5_PROFILE_PATH = '/patient-h5/#/pages/profile/index'

/** 患者端存储键（apps/patient-miniapp/src/utils/token.ts 的 TOKEN_KEY / PATIENT_ID_KEY） */
const LS_PATIENT_TOKEN_KEY = 'bracesync_token'
const LS_PATIENT_ID_KEY = 'bracesync_patient_id'

/** 发号形状同 21 号（repo/pg.go 的 newPatientID：P + 四位年 + 12 位 hex） */
const PATIENT_ID_RE = /^P\d{4}[0-9a-f]{12}$/

/** 只用来探「路由在架」的假患者号，按真实形状拼出并在下面自证一次 */
const GHOST_PATIENT_ID = `P${'0'.repeat(16)}`
const NOT_REGISTERED_PATH = '/api/v1/zzz-t617-chain-c-remaining-not-registered-7b21'
if (!PATIENT_ID_RE.test(GHOST_PATIENT_ID)) {
  throw new Error(`T617 丁类探针患者号形状不符发号规则，代次探针打的不是真实形状：${GHOST_PATIENT_ID}`)
}

/** 本用例填进去的五格取值（体重走 type=digit，电话走 type=number，故两格的类型各证一次） */
const WEIGHT_TEXT = '46.5'
const WEIGHT_VALUE = 46.5
const EMC_NAME_TEXT = '测试联系人甲'
const EMC_PHONE_TEXT = '13012340001'
const EMC_RELATION_TEXT = '母亲'

/** GET /api/v1/patient/profile 与 GET /api/v1/admin/patients/:id 的出参（model.AdminPatientDTO，无 omitempty ⇒ 空值出 null） */
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
  emergencyContactPhone: string | null
  emergencyContactRelation: string | null
  phone: string
  status: string
  teamId: string | null
}

interface Envelope {
  status: number
  contentType: string
  /** null = 响应体不是 JSON（gin 自己的 404 正是这一形） */
  code: number | null
  message: string
  data: unknown
}

/** node 侧接口调用（不经浏览器，所以不会污染下面的页面网络采集） */
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
    /* 非 JSON：code 留 null，正是负对照要的形 */
  }
  return { status: res.status(), contentType: (res.headers()['content-type'] ?? '').split(';')[0], code, message, data }
}

/** 成功信封断言：只看 HTTP 200 就算过是这条链的红线 */
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
 * 代次前置探针（只读，零写）：① 未注册路径必须回非 JSON 的 gin 404，否则 ②③ 的
 * 「JSON code=10404」分不清「路由在架且资源不存在」与「路由没在架」；② T477 设口令在架；
 * ③ T467 删除在架。任一不成立即抛红，不 skip。
 */
async function preflightGeneration(page: Page, token: string): Promise<void> {
  for (const method of ['POST', 'PUT', 'DELETE'] as const) {
    const r = await callApi(page, method, NOT_REGISTERED_PATH, { token })
    if (r.code !== null) {
      throw new Error(
        `T617 丁类前置探针失效：未注册路径 ${method} ${NOT_REGISTERED_PATH} 应回非 JSON 的 gin 404，实得 status=${r.status} code=${r.code} ct=${r.contentType} —— 此时「JSON code=10404 ⇒ 路由在架」不成立，不能判 T477/T467`,
      )
    }
  }
  const t477 = await callApi(page, 'POST', `/api/v1/admin/patients/${GHOST_PATIENT_ID}/password`, { token })
  const t467 = await callApi(page, 'DELETE', `/api/v1/admin/patients/${GHOST_PATIENT_ID}`, { token })
  console.log(
    `[t617-ding][代次锚] 未注册路径=非JSON404(3 法) | T477 status=${t477.status} code=${t477.code} ct=${t477.contentType} | T467 status=${t467.status} code=${t467.code} ct=${t467.contentType}`,
  )
  if (t477.code !== 10404) {
    throw new Error(
      `T477 设口令端点不可用（期望 JSON code=10404「患者不存在」，实得 status=${t477.status} code=${t477.code} message=${t477.message}）。患者会话建不起来就别往下写`,
    )
  }
  if (t467.code !== 10404) {
    throw new Error(
      `T467 患者删除端点不可用（期望 JSON code=10404，实得 status=${t467.status} code=${t467.code} message=${t467.message}）。没有还原腿，本用例不许在环境里留行`,
    )
  }
}

/**
 * 按标签锚出唯一一格再填值。定位口径同 21 号（uni-app 把 input 渲染成 uni-input 里的孙节点，
 * 故走 descendant 轴；内层 input 高度近 0，故 force）。
 *
 * 标签正则本用例全部收口：`紧急联系人` 与 `紧急联系人电话` 共用前缀，用不锚尾的正则会让
 * toHaveCount(1) 命中两枚 —— 那正是「填进别的格子」的形状，锚尾之后不唯一就直接判红。
 */
async function fieldInput(sheet: Locator, labelRe: RegExp, tag: string): Promise<Locator> {
  const labelEl = sheet.locator('.form-label', { hasText: labelRe })
  await expect(labelEl, `弹层内「${tag}」标签应唯一`).toHaveCount(1)
  const input = labelEl.locator('xpath=../uni-input/descendant::input')
  await expect(input, `「${tag}」应与输入框同排（标签的兄弟节点里的原生控件）`).toHaveCount(1)
  return input
}

async function fillField(sheet: Locator, labelRe: RegExp, tag: string, value: string): Promise<Locator> {
  const input = await fieldInput(sheet, labelRe, tag)
  await input.fill(value, { force: true })
  await expect(input, `「${tag}」应真的写进了控件`).toHaveValue(value)
  return input
}

/**
 * 性别单选的一格：返回「可点的那整格」与「选中态的判据那颗」。
 *
 * 首跑红在 descendant::input 命中 0 —— uni-h5 把 <radio> 渲染成
 * uni-radio > div.uni-radio-wrapper > div.uni-radio-input，格内**没有**原生 input
 * （本机 node_modules/@dcloudio/uni-h5/dist/uni-h5.cjs.js:6042-6060 的渲染函数逐字可读），
 * 所以 toBeChecked() 在这颗组件上根本无处可判，硬转成点圈也判不出选中。
 * 选中态在 DOM 上只有一处落法：选中时 .uni-radio-input 内多一枚 svg 勾（同文件 :6060 那个
 * realCheckValue 三元分支的另一支是空串），所以判选中只押「svg 那颗的有无」。
 * 点击口径走 .form-radio 那颗 label：uni-radio 的 setup 把 _onClick 注册进了 uni-label
 * （同文件 :6039-6041），点整格（圈或文案）都算真点中；且 _onClick 在已选中时自己早退，
 * 那正是单选的语义，不需要用例这边兜。
 */
function genderCell(sheet: Locator, cn: string): { option: Locator; checkedDot: Locator } {
  const option = sheet.locator('.form-radio', { hasText: cn })
  return { option, checkedDot: option.locator('.uni-radio-input svg') }
}

/** 弹层里那一格的选中态（只押 svg 那颗：1＝选中，0＝未选中） */
async function expectGender(sheet: Locator, cn: string, checked: boolean): Promise<void> {
  const { option, checkedDot } = genderCell(sheet, cn)
  await expect(option, `性别单选应有唯一一格「${cn}」`).toHaveCount(1)
  await expect(
    option.locator('.uni-radio-input'),
    `「${cn}」那一格应渲染出 uni-radio 的圆点容器（取不到＝这一族控件不是本用例读到的那一形）`,
  ).toHaveCount(1)
  await expect(
    checkedDot,
    `「${cn}」应${checked ? '处于选中态（圆点内应有勾）' : '未选中（圆点内不该有勾）'}`,
  ).toHaveCount(checked ? 1 : 0)
}

/** 11 位、1 开头的测试号；建档按 phone_hash 查重，不许写死（同 21 号口径） */
function uniqueTestPhone(): string {
  return `135${String(Math.floor(Math.random() * 1e8)).padStart(8, '0')}`
}

test.describe('27-链 C 患者自助资料的其余白名单字段（T617 丁类）', () => {
  let createdPatientId: string | null = null
  let baselineTotal: number | null = null

  test('27.1 自建患者 → 真 UI 只走 21.1 未触及的五格（性别单选 + 体重 + 紧急联系人三格）→ PUT 体键集恰为七颗（空着的身高不得提交）→ 患者端点 / 重开的弹层 / 管理端三点回读', async ({
    page,
    browser,
  }) => {
    const adminNet: string[] = []
    page.on('request', (req) => {
      const u = new URL(req.url())
      if (u.pathname.startsWith('/api/v1/')) adminNet.push(`${req.method()} ${u.pathname}`)
    })

    const h5Net: string[] = []
    const h5Writes: Array<{ method: string; path: string; origin: string; body: unknown }> = []

    // ── 1) admin 真实表单登录，拿 JWT ─────────────────────────
    await realLogin(page)
    await expect(page).toHaveURL(/\/dashboard/, { timeout: 20_000 })
    const adminToken = await getAuthToken(page)
    expect(adminToken, '真实模式登录应把 admin JWT 写进 localStorage').toBeTruthy()
    expect(
      adminNet,
      'admin 腿应是浏览器真发出的表单登录（打空地址也会让下面的断言全绿，这条先自证）',
    ).toContain('POST /api/v1/auth/login')

    // ── 2) 代次前置探针（任何写之前）─────────────────────────
    await preflightGeneration(page, adminToken!)

    // ── 3) 患者总行数基线（收尾守恒基准）─────────────────────
    const before = await callOk<{ total: number }>(page, 'GET', '/api/v1/admin/patients?page=1&pageSize=10', {
      token: adminToken!,
      why: '读患者列表基线',
    })
    expect(typeof before.total, `列表基线应带数值 total，实得 ${JSON.stringify(before.total)}`).toBe('number')
    baselineTotal = before.total

    // ── 4) 自建患者：建档只给五格，体重与紧急联系人三格一律留空 ─
    const name = uniqueName(E2E_PATIENT_NAME_PREFIX)
    const phone = uniqueTestPhone()
    const created = await callOk<PatientProfile>(page, 'POST', '/api/v1/admin/patients', {
      token: adminToken!,
      body: { name, phone, gender: 'female', age: 14, diagnosis: '胸段侧弯', cobbAngle: 25 },
      why: '建档失败（message=patient already exists 即随机号撞了 phone_hash 查重，重跑一轮）',
    })
    createdPatientId = created.patientId
    expect(createdPatientId, '建档应回符合发号形状的患者号').toMatch(PATIENT_ID_RE)
    console.log(`[t617-ding][造数] patientId=${createdPatientId} name=${name}`)

    // ── 5) admin 设口令 → 患者口令登录（口令值不落日志）──────
    const pwd = await callOk<{ patientId: string; password: string }>(
      page,
      'POST',
      `${H5_ORIGIN}/api/v1/admin/patients/${createdPatientId}/password`,
      { token: adminToken!, why: '设口令失败 ⇒ 患者会话建不起来' },
    )
    expect(pwd.patientId, '口令端点应回同一患者号').toBe(createdPatientId)
    expect(pwd.password.length, '一次性口令长度应为 16（口令值不打印）').toBe(16)

    const login = await callOk<{ token: string; patientId: string; role: string }>(
      page,
      'POST',
      `${H5_ORIGIN}/api/v1/patient/login`,
      { body: { phone, password: pwd.password }, why: '患者口令登录失败' },
    )
    expect(login.patientId, '登录回的患者号应等于自建患者').toBe(createdPatientId)
    expect(login.role, '登录态角色应为 patient').toBe('patient')
    const patientToken = login.token

    // ── 6) 会话注入患者端 H5，只让资料编辑那一腿走真 UI ──────
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
          window.localStorage.setItem(tokenKey, token)
          window.localStorage.setItem(pidKey, pid)
        },
        { tokenKey: LS_PATIENT_TOKEN_KEY, pidKey: LS_PATIENT_ID_KEY, token: patientToken, pid: createdPatientId },
      )
      await h5.goto(`${H5_ORIGIN}${H5_PROFILE_PATH}`, { waitUntil: 'domcontentloaded' })

      await expect(
        h5.locator('.profile-card'),
        '注入患者会话后「我的」页应渲染档案卡（渲染不出＝登录态没被接住）',
      ).toBeVisible({ timeout: 25_000 })
      await expect(h5.locator('.profile-name')).toHaveText(name, { timeout: 15_000 })

      // ── 7) 编辑前两点对平：五格在患者端与管理端都是「真没有值」─
      const prePatient = await callOk<PatientProfile>(h5, 'GET', `${H5_ORIGIN}/api/v1/patient/profile`, {
        token: patientToken,
        why: '患者侧档案读端点应放行并回本人',
      })
      const preAdmin = await callOk<PatientProfile>(page, 'GET', `/api/v1/admin/patients/${createdPatientId}`, {
        token: adminToken!,
        why: 'admin 侧读同一患者（编辑前基线）',
      })
      for (const [side, prof] of [
        ['患者端', prePatient],
        ['管理端', preAdmin],
      ] as Array<[string, PatientProfile]>) {
        expect(prof.weightKg, `${side}建档未给体重，应读回 null`).toBeNull()
        expect(prof.heightCm, `${side}建档未给身高，应读回 null`).toBeNull()
        for (const key of ['emergencyContactName', 'emergencyContactPhone', 'emergencyContactRelation'] as const) {
          expect(prof[key], `${side}建档未给 ${key}，应读回 null`).toBeNull()
        }
      }
      expect(prePatient.gender, '建档性别应为 female（本用例随后走单选改 male）').toBe('female')

      // ── 8) 真 UI：展开弹层，先证单选的回填方向 ─────────────
      await h5.locator('.profile-edit').click()
      const sheet = h5.locator('.bottom-sheet').filter({ hasText: '编辑个人信息' })
      await expect(sheet, '点「编辑」应展开个人信息弹层').toHaveClass(/bottom-sheet-show/, { timeout: 10_000 })
      await expect(
        sheet.locator('.form-section-label').first(),
        '弹层组标题应为「联系方式」，读到旧文案即该源的患者端产物早于 T444',
      ).toHaveText('联系方式', { timeout: 5_000 })

      await expectGender(sheet, '女', true)
      await expectGender(sheet, '男', false)

      // ── 9) 只填 21.1 未触及的四格文本 + 点一次性别单选 ──────
      const weightInput = await fillField(sheet, /^\s*体重/, '体重', WEIGHT_TEXT)
      const emcNameInput = await fillField(sheet, /^\s*紧急联系人\s*$/, '紧急联系人', EMC_NAME_TEXT)
      const emcPhoneInput = await fillField(sheet, /^\s*紧急联系人电话\s*$/, '紧急联系人电话', EMC_PHONE_TEXT)
      const emcRelationInput = await fillField(sheet, /^\s*与本人关系\s*$/, '与本人关系', EMC_RELATION_TEXT)
      await genderCell(sheet, '男').option.click()
      await expectGender(sheet, '男', true)
      await expectGender(sheet, '女', false)

      await sheet.locator('.sheet-confirm-text').click()
      await expect(sheet, '保存后弹层应收起').not.toHaveClass(/bottom-sheet-show/, { timeout: 15_000 })

      // ── 10) 回读一：患者端点值级 ───────────────────────────
      const post = await callOk<PatientProfile>(h5, 'GET', `${H5_ORIGIN}/api/v1/patient/profile`, {
        token: patientToken,
        why: '保存后回读本人档案',
      })
      expect(post.weightKg, 'PUT 后体重应落库').toBe(WEIGHT_VALUE)
      expect(post.emergencyContactName, 'PUT 后紧急联系人姓名应落库').toBe(EMC_NAME_TEXT)
      expect(post.emergencyContactPhone, 'PUT 后紧急联系人电话应逐字符落库').toBe(EMC_PHONE_TEXT)
      expect(post.emergencyContactRelation, 'PUT 后与本人关系应落库').toBe(EMC_RELATION_TEXT)
      expect(post.gender, '单选点过「男」后应落库为 male').toBe('male')
      // 空着不填的格不许被清成别的形状（服务端指针语义 nil=不改）
      expect(post.heightCm, '身高本用例没填，应仍为 null（不得被空串清出 0）').toBeNull()
      expect(post.name, '昵称未改动，应保持建档值').toBe(name)
      expect(post.age, '年龄未改动，应保持建档值').toBe(14)
      expect(post.diagnosis, '诊断不在白名单内，不该被前端空值清掉').toBe('胸段侧弯')
      expect(post.cobbAngle, 'Cobb 角由临床端写，患者保存不得清掉（T230 裁定 B）').toBe(25)

      // ── 11) 回读二：重开弹层，控件值等于下发值（页面面 == 下发面）
      await h5.locator('.profile-edit').click()
      await expect(sheet, '第二次点「编辑」应再次展开弹层').toHaveClass(/bottom-sheet-show/, { timeout: 10_000 })
      await expect(weightInput, '重开弹层后体重控件应回显落库值').toHaveValue(WEIGHT_TEXT)
      await expect(emcNameInput, '重开弹层后紧急联系人控件应回显落库值').toHaveValue(EMC_NAME_TEXT)
      await expect(emcPhoneInput, '重开弹层后紧急联系人电话控件应回显落库值').toHaveValue(EMC_PHONE_TEXT)
      await expect(emcRelationInput, '重开弹层后与本人关系控件应回显落库值').toHaveValue(EMC_RELATION_TEXT)
      await expectGender(sheet, '男', true)
      const meta = await h5.locator('.profile-meta').innerText()
      expect(meta, `档案副行应仍含未改动的年龄与患者号，实得 ${meta}`).toContain('14岁')
      expect(meta).toContain(createdPatientId)

      // 取消那一腿不发写：关弹层后写请求仍只有一枚
      await sheet.locator('.sheet-cancel-text').click()
      await expect(sheet, '点「取消」应收起弹层').not.toHaveClass(/bottom-sheet-show/, { timeout: 10_000 })

      // ── 12) 回读三：管理端逐格同值（patient 写 × admin 读这一族）
      const postAdmin = await callOk<PatientProfile>(page, 'GET', `/api/v1/admin/patients/${createdPatientId}`, {
        token: adminToken!,
        why: 'admin 侧读回同一患者（保存后）',
      })
      expect(postAdmin.weightKg, 'admin 侧与患者侧读到的体重应同值').toBe(post.weightKg)
      expect(postAdmin.emergencyContactName, 'admin 侧与患者侧读到的紧急联系人姓名应同值').toBe(
        post.emergencyContactName,
      )
      expect(postAdmin.emergencyContactPhone, 'admin 侧与患者侧读到的紧急联系人电话应同值').toBe(
        post.emergencyContactPhone,
      )
      expect(postAdmin.emergencyContactRelation, 'admin 侧与患者侧读到的与本人关系应同值').toBe(
        post.emergencyContactRelation,
      )
      expect(postAdmin.gender, 'admin 侧与患者侧读到的性别应同值').toBe(post.gender)
      expect(postAdmin.heightCm, 'admin 侧与患者侧读到的身高应同为 null').toBeNull()

      // ── 13) 反证一：患者端页面只发出那一条 PUT，且同源 ──────
      const wrote = h5Writes.map((w) => `${w.method} ${w.path}`)
      expect(
        wrote,
        `患者端页面只应发出 1 条写请求（PUT 本人档案），取消那一腿不发写；实得 ${JSON.stringify(wrote)}`,
      ).toEqual([`PUT /api/v1/patients/${createdPatientId}`])
      expect(h5Writes[0].origin, '患者端应用发出的请求应与页面同源').toBe(H5_ORIGIN)

      const putBody = h5Writes[0].body as Record<string, unknown> | null
      expect(putBody, 'PUT 应带 JSON 体').not.toBeNull()
      const keys = Object.keys(putBody!).sort()
      expect(
        keys,
        `弹层只应提交「有值的白名单字段」：本用例填了体重与紧急联系人三格并点过性别，身高留空 ⇒ 七颗；实得 ${keys.join('|')}`,
      ).toEqual([
        'age',
        'emergencyContactName',
        'emergencyContactPhone',
        'emergencyContactRelation',
        'gender',
        'name',
        'weightKg',
      ])
      expect(putBody!.weightKg, 'PUT 体 weightKg 应为数值（type=digit 的取值那一腿不许发字符串）').toBe(WEIGHT_VALUE)
      expect(typeof putBody!.weightKg, 'PUT 体 weightKg 的类型').toBe('number')
      expect(putBody!.emergencyContactPhone, 'PUT 体紧急联系人电话应逐字符等于填入值').toBe(EMC_PHONE_TEXT)
      expect(
        typeof putBody!.emergencyContactPhone,
        '紧急联系人电话落在 VARCHAR(32)，type=number 的输入框不许把它折成数值（会丢前导零）',
      ).toBe('string')
      expect(putBody!.gender, 'PUT 体 gender 应为 male').toBe('male')
      expect(putBody!.name, 'PUT 体 name 应等于未改动的建档昵称').toBe(name)
      for (const forbidden of [
        'phone',
        'cobbAngle',
        'diagnosis',
        'patientId',
        'id',
        'status',
        'teamId',
        'heightCm',
      ]) {
        expect(putBody!, `PUT 体不得含 ${forbidden}（白名单外或本用例留空的格）`).not.toHaveProperty(forbidden)
      }

      // ── 14) 反证二：串联顺序 读 → 写 → 保存后回读 ──────────
      const firstRead = h5Net.indexOf('GET /api/v1/patient/profile')
      const putIdx = h5Net.indexOf(`PUT /api/v1/patients/${createdPatientId}`)
      const lastRead = h5Net.lastIndexOf('GET /api/v1/patient/profile')
      expect(
        firstRead,
        `未捕获到患者页的档案读请求（说明没真落到「我的」页），页面请求序列=${h5Net.join(' / ')}`,
      ).toBeGreaterThanOrEqual(0)
      expect(putIdx, `未捕获到 PUT 写请求（保存那一步没真发出去），序列=${h5Net.join(' / ')}`).toBeGreaterThan(firstRead)
      expect(
        lastRead,
        `保存后页面应再读一次档案回显，顺序应为 读=${firstRead} 写=${putIdx} 回读=${lastRead}`,
      ).toBeGreaterThan(putIdx)
    } finally {
      await ctx.close()
    }
  })

  /**
   * 还原 + 报备：删掉本用例自建的患者，核对总行数回到建档前快照。
   * 删不动就把患者号打进日志并冠以「残留报备」，再把失败抛出去 —— 环境里任何一行 T053 数据都得让复核席看得见。
   */
  test.afterAll(async ({ browser }) => {
    if (!createdPatientId) return
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const token = await getAuthToken(page)
      if (!token) throw new Error('afterAll 取不到 admin JWT，无法还原')

      const del = await callApi(page, 'DELETE', `/api/v1/admin/patients/${createdPatientId}`, { token })
      console.log(
        `[t617-ding][还原] DELETE /api/v1/admin/patients/${createdPatientId} → status=${del.status} code=${del.code}`,
      )
      expect(del.code, `还原删除应回 code=0，实得 status=${del.status} code=${del.code} message=${del.message}`).toBe(0)

      const gone = await callApi(page, 'GET', `/api/v1/admin/patients/${createdPatientId}`, { token })
      expect(gone.code, `删后详情应回 code=10404（硬删），实得 ${gone.code}`).toBe(10404)

      const after = await callOk<{ total: number }>(page, 'GET', '/api/v1/admin/patients?page=1&pageSize=10', {
        token,
        why: '删行后读患者总数',
      })
      if (baselineTotal !== null) {
        expect(
          after.total,
          `行数守恒：删后 total=${after.total} 应回到建档前快照 total=${baselineTotal}`,
        ).toBe(baselineTotal)
      } else {
        console.log(`[t617-ding][还原] 无建档前快照可比对，删后 total=${after.total}`)
      }
    } catch (err) {
      console.log(
        `[t617-ding][残留报备] 自建患者未确认删除成功，patientId=${createdPatientId}，原因=${(err as Error).message}`,
      )
      throw err
    } finally {
      await ctx.close()
    }
  })
})
