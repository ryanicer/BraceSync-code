import { test, expect, type Page } from '@playwright/test'
import { realLogin, getAuthToken } from '../real-helpers'

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
 * 数据纪律（T654 卡面约束）：
 *   - 医护用 seed 家族账号 doctor_li（口令与 ops_admin 同族，走 realPassword 凭据门）；
 *   - 患者对象动态定位：取 doctor_li 现网团队下的一名 seed 患者（不新建患者行）；
 *   - 唯一的 staging 写 = 1 条 advice 记录，用例内 DELETE 收尾 + afterAll 兜底删除，
 *     后拍断言「条数=前拍」⇒ 库面守恒，不留残行（T467 口径的行数守恒）。
 *   - 教训锚（T641 写腿 run3-scope.txt）：患者现网 teamId 可能被历轮测试改派（P20260005
 *     已不在 seed 的 TEAM03），所以患者不许写死，必须动态按 doctor_li 的团队解析。
 *
 * 生产零写：入口守卫照抄 22 号（命中生产域名/IP 直接抛）。
 */

const ENTRY = process.env.E2E_STAGING_URL ?? 'http://localhost:2080'
if (/api\.hbksd\.com\.cn|49\.235\.137\.217/.test(ENTRY)) {
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

    // 定位测试对象：doctor_li 团队内的一名患者（active 优先，pending 也可——advice 写只判团队归属）
    // 注意分页信封：/admin/patients 的 data 是 {rows/list, total} 不是裸数组（与 /doctors 不同）
    const patientsPage = await callOk<{ rows?: PatientRow[]; list?: PatientRow[]; total?: number }>(
      adminPage,
      'GET',
      '/api/v1/admin/patients?page=1&pageSize=100',
      { token: adminToken, why: '读患者列表为动态定位同团队患者' },
    )
    const rows: PatientRow[] = patientsPage?.rows ?? patientsPage?.list ?? (Array.isArray(patientsPage) ? patientsPage : [])
    expect(rows.length, `患者列表应有数据（total=${patientsPage?.total}）`).toBeGreaterThan(0)
    const inTeam = rows.filter((x) => x.teamId === doctorTeamId)
    const target = inTeam.find((x) => x.status === 'active') ?? inTeam[0]
    expect(
      target,
      `doctor_li 团队 ${doctorTeamId} 下应有患者可作建议对象（列表 ${rows.length} 行，同团队 ${inTeam.length} 行）。` +
        `若为 0：seed 家族患者被历轮测试改派所致，属环境漂移，按 T654「seed 家族账号」约束此处判红并停，不另行改派患者。`,
    ).toBeTruthy()
    targetPatientId = target!.patientId

    // 跨团队负对照对象：任一 teamId 非空且不等于 doctor_li 团队的患者
    const outsider = rows.find((x) => x.teamId && x.teamId !== doctorTeamId)
    expect(outsider, '患者列表里应存在跨团队患者作负对照对象').toBeTruthy()
    crossTeamPatientId = outsider!.patientId
    console.log(`[t654][fixture] 正例患者=${targetPatientId}（${target!.name}） 负对照患者=${crossTeamPatientId}（${outsider!.name}）`)
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
  })
})
