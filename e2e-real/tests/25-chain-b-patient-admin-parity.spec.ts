import { test, expect, type Page } from '@playwright/test'
import { realLogin, getAuthToken, uniqueName, E2E_PATIENT_NAME_PREFIX } from '../real-helpers'

/**
 * T575 · 链 B：患者 ↔ 后台数据一致性（真实模式 / staging）
 *
 * 派发单要把 21.1 的三点对平复制五份（B1 感受日志 / B2 反馈求助 / B3 设备绑定 /
 * B4 同一份数据两侧同值 / B5 水平越权）。实测下来只有 B4、B5 能在 staging 上零不可逆写地跑完，
 * B1/B2/B3 的**写入腿**跑不了，原因写在下面（不是「没找到写法」，是写完删不掉）：
 *
 *   - 建档→设口令→口令登录→删号（T477 + T467）这套授权写法只在「该患者一行子记录都没有」时可逆：
 *     DeletePatient 先数关联面，任何一张子表有行就回 409 ErrPatientInUse（repo/pg.go:1466-1491 列 12 张
 *     带外键的表，feeling_logs 与 feedbacks 都在里面；:1535-1567 是那段计数与提前返回）。
 *   - 全仓没有任何 HTTP 路由删得掉一行感受日志或一条反馈：`DELETE FROM feeling_logs|feedbacks` 只出现在
 *     Go 集成测试里。代理层那句注释写得明白（proxy_admin.go:119「仅删无关联行的患者，否则 409」）。
 *   - 设备绑定这条腿对患者根本不开：POST /devices/:deviceId/bind 在网关是 staff-only（rbac.go:259），
 *     进 device-service 还要 admin|technician（handler/write_scope_t387.go:40-47）。
 *     而建设备行的 POST /api/v1/devices 没有对应的删除端点，device_secret 由服务端生成且从不返回
 *     （service/device.go:85-110）⇒ 建一行就留一行。
 *
 * 所以本文件交的是这三类的**门面与闸面**（写入被谁、在哪一层、以什么码拒掉，以及拒之前有没有先落库），
 * 加 B4、B5 两类的值级对平。写腿的缺口如实报 PM，最小 ask 见证据包，不用 route fulfill 造绿。
 *
 * 纪律对齐派发单第四节：不用 page.route 造数据（本文件一次 route 都没调）；生产零写；
 * staging 只按授权的「唯一名 + 唯一号建档 → 一次性口令 → 删号」写，收尾核对行数守恒。
 */

/** 发号形状（repo/pg.go:1262-1268：P + 四位年 + 12 位 hex） */
const PATIENT_ID_RE = /^P\d{4}[0-9a-f]{12}$/

/** 全仓未注册的路径 —— 负对照：没有它，「JSON 信封 ⇒ 路由在架」分不清「在架且资源不存在」与「压根没在架」 */
const NOT_REGISTERED_PATH = '/api/v1/zzz-t575-chain-b-not-registered-4b17'

/** 不存在的患者号，按真实发号形状拼出 */
const GHOST_PATIENT_ID = `P${'0'.repeat(16)}`

/** 探针用的设备号：必须不在架，否则绑定面探针会写到真设备上 */
const GHOST_DEVICE_ID = 'T575-CHAIN-B-GHOST'

/**
 * 日期枚必须由码点拼出，不能手写字面量。
 *
 * 原因不是洁癖：本轮实测到**显示面会把日期串里的斜杠渲染成横杠**（磁盘上的 2026/10/05 在编辑器与
 * 日志里显示成 2026-10-05）。服务端只接受 time.Parse("2006-01-02")（handler.go:1721），也就是横杠形；
 * 若这一枚被显示面「看着像横杠」而实际写成合法日期，感受日志就会真的落一行，自建患者随之变成 409
 * 删不掉 —— 那是不可逆写。所以这里用分隔符码点构造，并在用例里自证码点与两个形状判定。
 */
const SLASH = String.fromCharCode(47)
const DASH = String.fromCharCode(45)
const UNPARSEABLE_DATE = ['2026', '10', '05'].join(SLASH)
const LEGAL_DATE = ['2026', '10', '05'].join(DASH)

/** 服务端接受形状（逐条抄 handler.go:1698-1724 与 1341-1362，校验全部排在落库之前） */
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/
const FEELING_LEVELS = ['fitted', 'discomfort']
const FEELING_AREAS = ['右肩', '左肩', '胸椎', '右侧腰', '左侧腰', '骶骨', '右髂嵴', '左髂嵴']
const FEELING_NOTES_MAX = 200
const FEEDBACK_STATUSES = ['pending', 'replied', 'resolved']
const FEEDBACK_CONTENT_MAX = 500

const feelingWouldInsert = (b: Record<string, unknown>): boolean =>
  typeof b.feeling === 'string' &&
  FEELING_LEVELS.includes(b.feeling) &&
  (b.discomfortAreas === undefined ||
    (Array.isArray(b.discomfortAreas) &&
      (b.discomfortAreas as unknown[]).every((a) => FEELING_AREAS.includes(String(a))))) &&
  (b.notes === undefined || String(b.notes).length <= FEELING_NOTES_MAX) &&
  (b.logDate === undefined || (String(b.logDate).trim() !== '' && DATE_RE.test(String(b.logDate).trim())))

const feedbackWouldInsert = (b: Record<string, unknown>): boolean =>
  typeof b.content === 'string' &&
  (b.content as string).trim() !== '' &&
  (b.content as string).trim().length <= FEEDBACK_CONTENT_MAX &&
  (b.type === undefined || String(b.type).length <= 32) &&
  (b.status === undefined || FEEDBACK_STATUSES.includes(String(b.status).trim()))

interface Envelope {
  status: number
  contentType: string
  /** null = 响应体不是 JSON（gin 自己的 404 就是这一形） */
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

interface PatientProfile {
  patientId: string
  name: string
  age: number | null
  heightCm: number | null
  cobbAngle: number | null
  deviceId: string | null
  status: string
  teamId: string | null
}

/** 患者号只以后缀形态落日志，全号不进任何载体 */
const tail = (id: string): string => `${id.slice(0, 2)}**…${id.slice(-4)}`

const short = (r: Envelope): string =>
  `HTTP=${r.status} code=${r.code} ct=${r.contentType === 'application/json' ? 'JSON' : '非JSON'}`

/** 造出来的会话：口令明文只活在这个变量里，不进日志、不进卡面、不进 BBS */
let session: { patientId: string; phone: string; password: string } | null = null
let baselineTotal: number | null = null

/** 五面计数：探针前后各拍一次，任何一面移动就说明「这一轮写进了库」 */
async function faceCounts(p: Page, token: string): Promise<Record<string, number | string>> {
  const out: Record<string, number | string> = {}
  const read = async (key: string, path: string, pick: (d: unknown) => number | string) => {
    const r = await callApi(p, 'GET', path, { token })
    out[key] = r.code === 0 ? pick(r.data) : `读数失败(HTTP=${r.status} code=${r.code})`
  }
  await read('patients', '/api/v1/admin/patients?page=1&pageSize=1', (d) => Number((d as { total?: number }).total))
  await read('feelingLogs', '/api/v1/admin/feeling-logs?page=1&pageSize=1', (d) =>
    Number((d as { total?: number }).total),
  )
  await read('feedbacks', '/api/v1/feedbacks', (d) =>
    Array.isArray(d) ? d.length : JSON.stringify(d)?.slice(0, 40) ?? 'null',
  )
  await read('devices', '/api/v1/devices?page=1&pageSize=1', (d) => Number((d as { total?: number }).total))
  await read('alerts', '/api/v1/alerts?page=1&pageSize=1', (d) => Number((d as { total?: number }).total))
  return out
}

/**
 * 授权写法：唯一名 + 唯一号建档 → T477 一次性口令 → 口令登录拿患者 JWT。
 * 只在「该患者还没有任何子记录」时可逆（文件头那段），所以调用方一律是只读探针或本人写探针。
 */
async function createSelfPatient(page: Page, adminToken: string): Promise<void> {
  // 代次前置探针（只读，任何写之前）：T477 与 T467 都必须在架，否则写完删不掉
  for (const m of ['POST', 'PUT', 'DELETE'] as const) {
    const neg = await callApi(page, m, NOT_REGISTERED_PATH)
    expect(neg.code, `负对照失效：${m} 未注册路径应回非 JSON，实得 ${short(neg)}`).toBe(null)
  }
  const t477 = await callApi(page, 'POST', `/api/v1/admin/patients/${GHOST_PATIENT_ID}/password`, { token: adminToken })
  const t467 = await callApi(page, 'DELETE', `/api/v1/admin/patients/${GHOST_PATIENT_ID}`, { token: adminToken })
  expect(t477.code, `T477 设口令端点不在架（期望 JSON 10404），实得 ${short(t477)}`).toBe(10404)
  expect(t467.code, `T467 删患者端点不在架（期望 JSON 10404），实得 ${short(t467)}`).toBe(10404)

  const name = uniqueName(E2E_PATIENT_NAME_PREFIX)
  const phone = `135${String(Math.floor(Math.random() * 1e8)).padStart(8, '0')}`
  const created = await callApi(page, 'POST', '/api/v1/admin/patients', {
    token: adminToken,
    body: { name, phone, gender: 'female', age: 14, diagnosis: '胸段侧弯', cobbAngle: 25 },
  })
  expect(
    created.code,
    `建档失败（message 提示已存在即随机号撞了 phone_hash 查重，重跑一轮）：${short(created)}`,
  ).toBe(0)
  const patientId = (created.data as PatientProfile).patientId
  expect(patientId, '建档应回符合发号形状的患者号').toMatch(PATIENT_ID_RE)

  const pw = await callApi(page, 'POST', `/api/v1/admin/patients/${patientId}/password`, { token: adminToken })
  expect(pw.code, `设一次性口令失败：${short(pw)}`).toBe(0)
  const plain = (pw.data as { password: string }).password
  expect(plain.length, '服务端发号口令长度应为 16（值不落日志）').toBe(16)

  session = { patientId, phone, password: plain }
  console.log(`[t575][造数] 患者=${tail(patientId)} 名=${name}（唯一号建档，收尾删行）`)
}

/** 用授权造出的会话重新登录一次，拿患者 JWT（口令明文不外显） */
async function patientToken(page: Page): Promise<string> {
  if (!session) throw new Error('没有自建患者会话可复用，前面的造数腿没跑成')
  const login = await callApi(page, 'POST', '/api/v1/patient/login', {
    body: { phone: session.phone, password: session.password },
  })
  expect(login.code, `患者口令登录失败：${short(login)}`).toBe(0)
  const data = login.data as { token: string; patientId: string; role: string }
  expect(data.role, '登录态角色应为 patient').toBe('patient')
  expect(data.patientId, '登录回的患者号应等于自建患者').toBe(session.patientId)
  return data.token
}

test.describe('25-链 B 患者↔后台一致性（T575）', () => {
  test('25.1 越权两组对比：「存在的别人」与「不存在的别人」都拒，但落在不同的码与不同的层', async ({
    page,
  }) => {
    await realLogin(page)
    await expect(page).toHaveURL(/\/dashboard/, { timeout: 20_000 })
    const adminToken = await getAuthToken(page)
    expect(adminToken, '真实模式登录应把 admin JWT 写进 localStorage').toBeTruthy()

    const before = await faceCounts(page, adminToken!)
    console.log(`[t575-b5][基线] ${JSON.stringify(before)}`)
    baselineTotal = Number(before.patients)

    await createSelfPatient(page, adminToken!)
    const ptok = await patientToken(page)

    // 「存在的别人」：列表里第一行非本人患者
    const list = await callApi(page, 'GET', '/api/v1/admin/patients?page=1&pageSize=50', { token: adminToken! })
    expect(list.code, `读患者列表应 code=0，实得 ${short(list)}`).toBe(0)
    const rows = ((list.data as { list?: PatientProfile[] }).list ?? []).filter(
      (r) => r.patientId !== session!.patientId,
    )
    expect(rows.length, 'staging 上应有至少一行「存在的别人」供越权面对拍').toBeGreaterThan(0)
    const other = rows[0]
    console.log(
      `[t575-b5][目标] 本人=${tail(session!.patientId)} 别人=${tail(other.patientId)} 别人带设备=${String(!!other.deviceId)}`,
    )

    // 用户域 / 数据域的水平越权：同一类路径，两套域码，逐条钉死
    const domainDeny: Array<[string, number, string]> = [
      [`/api/v1/patients/${other.patientId}/feeling-logs`, 10403, 'user-service 域（感受日志）'],
      [`/api/v1/patients/${other.patientId}/review-records`, 10403, 'user-service 域（复查记录）'],
      [`/api/v1/patients/${other.patientId}/orthosis-plans`, 10403, 'user-service 域（矫形方案）'],
      [`/api/v1/patients/${other.patientId}/daily-wear`, 30403, 'data-service 域（佩戴日聚合）'],
      [`/api/v1/patients/${other.patientId}/realtime`, 30403, 'data-service 域（实时快照）'],
      [`/api/v1/patients/${other.patientId}/health-reports`, 30403, 'data-service 域（健康报告）'],
    ]
    for (const [path, code, why] of domainDeny) {
      const r = await callApi(page, 'GET', path, { token: ptok })
      console.log(
        `[t575-b5][存在的别人] ${path.replace(other.patientId, tail(other.patientId))} ${short(r)} 期望=${code}（${why}）`,
      )
      expect(r.status, `${why} 应 403，实得 ${short(r)}`).toBe(403)
      expect(r.code, `${why} 应落 ${code}，实得 ${short(r)}`).toBe(code)
    }

    // records 带必填 date，越权时同样先被数据域挡下（30403 而不是 30001 ⇒ 鉴权排在参数之前）
    const rec = await callApi(page, 'GET', `/api/v1/patients/${other.patientId}/records?period=day&date=${LEGAL_DATE}`, {
      token: ptok,
    })
    console.log(`[t575-b5][存在的别人] records?date=… ${short(rec)} 期望=403/30403`)
    expect(rec.code, `越权读他人历史页应 30403，实得 ${short(rec)}`).toBe(30403)

    // 后台面：网关 roleAuthz 先挡，deny 体的 code 就是 HTTP 状态 403（不是 10403），后端零调用
    const staffOnly: Array<[string, string]> = [
      [`/api/v1/admin/patients/${other.patientId}`, '后台患者详情'],
      [`/api/v1/admin/feeling-logs?patientId=${other.patientId}`, '后台跨患者感受流'],
      [`/api/v1/feedbacks?patientId=${other.patientId}`, '后台反馈面'],
    ]
    for (const [path, why] of staffOnly) {
      const r = await callApi(page, 'GET', path, { token: ptok })
      console.log(`[t575-b5][存在的别人] ${why} ${short(r)} 期望=403/403（网关 roleAuthz）`)
      expect(r.status, `${why} 应 403`).toBe(403)
      expect(r.code, `${why} 网关拒绝的 code 应等于 HTTP 状态 403，实得 ${short(r)}`).toBe(403)
    }

    // D3 的另一组：同一批路径换成「不存在的患者」。鉴权排在存在性之前
    // （patient_404_t353_test.go:51-60 钉的就是这个序），所以患者令牌永远看不到 10404；
    // 换成管理端令牌才看得到。两组之差就是这两层的位置差，而不是「都返回拒绝」这一句空话。
    for (const [path, code, why] of [
      [`/api/v1/patients/${GHOST_PATIENT_ID}/feeling-logs`, 10403, 'user 域'],
      [`/api/v1/patients/${GHOST_PATIENT_ID}/realtime`, 30403, 'data 域'],
    ] as Array<[string, number, string]>) {
      const r = await callApi(page, 'GET', path, { token: ptok })
      console.log(
        `[t575-b5][不存在的别人·患者令牌] ${why} ${short(r)} 期望=${code}（鉴权先于存在性 ⇒ 不许泄露 10404）`,
      )
      expect(r.code, `${why} 用患者令牌打不存在的号应仍是越权码，实得 ${short(r)}`).toBe(code)
    }
    for (const [path, why] of [
      [`/api/v1/admin/patients/${GHOST_PATIENT_ID}`, '后台详情'],
      [`/api/v1/patients/${GHOST_PATIENT_ID}/feeling-logs`, '同址读'],
    ] as Array<[string, string]>) {
      const r = await callApi(page, 'GET', path, { token: adminToken! })
      console.log(`[t575-b5][不存在·管理端令牌] ${why} ${short(r)} 期望=404/10404`)
      expect(r.code, `${why} 换管理端令牌应见 10404（存在性检查在鉴权之后），实得 ${short(r)}`).toBe(10404)
    }

    // 唯一一条「患者令牌带着别人的号却回 200」的路径：/api/v1/alerts。
    // 它在网关是 publicPatterns，进 alert-service 后 patientId 被 X-User-Id 强制覆盖
    // （alert-service/internal/handler/public.go:184-190）。200 是授权行为，但必须证到「覆盖后只剩本人」。
    const forced = await callApi(page, 'GET', `/api/v1/alerts?patientId=${other.patientId}`, { token: ptok })
    expect(forced.code, `告警面应放行（200 是授权行为），实得 ${short(forced)}`).toBe(0)
    const pageData = forced.data as { list?: Array<Record<string, unknown>>; total?: number }
    const foreign = (pageData.list ?? []).filter((a) => a.patientId && a.patientId !== session!.patientId)
    console.log(
      `[t575-b5][强制覆盖] total=${pageData.total} 行数=${(pageData.list ?? []).length} 非本人行数=${foreign.length}`,
    )
    expect(foreign.length, '告警列表里不许出现别人的行').toBe(0)
    expect(
      pageData.total,
      '自建新患者没有任何告警 ⇒ 覆盖生效时 total 应为 0（带着别人的号参数却没读到别人的数据）',
    ).toBe(0)
  })

  test('25.2 同一份数据两侧同值：患者令牌与管理端令牌读到的是同一份，逐字节等值', async ({ page }) => {
    await realLogin(page)
    const adminToken = await getAuthToken(page)
    expect(adminToken, '真实模式登录应拿到 admin JWT').toBeTruthy()
    if (!session) throw new Error('25.2 依赖 25.1 的自建患者；25.1 没建成就立刻抛，不在别人留下的行上做对平')
    const pid = session.patientId
    const ptok = await patientToken(page)

    const selfPaths = [
      `/api/v1/patients/${pid}/feeling-logs`,
      `/api/v1/patients/${pid}/realtime`,
      `/api/v1/patients/${pid}/records?period=day&date=${LEGAL_DATE}`,
      `/api/v1/patients/${pid}/daily-wear?date=${LEGAL_DATE}`,
      `/api/v1/patients/${pid}/review-records`,
      `/api/v1/patients/${pid}/orthosis-plans`,
    ]
    for (const path of selfPaths) {
      const a = await callApi(page, 'GET', path, { token: ptok })
      const b = await callApi(page, 'GET', path, { token: adminToken! })
      const same = a.code === 0 && b.code === 0 && JSON.stringify(a.data) === JSON.stringify(b.data)
      console.log(
        `[t575-b4] ${path.replace(pid, tail(pid))} 患者侧 ${short(a)} | 后台侧 ${short(b)} | 逐字节同=${String(same)}`,
      )
      expect(a.code, `患者令牌读本人 ${path} 应放行，实得 ${short(a)}`).toBe(0)
      expect(b.code, `后台令牌读同一 ${path} 应放行，实得 ${short(b)}`).toBe(0)
      expect(same, `${path} 两侧读数应逐字节等值`).toBe(true)
    }

    // 档案逐字段对平：患者侧 GET /patient/profile vs 后台侧 GET /admin/patients/:id
    const prof = await callApi(page, 'GET', '/api/v1/patient/profile', { token: ptok })
    const profAdmin = await callApi(page, 'GET', `/api/v1/admin/patients/${pid}`, { token: adminToken! })
    expect(prof.code, `患者侧档案读应放行，实得 ${short(prof)}`).toBe(0)
    expect(profAdmin.code, `后台侧档案读应放行，实得 ${short(profAdmin)}`).toBe(0)
    const keys: Array<keyof PatientProfile> = [
      'patientId',
      'name',
      'age',
      'heightCm',
      'cobbAngle',
      'deviceId',
      'status',
      'teamId',
    ]
    const pf = prof.data as PatientProfile
    const pa = profAdmin.data as PatientProfile
    // 逐字段判等只准写成「两侧同名字段比同名字段」。上一版把它写成 `pf[k] === pa && pa[k]`
    // （先拿字段值去比整个对象），于是八格全报「异」而打印出来的两列明明同值 —— 那是尺子的错，
    // 不是现网的错；红在这种地方会把人引去查一个不存在的回归。
    const diffs = keys.filter((k) => pf[k] !== pa[k])
    // 患者号不进运行日志（其它字段照打），所以逐字段那一条要过 tail 一次
    const shown = (k: keyof PatientProfile, v: unknown): string =>
      k === 'patientId' && typeof v === 'string' ? tail(v) : JSON.stringify(v)
    console.log(
      `[t575-b4][档案逐字段] ${keys.map((k) => `${k}=${shown(k, pf[k])}|${shown(k, pa[k])}`).join(' ')}`,
    )
    expect(diffs, `档案八字段两侧应等值，不一致的是：${diffs.join(',')}`).toEqual([])
    expect(pf.patientId, '患者侧档案应是本人').toBe(pid)

    // 反证（派发单 D2 在只读面上的等价物）：患者令牌在本用例里只被允许打到本人号上；
    // 打不存在的号必须被越权面挡下（10403），而不是顺着参数读到别人的数据。
    const ghost = await callApi(page, 'GET', `/api/v1/patients/${GHOST_PATIENT_ID}/feeling-logs`, { token: ptok })
    console.log(`[t575-b4][反证] 患者令牌打不存在的号 ${short(ghost)}（期望 403/10403：鉴权先挡）`)
    expect(ghost.code, '患者令牌读不存在号应被越权面挡下').toBe(10403)

    // 告警没有 /patients/:id/alerts 这条路由 —— 与未注册路径同形（非 JSON 的 gin 404）。
    // 钉住这一条，是为了让 B4 的告警面只有一个入口（25.1 里那条强制覆盖的 /api/v1/alerts）。
    const noRoute = await callApi(page, 'GET', `/api/v1/patients/${pid}/alerts`, { token: ptok })
    console.log(`[t575-b4][路由形状] /patients/:id/alerts ${short(noRoute)}（期望与未注册路径同形）`)
    expect(noRoute.code, '患者告警不该有独立路由，回出 JSON 信封说明形状变了').toBe(null)
    expect(noRoute.status, '未注册形状应是 gin 404').toBe(404)
  })

  test('25.3 B1/B2/B3 的门面与闸面：写入在落库之前被拒，12 枚探针没有一枚回 code=0', async ({ page }) => {
    await realLogin(page)
    const adminToken = await getAuthToken(page)
    if (!session) throw new Error('25.3 依赖自建患者')
    const pid = session.patientId
    const ptok = await patientToken(page)

    // 审计先于开火：日期枚的形状必须自证（分隔符码点 + 两种判定），否则「靠形状拒掉」这句话没牙。
    expect(
      UNPARSEABLE_DATE.charCodeAt(4),
      `斜杠形那枚的分隔符码点应为 47，实得 ${UNPARSEABLE_DATE.charCodeAt(4)}`,
    ).toBe(47)
    expect(DATE_RE.test(LEGAL_DATE), '正对照：横杠形必须被判为合法，否则谓词恒假').toBe(true)
    expect(DATE_RE.test(UNPARSEABLE_DATE), '斜杠形必须被判为不合法').toBe(false)
    expect(feelingWouldInsert({ feeling: 'fitted', logDate: LEGAL_DATE }), '正对照：合法感受体应判会落库').toBe(true)
    expect(feedbackWouldInsert({ content: 'x' }), '正对照：合法反馈体应判会落库').toBe(true)

    // 设备锚：探针设备号必须不在架（列表条数是「这把尺扫过了在场面」的正对照）
    const dvList = await callApi(page, 'GET', '/api/v1/devices?page=1&pageSize=100', { token: adminToken! })
    expect(dvList.code, `设备列表应可读，实得 ${short(dvList)}`).toBe(0)
    const dvIds = (((dvList.data as { list?: Array<{ deviceId: string }> }).list) ?? []).map((d) => d.deviceId)
    console.log(
      `[t575-gate][设备锚] 列表条数=${dvIds.length}（在场正对照） 探针号在内=${String(dvIds.includes(GHOST_DEVICE_ID))}`,
    )
    expect(dvIds.includes(GHOST_DEVICE_ID), '探针设备号竟在架 ⇒ 绑定面会写到真设备上，立刻停手').toBe(false)

    const list = await callApi(page, 'GET', '/api/v1/admin/patients?page=1&pageSize=50', { token: adminToken! })
    const other = ((list.data as { list?: PatientProfile[] }).list ?? []).find((r) => r.patientId !== pid)
    if (!other) throw new Error('没有第二行患者，「别人」那一组越权探针打不出来')

    const legs: Array<{
      label: string
      path: string
      body: Record<string, unknown>
      token: string
      pred: ((b: Record<string, unknown>) => boolean) | null
      expect: string
    }> = [
      { label: 'B1 本人·缺 feeling', path: `/api/v1/patients/${pid}/feeling-logs`, body: { notes: 't575-gate' }, token: ptok, pred: feelingWouldInsert, expect: '400/10400' },
      { label: 'B1 本人·档位非法', path: `/api/v1/patients/${pid}/feeling-logs`, body: { feeling: 'bogus' }, token: ptok, pred: feelingWouldInsert, expect: '400/10400' },
      { label: 'B1 本人·部位名非法', path: `/api/v1/patients/${pid}/feeling-logs`, body: { feeling: 'discomfort', discomfortAreas: ['不存在的区'] }, token: ptok, pred: feelingWouldInsert, expect: '400/10400' },
      { label: 'B1 本人·日期不可解析', path: `/api/v1/patients/${pid}/feeling-logs`, body: { feeling: 'fitted', logDate: UNPARSEABLE_DATE }, token: ptok, pred: feelingWouldInsert, expect: '400/10400' },
      { label: 'B1 别人·合法体', path: `/api/v1/patients/${other.patientId}/feeling-logs`, body: { feeling: 'fitted' }, token: ptok, pred: null, expect: '403/10403' },
      { label: 'B2 本人·缺 content', path: '/api/v1/feedbacks', body: { patientId: pid, type: 'T575闸面' }, token: ptok, pred: feedbackWouldInsert, expect: '400/10400' },
      { label: 'B2 本人·状态枚举非法', path: '/api/v1/feedbacks', body: { patientId: pid, content: 'x', status: 'processing' }, token: ptok, pred: feedbackWouldInsert, expect: '400/10400' },
      { label: 'B2 别人·合法体', path: '/api/v1/feedbacks', body: { patientId: other.patientId, content: 'T575 闸面探针' }, token: ptok, pred: null, expect: '403/10403' },
      { label: 'B2 不存在的患者', path: '/api/v1/feedbacks', body: { patientId: GHOST_PATIENT_ID, content: 'T575 闸面探针' }, token: adminToken!, pred: null, expect: '404/10404' },
      { label: 'B3 患者令牌绑他人设备', path: `/api/v1/devices/${GHOST_DEVICE_ID}/bind`, body: { patientId: pid }, token: ptok, pred: null, expect: '403/403 网关 staff-only' },
      { label: 'B3 管理端绑不存在的设备', path: `/api/v1/devices/${GHOST_DEVICE_ID}/bind`, body: { patientId: pid }, token: adminToken!, pred: null, expect: '404/20404 设备不在架' },
      { label: 'B3 患者令牌解绑', path: `/api/v1/devices/${GHOST_DEVICE_ID}/unbind`, body: {}, token: ptok, pred: null, expect: '403/403 网关 staff-only' },
    ]
    const unsafe = legs.filter((l) => l.pred && l.pred(l.body)).map((l) => l.label)
    console.log(
      `[t575-gate][审计] 负对照=${legs.length - unsafe.length}/${legs.length} 会落库的枚数=${unsafe.length} 名单=${unsafe.join('|') || '无'}`,
    )
    expect(unsafe, '闸面负对照失效：这些体按源码谓词会真的落库，整批不许发射').toEqual([])

    const beforeProbe = await faceCounts(page, adminToken!)
    console.log(`[t575-gate][探针前基线] ${JSON.stringify(beforeProbe)}`)

    for (const l of legs) {
      const r = await callApi(page, 'POST', l.path, { token: l.token, body: l.body })
      console.log(`[t575-gate] ${l.label} ${short(r)} 期望=${l.expect} message=${r.message.slice(0, 24)}`)
      // 共同底线：这批一枚都不许 code=0。0 面即探针写进了库 ——
      // 本人写 ⇒ 患者 409 删不掉；别人写 ⇒ 动了既有患者数据（越界）；设备写 ⇒ 真绑定或真解绑。
      expect(r.code, `${l.label} 竞回 code=0，探针写进了 staging（${short(r)}）`).not.toBe(0)
      if (l.pred) expect(r.code, `${l.label} 应被参数校验拒掉 10400，实得 ${short(r)}`).toBe(10400)
    }

    const afterProbe = await faceCounts(page, adminToken!)
    console.log(`[t575-gate][探针后基线] ${JSON.stringify(afterProbe)}`)
    for (const key of Object.keys(beforeProbe)) {
      if (key === 'patients') continue // 自建患者那一行是本文件自己的授权写
      expect(
        afterProbe[key],
        `守恒尺：${key} 面从 ${beforeProbe[key]} 动到了 ${afterProbe[key]} ⇒ 上面某枚探针写进了库`,
      ).toBe(beforeProbe[key])
    }

    // 自建患者本人面必须还是 0 行：这是「闸面没落库」的直接证据，也是删得掉的前提
    const own = await callApi(page, 'GET', `/api/v1/patients/${pid}/feeling-logs`, { token: ptok })
    expect(Array.isArray(own.data) ? own.data.length : null, `本人感受日志应仍是 0 行，实得 ${short(own)}`).toBe(0)
  })

  test.afterAll(async ({ browser }) => {
    if (!session) return
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    try {
      await realLogin(page)
      const token = await getAuthToken(page)
      if (!token) throw new Error('afterAll 取不到 admin JWT，无法还原')
      const pid = session.patientId
      const del = await callApi(page, 'DELETE', `/api/v1/admin/patients/${pid}`, { token })
      console.log(`[t575][还原] DELETE ${tail(pid)} ${short(del)} message=${del.message.slice(0, 40)}`)
      expect(
        del.code,
        `还原删除未成功 ⇒ staging 上留了一行 ${E2E_PATIENT_NAME_PREFIX} 数据（患者号后四位 ${pid.slice(-4)}），需人工清：${short(del)}`,
      ).toBe(0)
      const gone = await callApi(page, 'GET', `/api/v1/admin/patients/${pid}`, { token })
      expect(gone.code, `删后详情应回 10404（硬删），实得 ${short(gone)}`).toBe(10404)
      const after = await callApi(page, 'GET', '/api/v1/admin/patients?page=1&pageSize=1', { token })
      const now = Number((after.data as { total?: number }).total)
      if (baselineTotal !== null) {
        console.log(`[t575][还原] 患者 total 删后=${now}，建档前快照=${baselineTotal}`)
        expect(now, `行数守恒：删后 total=${now} 应回到建档前快照 ${baselineTotal}`).toBe(baselineTotal)
      } else {
        console.log(`[t575][还原] 无建档前快照可比对（25.1 在建号前就红了），只留删后读数 total=${now}`)
      }
    } finally {
      await ctx.close()
    }
  })
})
