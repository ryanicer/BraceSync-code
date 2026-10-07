import { test, expect, type Page } from '@playwright/test'
import { realLogin, getAuthToken, uniqueName, E2E_PATIENT_NAME_PREFIX } from '../real-helpers'
import { resolveH5Origin } from '../h5-origin'
import { requireDeployedBuild } from '../deploy-guard'

/**
 * T614 第三项（PM 2026-10-07 22:52 并入本卡，源自 T563 那条悬空的 e2e 断言派单）
 * —— 佩戴期望时长（sys_configs.wear_target_hours）的下发链在真实环境里要有断言，
 *    而且断言里一个数字都不许是写死的期望值。
 *
 * 为什么补这一条（Peter 2026-10-07 收口轮现读，登记为「已实现、现网无断言」）：
 *   `dailyWearTargetHours` 在整个 e2e-real 面此前只命中 08-settings.spec.ts（那是配置页自己的
 *   回显，证的是「改得动」）；患者侧 21-chain-c 断言了四个档案字段却不含该键；e2e-miniapp 面
 *   没有 wearing / anomaly 两页。⇒ 「配置值 → profile 下发 → 页面渲染成目标线」这条链
 *   在任何一套真实环境里从没被验过，本用例把它钉成三面对平。
 *
 * 为什么写死数字不行（PM 22:52 三条现读）：seed 基准 22、TST 现读 22、staging 现读 9。
 *   配置本就该可调（§7D.12 系统配置页就是它的维护入口），所以「值是多少」不是缺陷；
 *   缺陷是断言把值钉住 —— 同一份代码、同一个用例，在 staging 读 9、在 TST 读 22，
 *   一边绿一边红，而两边都没坏，红绿就此失去可读性。
 *   ⇒ 本用例的期望值全部来自当轮接口现读：配置面（admin settings）作基准，
 *     下发面（patient profile）与页面面（DOM 文本解析）与它逐格对平。
 *     全文没有一处 `toBe(<数字>)` 形态的期望值。
 *
 * 三面对平的每一格各证什么（不可互相替代）：
 *   甲 配置面 == 下发面 —— 后端把 sys_configs 那一枚键读出来并塞进患者档案出参（T576 甲案）。
 *   乙 下发面的响应体 == 页面渲染值 —— 前端在消费下发字段（T579 唯一入口 utils/wear-target），
 *      而不是拿本地兜底常量渲染。这一格必须靠「页面自己发出的那一发 GET 的响应体」对，
 *      不能靠常量对：环境真值恰与兜底同值时（TST=22 就是这形），DOM 等常量是假绿。
 *   丙 注牙（26.3）—— 把页面那一发响应改写成一枚不等于配置面的值，渲染必须跟着走。
 *      这一格证的正是「乙的等值不是常量蒙对」，牙打在页面真正读的那个面上。
 *      甲格附带的契约域 [1,24]（handler.go:2158 validateSettings）是边界检查，不是期望值。
 *
 * 环境错位探针（顺带）：配置面走 baseURL（跟着 E2E_STAGING_URL），患者两条腿走 H5_ORIGIN
 *   （跟着 E2E_PATIENT_H5_URL，理由见 ../h5-origin 顶部）。换环境要三颗一起换；只换第一颗时
 *   h5-origin 会点名判红，而本用例甲格还会再兜一层 —— 两腿分属两套环境时令牌不通用，
 *   等值那一发会红在明确的位子上，不会静默比出个「看起来通过」的结果。
 *
 * 可逆性：与 21-chain-c / 25-chain-b 同一套授权写法 —— 唯一名 + 唯一号自建患者，
 *   T477 设一次性口令拿会话，afterAll 用 T467 删行并核对患者总数回到建档前快照。
 *   口令明文只活在 session 变量里，不进日志、不进卡面、不进板。
 *
 * 部署边界：乙/丙两格（页面面）要该环境投放了 patient-h5 产物才成立。TST 当前未投放
 *   （2026-10-07 真读 /patient-h5/ 403、index.html 500，证据见 docs 仓 T614-evidence/05），
 *   所以乙格走 e2e-real/deploy-guard.ts：缺产物 ⇒ 显式 post-deploy 跳过并把原因挂在报告上，
 *   定时/手动阶段（E2E_POST_DEPLOY_STRICT=1）仍缺则判红。甲格不受此影响，两套环境都真跑。
 */

/** 患者端 H5 源；T614 起由 h5-origin 统一解析并点名缺失变量 */
const H5_ORIGIN = resolveH5Origin('E2E_PATIENT_H5_URL', 'T614 佩戴期望时长下发链')
const H5_WEARING_PATH = '/patient-h5/#/pages/wearing/index'
/** 产物存在性探测位（只问在不在，不做业务断言 —— deploy-guard 对 probe 的要求） */
const H5_ARTIFACT_PROBE = `${H5_ORIGIN}/patient-h5/index.html`

const SETTINGS_API = '/api/v1/admin/settings'
const PROFILE_API = '/api/v1/patient/profile'

/** 患者端存储键（apps/patient-miniapp/src/utils/token.ts 的 TOKEN_KEY / PATIENT_ID_KEY） */
const LS_PATIENT_TOKEN_KEY = 'bracesync_token'
const LS_PATIENT_ID_KEY = 'bracesync_patient_id'

/** newPatientID 发号形状（同 21-chain-c：P + 四位年 + 12 位 hex） */
const PATIENT_ID_RE = /^P\d{4}[0-9a-f]{12}$/
const GHOST_PATIENT_ID = `P${'0'.repeat(16)}`
const NOT_REGISTERED_PATH = '/api/v1/zzz-t614-wear-target-not-registered-6b21'
if (!PATIENT_ID_RE.test(GHOST_PATIENT_ID)) {
  throw new Error(`T614 探针患者号形状不符发号规则，代次探针打的不是真实形状：${GHOST_PATIENT_ID}`)
}

/** 后端 validateSettings 的合法域（契约边界，不是期望值） */
const CONTRACT_MIN_TARGET_HOURS = 1
const CONTRACT_MAX_TARGET_HOURS = 24

interface Envelope {
  status: number
  contentType: string
  /** null = 响应体不是 JSON（gin 自己的 404 就是这一形，负对照要的正是它） */
  code: number | null
  message: string
  data: unknown
}

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
  return {
    status: res.status(),
    contentType: (res.headers()['content-type'] ?? '').split(';')[0],
    code,
    message,
    data,
  }
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
 * 甲格基准：配置面现读。
 * 只断言「是数值且落在契约域」—— 具体是多少由环境自己决定，本用例不关心也不锁定。
 */
async function readConfigTargetHours(p: Page, token: string): Promise<number> {
  const s = await callOk<{ dailyWearTargetHours?: unknown }>(p, 'GET', SETTINGS_API, {
    token,
    why: '读系统配置（wear_target_hours 的维护入口，PRD §7D.12）',
  })
  const raw = s.dailyWearTargetHours
  expect(
    typeof raw,
    `配置面 dailyWearTargetHours 应是数值，实得 ${JSON.stringify(raw)}（键不在场即后端出参退化）`,
  ).toBe('number')
  const n = raw as number
  expect(
    Number.isFinite(n) && n >= CONTRACT_MIN_TARGET_HOURS && n <= CONTRACT_MAX_TARGET_HOURS,
    `配置面应落在契约域 [${CONTRACT_MIN_TARGET_HOURS},${CONTRACT_MAX_TARGET_HOURS}]，实得 ${n}`,
  ).toBeTruthy()
  return n
}

/** 页面文本里的目标值解析不出就是「页面没按口径渲染」，比拿字符串比较更早就暴露形状漂移 */
function parseRenderedTargetHours(text: string): number {
  const m = text.match(/目标[:：]\s*([\d.]+)\s*小时\/天/)
  if (!m) throw new Error(`佩戴管理页目标行未渲染成「目标: <数值>小时/天」口径，实得「${text}」`)
  return Number(m[1])
}

/** 患者号只以后缀形态落日志，全号不进任何载体（同 25-chain-b 口径） */
const tail = (id: string): string => `${id.slice(0, 2)}**…${id.slice(-4)}`

/**
 * 自建患者会话按 worker 缓存。
 * ⚠️ 必须是「谁要用谁造」而不是「26.1 造好后面两格捡现成」：2026-10-07 staging 实测
 *    26.2 判红后 Playwright 换了 worker，新 worker 里 session 为空，而旧 worker 的
 *    afterAll 已经把那一行删掉 ⇒ 26.3 红在「没有会话可复用」上，读起来像被测面的缺陷，
 *    实际是取数腿对 worker 复用的隐含假设。三格各自都能独立起跑，红才只有一种含义。
 */
interface PatientSession {
  patientId: string
  phone: string
  password: string
  /** 建档前的患者总数，afterAll 用它核对行数守恒 */
  baselineTotal: number
}
let session: PatientSession | null = null

/**
 * 授权造数（幂等）：负对照 → T477/T467 在架探针 → 总数基线 → 唯一名+唯一号建档 → 一次性口令。
 * 探针排在任何写之前：负对照不立起来就分不清「路由在架且资源不存在」与「路由没在架」。
 */
async function ensurePatientSession(p: Page): Promise<PatientSession> {
  if (session) return session

  const adminToken = await getAuthToken(p)
  expect(adminToken, '真实模式登录应把 admin JWT 写进 localStorage').toBeTruthy()

  for (const m of ['POST', 'PUT', 'DELETE'] as const) {
    const neg = await callApi(p, m, NOT_REGISTERED_PATH)
    expect(
      neg.code,
      `负对照失效：${m} 未注册路径应回非 JSON 的 gin 404，实得 status=${neg.status} code=${neg.code} ct=${neg.contentType}`,
    ).toBe(null)
  }
  const t477 = await callApi(p, 'POST', `/api/v1/admin/patients/${GHOST_PATIENT_ID}/password`, {
    token: adminToken!,
  })
  const t467 = await callApi(p, 'DELETE', `/api/v1/admin/patients/${GHOST_PATIENT_ID}`, {
    token: adminToken!,
  })
  expect(
    t477.code,
    `T477 设口令端点不在架（期望 JSON 10404），实得 status=${t477.status} code=${t477.code} —— 没有它建不起患者会话`,
  ).toBe(10404)
  expect(
    t467.code,
    `T467 删患者端点不在架（期望 JSON 10404），实得 status=${t467.status} code=${t467.code} —— 没有它收尾还原不了`,
  ).toBe(10404)

  const before = await callOk<{ total: number }>(p, 'GET', '/api/v1/admin/patients?page=1&pageSize=10', {
    token: adminToken!,
    why: '读患者总数基线',
  })

  const name = uniqueName(E2E_PATIENT_NAME_PREFIX)
  const phone = `135${String(Math.floor(Math.random() * 1e8)).padStart(8, '0')}`
  const created = await callOk<{ patientId: string }>(p, 'POST', '/api/v1/admin/patients', {
    token: adminToken!,
    body: { name, phone, gender: 'female', age: 14, diagnosis: '胸段侧弯', cobbAngle: 25 },
    why: '建档失败（message 提示已存在即随机号撞了 phone_hash 查重，重跑一轮）',
  })
  expect(created.patientId, '建档应回符合发号形状的患者号').toMatch(PATIENT_ID_RE)
  const pwd = await callOk<{ patientId: string; password: string }>(
    p,
    'POST',
    `/api/v1/admin/patients/${created.patientId}/password`,
    { token: adminToken!, why: '设一次性口令失败 ⇒ 患者会话建不起来' },
  )
  expect(pwd.patientId, '口令端点应回同一患者号').toBe(created.patientId)
  // 口令值不落日志、不进卡面，只量长度（服务端 genDoctorPassword：Br + 12 随机位 + #7 = 16）
  expect(pwd.password.length, '一次性口令长度应为 16（值不外显）').toBe(16)
  session = { patientId: created.patientId, phone, password: pwd.password, baselineTotal: before.total }
  console.log(
    `[t614-cfg][造数] 患者=${tail(created.patientId)} 名=${name} 建档前 total=${before.total}（收尾删行）`,
  )
  return session
}

/** 患者会话：口令登录走下发腿的源（H5_ORIGIN），返回值同时带上患者号供注入 localStorage */
async function patientSessionToken(p: Page): Promise<{ token: string; patientId: string }> {
  const s = await ensurePatientSession(p)
  const login = await callOk<{ token: string; patientId: string; role: string }>(
    p,
    'POST',
    `${H5_ORIGIN}/api/v1/patient/login`,
    { body: { phone: s.phone, password: s.password }, why: '患者口令登录失败' },
  )
  expect(login.role, '登录态角色应为 patient').toBe('patient')
  expect(login.patientId, '登录回的患者号应等于自建患者').toBe(s.patientId)
  return login
}

test.describe('26-佩戴期望时长下发链（T614 第三项，期望值全部来自接口现读）', () => {
  test('26.1 配置面 == 下发面：sys_configs 现值经 profile 出参原样下发（不比对任何固定数字）', async ({
    page,
  }) => {
    await realLogin(page)
    await expect(page).toHaveURL(/\/dashboard/, { timeout: 20_000 })
    const adminToken = await getAuthToken(page)
    expect(adminToken, '真实模式登录应把 admin JWT 写进 localStorage').toBeTruthy()

    // 授权造数（唯一名 + 唯一号 → T477 一次性口令 → 口令登录）；探针与基线都在里面
    const { patientId, token } = await patientSessionToken(page)

    // 甲格：配置面现读 → 下发面现读 → 等值（期望值 = 本轮配置读数，不是常量）
    const cfg = await readConfigTargetHours(page, adminToken!)
    const prof = await callOk<{ patientId: string; dailyWearTargetHours?: unknown }>(
      page,
      'GET',
      `${H5_ORIGIN}${PROFILE_API}`,
      { token, why: '患者档案出参应含佩戴期望时长（T576 甲案）' },
    )
    expect(prof.patientId, 'profile 端点应回本人').toBe(patientId)
    const pushed = prof.dailyWearTargetHours
    expect(
      typeof pushed,
      `下发面 dailyWearTargetHours 应是数值，实得 ${JSON.stringify(pushed)} —— 键不在场即出参退化，页面只能吃兜底`,
    ).toBe('number')
    expect(
      pushed,
      `配置面与下发面应同值（真源同一个 sys_configs 键）：配置面=${cfg} 下发面=${String(pushed)} | 配置腿源=${process.env.E2E_STAGING_URL ?? '<baseURL 默认>'} 下发腿源=${H5_ORIGIN}`,
    ).toBe(cfg)
    console.log(
      `[t614-cfg][甲] 配置面=${cfg} 下发面=${pushed} 等值=true | 目标环境=${process.env.E2E_STAGING_URL ?? '<未设，走 config 默认>'}`,
    )
  })

  /**
   * 乙格：页面渲染值 == 页面自己那一发 GET 的响应体 == 配置面现读。
   *
   * 为什么拿响应体对而不拿常量对：环境真值可能与前端兜底常量同值（TST=22 就是这形），
   * 那时「DOM == 常量」证不了前端在消费下发字段；而「DOM == 这一发请求的响应体」两形都成立。
   */
  test('26.2 页面面 == 下发面：佩戴管理页把下发字段渲染成目标线（期望值取自本轮响应体）', async ({
    page,
    browser,
  }) => {
    await realLogin(page)
    const adminToken = await getAuthToken(page)
    expect(adminToken, '真实模式登录应把 admin JWT 写进 localStorage').toBeTruthy()

    // 产物存在性探针走 HTTP，不依赖页面结构（缺产物时页面本身打不开，DOM 探测会抛）
    await requireDeployedBuild(page, {
      marker: 'T614-patient-h5-wear-target-page',
      why:
        '该环境的 patient-h5 产物尚未投放（乙格要真页面）；甲格不受此门影响。TST 现读 /patient-h5/ 403、index.html 500',
      probe: async (p) => {
        const res = await p.request.get(H5_ARTIFACT_PROBE)
        const ok200 = res.status() === 200
        console.log(
          `[t614-cfg][产物探针] GET ${H5_ARTIFACT_PROBE} → status=${res.status()} present=${String(ok200)}`,
        )
        return ok200
      },
    })

    const cfg = await readConfigTargetHours(page, adminToken!)
    const { token: h5Token, patientId } = await patientSessionToken(page)

    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 } })
    try {
      const h5 = await ctx.newPage()
      await h5.addInitScript(
        ({ tokenKey, pidKey, tk, pid }) => {
          window.localStorage.setItem(tokenKey, tk)
          window.localStorage.setItem(pidKey, pid)
        },
        {
          tokenKey: LS_PATIENT_TOKEN_KEY,
          pidKey: LS_PATIENT_ID_KEY,
          tk: h5Token,
          pid: patientId,
        },
      )
      // 等待器必须先于导航挂好：profile 那一发是页面挂载时自己发的
      const profileResp = h5
        .waitForResponse(
          (res) =>
            res.request().method() === 'GET' &&
            res.url().includes(PROFILE_API) &&
            res.status() === 200,
          { timeout: 25_000 },
        )
        .catch(() => null)
      await h5.goto(`${H5_ORIGIN}${H5_WEARING_PATH}`, { waitUntil: 'domcontentloaded' })

      await expect(
        h5.locator('.ring-target'),
        '注入患者会话后佩戴管理页应渲染目标行（渲染不出＝登录态没被接住）',
      ).toBeVisible({ timeout: 25_000 })

      const resp = await profileResp
      expect(resp, `未捕获到患者页自己发出的 GET ${PROFILE_API} —— 页面没发这一发，乙格无从判`).not.toBeNull()
      const respOrigin = new URL(resp!.url()).origin
      expect(respOrigin, '患者页的档案读应与页面同源（全仓无 CORS 放行，跨源即被浏览器拦掉）').toBe(H5_ORIGIN)
      const body = (await resp!.json().catch(() => null)) as { data?: { dailyWearTargetHours?: unknown } } | null
      expect(body?.data, `档案响应体应是含 data 的 JSON 信封，实得 ${JSON.stringify(body)?.slice(0, 120)}`).toBeTruthy()
      const pushed = body!.data!.dailyWearTargetHours
      expect(
        typeof pushed,
        `页面那一发响应体的 dailyWearTargetHours 应是数值，实得 ${JSON.stringify(pushed)}`,
      ).toBe('number')

      const rendered = parseRenderedTargetHours((await h5.locator('.ring-target').textContent()) ?? '')
      expect(
        pushed,
        `页面那一发响应体应与配置面同值：配置面=${cfg} 响应体=${String(pushed)}`,
      ).toBe(cfg)
      expect(
        rendered,
        `页面渲染的目标线应等于它自己拿到的下发值（不是本地兜底常量）：响应体=${String(pushed)} 页面面=${rendered} | 渲染文本=${(
          await h5.locator('.ring-target').textContent()
        )?.trim()}`,
      ).toBe(Number(pushed))
      console.log(
        `[t614-cfg][乙] 配置面=${cfg} 下发面(页面响应体)=${Number(pushed)} 页面面=${rendered} | 页面=${H5_ORIGIN}${H5_WEARING_PATH}`,
      )
    } finally {
      await ctx.close()
    }
  })

  /**
   * 丙格（注牙）：把页面那一发 profile 响应里的期望时长改写成一枚**必然不等于配置面**的值，
   * 页面若真在消费下发字段，渲染值就该跟着改写值走。
   *
   * 为什么要有这一格：26.2 的等值判据在「环境真值恰好等于前端兜底常量」那一形里是空的
   *  —— TST 现读就是 22，与 wear-target.ts 的兜底同值，此时「DOM == 响应体」即便由常量蒙对也照样绿。
   * 丙格把响应改写成一个不同的数，常量蒙不出来：改写值跟着渲染 ⇒ 消费链在场；
   * 渲染仍停在同一个数 ⇒ 页面根本没吃下发字段，26.2 就是假绿。
   * 牙打在执行体真正读的那个面上（页面自己的那一发请求），不是打在提前 return 的分支上。
   */
  test('26.3 注牙：改写页面那一发的下发值，渲染值必须跟着走（证明目标线不是常量蒙对）', async ({
    page,
    browser,
  }) => {
    await realLogin(page)
    const adminToken = await getAuthToken(page)
    expect(adminToken, '真实模式登录应把 admin JWT 写进 localStorage').toBeTruthy()

    await requireDeployedBuild(page, {
      marker: 'T614-patient-h5-wear-target-page',
      why: '丙格与乙格同属页面面，同一枚产物探针（命中缓存，不再发第二发）',
      probe: async (p) => {
        const res = await p.request.get(H5_ARTIFACT_PROBE)
        return res.status() === 200
      },
    })

    const cfg = await readConfigTargetHours(page, adminToken!)
    // 改写值只从契约域里取，不引入新的魔法数字；与配置面不等是本格的全部前提
    const mutated = cfg >= CONTRACT_MAX_TARGET_HOURS ? cfg - 1 : cfg + 1
    expect(mutated, `注牙前提失效：改写值 ${mutated} 应不等于配置面 ${cfg}`).not.toBe(cfg)

    const { token: h5Token, patientId } = await patientSessionToken(page)
    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 } })
    try {
      const h5 = await ctx.newPage()
      await h5.addInitScript(
        ({ tokenKey, pidKey, tk, pid }) => {
          window.localStorage.setItem(tokenKey, tk)
          window.localStorage.setItem(pidKey, pid)
        },
        {
          tokenKey: LS_PATIENT_TOKEN_KEY,
          pidKey: LS_PATIENT_ID_KEY,
          tk: h5Token,
          pid: patientId,
        },
      )
      /**
       * 处理器是异步的，而页面可以先用本地兜底常量把 .ring-target 渲染出来：
       * 只看「行可见」就去断言，会在 route.fetch() 还没回来的那一瞬取数 ⇒
       * rewritten=0 而命中=1 的假红（2026-10-07 staging 第二跑就是这个形状：
       * 命中行照打、放行分支的行没打、屏上没有任何异常）。
       * 所以收尾要自己等：handlerDone 由处理器 finally 置位，networkidle 兜住
       * 「改写后的响应真正交付给页面且页面吃完这一发」，两把都押在同一发上。
       */
      let hits = 0
      let rewritten = 0
      let settled = 0
      let lastStatus = -1
      let lastHead = ''
      let handlerErr = '<无>'
      let markDone: () => void = () => {}
      const handlerDone = new Promise<void>((resolve) => {
        markDone = resolve
      })
      const net: string[] = []
      h5.on('request', (req) => {
        const u = new URL(req.url())
        if (!u.pathname.startsWith('/api/v1/')) return
        // 诊断行也要过同一条号规则：患者号只以后缀形态进屏（2026-10-07 首版把整条 URL 贴进
        // 失败信息，那一屏因此不能入库）
        const redacted = u.pathname.replace(new RegExp(PATIENT_ID_RE.source.slice(1, -1), 'g'), (m) => tail(m))
        net.push(`${req.method()} ${redacted}${u.search}`)
      })
      // 匹配按 pathname 判定，不写 `**` 通配：通配串不认带 query 的 URL，
      // 命中 0 分不清「页面没发这一发」与「牙没装到位」（2026-10-07 首跑就是这么红的）
      await h5.route((u: URL) => u.pathname === PROFILE_API, async (route) => {
        try {
          hits += 1
          const res = await route.fetch()
          lastStatus = res.status()
          const text = await res.text()
          lastHead = text.slice(0, 160).replace(/\s+/g, ' ')
          let env: { code?: unknown; data?: Record<string, unknown> } | null = null
          try {
            env = JSON.parse(text)
          } catch {
            env = null
          }
          if (!env || typeof env.code !== 'number' || !env.data || typeof env.data !== 'object') {
            // 放行分支不做业务断言（deploy-guard 对探针的同款要求），只在屏上留形状
            await route.fallback()
            return
          }
          env.data.dailyWearTargetHours = mutated
          await route.fulfill({
            status: res.status(),
            contentType: (res.headers()['content-type'] ?? 'application/json').split(';')[0],
            body: JSON.stringify(env),
          })
          rewritten += 1
        } catch (err) {
          handlerErr = `${(err as Error).name}: ${(err as Error).message}`
          // 处理器自己出事时不许把页面吊着：按原样放行，让后面的读数说明「牙没装上」而不是超时
          await route.fallback().catch(() => {})
        } finally {
          settled += 1
          markDone()
        }
      })

      await h5.goto(`${H5_ORIGIN}${H5_WEARING_PATH}`, { waitUntil: 'domcontentloaded' })
      const waitMs = 25_000
      const doneInTime = await Promise.race([
        handlerDone.then(() => true),
        new Promise<boolean>((resolve) => {
          setTimeout(() => resolve(false), waitMs)
        }),
      ])
      await h5.waitForLoadState('networkidle', { timeout: waitMs }).catch(() => {})
      const line = () =>
        `命中=${hits} 改写=${rewritten} 收尾=${settled} 处理器异常=${handlerErr} 放行分支=${
          hits > 0 && rewritten === 0 && handlerErr === '<无>' ? `是（status=${lastStatus} head=${lastHead}）` : '否'
        } | 请求序列=${net.join(' → ') || '<一条都没有>'}`
      expect(
        doneInTime,
        `注牙前提失效：处理器在 ${waitMs}ms 内没收尾（牙没装上或页面没发这一发）| ${line()}`,
      ).toBeTruthy()
      await expect(
        h5.locator('.ring-target'),
        '注牙轮：页面应照常渲染目标行（渲染不出＝这一格没打到执行体）',
      ).toBeVisible({ timeout: waitMs })
      expect(hits, `注牙前提失效：页面那一发 ${PROFILE_API} 没进改写处理器 | ${line()}`).toBeGreaterThan(0)
      expect(rewritten, `注牙前提失效：进了处理器但没改成 | ${line()}`).toBe(hits)

      const rendered = parseRenderedTargetHours((await h5.locator('.ring-target').textContent()) ?? '')
      expect(
        rendered,
        `注牙判据：改写响应体后渲染值应跟着走 —— 改写值=${mutated} 渲染值=${rendered}（渲染仍停在同一个数＝页面没消费下发字段，26.2 属假绿）| ${line()}`,
      ).toBe(mutated)
      expect(rendered, `注牙反证：渲染值不该等于配置面现读 ${cfg}`).not.toBe(cfg)
      console.log(
        `[t614-cfg][丙·注牙] 配置面=${cfg} 改写下发值=${mutated} 渲染值=${rendered} | 跟随改写=true 命中=${hits} 改写次数=${rewritten} 收尾=${settled}`,
      )
    } finally {
      await ctx.close()
    }
  })

  /**
   * 还原 + 报备（用例主体半路红也要跑，afterAll 不看用例状态）。
   * 删掉自建患者并核对总数回到本 worker 建档前快照；删不动就登记「残留报备」再把失败抛出去。
   *
   * total 要稳定读：worker 被回收时新 worker 的建档可能落在旧 worker 的删除之前，
   * 于是「我删完后的 total」短暂比我的基线多 1 —— 那是兄弟 worker 的行不是残留。
   * 所以最多重读三回，每回都留屏；三回都不等才判红（残留只准在屏上出现，不准被吞掉）。
   */
  test.afterAll(async ({ browser }) => {
    const mine = session
    if (!mine) return
    const ctx = await browser.newContext()
    const p = await ctx.newPage()
    try {
      await realLogin(p)
      const token = await getAuthToken(p)
      if (!token) throw new Error('afterAll 取不到 admin JWT，无法还原')

      const del = await callApi(p, 'DELETE', `/api/v1/admin/patients/${mine.patientId}`, { token })
      console.log(
        `[t614-cfg][还原] DELETE /api/v1/admin/patients/${tail(mine.patientId)} → status=${del.status} code=${del.code}`,
      )
      expect(
        del.code,
        `还原删除应回 code=0，实得 status=${del.status} code=${del.code} message=${del.message}`,
      ).toBe(0)

      const gone = await callApi(p, 'GET', `/api/v1/admin/patients/${mine.patientId}`, { token })
      expect(gone.code, `删后详情应回 code=10404（硬删），实得 ${gone.code}`).toBe(10404)

      const frames: number[] = []
      for (let attempt = 0; attempt < 3; attempt += 1) {
        const after = await callOk<{ total: number }>(
          p,
          'GET',
          '/api/v1/admin/patients?page=1&pageSize=10',
          { token, why: '删行后读患者总数' },
        )
        frames.push(after.total)
        if (after.total === mine.baselineTotal) break
        if (attempt < 2) await new Promise((r) => setTimeout(r, 1500))
      }
      console.log(
        `[t614-cfg][还原] 建档前 total=${mine.baselineTotal} 删后各次读数=${frames.join(',')}`,
      )
      expect(
        frames[frames.length - 1],
        `行数守恒：删后 total 应收敛到建档前快照 ${mine.baselineTotal}，各次读数=${frames.join(',')}`,
      ).toBe(mine.baselineTotal)
    } catch (err) {
      console.log(
        `[t614-cfg][残留报备] 自建患者未确认删除成功，patientId=${tail(mine.patientId)}，原因=${
          (err as Error).message
        }`,
      )
      throw err
    } finally {
      await ctx.close()
    }
  })
})
