import { test, expect, type APIRequestContext } from '@playwright/test'
// T506：口令经凭据门取数；登录 401 时把失败信息定性为凭据漂移，而不是留给下游断言差异
import { credentialDriftMessage, realPassword } from '../real-helpers'

/**
 * T371-B1 · 团队「患者数」实时计数的现网防回归（真实模式 / staging，全 GET 只读）
 *
 * 要盯的缺陷：GET /api/v1/teams 的 patientCount 原本读 teams.patient_count 快照列 —— 全仓无应用写
 * 路径（CreateTeam 的 INSERT 不带它，删除守卫按 team_id 现场数完也不写回），于是列表显示 0/2 的团队
 * 点删除会得 409，同页四张统计卡与列表互相打脸（Joe 现网基线：一组列表 2 / 现场 4，合计 5 对 7）。
 * T371 把读源换成 pg.go:teamPatientCountExpr 的实时子查询（谓词与 data-service 团队排行、删除守卫同源）。
 *
 * 本文件的四条判据 = PM 在 T371 卡内 2026-09-26 授权的那条增量：「顺手在 e2e-real 加一条 patientCount
 * 只读值断言（列表读数 == 按 team_id 现场分组计数，样本须非空），这条判据增量我认了，归补验轮执行」。
 * 它同时补上验收报告 §5.3 记的那个缺口：修后行为在 main 上只有 Go 单测盯 SQL 文本，现网面没门禁 ——
 * 谁把读源改回快照列，CI 与定时巡检此前都不会红。
 *
 * 为什么不套 requireDeployedBuild（区别于 tests/11 的 11.2 / 11.3）：
 *   被测合并笔 d9fc880 已在 staging 现网包，2026-09-27 00:59 只读探针三项同时成立
 *   （server worktree head=c2cda00、contains_d9fc880=YES、user/data/gateway 镜像全是 staging-c2cda00），
 *   且四条判据只走后端读端点、不依赖 admin-web 构建 ⇒ 无条件真跑。
 *   线上若回退成读快照列，本用例当场判红 —— 这正是它存在的意义，不拿守卫把已上线的行为挂到跳过上。
 *
 * 判别力（防「零对零恒真」）：四条判据全是「两条读路径互等」，不与任何常量比，所以数据漂移不会误报，
 *   但样本全空时它们会恒真。故 16.1 先钉「样本非空」硬守卫（现场至少一名患者挂在团队上 + 列表至少一个
 *   团队 patientCount 大于 0）；「大于等于 2」那一格只登记覆盖缺口，不判红 —— 现网 TEAM01 是 4，
 *   而库内快照列仍是 2（证据包 t371-probe-live-out.txt 的 [V6] 行），这格有牙由现网数据保证，
 *   不该由用例把它钉死成常量。
 *
 * 数据纪律：全量只读 GET，不建不改不删；不硬编码团队 ID / 患者 ID / 计数值。
 *   串行前提：playwright.real.config.ts 是 workers=1，本用例内「列表 → 患者全量 → 排行 → 统计卡」
 *   四组读数之间不会有别的用例并发写库；改成多 worker 前先给这段对拍加同快照窗口的重读容差。
 */

interface TeamRow {
  teamId: string
  name: string
  patientCount: number
}

interface RankingRow {
  teamName: string
  patientCount: number
}

interface TeamStats {
  teamCount: number
  managedPatientCount: number
  unassignedPatientCount: number
}

/** 只读 GET：码值 + 业务码就地钉死，返回 data。打一行方法 + URL + 结论，供 run 日志反查「确实打了 staging」 */
async function okGet(req: APIRequestContext, token: string, path: string): Promise<any> {
  const res = await req.get(path, { headers: { Authorization: `Bearer ${token}` } })
  const body = await res.json().catch(() => null)
  console.log(`[e2e-real][t371] GET ${res.url()} -> HTTP ${res.status()} code=${body?.code}`)
  expect(res.status(), `GET ${path} 应 200，实得 ${res.status()}`).toBe(200)
  expect(body?.code, `GET ${path} 业务码应为 0，实得 ${body?.code}`).toBe(0)
  return body?.data
}

async function realToken(req: APIRequestContext): Promise<string> {
  const password = realPassword('16-team-patient-count.realToken')
  const res = await req.post('/api/v1/auth/login', {
    data: { username: 'ops_admin', password },
    headers: { 'Content-Type': 'application/json' },
  })
  const status = res.status()
  expect(status, status === 401 ? credentialDriftMessage('ops_admin', status, password) : '运营账号登录应 200').toBe(200)
  const token = (await res.json())?.data?.token
  expect(typeof token, '应拿到 JWT').toBe('string')
  return token as string
}

/** DS2 读源本体：user-service ListTeams（无分页，全量团队） */
async function listTeams(req: APIRequestContext, token: string): Promise<TeamRow[]> {
  const data = await okGet(req, token, '/api/v1/teams')
  const list: TeamRow[] = Array.isArray(data) ? data : []
  expect(list.length, 'staging 应至少有一个团队可对拍').toBeGreaterThan(0)
  return [...list].sort((a, b) => String(a.teamId).localeCompare(String(b.teamId)))
}

/** 现场谓词：GET /api/v1/admin/patients 全量翻页后按 teamId 分组（不信任何 seed 计数） */
async function liveTeamCounts(req: APIRequestContext, token: string): Promise<Map<string, number>> {
  const pageSize = 50
  const seen = new Set<string>()
  const counts = new Map<string, number>()
  let total = -1
  for (let page = 1; page <= 20; page++) {
    const data = await okGet(req, token, `/api/v1/admin/patients?page=${page}&pageSize=${pageSize}`)
    const rows: Array<{ patientId?: string; teamId?: string | null }> = data?.list ?? []
    total = Number(data?.total ?? 0)
    for (const p of rows) {
      const pid = String(p?.patientId ?? '')
      expect(pid, '患者行应带 patientId').not.toBe('')
      if (seen.has(pid)) continue // 翻页窗口漂移兜底：同一患者只数一次
      seen.add(pid)
      const key = p?.teamId ? String(p.teamId) : ''
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
    if (seen.size >= total) break
    expect(rows.length, `第 ${page} 页应满页 ${pageSize} 行（否则 total 读不到底）`).toBe(pageSize)
  }
  expect(seen.size, `患者全量应翻到 total=${total}，实得 ${seen.size}`).toBe(total)
  return counts
}

/** 同服务第二条读路径：patients 列表按 teamId 过滤后的服务端 COUNT(*)（与删除守卫同一谓词族） */
async function patientTotalOfTeam(req: APIRequestContext, token: string, teamId: string): Promise<number> {
  const data = await okGet(req, token, `/api/v1/admin/patients?teamId=${encodeURIComponent(teamId)}&page=1&pageSize=1`)
  return Number(data?.total ?? NaN)
}

/** DS3 跨服务读路径：data-service 团队排行（Top 10，按 patients.team_id 分组） */
async function teamRanking(req: APIRequestContext, token: string): Promise<RankingRow[]> {
  const data = await okGet(req, token, '/api/v1/admin/dashboard/team-ranking')
  return Array.isArray(data) ? data : []
}

async function teamStats(req: APIRequestContext, token: string): Promise<TeamStats> {
  return await okGet(req, token, '/api/v1/admin/teams/stats')
}

function gap(desc: string): void {
  console.log(`[e2e-real][t371] 覆盖缺口 —— ${desc}`)
  test.info().annotations.push({ type: 'coverage-gap', description: desc })
}

test.describe('16-团队患者数实时计数（T371-B1 现网防回归）', () => {
  let token = ''
  let teams: TeamRow[] = []
  let live: Map<string, number> = new Map()

  test.beforeEach(async ({ page }) => {
    token = await realToken(page.request)
    teams = await listTeams(page.request, token)
    live = await liveTeamCounts(page.request, token)
  })

  test('16.1 逐团队：列表 patientCount == 按 team_id 现场分组计数（样本非空）', async ({ page }) => {
    const managedLive = [...live.entries()].filter(([teamId]) => teamId !== '')
    expect(
      managedLive.reduce((s, [, n]) => s + n, 0),
      '现网应有至少一名患者挂在团队上，否则「两条读路径互等」会在空集上恒真',
    ).toBeGreaterThan(0)
    expect(
      teams.some((t) => Number(t.patientCount) > 0),
      '列表里应至少有一个团队读数大于 0，否则本用例只在对 0 打靶',
    ).toBe(true)

    const maxLive = managedLive.reduce((m, [, n]) => Math.max(m, n), 0)
    if (maxLive < 2) {
      gap(
        'T371-patient-count-realtime：现网没有任何团队挂着 2 名以上患者 ⇒ 「读数与快照列不同」这格判别力降级' +
          '（1 对 1 与旧快照口径可能同值）；等有人给某团队建档后此格自动回弹，本 run 未跑',
      )
    }

    const bad: string[] = []
    for (const t of teams) {
      const shown = Number(t.patientCount)
      const actual = live.get(t.teamId) ?? 0
      console.log(`[e2e-real][t371] ${t.teamId} 列表=${shown} 现场=${actual}`)
      if (shown !== actual) bad.push(`${t.teamId} 列表=${shown} 现场=${actual}`)
    }
    expect(
      bad,
      `团队列表 patientCount 必须等于按 patients.team_id 的现场计数（T371-B1 换掉的正是这条读源；` +
        `读回 teams.patient_count 快照列就会偏差），实际偏差：${bad.join('；')}`,
    ).toEqual([])

    // 反向一格：patients.team_id 有 FK（000001_init_schema.up.sql:78），故现场分组不该冒出列表之外的团队键
    const orphanKeys = [...live.keys()].filter((k) => k !== '' && !teams.some((t) => t.teamId === k))
    expect(orphanKeys, `现场分组出现团队列表之外的 team_id（FK 应挡住）：${orphanKeys.join('、')}`).toEqual([])
  })

  test('16.2 逐团队：列表 patientCount == 按 teamId 过滤的患者 total（同服务两条读路径）', async ({ page }) => {
    const bad: string[] = []
    let nonEmptyPairs = 0
    for (const t of teams) {
      const shown = Number(t.patientCount)
      const filtered = await patientTotalOfTeam(page.request, token, t.teamId)
      console.log(`[e2e-real][t371] ${t.teamId} 列表=${shown} teamId 过滤 total=${filtered}`)
      if (!Number.isFinite(filtered)) bad.push(`${t.teamId} total 不可解析`)
      else if (shown !== filtered) bad.push(`${t.teamId} 列表=${shown} 过滤 total=${filtered}`)
      if (shown > 0) nonEmptyPairs++
    }
    expect(nonEmptyPairs, '应有至少一对非空样本参与对拍').toBeGreaterThan(0)
    expect(
      bad,
      `「团队概要列表」与「患者列表按团队过滤的 COUNT(*)」两条读路径必须同值（删除守卫吃的就是后者，` +
        `不同值即「显示 0 却删不掉」那类打脸），实际偏差：${bad.join('；')}`,
    ).toEqual([])
  })

  test('16.3 跨服务：data-service 团队排行 == user-service 团队列表（按团队名 join）', async ({ page }) => {
    const ranking = await teamRanking(page.request, token)
    expect(ranking.length, '团队排行应至少回一行').toBeGreaterThan(0)
    if (teams.length > ranking.length) {
      gap(
        `T371-patient-count-realtime：团队排行 Top 10 截断（团队 ${teams.length} 个 / 排行 ${ranking.length} 行）` +
          '⇒ 未进排行的团队本 run 未参与跨服务对拍',
      )
    }
    const byName = new Map<string, number>()
    for (const r of ranking) byName.set(String(r.teamName), Number(r.patientCount))

    const bad: string[] = []
    let compared = 0
    for (const t of teams) {
      if (!byName.has(t.name)) continue
      compared++
      const shown = Number(t.patientCount)
      const ranked = byName.get(t.name) as number
      console.log(`[e2e-real][t371] ${t.teamId}「${t.name}」列表=${shown} 排行=${ranked}`)
      if (shown !== ranked) bad.push(`${t.teamId}「${t.name}」列表=${shown} 排行=${ranked}`)
    }
    expect(compared, '排行与列表应按团队名交得上，实得 0 对 ⇒ join 键已失效').toBeGreaterThan(0)
    expect(
      bad,
      `两个服务必须同口径（teamPatientCountExpr 与 teamRankingSQL 都以 patients.team_id 为谓词；` +
        `一侧退回 teams.patient_count 就会在跨服务对拍上露馅），实际偏差：${bad.join('；')}`,
    ).toEqual([])
  })

  test('16.4 打脸面闭合：列表之和 == 统计卡 managedPatientCount == 现场已挂团队患者数', async ({ page }) => {
    const stats = await teamStats(page.request, token)
    for (const key of ['teamCount', 'managedPatientCount', 'unassignedPatientCount'] as const) {
      const v = stats[key]
      expect(Number.isFinite(Number(v)), `统计卡字段 ${key} 应是数字，实得 ${JSON.stringify(v)}`).toBe(true)
    }
    const sumList = teams.reduce((s, t) => s + Number(t.patientCount), 0)
    const sumLive = [...live.entries()].filter(([teamId]) => teamId !== '').reduce((s, [, n]) => s + n, 0)
    const liveTotal = [...live.values()].reduce((s, n) => s + n, 0)
    console.log(`[e2e-real][t371] 列表之和=${sumList} 现场之和=${sumLive} 统计卡=${stats.managedPatientCount} 全量=${liveTotal} 待分配=${stats.unassignedPatientCount}`)

    expect(
      sumList,
      '团队管理页「各行患者数之和」必须等于统计卡 managedPatientCount（修前 5 对 7 打脸的那一格）',
    ).toBe(Number(stats.managedPatientCount))
    expect(
      sumLive,
      '现场按 team_id 数出的已挂团队患者数也必须等于统计卡 managedPatientCount（否则卡与列表各自读不同源）',
    ).toBe(Number(stats.managedPatientCount))
    expect(
      liveTotal,
      '患者全量应恰好拆成「已挂团队 + 待分配」两格（有第三态说明统计卡与现场口径分叉了）',
    ).toBe(Number(stats.managedPatientCount) + Number(stats.unassignedPatientCount))
  })
})
