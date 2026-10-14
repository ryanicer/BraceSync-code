import { test, expect, type APIRequestContext } from '@playwright/test'
// T506：口令经凭据门取数；登录 401 时把失败信息定性为凭据漂移，而不是留给下游断言差异
import {
  credentialDriftMessage,
  realPassword,
  releaseTeamMembers,
  restoreTeamMembers,
  expectDoctorsRestored,
} from '../real-helpers'

/**
 * T334 · 中文写入字节面防回归（真实模式 / staging）
 *
 * 由来：T244 走查（Alice）见「患者沟通」回复显示为问号 → T331（Andy）定性为【写入端编码纪律】：
 *   Windows PowerShell 5.1 的 Invoke-RestMethod 在「body 传字符串 + ContentType 不带 charset」时
 *   按 ASCII 编码请求体，中文在离手前即被替换成 0x3F；服务端与 DB 照收照存（不是后端缺陷）。
 *   而 T052 当时的验收记录把那一行写成「中文完整、无截断」——因为肉眼看着像填过了。
 *   ⇒ 结论：中文写入的验收必须核字节面，不能以页面显示为准（见 docs/tasks/ella/T052 勘误小节）。
 *
 * 本用例盯的是「链路上任何一环把中文换成问号/替换符」：客户端姿势、网关、服务、DB、读回 DTO
 * 任一处丢字，第 3 步的字节断言即判红。
 *
 * 数据纪律：只在【本用例自建】的团队行上做写操作（唯一命名 T334CJK-*），同一次运行内 DELETE 还原；
 *   不碰共享 seed（TEAM01/02/03），不碰 feedbacks（该端点单向、无 un-process 路由，见 07 spec 7.3 的停跑理由）。
 *   写入 → 断言 → 删除 → 确认回到原点，四步在一个用例里闭环：断言中途判红时删除不会执行，
 *   由 afterEach 按前缀兜底清扫，保证 staging 不留孤儿行。
 *
 * 判红自证开关（不是产品特性）：E2E_REAL_CJK_MUTATE=ascii-lossy 时，本用例故意用「ASCII 有损」
 *   姿势发体（等价于 T331 坐实的缺陷客户端），此时候选断言必须转红；若仍绿 ⇒ 断言无判据力。
 */

const CJK_TAIL = '康复医学科门诊复查' // 9 个汉字 → UTF-8 27 字节
const NAME_PREFIX = 'T334CJK-'
const MUTATE = process.env.E2E_REAL_CJK_MUTATE === 'ascii-lossy'

async function realToken(req: APIRequestContext): Promise<string> {
  const password = realPassword('10-cjk.realToken')
  const res = await req.post('/api/v1/auth/login', {
    data: { username: 'ops_admin', password },
    headers: { 'Content-Type': 'application/json' },
  })
  const status = res.status()
  expect(status, status === 401 ? credentialDriftMessage('ops_admin', status, password) : '登录应 200').toBe(200)
  const body = await res.json()
  expect(body.code, '登录 code 应为 0').toBe(0)
  const token = body?.data?.token
  expect(typeof token, '应拿到 JWT').toBe('string')
  return token as string
}

/** GET /api/v1/teams 的形状：{code,data:[{teamId,name,memberCount,patientCount}]}（无 description / leader） */
async function teamRows(req: APIRequestContext, token: string): Promise<any[]> {
  const res = await req.get('/api/v1/teams', { headers: { Authorization: `Bearer ${token}` } })
  expect(res.status(), 'GET /api/v1/teams 应 200').toBe(200)
  const json = await res.json()
  expect(json.code, '团队列表 code 应为 0').toBe(0)
  expect(Array.isArray(json.data), '团队列表 data 应为数组').toBe(true)
  return json.data ?? []
}

/** 显式 UTF-8 字节 body（T334 统一姿势）；MUTATE 开关打开时故意退化成 ASCII 有损，用来验断言会红 */
function utf8Body(payload: Record<string, string>): Buffer {
  const json = JSON.stringify(payload)
  return MUTATE ? Buffer.from(json.replace(/[^\x00-\x7F]/g, '?'), 'latin1') : Buffer.from(json, 'utf8')
}

const cjkCount = (s: string) => (s.match(/[\u4e00-\u9fff]/g) ?? []).length
const qmarkCount = (s: string) => (s.match(/\?/g) ?? []).length
const REPL = String.fromCharCode(0xfffd)
const replCount = (s: string) => s.split(REPL).length - 1

test.describe('10-中文写入字节面防回归', () => {
  let token = ''
  let createdTeamId: string | null = null
  // T682 迁回基数：建团队前负责人原本的归属（doctorId → teamId | null），见 real-helpers 说明
  let priorDoctorTeams: Record<string, string | null> = {}

  test.beforeEach(async ({ request }) => {
    token = await realToken(request)
  })

  /** 兜底清理：断言判红时用例内的 DELETE 不会执行，这里按唯一前缀把自建行清干净 */
  test.afterEach(async ({ request }) => {
    if (!token) return
    const headers = { Authorization: `Bearer ${token}` }
    // T682 B案：DELETE 前必须先解除负责人引用——POST /teams 会把 leader 迁进新队
    // （repo/pg.go:1874），DeleteTeam 见引用即拒（handler.go:3024 ⇒ 409/10409）。
    // 旧写法「delete 失败就 .catch(()=>null) 吞掉」⇒ 每轮红都留一行 T334CJK-* 孤儿
    //（本次开工实测：staging 残留 TEAM2622059968 / T334CJK-785368康复医学科门诊复查）。
    // 解除后必须「迁回」原团队，否则把 doctor_li 留在无队状态（2.4/29.1 会连累判红）。
    const purge = async (teamId: string) => {
      const rel = await releaseTeamMembers(request, token, teamId)
      const res = await request.delete(`/api/v1/teams/${teamId}`, { headers }).catch(() => null)
      if (!res || res.status() !== 200) {
        console.log(`[10-cleanup] 删除自建团队 ${teamId} 未 200：status=${res?.status()}`)
      }
      const restored = await restoreTeamMembers(request, token, priorDoctorTeams, rel.results)
      for (const r of restored) {
        if (r.status !== null && r.status !== 200) {
          console.log(`[10-cleanup] 迁回 ${r.memberId} → ${r.toTeamId} 未 200：status=${r.status}`)
        }
      }
    }
    if (createdTeamId) {
      await purge(createdTeamId)
      createdTeamId = null
    }
    const rows = await request.get('/api/v1/teams', { headers }).then((r) => (r.ok() ? r.json() : null)).catch(() => null)
    for (const r of rows?.data ?? []) {
      if (String(r.name ?? '').includes(NAME_PREFIX)) await purge(String(r.teamId))
    }
    // 清扫自证（T682）：兜底跑完必须回读「前缀残留 0 行」——上一版这里只删不证，
    // 于是 409 静默失败，把孤儿留在 staging 连累了后面每一轮的残留计数。
    const residue = await request
      .get('/api/v1/teams', { headers })
      .then((r) => (r.ok() ? r.json() : null))
      .catch(() => null)
    const left = (residue?.data ?? []).filter((r: any) => String(r.name ?? '').includes(NAME_PREFIX))
    expect(
      left.map((r: any) => `${r.teamId}/${r.name}`),
      'afterEach 清扫后 T334CJK-* 残留应为 0 行（非 0 ⇒ 解除引用或 DELETE 未生效，须报红）',
    ).toEqual([])
  })

  test('10.1 写入中文 → 读回核字节面 → 删除还原到原点', async ({ request }) => {
    // ── 0) 原点快照（用于最后确认 seed 未被改动）
    const baseline = await teamRows(request, token)
    const seedCount = baseline.filter((r) => !String(r.name ?? '').includes(NAME_PREFIX)).length
    expect(seedCount, 'staging 团队 seed 应 ≥3（T051 seed：TEAM01/02/03）').toBeGreaterThanOrEqual(3)

    // ── 1) leader 取库里真实存在的医生（后端 CreateTeam 校验 leader 存在性）
    const doctors = await request.get('/api/v1/doctors', { headers: { Authorization: `Bearer ${token}` } })
    expect(doctors.status(), 'GET /api/v1/doctors 应 200').toBe(200)
    const doctorRows: any[] = (await doctors.json())?.data ?? []
    expect(doctorRows.length, 'staging 应有医生可作负责人').toBeGreaterThan(0)
    const leaderId = String(doctorRows[0].doctorId ?? doctorRows[0].id ?? '')
    expect(leaderId.length, '医生 doctorId 非空').toBeGreaterThan(0)
    // T682 迁回基数：从这同一次 GET /doctors 读数里取每位医护的当前归属（建队后就被覆写了）
    priorDoctorTeams = Object.fromEntries(
      doctorRows.map((r: any) => [String(r.doctorId ?? r.id ?? ''), r.teamId ?? null]),
    )

    // ── 2) 唯一命名（ASCII 前缀保证即使中文被吞也能定位、能删干净）+ 显式 UTF-8 字节 body 写入
    const ts = Date.now().toString().slice(-6)
    const teamName = `${NAME_PREFIX}${ts}${CJK_TAIL}`
    const sentBytes = Buffer.byteLength(teamName, 'utf8')
    expect(teamName.length, '夹具自校验：字符数 = 前缀 + 6 位时间戳 + 9 汉字').toBe(
      NAME_PREFIX.length + 6 + CJK_TAIL.length,
    )
    expect(sentBytes, '夹具自校验：UTF-8 字节数须大于字符数（否则本用例证不了多字节）').toBeGreaterThan(teamName.length)

    const create = await request.post('/api/v1/teams', {
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json; charset=utf-8',
      },
      data: utf8Body({ name: teamName, leader: leaderId, description: CJK_TAIL }),
    })
    const createJson = await create.json().catch(() => null)
    expect(create.status(), `POST /api/v1/teams 应 200，实际：${JSON.stringify(createJson)}`).toBe(200)
    expect(createJson?.code, '建团队 code 应为 0').toBe(0)
    createdTeamId = String(createJson?.data?.teamId ?? '')
    expect(createdTeamId.length, '应返回 teamId').toBeGreaterThan(0)

    // ── 3) 读回核字节面（不看页面显示，只看库回给客户端的字节）
    const mine = (await teamRows(request, token)).filter((r) => String(r.teamId) === createdTeamId)
    expect(mine.length, '列表里应且只应找到 1 行自建团队').toBe(1)
    const readName = String(mine[0].name)

    const readBytes = Buffer.byteLength(readName, 'utf8')
    // 逐条判据：链路上任何一环丢字都会命中
    expect(qmarkCount(readName), '读回不得含 0x3F 问号（中文被有损编码替换的特征）').toBe(0)
    expect(replCount(readName), '读回不得含 U+FFFD 替换符').toBe(0)
    expect(cjkCount(readName), '读回应保留全部 9 个汉字（多字节字符数）').toBe(CJK_TAIL.length)
    expect(readName.length, '读回字符数应与写入一致').toBe(teamName.length)
    expect(readBytes, '读回 UTF-8 字节数应与写入完全相等').toBe(sentBytes)
    expect(readBytes, '字节数 > 字符数 ⇒ 多字节确实存活到库里').toBeGreaterThan(readName.length)
    expect(readName, '读回字符串应与写入逐字符相等').toBe(teamName)
    if (!MUTATE) {
      expect(
        Buffer.from(readName, 'utf8').equals(Buffer.from(teamName, 'utf8')),
        'UTF-8 字节序列应完全相同',
      ).toBe(true)
    }

    // ── 4) 自带还原：先解除负责人引用（T682 B案），再删自建行，确认自建行消失、seed 未被改动
    // 不解除就 DELETE ⇒ 恒 409（T682 修前实测读数：Expected 200 / Received 409，spec:151）。
    const selfId = createdTeamId
    const rel = await releaseTeamMembers(request, token, selfId)
    expect(rel.listStatus, `GET /teams/${selfId}/members 应 200（实得 ${rel.listStatus}）`).toBe(200)
    for (const r of rel.results) {
      expect(r.status, `解除${r.memberType}引用 ${r.memberId} 应 200（实得 ${r.status}）`).toBe(200)
    }
    const del = await request.delete(`/api/v1/teams/${selfId}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(del.status(), `解除引用后 DELETE 自建团队应 200，实得 ${del.status()}`).toBe(200)
    createdTeamId = null

    // 迁回 + 回读（T682）：负责人被 POST /teams 迁进来、又被解除置 NULL，
    // 不还回去就是「把 doctor_li 测成无团队医护」⇒ 2.4/29.1 连红。
    const restored = await restoreTeamMembers(request, token, priorDoctorTeams, rel.results)
    for (const r of restored) {
      expect(
        r.status === null || r.status === 200,
        `迁回${r.memberType} ${r.memberId} → ${r.toTeamId} 应 200 或本无归属（实得 ${r.status}）`,
      ).toBe(true)
    }
    await expectDoctorsRestored(request, token, restored)

    // 反证走接口面：硬删后详情应 404，列表里也不该再有这一行
    const gone = await request.get(`/api/v1/teams/${selfId}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(gone.status(), `删除后 GET /teams/${selfId} 应 404，实得 ${gone.status()}`).toBe(404)
    const after = await teamRows(request, token)
    expect(after.filter((r) => String(r.teamId) === selfId).length, `自建团队 ${selfId} 应从列表消失`).toBe(0)
    // T682 口径修正：残留判据收窄到「我这一行」。全前缀计数会把上一轮红遗留的 T334CJK-* 孤儿
    // 算成我的红（13:56 实跑就是栽在这：残 1 行 = 上午那轮 409 留下的 TEAM2622059968），
    // 而孤儿归 afterEach 的前缀兜底清扫负责，并由它自证归零（见下方 afterEach）。
    expect(
      after.filter((r) => !String(r.name ?? '').includes(NAME_PREFIX)).length,
      '非本前缀团队总数应回到原点（seed 未被改动）',
    ).toBe(seedCount)
  })
})
