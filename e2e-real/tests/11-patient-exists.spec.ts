import { test, expect, type APIRequestContext } from '@playwright/test'
import { requireDeployedBuild } from '../deploy-guard'

/**
 * T353 · 跨服务「查无此人」判定防回归（真实模式 / staging，全 GET 只读）
 *
 * 要盯的缺陷：按 patientId 查询的读端点把「这个患者不存在」和「患者存在但还没有数据」都回成
 * 200 + 空列表，前端只能显示「暂无数据」，患者录错 ID / 档案被删后调用方完全无从分辨。
 * T340 已在 data-service 定下口径：404 + 业务码 10404，且判定排在水平鉴权之后
 * （存在性不泄露给无权调用方）。T353 把同一口径铺到 user-service / msg-service 余下的入口。
 *
 * 为什么分三组、其中两组带部署守卫：
 *   · 11.1 的 4 条是 T340 行为，staging 上已生效（2026-09-23 现网实测 404 + 10404）⇒ 无条件真跑；
 *   · 11.2 / 11.3 是本轮（T353）改的 user-service / msg-service，本 PR 合并并部署到 staging 之前
 *     线上仍是旧行为（2026-09-23 现网实测：orthosis-plans 回 200 空列表、subscription-quota 回默认
 *     额度 3/3）⇒ 走 e2e-real/deploy-guard.ts：PR 阶段缺行为 ⇒ 显式 post-deploy 跳过并写进 job
 *     summary；定时 / 手动阶段（E2E_POST_DEPLOY_STRICT=1）仍缺 ⇒ 判红。判据一条没放宽，
 *     也没加 continue-on-error / `|| true`。
 *   · 两条链路分开探，是因为 user-service 与 msg-service 的部署进度可以不同——一起跳会把
 *     「其中一边其实已经上了」这件事盖掉。
 *
 * probe 的边界（防后来人把「跳过」读成「能掩盖回归」）：
 *   探测只问「该链路的 404 行为在不在 staging」，取状态码，不碰下面任何一条断言；
 *   真判据（业务码 10404、data 为 null、现存患者仍 200）全在守卫之后跑。
 *   ⇒ 有人把判定改成「所有患者都 404」这种坏修法时，probe 仍为 true，但「现存患者」那半条会红；
 *     PR 之后若整体回归，定时阶段 probe 变 false 也会以「缺行为」判红，不会静默放行。
 *
 * 数据纪律：全量只读 GET，不建不改不删；不存在患者用固定 P99999999（现网实测 patients 表无此 ID），
 * 现存患者从 GET /api/v1/admin/patients 取当次首行，不硬编码 seed 患者 ID。
 *
 * ── T350 返工追加的 11.4（派发单 c 项：本文件原来只登运营账号，医护身份一层从不覆盖）──────
 * 三条被返工的端点（daily-wear / feeling-logs / review-records）在返工前后都回 403，
 * 状态码差探不出部署与否 —— 返工前是 T264 的 self-only 403，返工后是团队范围 403。
 * 故 probe 按【文案】探（新文案含 "out of your data scope"，旧文案是 "your own ..."），
 * 它仍只做存在性探测、不含业务判据，符合本文件顶部对 probe 的边界。
 *
 * 覆盖缺口要说清（现网数据决定的，不是用例写松）：staging 的 doctor_li 所在团队当前零患者，
 * 所以「本团队患者 200 读回」这条在现网拿不到样本 ⇒ 该腿按数据可得性条件执行，
 * 拿不到时在报告 annotations 与 run 日志里显式登记「未跑」。一旦有人给该团队建档，自动转真跑。
 * 该缺口的代码侧对位判据（同团队可读、跨团队零触库）在单测层：
 *   services/user-service/internal/handler/team_scope_t350_rework_test.go
 *   services/data-service/internal/handler/team_scope_t350_test.go
 */

const GONE_PID = 'P99999999'

/** T340 已上线口径（data-service 4 条，无条件真跑） */
const DATA_ENDPOINTS: Array<[string, (pid: string) => string]> = [
  ['records', (p) => `/api/v1/patients/${p}/records?date=2026-09-01`],
  ['realtime', (p) => `/api/v1/patients/${p}/realtime`],
  ['health-reports', (p) => `/api/v1/patients/${p}/health-reports?pageSize=5`],
  ['daily-wear', (p) => `/api/v1/patients/${p}/daily-wear?days=7`],
]

/** 本轮 T353 新增判定：user-service 3 条（尚未部署 ⇒ 带部署守卫） */
const USER_ENDPOINTS: Array<[string, (pid: string) => string]> = [
  ['orthosis-plans', (p) => `/api/v1/patients/${p}/orthosis-plans`],
  ['feeling-logs', (p) => `/api/v1/patients/${p}/feeling-logs`],
  ['review-records', (p) => `/api/v1/patients/${p}/review-records`],
]

/** 本轮 T353 新增判定：msg-service 3 条（尚未部署 ⇒ 带部署守卫） */
const MSG_ENDPOINTS: Array<[string, (pid: string) => string]> = [
  ['subscription-quota', (p) => `/api/v1/patients/${p}/subscription-quota`],
  ['wear-reminder', (p) => `/api/v1/patients/${p}/wear-reminder`],
  ['notifications', (p) => `/api/v1/patients/${p}/notifications?pageSize=5`],
]

/** T350 返工（D-1/D-2/D-3）纳入团队推导的三条：医护身份腿只跑这三条 */
const REWORK_ENDPOINTS: Array<[string, (pid: string) => string]> = [
  ['daily-wear', (p) => `/api/v1/patients/${p}/daily-wear?days=7`],
  ['feeling-logs', (p) => `/api/v1/patients/${p}/feeling-logs`],
  ['review-records', (p) => `/api/v1/patients/${p}/review-records`],
]

/** staging 预置医护账号（无团队归属 ⇒ 全部患者对其越界，见 11.4 的覆盖缺口说明） */
const DOCTOR_USERNAME = 'doctor_li'

/** 团队范围 403 的新文案（denyCrossTeam），旧文案是 T264 self-only 的 "your own ..." */
const SCOPE_DENY_TEXT = 'out of your data scope'

async function realToken(
  req: APIRequestContext,
  username: string = 'ops_admin',
): Promise<string> {
  const res = await req.post('/api/v1/auth/login', {
    data: { username, password: 'admin123' },
    headers: { 'Content-Type': 'application/json' },
  })
  expect(res.status(), `${username} 登录应 200`).toBe(200)
  const body = await res.json()
  expect(body.code, '登录 code 应为 0').toBe(0)
  const token = body?.data?.token
  expect(typeof token, '应拿到 JWT').toBe('string')
  return token as string
}

/** 取当次库里真实存在的一名患者（只读，不硬编码 seed ID） */
async function existingPatient(req: APIRequestContext, token: string): Promise<string> {
  const list = await patientIds(req, token, 1)
  expect(list.length, 'staging 应至少有一名患者可作「存在但无数据」对照').toBeGreaterThan(0)
  return list[0]
}

/** 某令牌可见的患者 ID 列表（医护令牌由服务端按 doctors.team_id 过滤，不接受入参） */
async function patientIds(req: APIRequestContext, token: string, pageSize: number): Promise<string[]> {
  const res = await req.get(`/api/v1/admin/patients?page=1&pageSize=${pageSize}`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  expect(res.status(), 'GET /api/v1/admin/patients 应 200').toBe(200)
  const json = await res.json()
  const list: Array<{ patientId?: unknown }> = Array.isArray(json?.data?.list) ? json.data.list : []
  return list.map((p) => String(p?.patientId ?? '')).filter((s) => s !== '')
}


/**
 * 「查无此人 → 404 + 10404 + data:null」与「现存患者 → 仍 200」两条判据逐端点跑。
 * 只在部署守卫之后调用，故这里每一条都是硬断言；偏差累计成一行清单，
 * 免得 7 个端点里第 3 个红掉就看不到第 4~7 个的现状。
 */
async function assertNotFoundIs404(
  req: APIRequestContext,
  token: string,
  livePid: string,
  group: Array<[string, (pid: string) => string]>,
): Promise<void> {
  const headers = { Authorization: `Bearer ${token}` }
  const badGone: string[] = []
  const badLive: string[] = []

  for (const [name, build] of group) {
    const gone = await req.get(build(GONE_PID), { headers })
    const goneBody = await gone.json().catch(() => null)
    // 只打方法 + URL + 结论，供 run 日志反查「确实打了 staging」（不打 header，避免带出 Authorization）
    console.log(`[e2e-real][t353] GET ${gone.url()} -> HTTP ${gone.status()} code=${goneBody?.code}`)
    if (gone.status() !== 404) badGone.push(`${name} HTTP=${gone.status()}`)
    if (goneBody?.code !== 10404) badGone.push(`${name} code=${goneBody?.code}`)
    if (goneBody?.data !== null) badGone.push(`${name} data 非 null`)

    const live = await req.get(build(livePid), { headers })
    const liveBody = await live.json().catch(() => null)
    console.log(`[e2e-real][t353] GET ${live.url()} -> HTTP ${live.status()} code=${liveBody?.code}`)
    if (live.status() !== 200 || liveBody?.code !== 0) {
      badLive.push(`${name} HTTP=${live.status()} code=${liveBody?.code}`)
    }
  }

  expect(badGone, `不存在患者应 404 + 业务码 10404 + data:null，实际偏差：${badGone.join('；')}`).toEqual([])
  expect(badLive, `现存患者应仍 200 + code 0（存在性判定不得把有档案的一起挡掉）：${badLive.join('；')}`).toEqual([])
}

/** 存在性探测：只看该链路的 404 行为是否已在 staging 生效，不写业务判据 */
async function probe404(req: APIRequestContext, token: string, path: string): Promise<boolean> {
  const res = await req.get(path, { headers: { Authorization: `Bearer ${token}` } })
  return res.status() === 404
}

/**
 * 存在性探测（文案版，T350 返工用）：返工前后对医护都是 403，状态码探不出部署与否
 * （旧的是 T264 self-only，新的是团队范围），故这里探「拒绝文案是不是团队范围口径」。
 * 同样只做存在性探测，不含业务判据。
 */
async function probeDenyText(req: APIRequestContext, token: string, path: string): Promise<boolean> {
  const res = await req.get(path, { headers: { Authorization: `Bearer ${token}` } })
  const body = await res.json().catch(() => null)
  return String(body?.message ?? '').includes(SCOPE_DENY_TEXT)
}

/** 把调用者自己填的患者号折成占位符，用于比对两种拒绝是否「同形」 */
function foldPid(message: string, pid: string): string {
  return message.split(pid).join('<pid>')
}

/**
 * 医护身份腿（T350 返工派发单 c 项）：三条端点在患者号维度对 ROLE_DOCTOR 一律 403 且同形，
 * 本团队患者必须读得回 200 —— 后面两格按现网数据可得性执行，拿不到样本由调用方登记缺口。
 * 偏差同样累计成清单，一条端点红掉不挡其余端点的现状。
 */
async function assertDoctorScope(
  req: APIRequestContext,
  docToken: string,
  ownPids: string[],
  outPid: string,
  group: Array<[string, (pid: string) => string]>,
): Promise<void> {
  const headers = { Authorization: `Bearer ${docToken}` }
  const ownPid = ownPids[0] ?? ''
  const badGone: string[] = []
  const badOut: string[] = []
  const badShape: string[] = []
  const badLive: string[] = []

  for (const [name, build] of group) {
    const gone = await req.get(build(GONE_PID), { headers })
    const goneBody = await gone.json().catch(() => null)
    console.log(`[e2e-real][t350r] GET ${gone.url()} -> HTTP ${gone.status()} code=${goneBody?.code}`)
    if (gone.status() !== 403) badGone.push(`${name} HTTP=${gone.status()}`)
    if (goneBody?.data !== null) badGone.push(`${name} data 非 null`)
    if (!String(goneBody?.message ?? '').includes(SCOPE_DENY_TEXT)) {
      badGone.push(`${name} 拒绝文案不是团队范围口径：${goneBody?.message}`)
    }

    if (outPid) {
      const out = await req.get(build(outPid), { headers })
      const outBody = await out.json().catch(() => null)
      console.log(`[e2e-real][t350r] GET ${out.url()} -> HTTP ${out.status()} code=${outBody?.code}`)
      if (out.status() !== 403) badOut.push(`${name} HTTP=${out.status()}`)
      if (
        foldPid(String(outBody?.message ?? ''), outPid) !==
        foldPid(String(goneBody?.message ?? ''), GONE_PID)
      ) {
        badShape.push(`${name} 跨团队「${outBody?.message}」与查无此人「${goneBody?.message}」不同形`)
      }
    }

    if (ownPid) {
      const live = await req.get(build(ownPid), { headers })
      const liveBody = await live.json().catch(() => null)
      console.log(`[e2e-real][t350r] GET ${live.url()} -> HTTP ${live.status()} code=${liveBody?.code}`)
      if (live.status() !== 200 || liveBody?.code !== 0) {
        badLive.push(`${name} HTTP=${live.status()} code=${liveBody?.code}`)
      }
    }
  }

  expect(badGone, `医护读不存在患者应折进 403（永不出 404），实际偏差：${badGone.join('；')}`).toEqual([])
  expect(badOut, `医护读越界患者应 403，实际偏差：${badOut.join('；')}`).toEqual([])
  expect(badShape, `跨团队与查无此人必须同形，否则患者号存在性可枚举：${badShape.join('；')}`).toEqual([])
  expect(
    badLive,
    `医护读本团队患者应 200 + code 0（D-1/D-2/D-3 要修的就是这一格），实际偏差：${badLive.join('；')}`,
  ).toEqual([])
}

function t350rGap(desc: string): void {
  console.log(`[e2e-real][t350r] 覆盖缺口 —— ${desc}`)
  test.info().annotations.push({ type: 'coverage-gap', description: desc })
}

test.describe('11-按 patientId 查询的存在性判定', () => {
  let token = ''
  let livePid = ''

  test.beforeEach(async ({ page }) => {
    token = await realToken(page.request)
    livePid = await existingPatient(page.request, token)
    expect(livePid).not.toBe(GONE_PID)
  })

  test('11.1 data-service 四条（T340 口径）不存在患者 404、现存患者 200', async ({ page }) => {
    await assertNotFoundIs404(page.request, token, livePid, DATA_ENDPOINTS)
  })

  test('11.2 user-service 三条（T353）不存在患者 404、现存患者 200', async ({ page }) => {
    await requireDeployedBuild(page, {
      marker: 'T353-user-patient-404',
      why: 'GET /patients/:id/orthosis-plans 仍回 200 空列表',
      probe: () => probe404(page.request, token, `/api/v1/patients/${GONE_PID}/orthosis-plans`),
    })
    await assertNotFoundIs404(page.request, token, livePid, USER_ENDPOINTS)
  })

  test('11.3 msg-service 三条（T353）不存在患者 404、现存患者 200', async ({ page }) => {
    await requireDeployedBuild(page, {
      marker: 'T353-msg-patient-404',
      why: 'GET /patients/:id/subscription-quota 仍回默认额度 200',
      probe: () => probe404(page.request, token, `/api/v1/patients/${GONE_PID}/subscription-quota`),
    })
    await assertNotFoundIs404(page.request, token, livePid, MSG_ENDPOINTS)
  })

  test('11.4 doctor_li（T350 返工 D-1/D-2/D-3）：患者号折进 403、本团队患者读得回', async ({ page }) => {
    const docToken = await realToken(page.request, DOCTOR_USERNAME)
    await requireDeployedBuild(page, {
      marker: 'T350-doctor-scope',
      why: `daily-wear 对医护仍吃 T264 self-only 403（文案不含「${SCOPE_DENY_TEXT}」）`,
      probe: () =>
        probeDenyText(page.request, docToken, `/api/v1/patients/${GONE_PID}/daily-wear?days=7`),
    })

    // 「本团队患者」不硬编码：由服务端按 doctors.team_id 过滤后的患者列表给出
    const ownPids = await patientIds(page.request, docToken, 100)
    const opsPids = await patientIds(page.request, token, 100)
    const outPid = ownPids.length
      ? opsPids.find((p) => !ownPids.includes(p)) ?? ''
      : livePid // 医护团队零患者 ⇒ 运营取到的任一名患者都是越界样本

    if (!ownPids.length) {
      t350rGap(
        'T350-doctor-scope：doctor_li 所在团队在 staging 当前零患者 ⇒ 「本团队患者 200 读回」一格' +
          '现网无样本，本 run 未跑（该格的代码侧对位判据在 handler 单测）；有人为该团队建档后此格自动转真跑',
      )
    } else if (!outPid) {
      t350rGap(
        'T350-doctor-scope：运营可见患者全部落在该医护团队内 ⇒ 「跨团队 403 与查无此人同形」一格无样本，本 run 未跑',
      )
    }

    await assertDoctorScope(page.request, docToken, ownPids, outPid, REWORK_ENDPOINTS)
  })
})
