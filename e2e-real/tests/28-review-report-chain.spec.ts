import { test, expect, type Page, type Locator } from '@playwright/test'
import {
  realLogin,
  adminLogout,
  gotoMenu,
  menuItems,
  adminMessage,
  tableRows,
  pickSelectOption,
  getAuthToken,
} from '../real-helpers'
import { resolveH5Origin } from '../h5-origin'

/**
 * T632 段2+段3 · 复查报告链（真实 COS 直传 → 复查记录 → 患者端 H5 可见可下载）
 *
 * ── 编号映射（先说明，免得按卡面找不到文件）──────────────────────
 * 派发单原文建议「新增 21-review-report-chain.spec」，但 e2e-real/tests/21-* 已被
 * T462 链 C（21-chain-c-patient-profile.spec.ts）占用，tests 目录按序号全局排号，
 * 同号会撞。故落地为 **28**-review-report-chain.spec.ts，卡面 21 ↔ 实际 28 一并登记在
 * 交件说明里，不回改卡面（卡面是当时的真相）。
 *
 * ── 三段链里本文件负责的两段 ──────────────────────────────────
 *   段2：医生在「复查报告」页上传 PDF / JPG / PNG ⇒ presign ⇒ 浏览器直传真 COS
 *        ⇒ upload-complete ⇒ files 行转 uploaded ⇒ 下载预签名可用 ⇒ 提交复查记录
 *   段3：患者端 H5「复查管理」页真实渲染出这三条记录（含报告名），并能在页内发起下载
 *   段1（医护账号补手机号→重置密码→登录）在 09-doctor-accounts.spec.ts 的 09b 组。
 *
 * ── 为什么页面源不是 E2E_STAGING_URL 而是域名等价源（这条最容易踩，写死在此）──
 * CI 的 real 档把 E2E_STAGING_URL 注成 http://106.52.39.208:81。而浏览器向 COS 直传要过
 * 桶侧 CORS：PUT 带 Content-Type: application/pdf 会先发 OPTIONS 预检，桶的放行面只认
 * 产物构建期写死的那枚源 http://hbksd.com.cn:81（2026-10-09 实测：同一把 OPTIONS 打到
 * 域名源回 200 且带 Access-Control-Allow-Origin，打到 IP 源回 403 AccessForbidden）。
 * 两者是同一台 staging 的两枚入口（remote_ip 相同），所以本用例把 admin/医生两腿固定在
 * 域名源打开，判据一条不减。解析见 resolvePageOrigin()：
 *   · 注入位 E2E_T632_PAGE_ORIGIN 优先（也是负对照的把手，见 28.3）；
 *   · 未注入时，目标是 staging 的两枚入口之一 ⇒ 取域名源；
 *   · 其它环境（含 runbook 的 ssh -L 回环隧道）⇒ 抛红点名要注入，不静默走默认值——
 *     回环隧道下桶侧放行面对不上，直传必 403，与其让它在上传那步糊成一团红，不如开局就说清。
 *
 * ── 🔴 判据打的是真 COS，不接受 mock 兜底 ──────────────────────
 *   1) presign 回的 signature_url 主机必须落在 *.myqcloud.com；出现 mock-cos.example.com
 *      （apps/admin-web/src/api/index.ts:723 的 mock 分支形状）直接判红。
 *   2) 每种形状上传完，再从 node 侧把「下载预签名 URL」真取回字节，与上传的字节逐字节对平
 *      ——这证的是桶里确实有那个对象，而不只是库里有一行写着 uploaded。
 *   3) 服务端 staging 侧 COS_* env 实测无 FILE_MOCK_COS；本用例不引入任何 mock 开关。
 *
 * ── 数据自建自清与不可撤残留（红线：不污染共享 seed、不用真实患者数据）──
 *   · 医护行、患者行都是本用例自建（前缀 T632链路医护 / T632链路患者），**复用不新建**，
 *     seed 的 D000x / PT-00x 一行不碰；团队归属取最空的那枚。
 *   · 撤不掉的（无删除端点，实测路由面）：files 行、review_records 行、桶内对象、
 *     自建医护行、自建患者行。前两枚每轮各 +3/+3（PDF/JPG/PNG 各一条记录，一条记录绑一个
 *     文件，不留「uploaded 却没被任何记录引用」的孤儿文件行），负对照 28.3 另 +1 枚 pending
 *     files 行。自建医护/患者行各 1 枚长期复用 ⇒ 行数不随轮次增长。
 *   · 之所以每轮给患者换一个手机号：患者登录只认 phone+password（handler.go:644-667），
 *     而建档时的明文号不回显（列表/详情两侧都不投该列，model.go:149），复用行若不换号就
 *     读不回自己上一轮的号 ⇒ 用 admin 的改号通道 PUT /admin/patients/:id/phone 每轮设新号。
 *     改的是自建行的号，不动 seed。
 *   · 一次性口令（医护/患者）只从接口取、只打长度，值不进日志、不进断言消息（失败留档的
 *     trace/screenshot 会随 CI 工件上传）。
 */

const MENU_TITLE = '复查报告'
const DOCTOR_PREFIX = 'T632链路医护'
const PATIENT_PREFIX = 'T632链路患者'
const DOCTOR_TITLE = '主治医师'
const DOCTOR_DEPARTMENT = 'T632测试科室'

/** 患者端「复查管理」页（apps/patient-miniapp/src/pages.json:44） */
const H5_REPORT_PATH = '/patient-h5/#/pages/report/index'
const H5_ORIGIN = resolveH5Origin('E2E_PATIENT_H5_URL', 'T632 段3 患者端')

/** 患者端存储键（apps/patient-miniapp/src/utils/token.ts，同 21-chain-c 的口径） */
const LS_PATIENT_TOKEN_KEY = 'bracesync_token'
const LS_PATIENT_ID_KEY = 'bracesync_patient_id'

/** 三种形状的取样：文件头魔数按服务端权威表取（service/presigner.go:171-180） */
interface Shape {
  ext: string
  mime: string
  magic: Buffer
  /** 桶里 object_key 的结尾扩展名 */
  keyExt: string
}
const SHAPES: Shape[] = [
  { ext: '.pdf', mime: 'application/pdf', magic: Buffer.from('%PDF-1.4\n%T632 e2e real-cos fixture\n', 'latin1'), keyExt: '.pdf' },
  { ext: '.jpg', mime: 'image/jpeg', magic: Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46]), keyExt: '.jpg' },
  { ext: '.png', mime: 'image/png', magic: Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), keyExt: '.png' },
]

/** 服务端 file-service 的信封码（handler/error_response.go:13-25）；admin 侧文案见 shared-utils/errorCopy.ts:71 */
const CODE_INVALID_REQUEST = 60001
const CODE_FILE_NOT_FOUND = 61001
const INVALID_REQUEST_TOAST = '文件参数有误，请重新选择文件后重试'
const UPLOAD_OK_TOAST = '文件上传成功'
const SUBMIT_OK_TOAST = '复查记录已提交'

/** review_records.review_type 的 CHECK 取值（scripts/db/migrations/000009_review_records.up.sql:12） */
const REVIEW_TYPE = 'follow-up'

interface DoctorDTO {
  doctorId: string
  name: string
  title: string | null
  department: string | null
  teamId: string | null
  phoneState: string
  status: string
  username: string | null
}

interface TeamDTO {
  teamId: string
  name: string
  patientCount: number
}

interface PatientDTO {
  patientId: string
  name: string
  teamId: string | null
  doctorId: string | null
  status: string
}

interface PatientListResp {
  list: PatientDTO[]
  total: number
  page: number
  pageSize: number
}

/** file-service 的 FileMetadata 是 snake_case 直出（model/file_meta.go:55-69） */
interface FileRow {
  file_id: string
  bucket: string
  object_key: string
  url: string
  file_type: string
  owner_type: string
  owner_id: string
  size: number
  content_type: string
  status: string
  uploaded_at: string | null
  created_at: string
}

interface FilesQuery {
  list: FileRow[]
  total: number
  page: number
  pageSize: string | number
}

interface ReviewRecordDTO {
  reviewId: string
  patientId: string
  reviewDate: string
  reviewType: string
  findings: string | null
  doctorId: string | null
  reportFileId: string | null
  reportFileName: string | null
  reportContentType: string | null
  reportDownloadUrl: string | null
}

interface Envelope {
  status: number
  code: number | null
  message: string
  data: unknown
}

async function callApi(
  p: Page,
  method: string,
  urlPath: string,
  opts: { token?: string; body?: unknown; headers?: Record<string, string> } = {},
): Promise<Envelope> {
  const headers: Record<string, string> = { ...opts.headers }
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
    /* 非 JSON：code 留 null，交给调用方判形状 */
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
 * 本用例两腿（admin / 医生）打开页面的源。
 * 判据形状与 resolveH5Origin 同族：http + 显式端口、拒绝生产入口，只是多一条「为什么不是 IP」。
 */
function resolvePageOrigin(): string {
  const injected = (process.env.E2E_T632_PAGE_ORIGIN ?? '').trim()
  const target = (process.env.E2E_STAGING_URL ?? '').trim()
  const stagingDomain = 'http://hbksd.com.cn:81'

  if (injected) {
    if (!/^http:\/\/[^/]+:\d+$/.test(injected)) {
      throw new Error(`E2E_T632_PAGE_ORIGIN 应是 http + 显式端口的源，实得 ${injected}`)
    }
    if (/api\.hbksd\.com\.cn/.test(injected)) { // 49.235.137.217 自 2026-10-11 起为 TST（Boss 口径，TST 写段经批 A 授权）；生产针保留 api.hbksd.com.cn
      throw new Error(`T632 复查链命中生产入口，红线拒绝：${injected}`)
    }
    console.log(`[t632-复查链][页面源] E2E_T632_PAGE_ORIGIN 已注入 ⇒ 源=${injected}（负对照时这就是那把把手）`)
    return injected
  }

  const host = target ? new URL(target).hostname : 'hbksd.com.cn'
  if (host === '49.235.137.217') {
    // T651 双通道 tst 档（2026-10-11 Boss 口径）：TST admin 与入口同源投放，页面源=入口本体。
    // 前置：COS 桶 CORS 放行面须含这枚源，否则浏览器直传 403（TST 基建就绪度由本链实跑出读数）。
    console.log(`[t632-复查链][页面源] 目标=${target} 为 tst 档 ⇒ 页面源取 ${target}（桶 CORS 放行面须含 TST admin 源，未放行时直传腿红=基建读数非代码回归）`)
    return target!
  }
  if (host === 'hbksd.com.cn' || host === '106.52.39.208') {
    console.log(
      `[t632-复查链][页面源] 目标=${target || '<未设>'} ⇒ 页面源取 ${stagingDomain}` +
        `（同一台 staging 的域名入口；桶侧 CORS 只放行这枚源，直传腿必须从它发起）`,
    )
    return stagingDomain
  }
  throw new Error(
    `T632 复查链未知目标环境 ${target || '<未设 E2E_STAGING_URL>'}（host=${host}）：` +
      `COS 桶的 CORS 放行面是按 staging 的产物源配的，换环境请把该环境实际投放的 admin 源` +
      `（必须与桶放行面同源，否则浏览器直传必 403）注入 E2E_T632_PAGE_ORIGIN 再跑。`,
  )
}

const PAGE_ORIGIN = resolvePageOrigin()

function uploadInput(page: Page): Locator {
  return page.locator('input.el-upload__input')
}
function uploadedTag(page: Page): Locator {
  return page.locator('.el-tag', { hasText: '已上传' })
}
/** 两个 .page-card：第 1 张上传区、第 2 张历史列表（与 mock 线 e2e/tests/admin-review.spec.ts 同一作用域口径） */
function listCard(page: Page): Locator {
  return page.locator('.page-card').nth(1)
}
function listTitle(page: Page): Locator {
  return listCard(page).locator('.page-card-title')
}
function rowsIn(page: Page): Locator {
  return tableRows(page, listCard(page))
}

function uniqueTestPhone(): string {
  return `135${String(Math.floor(Math.random() * 1e8)).padStart(8, '0')}`
}

/** 只用作接口锚：今天(本地)往前推 i 天的 YYYY-MM-DD，保证不落在「未来」这类可疑取值上 */
function reviewDateBack(i: number): string {
  const d = new Date(Date.now() - i * 86_400_000)
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${mm}-${dd}`
}

async function pickSparseTeam(p: Page, token: string): Promise<TeamDTO> {
  const teams = await callOk<TeamDTO[]>(p, 'GET', '/api/v1/teams?page=1&pageSize=100', {
    token,
    why: '读团队列表以挑选本用例的归属团队',
  })
  expect(teams.length, '团队列表为空 ⇒ 测试医护无处安放').toBeGreaterThan(0)
  const sorted = [...teams].sort((a, b) => a.patientCount - b.patientCount || a.teamId.localeCompare(b.teamId))
  const chosen = sorted[0]
  console.log(`[t632-复查链][选团队] ${chosen.teamId} 患者数=${chosen.patientCount}（取最空那枚，只在首轮新建时用到）`)
  return chosen
}

/**
 * 复用优先拿本用例的测试医护（前缀命中就沿用，按 doctorId 升序取第一枚）。
 * 复用而不是每轮新建：/admin/doctors 建的账号行没有删除端点，新建会让 staging 每轮多一枚账号。
 */
async function ensureFixtureDoctor(p: Page, token: string): Promise<DoctorDTO> {
  const all = await callOk<DoctorDTO[]>(p, 'GET', '/api/v1/doctors', { token, why: '读医护列表以复用/新建测试医护' })
  const mine = all.filter((d) => (d.name || '').startsWith(DOCTOR_PREFIX)).sort((a, b) => a.doctorId.localeCompare(b.doctorId))
  if (mine.length > 0) {
    console.log(
      `[t632-复查链][复用医护] doctorId=${mine[0].doctorId} username=${mine[0].username} teamId=${mine[0].teamId}（前缀命中 ${mine.length} 枚）`,
    )
    return mine[0]
  }
  const team = await pickSparseTeam(p, token)
  const name = `${DOCTOR_PREFIX}-${Date.now().toString().slice(-6)}`
  const created = await callOk<DoctorDTO>(p, 'POST', '/api/v1/admin/doctors', {
    token,
    body: { name, title: DOCTOR_TITLE, department: DOCTOR_DEPARTMENT, teamId: team.teamId },
    why: '新建测试医护失败',
  })
  expect(created.username, '建档应回系统生成的登录账号').toMatch(/^doc\d{5}$/)
  console.log(`[t632-复查链][新建医护] doctorId=${created.doctorId} username=${created.username} teamId=${created.teamId}`)
  return created
}

/**
 * 按前缀在患者列表里找自建患者。列表按 created_at DESC，pageSize 契约上限 100，
 * 而自建患者是第一轮建的（会越来越靠后）⇒ 必须翻页找，不能只看首面。
 * total 是过滤后全量计数，用它当翻页上界；翻完仍未命中才算「不存在」。
 */
async function findPatientByPrefix(p: Page, token: string): Promise<PatientDTO | null> {
  let scanned = 0
  for (let page = 1; page <= 20; page++) {
    const res = await callOk<PatientListResp>(p, 'GET', `/api/v1/admin/patients?page=${page}&pageSize=100`, {
      token,
      why: `翻患者列表第 ${page} 面`,
    })
    const hit = (res.list || []).find((x) => (x.name || '').startsWith(PATIENT_PREFIX))
    if (hit) {
      console.log(
        `[t632-复查链][复用患者] patientId=${hit.patientId} name=${hit.name} teamId=${hit.teamId}（扫到第 ${page} 面 / total=${res.total}）`,
      )
      return hit
    }
    scanned += (res.list || []).length
    if (scanned >= res.total) return null
  }
  throw new Error(`患者列表翻页超过 20 面（2000 行）仍未扫完，total 口径与本用例假设不符，先停手别把「没找到」当成「不存在」`)
}

/** 自建患者（首轮建，后续复用）：团队/医护与本用例的测试医护绑定，医生腿才够得到 */
async function ensureFixturePatient(p: Page, token: string, doctor: DoctorDTO): Promise<PatientDTO> {
  const found = await findPatientByPrefix(p, token)
  if (found) return found
  const name = `${PATIENT_PREFIX}-${Date.now().toString().slice(-6)}`
  const created = await callOk<PatientDTO>(p, 'POST', '/api/v1/admin/patients', {
    token,
    body: {
      name,
      phone: uniqueTestPhone(),
      gender: 'female',
      age: 15,
      diagnosis: '胸段侧弯（T632 自建）',
      cobbAngle: 27,
      teamId: doctor.teamId,
      doctorId: doctor.doctorId,
    },
    why: '新建测试患者失败（message=patient already exists ⇒ 随机手机号撞了查重，重跑一轮）',
  })
  expect(created.patientId, '建档应回患者号').toMatch(/^P\d{4}[0-9a-f]{12}$/)
  console.log(`[t632-复查链][新建患者] patientId=${created.patientId} name=${name} teamId=${doctor.teamId}`)
  return created
}

/** 某患者名下 review_report 文件行（admin 视角，按 owner 过滤；分页扫到 total 为止） */
async function queryReportFiles(p: Page, token: string, patientId: string): Promise<FileRow[]> {
  const rows: FileRow[] = []
  let total = 0
  for (let page = 1; page <= 10; page++) {
    const res = await callOk<FilesQuery>(
      p,
      'GET',
      `/api/v1/files/query?owner_type=patient&owner_id=${patientId}&file_type=review_report&page=${page}&pageSize=100`,
      { token, why: `读该患者的报告文件行第 ${page} 面` },
    )
    rows.push(...(res.list || []))
    total = res.total
    if (rows.length >= total) break
  }
  expect(
    rows.length,
    `files/query 翻页后行数应与 total 对平（实得 ${rows.length}，total=${total}），否则「集合差找新行」不稳`,
  ).toBe(total)
  return rows
}

/** 桶侧预签名 URL 的主机必须是真 COS，出现 mock 形状直接判红 */
function assertRealCosHost(rawUrl: string, why: string): URL {
  const u = new URL(rawUrl)
  expect(
    u.hostname.endsWith('.myqcloud.com'),
    `${why}：主机应是腾讯云 COS 域（*.myqcloud.com），实得 ${u.hostname} —— 直传没打真桶`,
  ).toBe(true)
  expect(
    u.hostname.includes('mock-cos.example.com') || rawUrl.includes('mock-cos'),
    `${why}：拿到的是 mock 地址（${u.hostname}），本用例红线是不接受 FILE_MOCK_COS 兜底假绿`,
  ).toBe(false)
  return u
}

test.describe('28-T632 段2+3 复查报告链（真 COS 直传 → 复查记录 → 患者端可见）', () => {
  /** 供 afterAll 报备残留用（本用例不删行：files / review_records 无删除端点） */
  let patientIdForReport = ''
  let doctorIdForReport = ''

  test('28.1 PDF/JPG/PNG 三形状：直传真桶 → 记录 uploaded → 下载取回字节 → 患者端渲染', async ({
    browser,
  }) => {
    // ── 1) admin 腿：建/复用靶子，拿三类一次性凭据 ─────────────────
    const adminCtx = await browser.newContext({ baseURL: PAGE_ORIGIN })
    const api = await adminCtx.newPage()
    let patient = ''
    try {
      await realLogin(api)
      await expect(api).toHaveURL(/dashboard$/, { timeout: 20_000 })
      const adminToken = await getAuthToken(api)
      expect(adminToken, 'admin 登录应把 JWT 写进 localStorage').toBeTruthy()

      const doctor = await ensureFixtureDoctor(api, adminToken!)
      doctorIdForReport = doctor.doctorId
      const fixturePatient = await ensureFixturePatient(api, adminToken!, doctor)
      patient = fixturePatient.patientId
      patientIdForReport = patient

      // 复用行时团队/医护必须仍指向本用例的医护，否则医生腿在 presign 的归属判定（T378）会 403，
      // 而那红看着像「测试坏了」。这里把它钉成开局断言，别让它漂。
      expect(fixturePatient.teamId, '患者团队应与测试医护同枚（首轮绑定后不再变）').toBe(doctor.teamId)

      // ── 2) 患者会话凭据：先设口令（一次性），再把手机号换成本轮的新号 ──
      //     患者登录只认 phone+password，而历史轮的明文号读不回（列表/详情两侧都不投该列）。
      const patientPwd = await callOk<{ patientId: string; password: string }>(
        api,
        'POST',
        `${PAGE_ORIGIN}/api/v1/admin/patients/${patient}/password`,
        { token: adminToken!, why: '设患者登录口令（段3 的会话靠它）' },
      )
      expect(patientPwd.patientId, '口令端点应回同一患者').toBe(patient)
      expect(patientPwd.password.length, '患者一次性口令长度应为 16（值不打印）').toBe(16)

      const phone = uniqueTestPhone()
      await callOk<unknown>(api, 'PUT', `/api/v1/admin/patients/${patient}/phone`, {
        token: adminToken!,
        body: { phone, reason: 'T632 e2e 复查链本轮患者会话' },
        why: '给自建患者设本轮登录手机号',
      })

      // 医护口令：走 admin 的重置通道拿明文（09b 段已验过弹层那一腿，这里只需要凭据本身）
      const doctorReset = await callOk<{ doctorId: string; username: string; password: string }>(
        api,
        'POST',
        `/api/v1/admin/doctors/${doctor.doctorId}/reset-password`,
        { token: adminToken!, why: '重置测试医护口令（供医生腿登录）' },
      )
      expect(doctorReset.username, '重置回登录账号应与建档同枚').toBe(doctor.username)
      expect(doctorReset.password.length, '医护一次性口令长度应为 16（值不打印）').toBe(16)

      // ── 3) 医生腿：在域名源登录并进入「复查报告」页 ─────────────
      const doctorCtx = await browser.newContext({ baseURL: PAGE_ORIGIN })
      let doctorPage: Page | null = null
      try {
        doctorPage = await doctorCtx.newPage()
        await realLogin(doctorPage, doctor.username!, doctorReset.password)
        await expect(doctorPage).toHaveURL(/dashboard$/, { timeout: 20_000 })
        await expect(menuItems(doctorPage).first()).toBeVisible({ timeout: 20_000 })
        await gotoMenu(doctorPage, MENU_TITLE)
        await expect(doctorPage).toHaveURL(/review-records$/, { timeout: 15_000 })

        // 角色面自证：本页对 doctor 渲染「仅本团队患者」提示（review-records/index.vue:11）
        await expect(
          doctorPage.locator('.page-toolbar .el-tag'),
          '医生身份进入本页应带「医护工作台：仅本团队患者」标记',
        ).toContainText('医护工作台')

        await pickSelectOption(doctorPage, doctorPage.locator('.patient-select'), fixturePatient.name)
        // 选完患者后上传卡才渲染（整块在 v-if="patientId" 里）
        await expect(doctorPage.locator('.page-card').first()).toBeVisible({ timeout: 15_000 })

        const markerBase = `T632链路上报-${Date.now().toString().slice(-6)}`
        const records: Array<{ shape: Shape; fileId: string; objectKey: string; marker: string }> = []
        const uploadedBytes = new Map<string, Buffer>()
        for (const s of SHAPES) uploadedBytes.set(s.ext, s.magic)

        // 患者行是复用的（无删除端点），历史记录逐轮累积 ⇒ 一律按「基线 + 本轮新增」断，
        // 不能假定列表里只有本轮那 3 条（首轮能过、第二轮起恒红的就是这个坑）。
        const doctorToken0 = await getAuthToken(doctorPage)
        const baseline = await callOk<ReviewRecordDTO[]>(
          doctorPage,
          'GET',
          `${PAGE_ORIGIN}/api/v1/patients/${patient}/review-records`,
          { token: doctorToken0!, why: '读该患者复查记录基线（复用行会带历史轮记录）' },
        )
        const baselineCount = baseline.length
        console.log(`[t632-复查链][基线] patientId=${patient} 已有复查记录 ${baselineCount} 条（本轮另加 ${SHAPES.length} 条）`)

        let corsProbed = false

        for (let i = 0; i < SHAPES.length; i++) {
          const shape = SHAPES[i]
          const marker = `${markerBase}-${shape.ext.replace('.', '').toUpperCase()}`
          const before = await queryReportFiles(api, adminToken!, patient)
          const beforeIds = new Set(before.map((r) => r.file_id))

          // 采集这一腿浏览器真发出的请求：presign 响应里有 signature_url，后面的 CORS 对照要用它
          const presignResponse = doctorPage
            .waitForResponse((r) => r.url().includes('/api/v1/files/presign'), { timeout: 30_000 })
            .catch(() => null)

          await doctorPage.locator('.review-form input').first().fill(reviewDateBack(i + 1))
          await doctorPage.locator('.review-form input').first().press('Enter')
          await doctorPage.locator('.review-form textarea').fill(marker)
          await uploadInput(doctorPage).setInputFiles({
            name: `T632-report${shape.ext}`,
            mimeType: shape.mime,
            buffer: shape.magic,
          })

          await expect(adminMessage(doctorPage), `${shape.ext} 直传应落「文件上传成功」`).toContainText(
            UPLOAD_OK_TOAST,
            { timeout: 30_000 },
          )
          await expect(uploadedTag(doctorPage), `${shape.ext} 上传完应渲染「已上传」标`).toHaveCount(1)

          const presignResp = await presignResponse
          expect(presignResp, `${shape.ext} 未见浏览器发出 presign 请求 ⇒ 直传链没真跑`).not.toBeNull()
          const presignBody = (await presignResp!.json()) as { data?: { signature_url?: string } }
          const signatureUrl = presignBody?.data?.signature_url ?? ''
          assertRealCosHost(signatureUrl, `${shape.ext} 的 presign 上传地址`)

          // ── 桶侧 CORS 的放行面与反证（只在第一枚形状上探一次）──
          if (!corsProbed) {
            corsProbed = true
            const allow = await api.request.fetch(signatureUrl, {
              method: 'OPTIONS',
              headers: { Origin: PAGE_ORIGIN, 'Access-Control-Request-Method': 'PUT' },
            })
            const allowOriginHdr = allow.headers()['access-control-allow-origin'] ?? ''
            const methodsHdr = allow.headers()['access-control-allow-methods'] ?? ''
            expect(
              allow.status(),
              `桶侧对页面源 ${PAGE_ORIGIN} 的预检应放行（直传靠它），实得 HTTP ${allow.status()}`,
            ).toBeLessThan(300)
            expect(
              allowOriginHdr === PAGE_ORIGIN || allowOriginHdr === '*',
              `预检回包应把页面源列进 Allow-Origin，实得「${allowOriginHdr}」`,
            ).toBe(true)
            expect(methodsHdr.toUpperCase()).toContain('PUT')

            const deny = await api.request.fetch(signatureUrl, {
              method: 'OPTIONS',
              headers: { Origin: 'http://t632-negative-control.invalid', 'Access-Control-Request-Method': 'PUT' },
            })
            const denyOriginHdr = deny.headers()['access-control-allow-origin'] ?? ''
            expect(
              denyOriginHdr === PAGE_ORIGIN || denyOriginHdr === '*',
              `CORS 对照腿失效：陌生源也拿到了放行头（Allow-Origin=「${denyOriginHdr}」）⇒ 上面那条「放行面」断言没有牙`,
            ).toBe(false)
            console.log(
              `[t632-复查链][桶侧CORS] 页面源 ${PAGE_ORIGIN} 预检=${allow.status()} Allow-Origin=${allowOriginHdr || '<无>'} Methods=${methodsHdr || '<无>'} | 陌生源预检=${deny.status()} Allow-Origin=${denyOriginHdr || '<无>'}`,
            )
          }

          // ── 值级回读：files 表里新增的正好一行，且状态 uploaded ──
          const after = await queryReportFiles(api, adminToken!, patient)
          const fresh = after.filter((r) => !beforeIds.has(r.file_id))
          expect(
            fresh.length,
            `${shape.ext} 直传后该患者的报告文件行应恰好新增 1 枚，实得 ${fresh.length}（前后集合 ${before.length}→${after.length}）`,
          ).toBe(1)
          const row = fresh[0]
          expect(row.status, `${shape.ext} 的 files 行应为 uploaded（upload-complete 没闭环就是这一格）`).toBe('uploaded')
          expect(row.content_type, `${shape.ext} 落库的 content_type 应与上传一致`).toBe(shape.mime)
          expect(row.owner_type).toBe('patient')
          expect(row.owner_id, `文件行 owner 应是被选中的患者 ${patient}`).toBe(patient)
          expect(
            row.object_key,
            `object_key 应是 patient/<患者号>/<纳秒>_<随机>.<ext> 形状，实得 ${row.object_key}`,
          ).toMatch(new RegExp(`^patient/${patient}/\\d+_[0-9a-f]+${shape.keyExt.replace('.', '\\.')}$`))
          expect(row.uploaded_at, 'uploaded 行的 uploaded_at 应已回填').not.toBeNull()

          // ── 下载腿：预签名 GET 真取回字节，与上传字节逐字节对平（证桶里有货，不只库里说 uploaded）
          const dl = await callOk<{ file_id: string; download_url: string; expires_at: string }>(
            api,
            'GET',
            `${PAGE_ORIGIN}/api/v1/files/${row.file_id}/download`,
            { token: adminToken!, why: `${shape.ext} 已 uploaded，应能签发下载地址` },
          )
          expect(dl.file_id).toBe(row.file_id)
          const dlUrl = assertRealCosHost(dl.download_url, `${shape.ext} 的下载地址`)
          const fetched = await api.request.get(dlUrl.toString())
          expect(fetched.status(), `${shape.ext} 从桶里取回应 200`).toBe(200)
          const got = await fetched.body()
          expect(got.equals(uploadedBytes.get(shape.ext)!), `${shape.ext} 桶里取回的字节应与上传一致`).toBe(true)

          // ── 提交复查记录（一种形状一条，不留「uploaded 却没被记录引用」的孤儿文件行）
          await expect(doctorPage.getByRole('button', { name: '提交复查记录' })).toBeEnabled()
          await doctorPage.getByRole('button', { name: '提交复查记录' }).click()
          await expect(adminMessage(doctorPage), '提交应落「复查记录已提交」').toContainText(SUBMIT_OK_TOAST, {
            timeout: 30_000,
          })

          records.push({ shape, fileId: row.file_id, objectKey: row.object_key, marker })

          // 历史列表按 marker 锚出唯一一行，且报告文件列就是那个 object key
          const recRow = rowsIn(doctorPage).filter({ hasText: marker })
          await expect(recRow, `提交后历史列表应出现「${marker}」那一行`).toHaveCount(1)
          await expect(recRow).toContainText(reviewDateBack(i + 1))
          await expect(recRow).toContainText('复诊')
          await expect(recRow.locator('td').nth(4), '报告文件列应显示 object key').toContainText(row.object_key)
          await expect(recRow.getByRole('button', { name: '下载' })).toHaveCount(1)
        }

        // 计数标题与行数：基线 + 本轮 3 条（复用患者会累积历史轮记录，见上面基线那段）
        await expect(listTitle(doctorPage)).toHaveText(
          `历史复查记录（${baselineCount + records.length}）`,
        )
        await expect(rowsIn(doctorPage)).toHaveCount(baselineCount + records.length)

        // ── 4) 接口面（医生 JWT）与页面面同值，且 reportFileName=object key ──
        const doctorToken = doctorToken0
        const asDoctor = await callOk<ReviewRecordDTO[]>(
          doctorPage,
          'GET',
          `${PAGE_ORIGIN}/api/v1/patients/${patient}/review-records`,
          { token: doctorToken!, why: '医生读本团队患者的复查记录' },
        )
        expect(
          asDoctor.length,
          `接口面记录数应为基线 ${baselineCount} + 本轮 ${records.length}，实得 ${asDoctor.length}`,
        ).toBe(baselineCount + records.length)
        for (const rec of records) {
          const hit = asDoctor.find((x) => x.findings === rec.marker)
          expect(hit, `接口面应能按 marker「${rec.marker}」找到那条记录`).toBeTruthy()
          expect(hit!.reportFileId, '记录的 reportFileId 应是直传新增的那枚文件行').toBe(rec.fileId)
          expect(hit!.reportFileName, 'reportFileName 是 object key（handler/review.go:142）').toBe(rec.objectKey)
          expect(hit!.reviewType).toBe(REVIEW_TYPE)
          expect(hit!.doctorId ?? doctor.doctorId, '记录应归属本用例的测试医护').toBe(doctor.doctorId)
          assertRealCosHost(hit!.reportDownloadUrl ?? '', `${rec.shape.ext} 记录里的下载预签名地址`)
        }

        // ── 5) 段3：患者端 H5 真实渲染 ────────────────────────────
        const login = await callOk<{ token: string; patientId: string; role: string }>(
          api,
          'POST',
          `${H5_ORIGIN}/api/v1/patient/login`,
          { body: { phone, password: patientPwd.password }, why: '患者口令登录失败' },
        )
        expect(login.patientId, '登录回的患者号应是本用例自建患者').toBe(patient)
        expect(login.role).toBe('patient')

        const h5Ctx = await browser.newContext({ viewport: { width: 390, height: 844 } })
        const h5Net: string[] = []
        try {
          const h5 = await h5Ctx.newPage()
          h5.on('request', (req) => {
            const u = new URL(req.url())
            if (u.pathname.startsWith('/api/v1/')) h5Net.push(`${req.method()} ${u.pathname}`)
          })
          await h5.addInitScript(
            ({ tokenKey, pidKey, token, pid }) => {
              window.localStorage.setItem(tokenKey, token)
              window.localStorage.setItem(pidKey, pid)
            },
            {
              tokenKey: LS_PATIENT_TOKEN_KEY,
              pidKey: LS_PATIENT_ID_KEY,
              token: login.token,
              pid: patient,
            },
          )
          await h5.goto(`${H5_ORIGIN}${H5_REPORT_PATH}`, { waitUntil: 'domcontentloaded' })
          await expect(
            h5.locator('.review-card').first(),
            '患者端「复查管理」应渲染出记录卡（渲染不出＝会话没被接住或列表请求跨源被拦）',
          ).toBeVisible({ timeout: 25_000 })
          await expect(h5.locator('.review-card')).toHaveCount(baselineCount + records.length)
          expect(
            h5Net,
            `患者端应真发本人复查记录列表请求，实得 ${JSON.stringify(h5Net)}`,
          ).toContain(`GET /api/v1/patients/${patient}/review-records`)

          for (const rec of records) {
            const card = h5.locator('.review-card').filter({ hasText: rec.marker })
            await expect(card, `患者端应有「${rec.marker}」那一卡`).toHaveCount(1)
            await expect(card.locator('.review-type-tag')).toHaveText('复诊')
            await expect(card.locator('.review-findings')).toContainText(rec.marker)
            // 患者端看到的报告名就是 admin 接口面那个 object key（两面对平，不是各说各话）
            await expect(card.locator('.report-name')).toHaveText(rec.objectKey)
          }

          // 页内下载：点第一张卡的「下载报告」应真对桶发起 GET 且 200。
          // 只断这一腿：uni.openDocument 在 H5 产物里没有对应能力，其后的 toast 不是本用例的判据。
          const cosGet = h5
            .waitForResponse(
              async (r) => {
                const u = new URL(r.url())
                return u.hostname.endsWith('.myqcloud.com') && r.request().method() === 'GET'
              },
              { timeout: 20_000 }
            )
            .catch(() => null)
          await h5.locator('.review-card').first().locator('.download-btn').click()
          const gotResp = await cosGet
          expect(
            gotResp,
            '患者端点「下载报告」后应看到对 COS 的 GET（downloadFile 没发起＝患者端下载腿没跑通）',
          ).not.toBeNull()
          expect(gotResp!.status(), '患者端发起的桶侧 GET 应 200').toBe(200)
          console.log(
            `[t632-复查链][患者端] ${records.length} 卡全部渲染；页内 GET ${new URL(gotResp!.url()).hostname} → ${gotResp!.status()}`,
          )
        } finally {
          await h5Ctx.close()
        }
      } finally {
        if (doctorPage) await adminLogout(doctorPage).catch(() => {})
        await doctorCtx.close()
      }
    } finally {
      await adminCtx.close()
      // api 页面在 adminCtx 里，close 已连带回收；这里只留一行读数便于反查
      console.log(
        `[t632-复查链][靶子] patientId=${patient || '<未建>'} doctorId=${doctorIdForReport || '<未建>'}`,
      )
    }
  })

  /**
   * 28.2 有牙负腿：坏魔数 / 白名单外扩展名必须在发起上传之前就被拦下，且库内零新增。
   * 派发单要求「人为制造上传故障时用例能红」——这条证的是反向：故障输入下用例确实会判红，
   * 而桶/库里不留货（拒绝路径排在 pending 行落库之前，见 handler/file_handler.go:150-175 的判序）。
   */
  test('28.2 上传被拦的两形：假魔数（服务端 60001）与 .txt（前端白名单），且 files 行数不变', async ({
    browser,
  }) => {
    const ctx = await browser.newContext({ baseURL: PAGE_ORIGIN })
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const adminToken = await getAuthToken(page)
      expect(adminToken).toBeTruthy()

      const doctor = await ensureFixtureDoctor(page, adminToken!)
      const fixturePatient = await ensureFixturePatient(page, adminToken!, doctor)
      const before = await queryReportFiles(page, adminToken!, fixturePatient.patientId)

      await gotoMenu(page, MENU_TITLE)
      await expect(page).toHaveURL(/review-records$/, { timeout: 15_000 })
      await pickSelectOption(page, page.locator('.patient-select'), fixturePatient.name)

      // 采集从「形二」之前才开始挂：形一（假魔数）本来就该发 presign（服务端判 60001），
      // 把两形共用一个计数器会把「前端拦截」这条腿读成假红——首跑就是这么撞上的（数组里那 1 发是形一的）。
      const presigned: string[] = []

      // 形一：扩展名合法但魔数不符 ⇒ 服务端权威校验 60001（文案是这一形，码是这一形）
      const badPresign = page
        .waitForResponse((r) => r.url().includes('/api/v1/files/presign'), { timeout: 20_000 })
        .catch(() => null)
      await uploadInput(page).setInputFiles({
        name: 't632-fake.pdf',
        mimeType: 'application/pdf',
        buffer: Buffer.from('this is definitely not a pdf'),
      })
      await expect(adminMessage(page), '假魔数应被服务端判成 60001 的文案').toContainText(INVALID_REQUEST_TOAST, {
        timeout: 20_000,
      })
      await expect(uploadedTag(page), '被拒的这发不该渲染出「已上传」标').toHaveCount(0)
      const badResp = await badPresign
      expect(badResp, '假魔数那发应真的打到 presign 接口（否则文案是被别处渲染的）').not.toBeNull()
      const badBody = (await badResp!.json().catch(() => null)) as { code?: number } | null
      expect(badBody?.code, '假魔数的服务端判码应是 60001').toBe(CODE_INVALID_REQUEST)

      // 形二：白名单外扩展名 ⇒ 前端白名单当场拦（review-report-whitelist.ts），一次接口都不发
      page.on('request', (req) => {
        if (req.method() === 'POST' && req.url().includes('/api/v1/files/presign')) presigned.push(req.url())
      })
      await uploadInput(page).setInputFiles({
        name: 't632-notes.txt',
        mimeType: 'text/plain',
        buffer: Buffer.from('plain text must not reach presign'),
      })
      await expect(adminMessage(page), '.txt 应被前端白名单拦下').toContainText('不支持的文件类型：.txt', {
        timeout: 15_000,
      })
      await expect(uploadedTag(page)).toHaveCount(0)
      expect(presigned, `前端拦截腿失效：真发出了 presign（${presigned.length} 发）`).toEqual([])

      const after = await queryReportFiles(page, adminToken!, fixturePatient.patientId)
      expect(
        after.length,
        `两形被拒后该患者的报告文件行应一条不增（前 ${before.length} 后 ${after.length}）`,
      ).toBe(before.length)

      // 形三（正向判据的反面）：只 presign 不 complete ⇒ 下载端点必须拒绝 61001。
      // 这一发会在 files 表留下 1 枚 pending 行（无删除端点），已在文件头残留清单登记。
      const pending = await callOk<{ file_id: string; status?: string }>(
        page,
        'POST',
        '/api/v1/files/presign',
        {
          token: adminToken!,
          body: {
            file_type: 'review_report',
            owner_type: 'patient',
            owner_id: fixturePatient.patientId,
            content_type: 'application/pdf',
            file_name: 't632-pending.pdf',
            file_header: Buffer.from('%PDF').toString('base64'),
          },
          why: '签一发给 pending 文件用于「未 complete 不可下载」的判据',
        },
      )
      const dl = await callApi(page, 'GET', `/api/v1/files/${pending.file_id}/download`, { token: adminToken! })
      expect(
        dl.code,
        `未 upload-complete 的文件不应签发下载地址，应回 code=${CODE_FILE_NOT_FOUND}，实得 status=${dl.status} code=${dl.code} message=${dl.message}`,
      ).toBe(CODE_FILE_NOT_FOUND)
      console.log(
        `[t632-复查链][残留报备] 形三留下 1 枚 pending files 行 file_id=${pending.file_id}（无删除端点，可撤面已在文件头列明）`,
      )
    } finally {
      await ctx.close()
    }
  })

  /**
   * 28.3 上传故障的可复现形状（派发单「人为制造上传故障时用例能红」这一格的替代做法）。
   *
   * 卡面原写法要在 COS 桶上临时摘掉 CORS 规则再跑，那是共享基建的写操作（不可回滚且影响他人），
   * 按红线不由本席自做。这里改用**等价的本地把手**：把页面源强制换成 IP 入口
   * （http://106.52.39.208:81）——它与域名入口是同一台 staging，但不在桶侧 CORS 的放行面里
   * （实测预检 403），于是 28.1 的直传腿必然红。跑法：
   *   E2E_STAGING_URL=http://106.52.39.208:81 E2E_T632_PAGE_ORIGIN=http://106.52.39.208:81 \
   *     npx playwright test --config=e2e-real/playwright.real.config.ts -g 28.1
   * ⇒ 期望原文：28.1 在「直传应落『文件上传成功』」那一行判红。这条 28.3 自己则把「把手确实在改变
   * 行为」证成一条断言：非放行源发起的预检拿不到 Allow-Origin ⇒ 浏览器必然拦掉 PUT。
   */
  test('28.3 故障把手自证：桶侧 CORS 的放行面只认页面源，IP 源预检拿不到 Allow-Origin', async ({ browser }) => {
    test.info().annotations.push({
      type: 'negative-control',
      description: '证明 28.1 的直传腿依赖真桶的 CORS 放行面：换源即拦，不是常绿装饰',
    })
    const ctx = await browser.newContext({ baseURL: PAGE_ORIGIN })
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const adminToken = await getAuthToken(page)
      expect(adminToken).toBeTruthy()
      const doctor = await ensureFixtureDoctor(page, adminToken!)
      const fixturePatient = await ensureFixturePatient(page, adminToken!, doctor)

      const presign = await callOk<{ file_id: string; signature_url: string }>(
        page,
        'POST',
        '/api/v1/files/presign',
        {
          token: adminToken!,
          body: {
            file_type: 'review_report',
            owner_type: 'patient',
            owner_id: fixturePatient.patientId,
            content_type: 'application/pdf',
            file_name: 't632-cors-probe.pdf',
            file_header: Buffer.from('%PDF').toString('base64'),
          },
          why: '签一枚地址用于 CORS 对照（同 28.3 说明：留 1 枚 pending 行）',
        },
      )
      assertRealCosHost(presign.signature_url, 'CORS 对照用的 presign 地址')

      const ipOrigin = 'http://106.52.39.208:81'
      const probe = await page.request.fetch(presign.signature_url, {
        method: 'OPTIONS',
        headers: { Origin: ipOrigin, 'Access-Control-Request-Method': 'PUT' },
      })
      const allowOrigin = probe.headers()['access-control-allow-origin'] ?? ''
      console.log(
        `[t632-复查链][故障把手] 预检 Origin=${ipOrigin} ⇒ HTTP ${probe.status()} Allow-Origin=${allowOrigin || '<无>'}（页面源=${PAGE_ORIGIN}）`,
      )
      expect(
        allowOrigin === ipOrigin || allowOrigin === '*',
        `把手失效：IP 源也被桶放行 ⇒ 那 28.1 换源跑不会红，这条负对照没有牙（Allow-Origin=「${allowOrigin}」）`,
      ).toBe(false)
    } finally {
      await ctx.close()
    }
  })

  test.afterAll(() => {
    console.log(
      `[t632-复查链][残留报备] 本文件每轮不可撤：files 行 +3(uploaded)+1~2(pending)、review_records 行 +3、COS 对象 +3；` +
        `自建医护/患者行各 1 枚长期复用（patientId=${patientIdForReport || '<本轮未建>'} doctorId=${doctorIdForReport || '<本轮未建>'}）。` +
        `清空这三类需 DBA 侧动作或补删除端点，已按 T632 交件说明报 PM。`,
    )
  })
})
