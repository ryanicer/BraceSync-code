import { readFileSync } from 'node:fs'
import { test, expect, type APIRequestContext } from '@playwright/test'
import { requireDeployedBuild } from '../deploy-guard'
// T506：口令经凭据门取数；登录 401 时把失败信息定性为凭据漂移，而不是留给下游断言差异
import { credentialDriftMessage, realPassword } from '../real-helpers'

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
 * T528 把这里的 probe 判据从「单看文案」升级为三元组，三项缺一即不成立：
 *   ① HTTP 403
 *   ② 响应体业务码 = 该域的越权码（daily-wear 属 data-service ⇒ 30403；码表按域分配，
 *      user-service 的 feeling-logs / review-records 是 10403 —— 见 REWORK_ENDPOINTS 第三列）
 *   ③ 服务端技术日志的同窗窗口里，该端点那一行含 "out of your data scope"
 *      （配对键是「同窗 + 同一不含 query 的路径段」，不是 request_id —— 它每请求随机，
 *       窗外语料永远配不上；本次的 request_id 仍打进行日志，供按窗反查）
 * 为什么必须补 ③：返工前后对医护都回 403 + 越权码（2026-10-02 现网两版实测同形），①② 单独
 * 探不出部署与否；而自 T464 起响应体 message 一律换成中文码表文案，旧判据「文案含
 * out of your data scope」当场恒假 —— 那句英文从此只存在于技术日志。
 * 它仍只做存在性探测、不含业务判据，符合本文件顶部对 probe 的边界。
 *
 * ③ 的取数在 CI 里不可得（e2e-real-staging job 的八步里没有 ssh / 日志拉取，网关也没有技术日志
 * 端点），故按「有语料才判」实现：E2E_SCOPE_LOG_FILE 指向一份日志窗口文件时三元组齐判、
 * 缺项即红；未设该变量时 ①② 硬判 + ③ 在 annotations 与 run 日志里显式登记「未量」，
 * 并打出复查通道与按 request_id 的复现姿势 —— 不静默、不假绿。
 * 若裁「把日志通道接进 CI」，只需在该 job 里落一份语料并设这个变量，判据本体不动。
 *
 * 覆盖缺口（2026-10-02 按现值订正，本段原写「doctor_li 团队当前零患者」）：staging 上该团队现有
 * 3 名患者、运营可见 10 名 ⇒ 「本团队患者 200 读回」与「跨团队 403 与查无此人同形」两格都已转真跑；
 * 缺口登记逻辑保留，数据再被清空时照常回报「未跑」。
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

/**
 * T350 返工（D-1/D-2/D-3）纳入团队推导的三条：医护身份腿只跑这三条。
 * 第三列是该域越权业务码（model.CodeForbidden 按域分配：data 30403 / user 10403），
 * 端点分属两个服务，故逐条登记而非共用一个常量。
 */
const REWORK_ENDPOINTS: Array<[string, (pid: string) => string, number]> = [
  ['daily-wear', (p) => `/api/v1/patients/${p}/daily-wear?days=7`, 30403],
  ['feeling-logs', (p) => `/api/v1/patients/${p}/feeling-logs`, 10403],
  ['review-records', (p) => `/api/v1/patients/${p}/review-records`, 10403],
]

/** 11.4 的 probe 走 daily-wear（data-service），故三元组第 ② 项按该域越权码取值 */
const SCOPE_DENY_CODE = 30403

/**
 * 三元组第 ③ 项在技术日志里的锚点（data-service assertTeamScope / user-service denyCrossTeam
 * 写的英文原文）。T464 后这句只进日志、不进响应体 message，所以它只能取一份日志窗口来配，
 * 不能从 HTTP 响应里读 —— 旧判据「响应文案含这句」在现网恒假。
 */
const SCOPE_DENY_LOG_MARK = 'out of your data scope'

/** ③ 的带外语料入口（见文件头：CI 无日志通道 ⇒ 未设即「未量」，不静默也不假绿） */
const SCOPE_LOG_ENV = 'E2E_SCOPE_LOG_FILE'

/** staging 预置医护账号（有团队归属：2026-10-02 实测该团队 3 名患者，运营可见 10 名） */
const DOCTOR_USERNAME = 'doctor_li'

async function realToken(
  req: APIRequestContext,
  username: string = 'ops_admin',
): Promise<string> {
  const password = realPassword(`11-patient-exists.realToken(${username})`)
  const res = await req.post('/api/v1/auth/login', {
    data: { username, password },
    headers: { 'Content-Type': 'application/json' },
  })
  // 401 单独定性为凭据漂移（T502 实测：doctor_li 口令被改时，这里红的是断言文案，
  // 读起来像代码回归）；其它非 200 保持原口径，不借漂移之名掩盖服务端问题。
  const status = res.status()
  expect(status, status === 401 ? credentialDriftMessage(username, status, password) : `${username} 登录应 200`).toBe(200)
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
 * 三元组①② 的取数：一次 GET，把判定要用的量一并取出（关联号取响应体 trace.requestId，
 * 与同请求的服务端日志行同源 —— 见 services/data-service/internal/handler/trace_t464.go）。
 */
async function readScopeDeny(
  req: APIRequestContext,
  token: string,
  path: string,
): Promise<{ http: number; code: unknown; requestId: string }> {
  const res = await req.get(path, { headers: { Authorization: `Bearer ${token}` } })
  const body = await res.json().catch(() => null)
  return {
    http: res.status(),
    code: body?.code,
    requestId: String(body?.trace?.requestId ?? ''),
  }
}

/** ③ 语料的本轮缓存：undefined 未取 / null 未设入口 / 数组为已读到的窗口 */
let scopeLogCorpus: string[] | null | undefined

/**
 * 读 E2E_SCOPE_LOG_FILE 指向的技术日志窗口（每行一条 JSON，含 request_id 与 message）。
 * 未设 ⇒ null（CI 的常态，按「未量」处理）；设了却读不到 ⇒ 抛，不降级成「未量」——
 * 配了语料又读失败是取数坏了，这种时候判红是对的，静默放行才是假绿。
 */
function readScopeLogCorpus(): string[] | null {
  if (scopeLogCorpus === undefined) {
    const file = process.env[SCOPE_LOG_ENV]
    if (!file) {
      scopeLogCorpus = null
    } else {
      const raw = readFileSync(file, 'utf8')
      scopeLogCorpus = raw.split(/\r?\n/).filter((line) => line.trim() !== '')
      console.log(`[e2e-real][t528] ③ 语料已载入 ${file} —— ${scopeLogCorpus.length} 行非空`)
    }
  }
  return scopeLogCorpus
}

/**
 * ③ 的配对判据：同一行里既有本次端点路径（日志记不含 query 的 path，且带 probe 患者号），
 * 又有英文锚点原文。
 * 为什么不用 trace.requestId 当配对键：它每请求随机（同服务 handler/trace_t464.go 的 newRequestID），
 * 而语料只能窗外语料化 ⇒ 本次请求的号必然不在手上这份窗口里，用它当键这条腿永远配不上。
 * 改按「同窗 + 同路径」后两跑即可闭合：第一跑让服务端落行，取覆盖它的窗口当语料，第二跑即命中。
 * 牙仍在（2026-10-02 按 git 一手读数核过）：返工前 daily-wear 对医护写的是 self-only 文案
 * 「may only query your own daily-wear stats」（见 148f9b6 前一版 handler.go 的 getDailyWear），
 * 不含本锚点；同一患者号在别的端点上的越权行路径段不同，也配不上本键 ⇒ 旧包探不出命中。
 */
function logScopeHit(path: string): boolean | null {
  const lines = readScopeLogCorpus()
  if (lines === null) return null
  const logPath = path.split('?')[0]
  return lines.some((line) => line.includes(logPath) && line.includes(SCOPE_DENY_LOG_MARK))
}

/** ③ 未量时的登记：把缺口写进 annotations，并把复查姿势连关联号一起打出来，不静默 */
function noteScopeLogGap(requestId: string, path: string): void {
  const desc =
    `T350-doctor-scope：三元组第 ③ 项本 run 未量（未设 ${SCOPE_LOG_ENV}，CI 无日志通道），` +
    `① ② 已硬判。复查通道按 docs/tasks/joe/LOG-QUERY-HOWTO.md 的 backend 子命令取 data-service 日志窗口，` +
    `按本次关联号 request_id=${requestId || '（响应未带 trace.requestId，需按同窗 path 反查）'}` +
    `（本次端点 ${path.split('?')[0]}）核对那一行是否含「${SCOPE_DENY_LOG_MARK}」；` +
    `要把这格转成真判：落一份覆盖上一次 probe 的窗口文件，把路径写进 ${SCOPE_LOG_ENV} 再跑，判据本体不动。`
  console.log(`[e2e-real][t528] ${desc}`)
  test.info().annotations.push({ type: 'coverage-gap', description: desc })
}

/**
 * 存在性探测（三元组版，T528）：① HTTP 403 ② 响应体业务码 = 该域越权码 ③ 同窗技术日志里
 * 该端点那一行含英文锚点。返工前后对医护都回 403 + 越权码（2026-10-02 现网两版实测同形），
 * ①② 单独探不出部署与否，而 T464 后那句英文只进日志不进响应体 ⇒ 旧「看文案」判据恒假。
 * 无语料（CI）时 ① ② 硬判 + ③ 登记「未量」；有语料时三项缺一即 false。
 * 仍然只做存在性探测：真业务判据在 assertDoctorScope，这里红不掉它。
 */
async function probeScopeDeny(req: APIRequestContext, token: string, path: string): Promise<boolean> {
  const deny = await readScopeDeny(req, token, path)
  const leg3 = logScopeHit(path)
  const ok1 = deny.http === 403
  const ok2 = deny.code === SCOPE_DENY_CODE
  console.log(
    `[e2e-real][t528] 三元组 ${path} —— ①HTTP=${deny.http}(${ok1 ? '合' : '不合'}) ` +
      `②code=${deny.code}(${ok2 ? '合' : '不合'}，应 ${SCOPE_DENY_CODE}) ` +
      `③log=${leg3 === null ? '未量' : leg3 ? '命中' : '未命中'} request_id=${deny.requestId || '-'}`,
  )
  if (leg3 === null) noteScopeLogGap(deny.requestId, path)
  return ok1 && ok2 && leg3 !== false
}

/** 把调用者自己填的患者号折成占位符，用于比对两种拒绝是否「同形」 */
function foldPid(message: string, pid: string): string {
  return message.split(pid).join('<pid>')
}

/**
 * 医护身份腿（T350 返工派发单 c 项）：三条端点在患者号维度对 ROLE_DOCTOR 一律 403 且同形，
 * 本团队患者必须读得回 200 —— 后面两格按现网数据可得性执行，拿不到样本由调用方登记缺口。
 * 偏差同样累计成清单，一条端点红掉不挡其余端点的现状。
 * T528 把「拒绝文案含团队范围口径」换成按域登记的越权码（group 第三列）：
 * T464 起 message 一律是中文码表文案，旧判据在现网恒假；三条端点分属 data / user 两个服务，
 * 码值不同（30403 / 10403），故逐条判而非共用一个常量。
 */
async function assertDoctorScope(
  req: APIRequestContext,
  docToken: string,
  ownPids: string[],
  outPid: string,
  group: Array<[string, (pid: string) => string, number]>,
): Promise<void> {
  const headers = { Authorization: `Bearer ${docToken}` }
  const ownPid = ownPids[0] ?? ''
  const badGone: string[] = []
  const badOut: string[] = []
  const badShape: string[] = []
  const badLive: string[] = []

  for (const [name, build, denyCode] of group) {
    const gone = await req.get(build(GONE_PID), { headers })
    const goneBody = await gone.json().catch(() => null)
    console.log(`[e2e-real][t350r] GET ${gone.url()} -> HTTP ${gone.status()} code=${goneBody?.code}`)
    if (gone.status() !== 403) badGone.push(`${name} HTTP=${gone.status()}`)
    if (goneBody?.code !== denyCode) badGone.push(`${name} code=${goneBody?.code}，应 ${denyCode}`)
    if (goneBody?.data !== null) badGone.push(`${name} data 非 null`)

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
      why:
        `探针三项未齐：① 应 403、② 业务码应 ${SCOPE_DENY_CODE}、③ 同窗日志里该端点那一行应含团队范围原文` +
        `（未设 ${SCOPE_LOG_ENV} 时 ③ 记「未量」、不参与判定，缺项即 false）`,
      probe: () =>
        probeScopeDeny(page.request, docToken, `/api/v1/patients/${GONE_PID}/daily-wear?days=7`),
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
