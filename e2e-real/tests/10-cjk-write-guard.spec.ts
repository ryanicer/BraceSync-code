import { test, expect, type APIRequestContext } from '@playwright/test'

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
  const res = await req.post('/api/v1/auth/login', {
    data: { username: 'ops_admin', password: 'admin123' },
    headers: { 'Content-Type': 'application/json' },
  })
  expect(res.status(), '登录应 200').toBe(200)
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

  test.beforeEach(async ({ request }) => {
    token = await realToken(request)
  })

  /** 兜底清理：断言判红时用例内的 DELETE 不会执行，这里按唯一前缀把自建行清干净 */
  test.afterEach(async ({ request }) => {
    if (!token) return
    const headers = { Authorization: `Bearer ${token}` }
    if (createdTeamId) {
      await request.delete(`/api/v1/teams/${createdTeamId}`, { headers }).catch(() => null)
      createdTeamId = null
    }
    const rows = await request.get('/api/v1/teams', { headers }).then((r) => (r.ok() ? r.json() : null)).catch(() => null)
    for (const r of rows?.data ?? []) {
      if (String(r.name ?? '').includes(NAME_PREFIX)) {
        await request.delete(`/api/v1/teams/${r.teamId}`, { headers }).catch(() => null)
      }
    }
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

    // ── 4) 自带还原：删掉自建行，确认前缀残留 0 行、总数回到原点
    const del = await request.delete(`/api/v1/teams/${createdTeamId}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(del.status(), 'DELETE 自建团队应 200').toBe(200)
    createdTeamId = null

    const after = await teamRows(request, token)
    expect(after.filter((r) => String(r.name ?? '').includes(NAME_PREFIX)).length, 'T334CJK-* 残留应为 0 行').toBe(0)
    expect(after.length, '团队总数应回到非本用例原点（seed 未被改动）').toBe(seedCount)
  })
})
