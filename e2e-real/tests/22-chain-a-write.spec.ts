import { test, expect, type Page, type Locator } from '@playwright/test'
import {
  realLogin,
  gotoMenuAndWaitTable,
  pickSelectOption,
  adminMessage,
  getAuthToken,
  uniqueName,
  E2E_PATIENT_NAME_PREFIX,
  E2E_TEAM_NAME_PREFIX,
} from '../real-helpers'

/**
 * T462 S2/S3 · 链 A 写段（真实模式 / staging，PM 2589 裁的甲案）
 *
 * 链路口径取自 T462 设计稿 §五「链 A」A2-A7 与 §七 S2/S3：
 *   登录 → 自建团队（API，见下方「为什么团队必须先建」）→ 真 UI 建档 → 真 UI 改手机号
 *   → 真 UI 编辑档案 → 真 UI 分配团队 →（22.2）真 UI 解绑微信 → 收尾删自建患者、再删自建团队
 *
 * 为什么这几条要串成一条，而不是各页单测（05/06 已经各自验过自己那页）：
 *   单页用例从不检查「同一次会话里连着写四次，前一次的落库会不会被后一次的请求体覆掉」。
 *   档案编辑是指针语义（nil=不改，admin_patient.go:199+），分配团队又是整体替换 detail.value，
 *   这两条只有按用户操作顺序连起来跑才验得出「改完号再改档案，号没被写回去」。
 *
 * 断言强度：值级 + 双通道。每一笔 UI 写之后，DOM 读到的格子、列表端点、详情端点三处必须
 *   等于我写进去的那个值；改手机号这一笔读侧根本没有 phone 列（患者域读不投影 phone_enc，
 *   apps/admin-web/src/pages/patients/index.vue:249-253 的页面注释同口径），所以它的值级回读
 *   只能落在操作日志的脱敏快照上 —— 那同时也是等保 §9.2 要求的留痕本身，一举两得。
 *
 * 现网预期红点（2026-09-29 第二十四轮 staging 实测，code main 3f16b3f）：22.1 会在第 9 节
 *   「留痕改前应等于建档号的脱敏值」那一格判红，红的是后端缺陷而不是用例写错 —— 患者读路径
 *   没投影 phone_enc，改号留痕的「改前」永远记 absent/空串（定位见 9.1）。断言保持严格，
 *   夜巡这条红就是缺陷信号；卡上已按待裁项回报 PM（甲=保持红等后端修，乙=扩范围补投影）。
 *   两个防「一条红遮住一片覆盖」的处置：改号的留痕回读压到用例末尾（第 6 节末留了说明），
 *   缺陷那三条用 expect.soft —— 软断言照样判红，只是不在第一处就中止，
 *   同一轮能把档案、团队、守恒、反证四格的读数一起拿回来。
 *
 * 甲案的清场纪律（快照-读数-还原-报备，四条都是断言不是承诺）：
 *   快照  写之前取 patients total 与团队字典；
 *   读数  每笔写后按 keyword 精确查回那一行；
 *   还原  afterAll 独立于用例结果，先删患者（T467 DELETE）再删团队（DELETE /teams/:teamId）；
 *   报备  还原失败就把患者号/团队名打进 stdout（前缀 [t462-s2s3][残留报备]）并把异常抛出去。
 *   删除顺序不许倒：DeleteTeam 见到 patients.team_id 有引用就 409（pg.go:1623-1647）。
 *
 * 生产零写：本文件只在 staging 入口跑。入口只读 E2E_STAGING_URL，命中生产域名/生产 IP 直接抛。
 *
 * 不动既有面：不改 real-helpers.ts、不改 playwright.real.config.ts、不动 05/06 那 16 条单页回归
 *   的断言语义（本卡对 05 只做两处「路径修正 + 停跑放开」，写在 05 文件内并留了出处）。
 *   callApi/callOk 有意在文件内复制 21 号用例的写法而没有提进 real-helpers：
 *   提出去要改 helper 文件，而 21 已交件、它的信封口径已经被 CI 绿锁住，不为本卡动它。
 */

/** staging 入口（与 e2e-real/playwright.real.config.ts 的 baseURL 同一个变量） */
const ENTRY = process.env.E2E_STAGING_URL ?? 'http://localhost:2080'
if (/api\.hbksd\.com\.cn|49\.235\.137\.217/.test(ENTRY)) {
  throw new Error(`T462 链 A 写段命中生产入口，红线拒绝：${ENTRY}`)
}

/**
 * newPatientID 的发号形状（repo/pg.go:1262-1268）：P + 四位年 + 12 位 hex。
 * 与 21 号用例同一条判据；首版把 hex 当纯数字写成了 /^P\d{16}$/，恒红，别再犯。
 */
const PATIENT_ID_RE = /^P\d{4}[0-9a-f]{12}$/

/** 按实测形状拼出的「不存在」患者号 / 团队号，只用来探删除端点在不在架 */
const GHOST_PATIENT_ID = `P${'0'.repeat(16)}`
const GHOST_TEAM_ID = 'TEAM-T462-NOT-EXIST'
if (!PATIENT_ID_RE.test(GHOST_PATIENT_ID)) {
  throw new Error(`T462 链 A 写段探针患者号形状不符发号规则，打的不是真实形状：${GHOST_PATIENT_ID}`)
}
/** 全仓未注册的路径：负对照用（缺了它，「JSON code=10404 ⇒ 路由在架」分不清路由在不在） */
const NOT_REGISTERED_PATH = '/api/v1/zzz-t462-chain-a-write-not-registered-4c8e'

/**
 * 后端脱敏口径的复刻（phone/phone.go:120-123，11 位取前 3 + **** + 后 4）。
 * 值级比较必须用和写侧一样的函数，否则比的是两套逻辑。
 */
const maskPhone = (p: string): string => `${p.slice(0, 3)}****${p.slice(-4)}`

/** 患者列表行 / 详情（model.AdminPatientDTO，列表与详情共用 pg.go:patientSelect 投影） */
interface PatientRow {
  patientId: string
  name: string
  gender: string | null
  age: number | null
  diagnosis: string | null
  cobbAngle: number | null
  deviceId: string | null
  teamId: string | null
  doctorId: string | null
  teamName: string | null
  doctorName: string | null
  createdAt: string
}

interface TeamRow {
  teamId: string
  name: string
  memberCount: number
  patientCount: number
}

interface AuditRow {
  logId: string
  action: string
  targetType: string
  targetId: string
  description: string
  /** 后端是 json.RawMessage（model/t252.go:100）⇒ 到前端就是结构化对象，不是字符串 */
  detail: Record<string, unknown> | null
}

interface Envelope {
  status: number
  contentType: string
  /** null = 响应体不是 JSON（gin 自己的 404 就是这一形，正是负对照要的） */
  code: number | null
  message: string
  data: unknown
}

/**
 * node 侧接口调用（绝对/相对地址都收，相对地址走 config 的 baseURL）。
 *
 * 这些请求走 APIRequestContext，不经页面帧，所以**不该**出现在 page.on('request') 里。
 * 这条前提以前没人证过（20/21 都只做 toContain，没有排除式断言），本用例末尾用一条
 * 通道分离断言把它钉死 —— 它同时也是「UI 写了哪几笔」那条精确集合断言成立的前提。
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

/** 成功信封断言：code 必须是 0 —— 「不看 HTTP 200 就算过」是这条链的红线 */
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
 * 写能力前置探针（在任何业务写之前，只读＋两条打不存在的号）。
 *
 * 三条判据，缺一不可：
 *  ① 未注册路径的 POST/PUT/DELETE 必须回非 JSON（负对照）；
 *  ② T467 患者删除端点对不存在的患者号回 JSON code=10404；
 *  ③ DELETE /teams/:teamId 对不存在的团队号回 JSON code=10404。
 * ②③ 是甲案「跑完能清场」的全部依据：任一不成立就 throw，绝不 skip ——
 * skip 会让「链 A 写段其实一行都没清」在夜巡里报成绿，staging 就此攒脏数据。
 */
async function preflightWrites(page: Page, token: string): Promise<void> {
  for (const method of ['POST', 'PUT', 'DELETE'] as const) {
    const r = await callApi(page, method, NOT_REGISTERED_PATH, { token })
    if (r.code !== null) {
      throw new Error(
        `S2 前置探针失效：未注册路径 ${method} ${NOT_REGISTERED_PATH} 应回非 JSON 的 gin 404，实得 status=${r.status} code=${r.code} ct=${r.contentType} —— 此时「JSON code=10404 ⇒ 路由在架」不成立，不能判删除端点`,
      )
    }
  }
  const delPatient = await callApi(page, 'DELETE', `/api/v1/admin/patients/${GHOST_PATIENT_ID}`, {
    token,
  })
  const delTeam = await callApi(page, 'DELETE', `/api/v1/teams/${GHOST_TEAM_ID}`, { token })
  console.log(
    `[t462-s2s3][代次锚] 未注册路径=非JSON404(3 法) | T467 DELETE /admin/patients status=${delPatient.status} code=${delPatient.code} ct=${delPatient.contentType} | DELETE /teams status=${delTeam.status} code=${delTeam.code} ct=${delTeam.contentType}`,
  )
  if (delPatient.code !== 10404) {
    throw new Error(
      `staging 上 T467 患者删除端点不可用（期望 JSON code=10404「患者不存在」，实得 status=${delPatient.status} code=${delPatient.code} message=${delPatient.message}）。没有它甲案就还原不了自建患者，本用例不许在 staging 留行`,
    )
  }
  if (delTeam.code !== 10404) {
    throw new Error(
      `staging 上团队删除端点不可用（期望 JSON code=10404，实得 status=${delTeam.status} code=${delTeam.code} message=${delTeam.message}）。链 A 写段的自建团队删不掉就别建`,
    )
  }
}

/**
 * 11 位、1 开头的测试号（建档按 phone_hash 查重，pg.go:1275-1284）。
 * 不许写死：13900000001 是 T041 播种技师账号的手机号（e2e/tech-helpers.ts:12），复用会撞查重
 * 或与技师链互踩。真撞上就是 10409，建档那步的 why 里已写明怎么辨。
 */
function uniqueTestPhone(): string {
  return `135${String(Math.floor(Math.random() * 1e8)).padStart(8, '0')}`
}

/** 患者列表卡（本页自 T289 起有第二张会出数的表，作用域必须收到这张卡，同 05/20 口径） */
function patientCard(page: Page): Locator {
  return page.locator('.patient-list-card')
}

/** 表头文案 → 列下标（绝不按写死序号取列） */
async function headerIndex(scope: Locator, title: string): Promise<number> {
  const texts = await scope
    .locator('.el-table__header-wrapper thead th')
    .evaluateAll((ths) => ths.map((th) => (th.textContent ?? '').trim()))
  const idx = texts.indexOf(title)
  expect(idx, `表头应含「${title}」列，实得 ${texts.join('|')}`).toBeGreaterThan(-1)
  return idx
}

/** 一行的各单元格文本（逐格取，整行拼接后无法定位具体列） */
async function cellTexts(row: Locator): Promise<string[]> {
  return row.evaluate((tr) =>
    Array.from(tr.querySelectorAll('td')).map((td) => (td.textContent ?? '').trim()),
  )
}

/** 打开中的弹窗（EP 关掉后节点仍在 DOM 里，故一律加 :visible 限定，否则 toBeVisible 会挑到已关的那个） */
function openDialog(page: Page, title: RegExp): Locator {
  return page.locator('.el-dialog:visible').filter({ hasText: title }).first()
}

/** 弹窗内按 form-item 文案锚出那一项（避免抓到别的字段或下拉的内层 input） */
function formItem(scope: Locator, label: string): Locator {
  return scope.locator('.el-form-item').filter({ hasText: label }).first()
}

/**
 * 等某笔写落进操作日志，并把那一整行交回。
 *
 * 为什么手写轮询而不是 expect.poll：中间件 auditTrail() 在 c.Next() **之后**才写库
 * （audit_t252.go:196-207），客户端拿到 200 时那一行还没落，一次读必然读不到；
 * 而 expect.poll 只给真值、不给值，我要的是 detail 里的改前/改后快照。
 * 超时抛错而不是放过：审计写失败在后端只打 WARN、不影响响应（audit_t252.go h.audit），
 * 「响应 200 但没留痕」正是要抓的那一条，等不到就必须判红。
 *
 * 返回整行而不是只返 detail：22.2 还要判 description 里有没有写明患者号，
 * 那一列在 AuditRow 上、不在 detail 里（只返 detail 就等于把那条判据悄悄删掉）。
 */
async function pollAuditRow(
  page: Page,
  token: string,
  patientId: string,
  changedKey: string,
  why: string,
): Promise<AuditRow> {
  const deadline = Date.now() + 20_000
  let lastRows = -1
  for (;;) {
    const data = await callOk<{ list: AuditRow[] }>(
      page,
      'GET',
      `/api/v1/admin/audit-logs?page=1&pageSize=20&targetType=patient&targetId=${encodeURIComponent(
        patientId,
      )}&action=data_modify`,
      { token, why: '读患者域操作日志（留痕回读）' },
    )
    const rows = data.list ?? []
    lastRows = rows.length
    const hit = rows.find((r) => {
      const changed = r.detail?.changed
      return Array.isArray(changed) && changed.includes(changedKey)
    })
    if (hit?.detail) return hit
    if (Date.now() > deadline) {
      throw new Error(
        `${why}：20s 内未等到 detail.changed 含「${changedKey}」的留痕行（末次按 targetType=patient&targetId=${patientId}&action=data_modify 读到 ${lastRows} 行）`,
      )
    }
    await new Promise((r) => setTimeout(r, 800))
  }
}

/** detail.changed 的可读化（后端是 []string，JSON 里是数组；不是数组就直接判红，别静默比空） */
function changedKeysOf(detail: Record<string, unknown>): string[] {
  const changed = detail.changed
  expect(
    Array.isArray(changed),
    `detail.changed 应为字符串数组，实得 ${JSON.stringify(changed)}（留痕结构变了，本用例判据需同步）`,
  ).toBe(true)
  return changed as string[]
}

test.describe('22-链 A 写段（T462 S2/S3，甲案）', () => {
  /** 本 describe 建过的行：afterAll 按「先患者、后团队」的顺序还原，不看用例是否通过 */
  const createdPatientIds: string[] = []
  const createdTeams: Array<{ teamId: string; name: string }> = []
  /** 写段开始前的 patients total，还原后必须回到这个值 */
  let baselineTotal: number | null = null

  test('22.1 建档→改号→改档案→分配团队：四笔真 UI 写逐笔值级回读，改号留痕脱敏对上', async ({
    page,
  }) => {
    /** 浏览器（admin-web 真 UI）发出的 /api/v1 请求，有序。node 侧 callApi 不进这里。 */
    const net: string[] = []
    page.on('request', (req) => {
      let path: string
      try {
        path = new URL(req.url()).pathname
      } catch {
        return
      }
      if (path.startsWith('/api/v1/')) net.push(`${req.method()} ${path}`)
    })

    // ── 1) 登录 + 写能力探针 + 快照 ────────────────────────────
    await realLogin(page)
    await expect(page).toHaveURL(/\/dashboard/, { timeout: 20_000 })
    const token = await getAuthToken(page)
    expect(token, '真实模式登录应把 JWT 写进 localStorage').toBeTruthy()

    await preflightWrites(page, token!)

    const before = await callOk<{ total: number }>(
      page,
      'GET',
      '/api/v1/admin/patients?page=1&pageSize=10',
      { token: token!, why: '读患者总数基线（快照）' },
    )
    baselineTotal = before.total

    // ── 2) 自建团队（走 API，且必须早于打开患者页）────────────
    // 为什么不能等患者页打开后再建：patients/index.vue:721+ 的 onMounted 里 fetchTeams() 取的是
    // 挂载那一刻的快照，分配团队弹窗与「所属团队」兜底都读这份 teams 字典 ⇒ 后建的本队根本
    // 不在下拉里。写段要选自己建的队（不动 seed 队，PM 口径），就只能先建队、后进页。
    const teamsBefore = await callOk<TeamRow[]>(page, 'GET', '/api/v1/teams', {
      token: token!,
      why: '读团队字典基线（快照）',
    })
    const doctorsRaw = await callOk<unknown>(page, 'GET', '/api/v1/doctors', {
      token: token!,
      why: '读医护字典（建团队必填负责人，createTeam 对不存在的 doctor_id 判 400）',
    })
    const doctors = (Array.isArray(doctorsRaw)
      ? doctorsRaw
      : ((doctorsRaw as { list?: unknown[] })?.list ?? [])) as Array<{ doctorId: string; name: string }>
    expect(doctors.length, 'staging 应至少有一名医护可当团队负责人').toBeGreaterThanOrEqual(1)
    const leaderId = doctors[0].doctorId

    const teamName = uniqueName(E2E_TEAM_NAME_PREFIX)
    const team = await callOk<{ teamId: string; name: string }>(page, 'POST', '/api/v1/teams', {
      token: token!,
      body: { name: teamName, leader: leaderId },
      why: `建自建团队失败（message 含 already exist 即 uniqueName 的秒级后缀撞了，重跑一轮）：${teamName}`,
    })
    expect(team.teamId, '建团队应回新团队号').toBeTruthy()
    expect(team.name, '建团队应回同一队名').toBe(teamName)
    createdTeams.push({ teamId: team.teamId, name: teamName })

    // 自建队必须从「零患者」起算，否则后面 patientCount===1 那条没有牙
    const teamRowAfterCreate = (await callOk<TeamRow[]>(page, 'GET', '/api/v1/teams', {
      token: token!,
      why: '读回自建团队（建后基线）',
    })).find((t) => t.teamId === team.teamId)
    expect(teamRowAfterCreate, `团队字典应能读到自建团队 ${team.teamId}`).toBeTruthy()
    expect(teamRowAfterCreate!.patientCount, '自建团队初始管理患者数应为 0').toBe(0)
    console.log(`[t462-s2s3][造数] teamId=${team.teamId} teamName=${teamName} leader=${leaderId}`)

    // ── 3) 进患者管理页 ────────────────────────────────────────
    await gotoMenuAndWaitTable(page, '患者管理', 'patients', patientCard(page))

    // ── 4) A2 真 UI 建档 ───────────────────────────────────────
    const patientName = uniqueName(E2E_PATIENT_NAME_PREFIX)
    const phoneA = uniqueTestPhone()
    await page
      .locator('.page-toolbar')
      .getByRole('button', { name: '添加患者' })
      .first()
      .click()
    const createDialog = openDialog(page, /新建患者/)
    await expect(createDialog, '点「添加患者」应开标题为「新建患者」的弹窗').toBeVisible({
      timeout: 10_000,
    })
    await formItem(createDialog, '姓名').locator('input').first().fill(patientName)
    await formItem(createDialog, '手机号').locator('input').first().fill(phoneA)
    await formItem(createDialog, '年龄').locator('input').first().fill('14')
    await formItem(createDialog, '诊断').locator('input').first().fill('胸段侧弯')
    await formItem(createDialog, 'Cobb角').locator('input').first().fill('25')
    const femaleRadio = formItem(createDialog, '性别').locator('.el-radio').filter({ hasText: '女' })
    await expect(femaleRadio, '性别应有「女」这一项').toHaveCount(1)
    await femaleRadio.click()
    await expect(femaleRadio.locator('input')).toBeChecked()
    // 团队/医生两项刻意留空：分配团队是被测的第五步，建档时就带上队等于没串
    await createDialog.getByRole('button', { name: '确定' }).first().click()
    // 文案按准确值等：登录那句「欢迎，运营小张」也是 .el-message，只等「出现任意一条」会误判（06 的 6.2 同律）
    await expect(adminMessage(page), '建档应回「创建成功」').toHaveText('创建成功', { timeout: 15_000 })
    await expect(createDialog).toBeHidden({ timeout: 5_000 })

    // 读数（列表端点，患者号从这里拿）
    const searched = await callOk<{ list: PatientRow[]; total: number }>(
      page,
      'GET',
      `/api/v1/admin/patients?page=1&pageSize=10&keyword=${encodeURIComponent(patientName)}`,
      { token: token!, why: '按唯一姓名查回自建患者' },
    )
    const hits = searched.list.filter((p) => p.name === patientName)
    expect(hits.length, `同名患者应只有 1 行（实得 ${hits.length}，说明历史残留未清或命名撞了）`).toBe(1)
    const pid = hits[0].patientId
    expect(pid, '建档应回符合发号形状的患者号').toMatch(PATIENT_ID_RE)
    createdPatientIds.push(pid)
    console.log(`[t462-s2s3][造数] patientId=${pid} name=${patientName}`)

    // 快照-读数守恒（甲案的「快照」那一腿）：多出来的就是我这 1 行。
    // 🔴 必须另读一次「无 keyword」的列表：ListPatients 的 total 是**筛选后**的行数
    // （handler.go:773-798 把 keyword 一起交给 store，COUNT 与 SELECT 同一条 WHERE），
    // 拿上面那次关键词查的 total 比基线，等于拿「1」比「基线 +1」，恒红。
    const totalAfterCreate = await callOk<{ total: number }>(
      page,
      'GET',
      '/api/v1/admin/patients?page=1&pageSize=10',
      { token: token!, why: '建档后读患者总数（无关键词，与基线同口径）' },
    )
    expect(
      totalAfterCreate.total,
      `建档后 total 应等于基线 +1（基线 ${baselineTotal}，实得 ${totalAfterCreate.total}）`,
    ).toBe(baselineTotal! + 1)
    // 建档落库值（含两项留空）
    expect(hits[0].gender, '建档性别应落 female').toBe('female')
    expect(hits[0].age, '建档年龄应落 14').toBe(14)
    expect(hits[0].diagnosis, '建档诊断应落「胸段侧弯」').toBe('胸段侧弯')
    expect(hits[0].cobbAngle, '建档 Cobb 角应落 25').toBe(25)
    expect(hits[0].teamId, '建档时刻不该带团队（团队是第五步）').toBeNull()
    expect(hits[0].deviceId, '新建患者无设备').toBeNull()

    // ── 4.5) 正向对照：建档那一步确实把手机号落库了 ──────────────
    // 下面要断言「改号留痕的改前 = 建档号的脱敏值」。现网若回 absent/空串，有两种可能：
    //   (甲) 库里根本没有手机号 ⇒ absent 是对的，是我的用例判错；
    //   (乙) 库里有号、读侧没投影 ⇒ 后端缺陷。
    // 两种情形在留痕里长得一模一样，留痕自证不了，所以先把 (甲) 排掉再谈 (乙)。
    // 判据用查重：同 phone_hash 再建一条必须 409 —— handler.go:2327-2331 把
    // repo.ErrPatientExists 映射成 409（model.go:23 CodeConflict=10409），而 ErrPatientExists
    // 只可能来自 phone_hash 命中（pg.go:1286-1294 的 PhoneHashTaken 预检，另有 23505 并发兜底
    // :1309-1314）；phone_enc 与 phone_hash 是同一条 INSERT 的两列（pg.go:1301-1305，取值都来自
    // 同一次 preparePhone:1062），⇒ 查重命中即证密文也在库里。
    const dupName = uniqueName(E2E_PATIENT_NAME_PREFIX)
    const dupProbe = await callApi(page, 'POST', '/api/v1/admin/patients', {
      token: token!,
      body: { name: dupName, phone: phoneA },
    })
    if (dupProbe.code === 0) {
      // (甲) 成立：建档压根没把手机号写进去 ⇒ 缺陷在写侧，改号留痕那一格的前提不成立。
      // 这一行是真建出来了，先进清理队列再抛，否则给 staging 留脏数据。
      const extra = (dupProbe.data as { patientId?: string } | null)?.patientId
      if (extra) createdPatientIds.push(extra)
      throw new Error(
        `[t462-s2s3][正向对照] 同 phone_hash 重复建档回了 code=0（应 409/code=10409）` +
          `⇒ 建档没把手机号落库，「改前 absent」是用例判错而非读侧缺陷，先修写侧这条：pid=${extra ?? dupName}`,
      )
    }
    expect(
      dupProbe.code,
      `[t462-s2s3][正向对照] 同键建档应查重命中 10409（以此证明库里确有建档号的哈希与密文），` +
        `实得 status=${dupProbe.status} code=${dupProbe.code} message=${dupProbe.message}`,
    ).toBe(10409)
    console.log(
      `[t462-s2s3][正向对照] 同键查重 status=${dupProbe.status} code=10409 ⇒ 建档手机号已落库，` +
        `改号留痕的「改前」若仍回 absent 即读侧投影缺陷`,
    )

    // DOM 侧同值（页面此刻仍是无关键词的首屏，列表按 created_at DESC ⇒ 新行在第 1 页）
    const card = patientCard(page)
    const nameIdx = await headerIndex(card, '姓名')
    const pidIdx = await headerIndex(card, '患者ID')
    const genderIdx = await headerIndex(card, '性别')
    const ageIdx = await headerIndex(card, '年龄')
    const diagIdx = await headerIndex(card, '诊断')
    const cobbIdx = await headerIndex(card, 'Cobb角')
    const teamIdx = await headerIndex(card, '绑定团队')

    // ── 5) 真 UI 按姓名搜索 → 打开详情抽屉 ─────────────────────
    const search = page.locator('.search-input input')
    await expect(search).toBeVisible({ timeout: 8_000 })
    await search.fill(patientName)
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).first().click()
    const targetRow = card.locator('.el-table__body-wrapper tbody tr').filter({ hasText: pid }).first()
    await expect
      .poll(async () => (await targetRow.count()) > 0, {
        timeout: 20_000,
        message: '搜索后 DOM 里应出现自建患者那一行',
      })
      .toBe(true)
    const createdCells = await cellTexts(targetRow)
    expect(createdCells[pidIdx], '命中行的患者ID列应等于建档患者号').toBe(pid)
    expect(createdCells[nameIdx], '建档后列表「姓名」格应等于建档值').toBe(patientName)
    expect(createdCells[genderIdx], '建档后列表「性别」格应显示「女」').toBe('女')
    expect(createdCells[ageIdx], '建档后列表「年龄」格').toBe('14')
    expect(createdCells[diagIdx], '建档后列表「诊断」格').toBe('胸段侧弯')
    expect(createdCells[cobbIdx], '建档后列表「Cobb角」格（前端加 ° 的渲染口径）').toBe('25°')
    expect(createdCells[teamIdx], '建档后列表「绑定团队」格应为未分配占位').toBe('-')

    await targetRow.locator('td').nth(nameIdx).click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible({ timeout: 8_000 })
    await expect(drawer.locator('.el-drawer__title')).toContainText(patientName, { timeout: 5_000 })
    await expect(drawer.locator('.el-drawer__title')).toContainText(pid)
    await expect(drawer.locator('.pid-card .pid-value')).toHaveText(pid)

    // ── 6) A3 真 UI 改手机号 → 操作日志脱敏快照回读 ────────────
    const phoneB = uniqueTestPhone()
    const reason = 'T462 链 A 写段改号留痕核对'
    await drawer
      .locator('.drawer-actions')
      .getByRole('button', { name: '改手机号' })
      .click()
    const phoneDialog = openDialog(page, /修改手机号/)
    await expect(phoneDialog, '抽屉「改手机号」应开标题为「修改手机号」的弹窗').toBeVisible({
      timeout: 10_000,
    })
    await formItem(phoneDialog, '新手机号').locator('input').first().fill(phoneB)
    await formItem(phoneDialog, '变更原因').locator('textarea').first().fill(reason)
    await phoneDialog.getByRole('button', { name: '确定' }).first().click()
    await expect(adminMessage(page), '改号应回「手机号已更新」').toHaveText('手机号已更新', {
      timeout: 15_000,
    })
    await expect(phoneDialog).toBeHidden({ timeout: 5_000 })
    // 改号那笔留痕的值级回读压到第 9 节：这一格在现网是缺陷红点（依据见 9.1 的注释），
    // 留在原位会让它遮住后面的档案、团队、守恒三格 —— 挪到末尾后一轮 run 仍照判红，
    // 但同一轮能把其余三格的读数一起拿回来。

    // ── 7) A4 真 UI 编辑档案（只改姓名与年龄）─────────────────
    const renamed = `${patientName}-改`
    await drawer.locator('.drawer-actions').getByRole('button', { name: '编辑档案' }).click()
    const editDialog = openDialog(page, /编辑档案/)
    await expect(editDialog, '抽屉「编辑档案」应开标题为「编辑档案」的弹窗').toBeVisible({
      timeout: 10_000,
    })
    // 弹窗把原值预填进输入框（openEditProfile 取 detail 快照），这里逐格改成新值
    await formItem(editDialog, '姓名').locator('input').first().fill(renamed)
    await formItem(editDialog, '年龄').locator('input').first().fill('16')
    const saveBtn = editDialog.getByRole('button', { name: '保存' }).first()
    // 有改动 ⇒ 保存按钮必须解禁（canSaveProfile 靠 editPatch 非 null，index.vue:589）
    await expect(saveBtn, '改了两格后「保存」不应置灰').toBeEnabled()
    await saveBtn.click()
    await expect(adminMessage(page), '档案编辑应回「档案已保存」').toHaveText('档案已保存', {
      timeout: 15_000,
    })
    await expect(editDialog).toBeHidden({ timeout: 5_000 })

    // 三通道对平：详情端点 == 列表端点 == DOM == 我写进去的值
    const detail = await callOk<PatientRow>(
      page,
      'GET',
      `/api/v1/admin/patients/${encodeURIComponent(pid)}`,
      { token: token!, why: '档案编辑后读详情' },
    )
    expect(detail.name, '详情端点应读到新姓名').toBe(renamed)
    expect(detail.age, '详情端点应读到新年龄').toBe(16)
    // 「只发改过的键」的反证：没动的三项必须仍是建档值（被空值清掉就是这条链要抓的缺陷）
    expect(detail.gender, '性别未提交，不得被改动').toBe('female')
    expect(detail.diagnosis, '诊断未提交，不得被清成空串或 null').toBe('胸段侧弯')
    expect(detail.cobbAngle, 'Cobb 角未提交，不得被清成 0').toBe(25)

    const listAfterEdit = await callOk<{ list: PatientRow[] }>(
      page,
      'GET',
      `/api/v1/admin/patients?page=1&pageSize=10&keyword=${encodeURIComponent(patientName)}`,
      { token: token!, why: '改名后仍按原关键词查（新名保留前缀，页面搜索框里的词没变）' },
    )
    const editedRow = listAfterEdit.list.find((p) => p.patientId === pid)
    expect(editedRow, '列表端点应仍含该患者').toBeTruthy()
    expect(editedRow!.name, '列表端点的姓名应等于详情端点的姓名').toBe(detail.name)
    expect(editedRow!.age, '列表端点的年龄应等于详情端点的年龄').toBe(detail.age)

    const editedCells = await cellTexts(targetRow)
    expect(editedCells[nameIdx], '列表「姓名」格应回显新姓名').toBe(renamed)
    expect(editedCells[ageIdx], '列表「年龄」格应回显新年龄').toBe('16')
    expect(editedCells[diagIdx], '列表「诊断」格应保持建档值').toBe('胸段侧弯')
    expect(editedCells[cobbIdx], '列表「Cobb角」格应保持建档值').toBe('25°')

    // 改号 + 改档案两次写叠在同一行上：档案留痕只应记它改过的两格
    const profileRow = await pollAuditRow(page, token!, pid, 'name', '档案编辑未落操作日志')
    const profileDetail = profileRow.detail!
    expect(changedKeysOf(profileDetail), '档案留痕只应记 name 与 age（没动的三项不得进快照）').toEqual([
      'name',
      'age',
    ])
    const pBefore = profileDetail.before as Record<string, unknown>
    const pAfter = profileDetail.after as Record<string, unknown>
    expect(pBefore.name, '档案留痕改前应等于建档姓名').toBe(patientName)
    expect(pAfter.name, '档案留痕改后应等于新姓名').toBe(renamed)
    expect(pBefore.age, '档案留痕改前年龄').toBe(14)
    expect(pAfter.age, '档案留痕改后年龄').toBe(16)
    // 「只提交改过的键」在文案通道上的读数：后端按 len(changed) 写「N 个字段变更」
    // （admin_patient.go:275-288，auditProfileDiff 的顺序是 name、gender、age、...，
    // 与 detail.changed 同一份切片）。这里判红时先看数字是否为 2 再看分隔符，别改断言。
    expect(
      profileRow.description,
      `档案留痕应写明两处变更与患者号，实得 ${profileRow.description}`,
    ).toContain(pid)
    expect(profileRow.description, '档案留痕的变更字段数应为 2（只改了姓名与年龄）').toContain('2 个字段变更')

    // ── 8) A5 真 UI 分配团队（目标＝自建队）────────────────────
    await drawer.locator('.drawer-actions').getByRole('button', { name: '分配团队' }).click()
    const assignDialog = openDialog(page, /分配团队/)
    await expect(assignDialog, '抽屉「分配团队」应开标题为「分配团队」的弹窗').toBeVisible({
      timeout: 10_000,
    })
    const teamSelect = assignDialog.locator('.el-select').first()
    await expect(teamSelect, '分配团队弹窗应有「目标团队」下拉').toBeVisible()
    await pickSelectOption(page, teamSelect, teamName)
    await assignDialog.getByRole('button', { name: '确定' }).first().click()
    await expect(adminMessage(page), '分配团队应回「分配成功」').toHaveText('分配成功', {
      timeout: 15_000,
    })
    await expect(assignDialog).toBeHidden({ timeout: 5_000 })

    const detailAfterAssign = await callOk<PatientRow>(
      page,
      'GET',
      `/api/v1/admin/patients/${encodeURIComponent(pid)}`,
      { token: token!, why: '分配团队后读详情' },
    )
    expect(detailAfterAssign.teamId, '详情端点应读到自建团队号').toBe(team.teamId)
    expect(detailAfterAssign.teamName, '详情端点 join 出的团队名应等于自建队名').toBe(teamName)
    // 串联不回退：分配团队整段替换 detail（index.vue:516），前面两笔写的值必须还在
    expect(detailAfterAssign.name, '分配团队不得改回旧姓名').toBe(renamed)
    expect(detailAfterAssign.diagnosis, '分配团队不得清掉诊断').toBe('胸段侧弯')

    // 列表那一格改走 locator 自动重等（T511 格 22.1）。旧写法在这里一次性读 DOM（cellTexts 的一行快照），
    // 而分配成功后页面自己重枪列表那一次是 fire-and-forget（apps/admin-web/src/pages/patients/index.vue:519
    // 的 loadData() 没有 await）⇒ 读到过期值就判红。CI run 36685175924 实证形状：
    // Expected "T053团队-082277" / Received "-"，而紧邻其上的详情端点已读到自建团队（650 行之前的两条先过）。
    // 这不是无条件重试：判据值与两条断言一字未动，到点仍不是队名就红；变的只是「等谁把 DOM 更新到位」。
    const assignedTeamCell = targetRow.locator('td').nth(teamIdx)
    await expect(assignedTeamCell, '列表「绑定团队」格应显示自建队名，而不是团队编号').toHaveText(teamName)
    await expect(assignedTeamCell, '团队格不得回落成原始编号').not.toHaveText(team.teamId)

    // 抽屉那一格走的是挂载时的 teams 字典兜底（写响应不带 teamName）⇒ 这正是「团队必须先建」的依据
    const drawerTeamCell = await drawer
      .locator('.el-descriptions')
      .evaluate((el) => {
        const label = Array.from(el.querySelectorAll('.el-descriptions__label')).find(
          (th) => (th.textContent ?? '').trim() === '所属团队',
        )
        return (label?.nextElementSibling?.textContent ?? '').trim()
      })
    expect(drawerTeamCell, '抽屉「所属团队」应与列表同一值（两个读端点同源）').toBe(teamName)

    // 团队侧的反向账：患者数从 0 变 1（teamPatientCountExpr 实时计数，pg.go:1515-1519）
    const teamsAfterAssign = await callOk<TeamRow[]>(page, 'GET', '/api/v1/teams', {
      token: token!,
      why: '分配团队后读团队字典（患者数守恒）',
    })
    const myTeamAfter = teamsAfterAssign.find((t) => t.teamId === team.teamId)
    expect(myTeamAfter, '团队字典应仍含自建团队').toBeTruthy()
    expect(myTeamAfter!.patientCount, '自建团队的「管理患者数」应为 1').toBe(1)
    // 别的队不该被这一笔动到（尤其 seed 队）
    for (const t of teamsBefore) {
      if (t.teamId === team.teamId) continue
      const now = teamsAfterAssign.find((x) => x.teamId === t.teamId)
      expect(
        now?.patientCount,
        `团队 ${t.teamId} 的患者数被链 A 写段动了（${t.patientCount} → ${now?.patientCount}）`,
      ).toBe(t.patientCount)
    }

    // ── 9) A3 改号留痕的值级回读（第 6 节末尾说明为什么要压到最后）──
    // 患者域读侧没有 phone 列（index.vue:249-253 页面注释同口径：读不投影 phone_enc），
    // 唯一不留脏数据的改后读数就是操作日志里的改前/改后脱敏快照 —— 它同时是等保 §9.2 的留痕。
    //
    // 9.1 🔴 before 那两条在现网会判红，红的是后端缺陷本身。不许在这里加「已知缺陷就放过」
    //     的分支（加了等于把真缺陷静音，夜巡就此永久失去这一格）。缺陷定位（code main 3f16b3f 实测）：
    //       patientSelect 没投影 p.phone_enc（pg.go:382-391），scanPatient 也不扫 PhoneEnc（:415-426）
    //       ⇒ GetPatient 回行的 PhoneEnc 恒为空 ⇒ admin_patient.go:158 的
    //         before := h.phoneView(patient.PhoneEnc) 必落 phone/phone.go:105-108 的 len(enc)==0 分支
    //       ⇒ 改前永远记 absent/空串，哪怕库里确有可解开的密文。
    //     「库里真没号」这一种解释已被第 4.5 节的同键查重 10409 排掉（建档确实把密文落了库）。
    //     契约与既有测试都站在这条断言这边：
    //       docs/api/api-contracts.ts:2097-2099 ——「改前值取不出来时它写 unreadable，同时
    //         before.phone 是占位符而非号码 ⇒ 读侧要分得开『没有号』与『有号但读不出』」；
    //       audit_t450_test.go:84-86 钉的也是 before.phone=138****1111 / phoneState=masked。
    //     为什么 Go 那两条绿而这里红：它们跑 fake store，fixture 直接把 PhoneEnc 塞进行里
    //       （admin_patient_maintenance_integration_test.go:80「含 phone_enc 的 fixture」），
    //       绕过了 repo 投影 ⇒ 只有真实模式（真库、真读路径）才暴露这一格。
    const phoneRow = await pollAuditRow(page, token!, pid, 'phone', '改号未落操作日志')
    const phoneDetail = phoneRow.detail!
    expect(changedKeysOf(phoneDetail), '改号留痕只应记 phone 一项').toEqual(['phone'])
    expect(phoneDetail.reason, '留痕应原样带上变更原因（后端不校验、只入审计）').toBe(reason)
    const beforeSnap = phoneDetail.before as Record<string, string>
    const afterSnap = phoneDetail.after as Record<string, string>
    expect(afterSnap.phone, '留痕改后应等于新号的脱敏值').toBe(maskPhone(phoneB))
    expect(afterSnap.phoneState, '改后号状态应为 masked').toBe('masked')
    // 🔴 下面三条是 expect.soft，不是「放宽判据」：Playwright 的软断言照样让本用例判红，
    //    区别只是不在第一处失败就中止 —— 这一格是已知缺陷（9.1），硬断言会把它后面的
    //    第 10 节（通道分离 + UI 写集合精确值）整段吃掉，一轮夜巡只拿到一条信息。
    //    后端修好后这三条自然转绿，软/硬在这个前提下等价，别把它们改回 toBe 之外的写法。
    expect.soft(beforeSnap.phone, '留痕改前应等于建档号的脱敏值（缺陷现形处，依据见 9.1）').toBe(
      maskPhone(phoneA),
    )
    expect.soft(beforeSnap.phoneState, '改前号应能被服务端解开（建档时就是它加密写的）').toBe('masked')
    // 两态不许塌成一态：改前后必须是不同的脱敏串
    expect(beforeSnap.phone, '改前改后脱敏值不得相同').not.toBe(afterSnap.phone)
    expect(JSON.stringify(phoneDetail), '留痕里不得出现明文手机号').not.toContain(phoneA)
    expect(JSON.stringify(phoneDetail), '留痕里不得出现明文手机号').not.toContain(phoneB)

    // 第二条读数通道：「操作描述」文案。审计页只渲染 description、不渲染 detail
    // （audit_t252.go:98 的口径），所以只比 detail 等于没验运营真看见的那一列。
    // 这里用 toContain 而不是整句 toBe：整句 fmt（admin_patient.go:161-162 +
    // auditPhonePhrase:192-197）是文案、不是契约，钉死它会让「后端改了一句描述」
    // 这种无害变更把这条链判红；而四个量（患者号/原因/改前后脱敏值）必须都在。
    expect(phoneRow.description, `操作描述应含患者号，实得 ${phoneRow.description}`).toContain(pid)
    expect(phoneRow.description, '操作描述应含变更原因').toContain(reason)
    expect(phoneRow.description, '操作描述应含改后脱敏号').toContain(maskPhone(phoneB))
    // 与 9.1 同一处缺陷的另一通道：absent 时后端写的是「无手机号（absent）」（auditPhonePhrase），
    // 句子成文、读侧无歧义，但那个「无手机号」与库里的密文不符 ⇒ 这条与上面同进同退。
    expect
      .soft(phoneRow.description, '操作描述应含改前脱敏号（与 detail.before 同一处缺陷）')
      .toContain(maskPhone(phoneA))
    expect(phoneRow.description, '操作描述里不得出现明文手机号').not.toContain(phoneA)
    expect(phoneRow.description, '操作描述里不得出现明文手机号').not.toContain(phoneB)

    // ── 10) 反证：这条链真的按顺序打了 staging，且 UI 腿只有那四笔写 ──
    expect(net, '应真的发出过登录请求（BASE_URL 打空也会让上面全绿）').toContain(
      'POST /api/v1/auth/login',
    )
    // 通道分离：node 侧 callApi 建的那条团队不该出现在页面请求里。这条红了就说明
    // page.request 也会进 page.on('request') ⇒ 下面那条「UI 写了什么」的精确集合失去意义，
    // 必须改成按 apiWrites 扣除后再比，别直接把断言删掉。
    expect(
      net.filter((m) => m === 'POST /api/v1/teams'),
      `采集面里混进了 node 侧 callApi 的请求（页面自己没建过团队）⇒ 通道分离前提已不成立，页面请求序列=${net.join(' → ')}`,
    ).toEqual([])

    const uiWrites = net.filter((m) => !m.startsWith('GET '))
    expect(
      uiWrites,
      `链 A 写段的 UI 腿应只发出声明的这四笔写（顺序即链路），实得 ${JSON.stringify(uiWrites)}`,
    ).toEqual([
      'POST /api/v1/auth/login',
      'POST /api/v1/admin/patients',
      `PUT /api/v1/admin/patients/${pid}/phone`,
      `PUT /api/v1/admin/patients/${pid}`,
      `PUT /api/v1/admin/patients/${pid}/team`,
    ])
  })

  /**
   * A6 解绑微信（对从未绑定过的患者）。
   *
   * 单独一条而不并进 22.1：22.1 那四笔是「一条链」的连续动作，而这一条要的量是
   * 「未绑定态下点解绑，后端无条件置 NULL 仍回 200、留痕把两态分开记」，
   * 它需要的是一个干净患者（自己建、自己删），不该复用 22.1 那行已经改过号的患者。
   * 建档这里走 API（入口不是被测对象，被测的是抽屉那个 danger 按钮 + 二次确认弹层）。
   */
  test('22.2 解绑微信（未绑定者）：真 UI 二次确认只发一条 POST，留痕分开记两态', async ({ page }) => {
    const net: string[] = []
    page.on('request', (req) => {
      let path: string
      try {
        path = new URL(req.url()).pathname
      } catch {
        return
      }
      if (path.startsWith('/api/v1/')) net.push(`${req.method()} ${path}`)
    })

    await realLogin(page)
    await expect(page).toHaveURL(/\/dashboard/, { timeout: 20_000 })
    const token = await getAuthToken(page)
    expect(token, '真实模式登录应把 JWT 写进 localStorage').toBeTruthy()
    await preflightWrites(page, token!)

    const before = await callOk<{ total: number }>(
      page,
      'GET',
      '/api/v1/admin/patients?page=1&pageSize=10',
      { token: token!, why: '读患者总数（本条自删自平）' },
    )

    const name = uniqueName(E2E_PATIENT_NAME_PREFIX)
    const created = await callOk<PatientRow>(page, 'POST', '/api/v1/admin/patients', {
      token: token!,
      body: { name, phone: uniqueTestPhone(), gender: 'male', age: 12, diagnosis: '腰段侧弯' },
      why: `建第二条探针患者失败（message 含 already exists 即随机号撞了 phone_hash 查重，重跑一轮）：${name}`,
    })
    createdPatientIds.push(created.patientId)
    console.log(`[t462-s2s3][造数] 22.2 patientId=${created.patientId} name=${name}`)

    await gotoMenuAndWaitTable(page, '患者管理', 'patients', patientCard(page))
    const search = page.locator('.search-input input')
    await search.fill(name)
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).first().click()
    const card = patientCard(page)
    const nameIdx = await headerIndex(card, '姓名')
    const row = card
      .locator('.el-table__body-wrapper tbody tr')
      .filter({ hasText: created.patientId })
      .first()
    await expect(row, '搜索后应出现探针患者那一行').toBeVisible({ timeout: 20_000 })
    await row.locator('td').nth(nameIdx).click()

    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible({ timeout: 8_000 })
    await drawer
      .locator('.drawer-actions')
      .getByRole('button', { name: '解绑微信' })
      .click()

    // 二次确认弹层（ElMessageBox）：取消/确定都在这儿分岔，所以「确认解绑」必须按文案点，不能按 nth
    const box = page.locator('.el-message-box')
    await expect(box, '点「解绑微信」应先弹二次确认').toBeVisible({ timeout: 8_000 })
    await expect(box.locator('.el-message-box__title')).toHaveText('解绑微信')
    await expect(box.locator('.el-message-box__message')).toContainText(created.patientId)
    await box.getByRole('button', { name: '确认解绑' }).first().click()

    await expect(adminMessage(page), '解绑应回「已解绑微信」').toHaveText('已解绑微信', {
      timeout: 15_000,
    })
    expect(
      net,
      `应捕获到解绑那一条 POST（UI 腿没发出去＝成功文案来自别处），序列=${net.join(' → ')}`,
    ).toContain(`POST /api/v1/admin/patients/${created.patientId}/unbind-wechat`)

    // 留痕两态分开记：before.wechatBound=false 是「服务端确认它没绑过」，
    // 写成 null 意味着那次只读探测失败（admin_patient.go:70-87 的 auditBoundLabel/auditBool 口径），
    // 那是真缺陷（把探测失败冒充成未绑定），这里判红并报出来。
    const auditRow = await pollAuditRow(
      page,
      token!,
      created.patientId,
      'wx_openid',
      '解绑未落操作日志',
    )
    const detail = auditRow.detail!
    expect(changedKeysOf(detail), '解绑留痕只应记 wx_openid').toEqual(['wx_openid'])
    expect((detail.before as Record<string, unknown>).wechatBound, '解绑前应确认未绑定（不是探测失败）').toBe(
      false,
    )
    expect((detail.after as Record<string, unknown>).wechatBound, '解绑后应为未绑定').toBe(false)
    expect(
      auditRow.description,
      `解绑留痕描述应写明患者号，实得 ${JSON.stringify(auditRow.description)}`,
    ).toContain(created.patientId)

    // 本条自己收口：探针患者当场删掉，并从 createdPatientIds 摘除 ——
    // 留着不管 afterAll 会再删一次，第二次是 10404，afterAll 那条「还原应回 code=0」就假红了。
    const del = await callApi(page, 'DELETE', `/api/v1/admin/patients/${created.patientId}`, {
      token: token!,
    })
    console.log(
      `[t462-s2s3][还原] 22.2 就地删除 DELETE /api/v1/admin/patients/${created.patientId} → status=${del.status} code=${del.code}`,
    )
    expect(del.code, `就地删除探针患者应回 code=0，实得 status=${del.status} code=${del.code} message=${del.message}`).toBe(
      0,
    )
    createdPatientIds.splice(
      createdPatientIds.indexOf(created.patientId),
      1,
    )
    const gone = await callApi(
      page,
      'GET',
      `/api/v1/admin/patients/${created.patientId}`,
      { token: token! },
    )
    expect(gone.code, `删后详情应回 code=10404（硬删），实得 ${gone.code}`).toBe(10404)
    const after = await callOk<{ total: number }>(
      page,
      'GET',
      '/api/v1/admin/patients?page=1&pageSize=10',
      { token: token!, why: '本条收尾后读患者总数' },
    )
    expect(after.total, `22.2 收尾后 total 应回到自己的基线 ${before.total}`).toBe(before.total)
  })

  /**
   * 甲案清场（还原 + 报备）。
   *
   * 不看用例状态：22.1 中途红也照跑，否则「失败的那一轮」正是最需要清场的一轮。
   * 顺序固定先患者后团队（反了必 409：DeleteTeam 见 patients.team_id 有引用就拒，pg.go:1623-1647）。
   * 团队只删自己建的那一条；字典里若还躺着别人的 T053团队-*（例如 06 历史遗留），
   * 那不是我这条链的债 ⇒ 打进日志报备，不判我的红。
   */
  test.afterAll(async ({ browser }) => {
    if (createdPatientIds.length === 0 && createdTeams.length === 0) return
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const token = await getAuthToken(page)
      if (!token) throw new Error('afterAll 取不到 admin JWT，无法还原')

      for (const pid of [...createdPatientIds]) {
        const del = await callApi(page, 'DELETE', `/api/v1/admin/patients/${pid}`, { token })
        console.log(
          `[t462-s2s3][还原] DELETE /api/v1/admin/patients/${pid} → status=${del.status} code=${del.code}`,
        )
        if (del.code !== 0) {
          if (del.code === 10409) {
            throw new Error(
              `患者 ${pid} 被别处引用，删除被拒（409）。逐表计数只在技术日志通道里、响应体不带表名（handler.go:359-368 的 T464 双通道：message 恒为 model.UserText(code)，repo/pg.go:1381-1395 的 "patient in use: 表=行数" 只进日志），所以这里停手回报、不绕库：请拿患者号与 request_id 查 user-service 技术日志的 per-table 计数，判断是谁引用了链 A 的自建患者`,
            )
          }
          throw new Error(
            `还原删除患者 ${pid} 未成功（应 code=0，实得 status=${del.status} code=${del.code} message=${del.message}）`,
          )
        }
        // 删干净的反证就地做（行读回来是 10404，不是「行还在、只是状态变了」）。
        // 不能留到循环后再统一反证：下面每次成功都会把该号从 createdPatientIds 摘掉，
        // 循环结束后那个数组必然为空，届时按它循环的「反证」是一段恒绿的死代码。
        const gonePatient = await callApi(page, 'GET', `/api/v1/admin/patients/${pid}`, { token })
        expect(
          gonePatient.code,
          `患者 ${pid} 删除后详情应回 code=10404（硬删），实得 status=${gonePatient.status} code=${gonePatient.code}`,
        ).toBe(10404)
        createdPatientIds.splice(createdPatientIds.indexOf(pid), 1)
      }

      for (const t of [...createdTeams]) {
        const del = await callApi(page, 'DELETE', `/api/v1/teams/${t.teamId}`, { token })
        console.log(
          `[t462-s2s3][还原] DELETE /api/v1/teams/${t.teamId} → status=${del.status} code=${del.code}`,
        )
        if (del.code !== 0) {
          if (del.code === 10409) {
            throw new Error(
              `团队 ${t.teamId}（${t.name}）仍被引用，删除被拒（409）。链 A 只写了自建患者这一条引用面，说明上面删患者没删干净或另有引用 ⇒ 停手回报，不动 seed 数据`,
            )
          }
          throw new Error(
            `还原删除团队 ${t.teamId} 未成功（应 code=0，实得 status=${del.status} code=${del.code} message=${del.message}）`,
          )
        }
        const goneTeam = await callApi(page, 'GET', `/api/v1/teams/${t.teamId}`, { token })
        expect(
          goneTeam.code,
          `团队 ${t.teamId} 删除后详情应回 code=10404（getTeam，handler.go:950-964），实得 status=${goneTeam.status} code=${goneTeam.code}`,
        ).toBe(10404)
        createdTeams.splice(createdTeams.indexOf(t), 1)
      }

      const after = await callOk<{ total: number }>(
        page,
        'GET',
        '/api/v1/admin/patients?page=1&pageSize=10',
        { token, why: '收尾后读患者总数' },
      )
      if (baselineTotal !== null) {
        expect(
          after.total,
          `行数守恒：收尾后 total=${after.total} 应回到写段前快照 total=${baselineTotal}`,
        ).toBe(baselineTotal)
      } else {
        // 快照前就红了 ⇒ 没有可比对的基数。行照删，读数留档。
        console.log(`[t462-s2s3][还原] 无写段前快照可比对，收尾后 total=${after.total}`)
      }

      const teamsNow = (await callOk<TeamRow[]>(page, 'GET', '/api/v1/teams', {
        token,
        why: '收尾后读团队字典（残留报备）',
      })).filter((t) => String(t.name).startsWith(E2E_TEAM_NAME_PREFIX))
      if (teamsNow.length > 0) {
        console.log(
          `[t462-s2s3][残留报备] 自建团队已删净，但字典里还有 ${teamsNow.length} 条 ${E2E_TEAM_NAME_PREFIX}* 前缀团队（不是本链建的，归 06 历史遗留）：${teamsNow
            .map((t) => `${t.teamId}/${t.name}`)
            .join('，')}`,
        )
      }
    } catch (err) {
      console.log(
        `[t462-s2s3][残留报备] 链 A 写段未确认清场，患者号=${createdPatientIds.join(
          '，',
        )} 团队=${createdTeams.map((t) => `${t.teamId}/${t.name}`).join('，')} 原因=${(err as Error).message}`,
      )
      throw err
    } finally {
      await ctx.close()
    }
  })
})
