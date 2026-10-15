import { test, expect, type Page } from '@playwright/test'
import { realLogin, getAuthToken, uniqueName } from '../real-helpers'

/**
 * T654 · 「康复建议」全链真实模式用例（e2e-real，打 staging）。
 *
 * 为什么必须有这一条（T641 验收 2026-10-11 独立发现）：
 *   e2e/tests/advice.spec.ts 走 mock 档，且 PR#368 checks 里「Playwright E2E (mock)」一行为
 *   skipping；e2e-real/tests 内没有任何 advice 用例 ⇒ 该全链在 CI 零真跑覆盖，
 *   T641 的判据 1/2 只能靠人工写腿验证。本用例把那条人工写腿固化成 CI 每轮真跑。
 *
 * 链路（与 T641 卡面判据同源，user-service handler.go:304-308 五端点）：
 *   前拍 → 负对照（跨团队医护发送 403）→ 医护发送（200，adviceId）
 *   → 读回（职称+时间+内容，姓名类键缺席，作者视角 editable=true）
 *   → 作者编辑（200）→ 读回内容已更新 → 作者删除（200）→ 后拍=前拍（库面守恒）
 *
 * 数据纪律（T654 卡面约束 + T697 治本改法）：
 *   - 医护用 seed 家族账号 doctor_li（口令与 ops_admin 同族，走 realPassword 凭据门）；
 *   - 🔴 T697：患者对象一律**本链自建**（建档 → 分配团队 → 读回校验 → 用后即删），
 *     不再借 seed 家族患者的团队归属。旧姿势把用例生死押在共享 seed 状态上：历轮测试
 *     （22-链A 的分配团队、24-链D 的改派）会把 seed 患者挪出 doctor_li 团队 ⇒ 本链稳定红
 *     （PR #411 首跑与 max 复跑同为 1 failed/95 passed；现读 TEAM26f7a1b7bd 列表 17 行 /
 *     同团队 0 行）。自己造的对象只有自己会挪，前置不再依赖任何共享行。
 *   - staging 写 = 2 条自建患者 + 1 条 advice，全部用例内 DELETE 收尾 + afterAll 兜底，
 *     后拍断言「advice 条数=前拍」且「患者总数=前拍」⇒ 库面守恒，不留残行（T467 口径）。
 *   - 教训锚（T641 写腿 run3-scope.txt）：患者 teamId 会被历轮测试改派 ⇒ 患者不许写死；
 *     T697 更进一步：连"动态定位"都不做，直接自建，把这条环境漂移从根上移出用例的依赖面。
 *
 * 负对照的真咬结构（T697 明确不弱化）：29.1 里同一枚 doctor 令牌对两枚患者各打一次 ——
 *   同团队（自建）必须 200、跨团队（自建）必须 403。两条断言同处一条用例，
 *   谁把负对照的对象挪回本团队，403 那条当场红 ⇒ 判据不靠"运气找到别人的患者"成立。
 *
 * 生产零写：入口守卫命中生产域名直接抛（生产针 = api.hbksd.com.cn；TST 基址 49.235.137.217 放行，口径同 PR382）。
 */

const ENTRY = process.env.E2E_STAGING_URL ?? 'http://localhost:2080'
if (/api\.hbksd\.com\.cn/.test(ENTRY)) { // 49.235.137.217 自 2026-10-11 起为 TST（Boss 口径，TST dispatch 由本链承运）；生产针保留 api.hbksd.com.cn。合序说明：本件随 PR380 合入时照抄了 22 号旧姿势，晚于 PR382 摘 IP，成了 main 唯一残留（alex 20261011-1734 escalation 指认），本笔对齐 382 同款。
  throw new Error(`T654 advice 全链命中生产入口，红线拒绝：${ENTRY}`)
}

/** 医生列表行（GET /api/v1/doctors 投影，T356 superset） */
interface DoctorRow {
  doctorId: string
  name: string
  teamId: string | null
  username?: string | null
}

/** 患者列表行（AdminPatientDTO，列表与详情共用投影） */
interface PatientRow {
  patientId: string
  name: string
  teamId: string | null
  status: string
}

/** 建议行（toAdviceDTO：handler.go:1946 一带）——姓名类键必须缺席 */
interface AdviceRow {
  adviceId: string
  patientId: string
  title: string
  content: string
  createdAt: string
  updatedAt: string
  /** 作者视角 true；其他任何读者 false（callerDoctorID 判定） */
  editable?: boolean
}

interface Envelope {
  status: number
  code: number | null
  message: string
  data: unknown
}

/** 与 22 号同款的信封读取（有意不提进 real-helpers：不为本卡动已绿锁的 helper 面） */
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
    /* 非 JSON 信封：code 留 null */
  }
  return { status: res.status(), code, message, data }
}

/** 成功信封断言：code 必须 0（「HTTP 200 就算过」是这条链的红线，同 22 号口径） */
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

/** T637 设计稿八的隐私判据：患者侧任何响应体不得出现医护姓名 —— 键缺席即过 */
const STAFF_NAME_KEYS = ['doctorName', 'teamName', 'realName', 'userName', 'name'] as const

const CONTENT_V1 = '【T654 CI 用例】请保持每日佩戴时长，两周后复查。（本条为自动化测试数据，将随即删除）'
const CONTENT_V2 = '【T654 CI 用例·编辑】请保持每日佩戴时长并注意皮肤状态。（本条为自动化测试数据，将随即删除）'

/** T697 自建夹具患者的命名前缀（残留可按前缀手工清，同 22-链A 的 T053 前缀口径） */
const FIXTURE_PATIENT_PREFIX = 'T697建议夹具'

/** 团队字典行（只取归属判定需要的两枚键） */
interface TeamRow {
  teamId: string
  teamType?: string
  name?: string
}

/** 患者总数信封（/admin/patients 是 {rows|list,total}，库面守恒两拍都读这一枚 total） */
interface PatientTotal {
  total?: number
}

/** 随机 11 位测试手机号（135 段，同 22-链A 姿势；撞 phone_hash 查重时 by why 里点名重跑） */
function uniqueFixturePhone(): string {
  return `135${String(Math.floor(Math.random() * 1e8)).padStart(8, '0')}`
}

/**
 * 建一枚夹具患者并分配进 teamId，随后读回校验归属真的落上。
 * 这里"校验"不"自愈"：分配没落上就红在本处并点名端点，绝不回头去挪别人的患者或改 seed 归属
 * （改派 seed 是环境动作，归值班席，不归用例）。
 */
async function createFixturePatient(p: Page, token: string, teamId: string, role: string): Promise<string> {
  const name = uniqueName(FIXTURE_PATIENT_PREFIX)
  const created = await callOk<PatientRow>(p, 'POST', '/api/v1/admin/patients', {
    token,
    body: { name, phone: uniqueFixturePhone(), gender: 'male', age: 12, diagnosis: 'T697 夹具（用后即删）' },
    why: `建${role}夹具患者失败（message 含 already exists 即随机号撞了 phone_hash 查重，重跑一轮）：${name}`,
  })
  const pid = String(created.patientId)
  await callOk<unknown>(p, 'PUT', `/api/v1/admin/patients/${encodeURIComponent(pid)}/team`, {
    token,
    body: { teamId },
    why: `把${role}夹具患者分配进团队 ${teamId}`,
  })
  const back = await callOk<PatientRow>(p, 'GET', `/api/v1/admin/patients/${encodeURIComponent(pid)}`, {
    token,
    why: `读回${role}夹具患者的团队归属（本链前置，不成立就红在这里）`,
  })
  expect(String(back.teamId), `${role}夹具患者应坐在 ${teamId}，实读 ${String(back.teamId)}`).toBe(String(teamId))
  console.log(`[t697][夹具] ${role} patientId=${pid} name=${name} teamId=${String(back.teamId)}`)
  return pid
}

test.describe('29-advice-chain · 康复建议全链（T654）', () => {
  /** 跨用例共享的清理句柄：用例体内写入的 adviceId，afterAll 兜底删除 */
  let adminPage: Page
  let doctorPage: Page
  let adminToken = ''
  let doctorToken = ''
  let doctorTeamId: string | null = null
  let targetPatientId = ''
  let crossTeamPatientId = ''
  let writtenAdviceId: string | null = null
  let baselineCount = -1
  /** T697 自建夹具患者号（afterAll 用后即删，库面守恒的另一半） */
  let createdPatientIds: string[] = []
  /** T697 前拍患者总数（afterAll 后拍要回到这一枚） */
  let patientBaselineTotal = -1

  test.beforeAll(async ({ browser }) => {
    const adminCtx = await browser.newContext()
    const doctorCtx = await browser.newContext()
    adminPage = await adminCtx.newPage()
    doctorPage = await doctorCtx.newPage()

    await realLogin(adminPage)
    adminToken = (await getAuthToken(adminPage)) ?? ''
    expect(adminToken, 'admin 登录后应有 JWT').not.toBe('')

    await realLogin(doctorPage, 'doctor_li')
    doctorToken = (await getAuthToken(doctorPage)) ?? ''
    expect(doctorToken, 'doctor_li 登录后应有 JWT').not.toBe('')

    // doctor_li 的现网团队（不许假设 seed 值——T641 写腿实测患者团队会被历轮测试改派）。
    // 用 admin 令牌读医生列表：GET /api/v1/doctors 对医生角色是网关 403（本地真跑实测），
    // 医生自己的团队归属只能从 admin 侧字典拿；doctor 令牌只用于 advice 写腿，
    // 写身份在服务层由网关注入的 X-User-Id 解析（DoctorIDByAdmin），不经本列表。
    const docs = await callOk<DoctorRow[]>(adminPage, 'GET', '/api/v1/doctors', {
      token: adminToken,
      why: 'admin 读医生列表定位 doctor_li 的团队归属',
    })
    const docRows = Array.isArray(docs) ? docs : []
    const me = docRows.find((d) => d.username === 'doctor_li') ?? docRows.find((d) => d.name === '李医师')
    expect(me, `医生列表里应能定位 doctor_li（实得 ${docRows.length} 行）`).toBeTruthy()
    doctorTeamId = me!.teamId
    console.log(`[t654][fixture] doctor_li doctorId=${me!.doctorId} teamId=${doctorTeamId}`)
    expect(doctorTeamId, 'doctor_li 应有团队归属（无团队则本链的「绑定团队成员」前提不成立）').toBeTruthy()

    // T697 治本段：两枚对象全部自建，用例不再读任何共享 seed 患者的归属。
    // 前拍先记患者总数（afterAll 用后即删后要回到这一枚）。
    const before = await callOk<PatientTotal>(
      adminPage,
      'GET',
      '/api/v1/admin/patients?page=1&pageSize=1',
      { token: adminToken, why: '前拍患者总数（库面守恒基数）' },
    )
    patientBaselineTotal = Number(before?.total ?? -1)
    expect(patientBaselineTotal, `患者总数应可读且非负（实得 ${patientBaselineTotal}）`).toBeGreaterThanOrEqual(0)

    // 负对照只需要"另一个 teamId 这一个值"：只读借用，不动那支团队的成员与归属。
    const teamsRaw = await callOk<TeamRow[] | { list?: TeamRow[] }>(adminPage, 'GET', '/api/v1/teams', {
      token: adminToken,
      why: '读团队字典取一枚非 doctor_li 团队 id（只借值，不改它的任何成员）',
    })
    const teamRows = Array.isArray(teamsRaw) ? teamsRaw : (teamsRaw?.list ?? [])
    const otherTeam = teamRows.find((t) => t.teamId && t.teamId !== doctorTeamId)
    expect(otherTeam, `团队字典里应有非 doctor_li 的团队可借（实读 ${teamRows.length} 行）`).toBeTruthy()

    targetPatientId = await createFixturePatient(adminPage, adminToken, doctorTeamId!, '正例（与 doctor_li 同团队）')
    createdPatientIds.push(targetPatientId)
    crossTeamPatientId = await createFixturePatient(
      adminPage,
      adminToken,
      otherTeam!.teamId,
      `负对照（跨团队 ${otherTeam!.teamId}）`,
    )
    createdPatientIds.push(crossTeamPatientId)
    console.log(
      `[t697][夹具] 正例=${targetPatientId}@${doctorTeamId} 负对照=${crossTeamPatientId}@${otherTeam!.teamId} ` +
        `患者总数前拍=${patientBaselineTotal}（本链自建 ${createdPatientIds.length} 枚，收尾全删）`,
    )
  })

  test('29.1 医护发送-读回-编辑-删除全链 + 跨团队 403 负对照（用后即删，库面守恒）', async () => {
    // ── 前拍 ──
    const pre = await callOk<AdviceRow[]>(doctorPage, 'GET', `/api/v1/patients/${targetPatientId}/advice`, {
      token: doctorToken,
      why: '前拍建议流',
    })
    baselineCount = Array.isArray(pre) ? pre.length : 0
    console.log(`[t654][前拍] 患者 ${targetPatientId} 建议条数=${baselineCount}`)

    // ── 负对照：对跨团队患者发送 → 403（归属判定在任何库写之前）──
    const neg = await callApi(doctorPage, 'POST', `/api/v1/patients/${crossTeamPatientId}/advice`, {
      token: doctorToken,
      body: { content: CONTENT_V1 },
    })
    expect(
      neg.status,
      `跨团队医护发送应被拒 403，实得 status=${neg.status} code=${neg.code} message=${neg.message}`,
    ).toBe(403)
    console.log(`[t654][负对照] 跨团队 POST → 403（code=${neg.code}）MATCH`)

    // ── 正例：同团队患者发送 → 200 + adviceId ──
    const created = await callOk<AdviceRow>(doctorPage, 'POST', `/api/v1/patients/${targetPatientId}/advice`, {
      token: doctorToken,
      body: { content: CONTENT_V1 },
      why: '绑定团队医护发送建议',
    })
    expect(created.adviceId, '发送应回 adviceId').toBeTruthy()
    expect(created.title, '发送应回作者职称（设计稿：展示角色+职称，无姓名）').toBeTruthy()
    writtenAdviceId = String(created.adviceId)
    console.log(`[t654][发送] adviceId=${writtenAdviceId} title=${created.title} editable=${created.editable}`)

    // ── 读回（doctor 视角）：条数 +1、行值级、隐私键面、editable 语义 ──
    const list1 = await callOk<AdviceRow[]>(doctorPage, 'GET', `/api/v1/patients/${targetPatientId}/advice`, {
      token: doctorToken,
      why: '发送后读回建议流',
    })
    expect(Array.isArray(list1) ? list1.length : 0, '发送后建议流应比前拍多 1 条').toBe(baselineCount + 1)
    const row = (list1 ?? []).find((x) => String(x.adviceId) === writtenAdviceId)
    expect(row, '读回应含刚写入的那条').toBeTruthy()
    expect(row!.content, '读回 content 应等于写入值').toBe(CONTENT_V1)
    expect(row!.title, '读回应带职称').toBeTruthy()
    expect(row!.createdAt, '读回应带 createdAt（时间轴展示面）').toBeTruthy()
    const hitNameKeys = STAFF_NAME_KEYS.filter((k) => k in row!)
    expect(hitNameKeys, `建议行不得出现姓名类键（命中：${hitNameKeys.join(',')}）`).toHaveLength(0)
    expect(row!.editable, '作者视角 editable 应为 true').toBe(true)
    console.log(`[t654][读回] 条数=${(list1 ?? []).length} title=${row!.title} createdAt=${row!.createdAt} 姓名键0 editable=true`)

    // ── 作者编辑 → 读回内容已更新 ──
    const upd = await callOk<AdviceRow>(doctorPage, 'PUT', `/api/v1/advice/${writtenAdviceId}`, {
      token: doctorToken,
      body: { content: CONTENT_V2 },
      why: '作者本人编辑建议',
    })
    expect(upd.content, '编辑响应应回新内容').toBe(CONTENT_V2)
    const list2 = await callOk<AdviceRow[]>(doctorPage, 'GET', `/api/v1/patients/${targetPatientId}/advice`, {
      token: doctorToken,
      why: '编辑后读回建议流',
    })
    const row2 = (list2 ?? []).find((x) => String(x.adviceId) === writtenAdviceId)
    expect(row2?.content, '编辑后读回 content 应为新值（UPDATE 落地）').toBe(CONTENT_V2)
    console.log(`[t654][编辑] 读回 content 已更新为编辑版`)
  })

  test('29.2 删除与库面守恒（后拍=前拍；含 afterAll 兜底）', async () => {
    // 29.1 若失败，writtenAdviceId 可能为 null —— 这里只清自己写的那条
    test.skip(!writtenAdviceId, '29.1 未成功写入（无残留可清），本条守恒断言随之前滚')

    // 非作者删除负对照：admin 无 doctors 行 ⇒ 写身份 403（adviceWriteIdentity）
    const negDel = await callApi(adminPage, 'DELETE', `/api/v1/advice/${writtenAdviceId}`, { token: adminToken })
    expect(negDel.status, `运营管理员删除应被拒 403，实得 ${negDel.status}`).toBe(403)

    // 作者删除 → 200
    const del = await callOk<null>(doctorPage, 'DELETE', `/api/v1/advice/${writtenAdviceId}`, {
      token: doctorToken,
      why: '作者本人删除建议（用后即删）',
    })
    expect(del).toBeNull()
    writtenAdviceId = null
    console.log(`[t654][删除] DELETE → 200`)

    // 后拍：条数=前拍 ⇒ 库面守恒（T467 行数守恒口径）
    const post = await callOk<AdviceRow[]>(doctorPage, 'GET', `/api/v1/patients/${targetPatientId}/advice`, {
      token: doctorToken,
      why: '后拍建议流',
    })
    const postCount = Array.isArray(post) ? post.length : 0
    expect(postCount, `后拍应等于前拍 ${baselineCount}（库面守恒，无残行），实得 ${postCount}`).toBe(baselineCount)
    console.log(`[t654][后拍] 条数=${postCount} = 前拍 ${baselineCount}，守恒 MATCH`)
  })

  test.afterAll(async () => {
    // 兜底清场：29.1 写入后若 29.2 未跑到（中断/软失败），这里删干净并报备
    if (writtenAdviceId && doctorToken) {
      const r = await callApi(doctorPage, 'DELETE', `/api/v1/advice/${writtenAdviceId}`, { token: doctorToken })
      console.log(`[t654][afterAll 兜底] adviceId=${writtenAdviceId} DELETE status=${r.status} code=${r.code}`)
      if (r.status !== 200) {
        throw new Error(`[t654][残留报备] adviceId=${writtenAdviceId} 兜底删除失败 status=${r.status} —— 请按 T053 前缀在 staging 手工清理`)
      }
      writtenAdviceId = null
    }

    // T697 用后即删：自建的两枚夹具患者也必须回库。删除顺序不可倒 —— advice 已在上面清完，
    // 这里只删患者行；删不动就报残像并抛（守恒是判据④，不允许"静默留行"过绿）。
    if (createdPatientIds.length > 0 && adminToken) {
      const leaked: string[] = []
      for (const pid of createdPatientIds) {
        const r = await callApi(adminPage, 'DELETE', `/api/v1/admin/patients/${encodeURIComponent(pid)}`, {
          token: adminToken,
        })
        console.log(`[t697][清场] DELETE /admin/patients/${pid} status=${r.status} code=${r.code}`)
        if (r.status !== 200) leaked.push(`${pid}(status=${r.status})`)
      }
      const after = await callOk<PatientTotal>(
        adminPage,
        'GET',
        '/api/v1/admin/patients?page=1&pageSize=1',
        { token: adminToken, why: '后拍患者总数（库面守恒）' },
      )
      const afterTotal = Number(after?.total ?? -1)
      console.log(
        `[t697][守恒] 患者总数 前拍=${patientBaselineTotal} 后拍=${afterTotal} ` +
          `${afterTotal === patientBaselineTotal ? 'MATCH' : 'DRIFT'}`,
      )
      if (leaked.length > 0) {
        throw new Error(
          `[t697][残留报备] 夹具患者未删净：${leaked.join('，')}（前缀 ${FIXTURE_PATIENT_PREFIX}）` +
            ` —— 本链不自行改环境，请按四纪律手工清理并回报 max`,
        )
      }
      if (patientBaselineTotal >= 0 && afterTotal !== patientBaselineTotal) {
        throw new Error(
          `[t697][残留报备] 患者总数未回平：前拍 ${patientBaselineTotal} 后拍 ${afterTotal}（差 ` +
            `${afterTotal - patientBaselineTotal}）—— 有行没删干净或本轮之外还有人写患者表，停手回报不自行修`,
        )
      }
      createdPatientIds = []
    }
  })
})
