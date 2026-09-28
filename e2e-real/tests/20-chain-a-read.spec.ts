import { test, expect, type Page, type Locator } from '@playwright/test'
import {
  realLogin,
  realLogout,
  gotoMenuAndWaitTable,
  tableRows,
  getAuthToken,
  isLoginPath,
  LS_TOKEN_KEY,
} from '../real-helpers'

/**
 * T462 S1 · 链 A 读段（真实模式 / staging，一条用例串完，零写）
 *
 * 链路口径取自 T462 设计稿 §四「链 A」的读侧半步 + §七 S1：
 *   登录 → 患者管理列表 → 按真实姓名搜索命中 → 打开该患者详情抽屉读回字段
 *   → 侧边栏进团队管理列表读回该患者所属团队那一行 → 退出登录回到登录页
 *
 * 为什么要单独一条（而不是靠 05/06 各自验自己那页）：
 *   05 与 06 各自只在自己那页内对平，从没证明过「同一次登录会话里，患者页显示的组织名
 *   和团队页那一行的组织名是同一个值」，也没证明过跨页导航后登录态仍续得上。
 *   串联链的失败模式（跳页丢态、跨页取数源不一致）恰恰是单页用例结构上碰不到的。
 *
 * 断言强度：值级。DOM 读到的每一格要和同一 page 上下文发出的 GET 响应体逐字段对上，
 *   不看 HTTP 200 就算过；两个读端点（列表 / 详情）之间也要求同值。
 *
 * 零写自证（本用例的红线，不只是注释）：全程只允许 `POST /api/v1/auth/login` 一条非 GET
 *   请求，其余一律 GET —— 见末尾 `nonGet` 那条 toEqual。跑完会有人拿这条链在 staging 上
 *   反复执行，所以「没写」必须是断言，不能是承诺。
 *
 * 不需要 requireDeployedBuild：本用例读的是抽屉字段（患者抽屉自仓初始提交就用
 *   `.el-drawer` + `el-descriptions` 渲染，不依赖未上线的前端构建）与团队列表行，
 *   两侧判据都已在现网成立。挂守卫反而会把已经生效的判据藏到「跳过」里。
 */

/** 患者列表行 / 详情共用形状（后端 model.AdminPatientDTO，列表与详情是同一条投影 pg.go:patientSelect） */
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
  patientCount: number
}

/** 患者列表卡（本页自 T289 起有第二张会出数的表，作用域必须收到这张卡，同 05 的 T358 口径） */
function patientCard(page: Page): Locator {
  return page.locator('.patient-list-card')
}

/**
 * 团队列表那张卡。团队页的「成员」表在 v-else 分支里（mode !== 'list'），正常不进 DOM，
 * 但仍按表头文案锚定，不靠「第一张 .page-card」这种位置假设 —— 统计卡哪天改成表格就会串。
 */
function teamCard(page: Page): Locator {
  return page
    .locator('.page-card')
    .filter({ has: page.locator('th', { hasText: '团队编号' }) })
    .first()
}

/** 表头文案 → 列下标（首列可能是 selection 复选框，绝不按写死序号取列） */
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

/** 按「患者ID / 团队编号」那一格的值找到行；返回 null 表示该目标不在当前 DOM 页 */
async function cellsOfRowByKey(
  scope: Locator,
  keyIdx: number,
  keyVal: string,
): Promise<string[] | null> {
  const row = scope.locator('.el-table__body-wrapper tbody tr').filter({ hasText: keyVal }).first()
  if ((await row.count()) === 0) return null
  const cells = await cellTexts(row)
  expect(
    cells[keyIdx],
    `定位行要用列下标 ${keyIdx} 的值，实得 ${cells[keyIdx]}（期望 ${keyVal}）`,
  ).toBe(keyVal)
  return cells
}

/**
 * 读抽屉里的 el-descriptions（label → value）。
 * EP 2.x 现网包的类名是 `.el-descriptions__label`（值在其 nextElementSibling），
 * 不是 2.2 时代的 `.el-descriptions-item__label` —— 用后者会静默匹配 0 个节点（同 07 的实测）。
 */
async function readDescriptions(drawer: Locator): Promise<Map<string, string>> {
  const pairs = await drawer
    .locator('.el-descriptions')
    .evaluate((el) =>
      Array.from(el.querySelectorAll('.el-descriptions__label')).map((th) => ({
        label: (th.textContent ?? '').trim(),
        value: (th.nextElementSibling?.textContent ?? '').trim(),
      })),
    )
  return new Map(pairs.map((p) => [p.label, p.value]))
}

/** 抽屉那 8 行的 label 文案（patients/index.vue:134-141，逐条对到前端渲染分支） */
const DRAWER_LABELS = [
  '性别',
  '年龄',
  '诊断',
  'Cobb角',
  '所属团队',
  '主治医生',
  '绑定设备',
  '建档时间',
] as const

/** 前端渲染口径复刻：值级比较要用和页面一样的兜底链，否则比的是两套逻辑 */
const genderLabel = (g: string | null): string => (g === 'male' ? '男' : g === 'female' ? '女' : '-')
const ageLabel = (v: number | null): string => (v === null || v === undefined ? '-' : String(v))
const textLabel = (v: string | null): string => v || '-'
const cobbLabel = (v: number | null): string => (v ? `${v}°` : '-')
const deviceLabel = (v: string | null): string => v || '未绑定'

test.describe('20-链 A 读段（T462 S1，零写）', () => {
  test('20.1 登录 → 患者搜索命中 → 详情抽屉字段 → 团队列表同源 → 退出：DOM 与 GET 响应体逐字段对上', async ({
    page,
  }) => {
    // 采集浏览器真实发出的 /api/v1 请求（有序）。既用于「真打了 staging」的反证，
    // 也用于「除登录外零写」的自证。
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

    /** 统一信封 { code, message, data }；非 2xx 或 code≠0 直接判红，不吞 */
    async function apiGet<T>(path: string): Promise<T> {
      const token = await getAuthToken(page)
      expect(token, `调用 ${path} 前应已持有 JWT`).toBeTruthy()
      const res = await page.request.get(path, { headers: { Authorization: `Bearer ${token}` } })
      expect(res.ok(), `GET ${path} 应 2xx，实得 ${res.status()}`).toBe(true)
      const body = await res.json()
      expect(body.code, `GET ${path} 信封 code 应为 0，实得 ${JSON.stringify(body.message)}`).toBe(0)
      return body.data as T
    }

    // ── 1) 登录 ────────────────────────────────────────────────
    await realLogin(page)
    await expect(page).toHaveURL(/\/dashboard/, { timeout: 20_000 })
    const token = await getAuthToken(page)
    expect(token, '真实模式登录应把 JWT 写进 localStorage').toBeTruthy()

    // ── 2) 侧边栏进患者管理（作用域收到患者列表卡）────────────
    await gotoMenuAndWaitTable(page, '患者管理', 'patients', patientCard(page))

    // 与页面首屏完全同参（patients/index.vue:295-296 的 page=1 / pageSize=10），DOM 才可比
    const pageOne = await apiGet<{ list: PatientRow[]; total: number }>(
      '/api/v1/admin/patients?page=1&pageSize=10',
    )
    expect(pageOne.list.length, '链 A 要首屏有患者行，读到 0 行等于什么都没串').toBeGreaterThanOrEqual(5)

    // 串联锚点：必须选一名「已归属团队、且后端 join 得出团队名」的患者，
    // 否则第 4 步在团队页没有对应的行可对。
    const target = pageOne.list.find((p) => p.teamId && p.teamName)
    expect(
      target,
      '首屏应至少有一名已分配团队且 join 出团队名的患者（链 A 靠它把患者页与团队页串起来）',
    ).toBeTruthy()
    const targetTeamId = String(target!.teamId)
    const targetTeamName = String(target!.teamName)

    // ── 3) 按真实姓名搜索并校验命中 ───────────────────────────
    const nameIdx = await headerIndex(patientCard(page), '姓名')
    const pidIdx = await headerIndex(patientCard(page), '患者ID')

    const search = page.locator('.search-input input')
    await expect(search).toBeVisible({ timeout: 8_000 })
    await search.fill(target!.name)
    await page.locator('.page-toolbar').getByRole('button', { name: '查询' }).first().click()

    // 搜索走的是后端（keyword ILIKE 姓名/患者ID），所以「命中」的判据是响应体 + DOM 双侧：
    const searched = await apiGet<{ list: PatientRow[] }>(
      `/api/v1/admin/patients?page=1&pageSize=10&keyword=${encodeURIComponent(target!.name)}`,
    )
    expect(
      searched.list.some((p) => p.patientId === target!.patientId),
      `GET 侧：关键词「${target!.name}」应能查到目标患者 ${target!.patientId}`,
    ).toBe(true)

    const rows = tableRows(page, patientCard(page))
    const targetRow = patientCard(page)
      .locator('.el-table__body-wrapper tbody tr')
      .filter({ hasText: target!.patientId })
      .first()
    await expect
      .poll(async () => (await targetRow.count()) > 0, {
        timeout: 20_000,
        message: '搜索后 DOM 里应出现目标患者那一行',
      })
      .toBe(true)

    // 逐行校验：搜索结果每一格的姓名都要含关键词（重名患者会一起回来，故不比行数）
    const hitCount = await rows.count()
    expect(hitCount, '按存在的姓名搜索不应清零').toBeGreaterThanOrEqual(1)
    for (let i = 0; i < hitCount; i++) {
      const cells = await cellTexts(rows.nth(i))
      expect(cells[nameIdx], `结果第 ${i + 1} 行的姓名列应含关键词`).toContain(target!.name)
    }

    // 目标行自身要落到「患者ID」那一列（hasText 命中的可能是别的格）
    const targetCells = await cellTexts(targetRow)
    expect(targetCells[pidIdx], '命中行的患者ID列应等于目标患者').toBe(target!.patientId)
    expect(targetCells[nameIdx], '命中行的姓名列应等于目标患者姓名').toBe(target!.name)

    // ── 4) 打开该患者的详情抽屉，读回字段 ─────────────────────
    await targetRow.locator('td').nth(nameIdx).click()

    const drawer = page.locator('.el-drawer')
    await expect(drawer).toBeVisible({ timeout: 8_000 })
    // 抽屉标题 = `${name}（${patientId}）`（patients/index.vue:107）
    await expect(drawer.locator('.el-drawer__title')).toContainText(target!.name, { timeout: 5_000 })
    await expect(drawer.locator('.el-drawer__title')).toContainText(target!.patientId)
    // 患者号卡（值级，不是「有这个东西」）
    await expect(drawer.locator('.pid-card .pid-value')).toHaveText(target!.patientId)

    // 抽屉的数据源是列表行（viewDetail 直接赋值，不发详情请求），详情端点是另一条读路径：
    // 两者必须同值，否则「同一个患者的两个视图」就是两个数 —— 这正是串联要盯的东西。
    const detail = await apiGet<PatientRow>(
      `/api/v1/admin/patients/${encodeURIComponent(target!.patientId)}`,
    )
    for (const key of [
      'patientId',
      'name',
      'gender',
      'age',
      'diagnosis',
      'cobbAngle',
      'deviceId',
      'teamId',
      'doctorId',
      'teamName',
      'doctorName',
      'createdAt',
    ] as const) {
      expect(detail[key], `列表行与详情端点的 ${key} 应同值`).toEqual(target![key])
    }

    const fields = await readDescriptions(drawer)
    for (const label of DRAWER_LABELS) {
      expect(fields.has(label), `抽屉应有「${label}」这一行（实得 ${[...fields.keys()].join('|')}）`).toBe(
        true,
      )
    }
    expect(fields.get('性别'), '抽屉「性别」').toBe(genderLabel(detail.gender))
    expect(fields.get('年龄'), '抽屉「年龄」').toBe(ageLabel(detail.age))
    expect(fields.get('诊断'), '抽屉「诊断」').toBe(textLabel(detail.diagnosis))
    expect(fields.get('Cobb角'), '抽屉「Cobb角」').toBe(cobbLabel(detail.cobbAngle))
    expect(fields.get('所属团队'), '抽屉「所属团队」').toBe(targetTeamName)
    expect(fields.get('主治医生'), '抽屉「主治医生」').toBe(detail.doctorName || '-')
    expect(fields.get('绑定设备'), '抽屉「绑定设备」').toBe(deviceLabel(detail.deviceId))
    expect(fields.get('建档时间'), '抽屉「建档时间」').toBe(detail.createdAt.slice(0, 10))
    // 兜底：任何一格都不该把 undefined / null 直接印到页面上
    for (const [label, value] of fields) {
      expect(value, `抽屉「${label}」不应渲染成 undefined/null`).not.toMatch(/undefined|null|\[object/)
    }

    // 抽屉关掉再走跨页导航：链 A 之后要接写段，写段的入口就在抽屉里，
    // 面板不收起就说明路由切换是靠组件整体卸载兜底的，那条路径不能算「用户操作序列」。
    await page.keyboard.press('Escape')
    await expect(drawer).toBeHidden({ timeout: 8_000 })

    // ── 5) 侧边栏进团队管理，读回该患者所属团队那一行 ─────────
    await gotoMenuAndWaitTable(page, '团队管理', 'teams', teamCard(page))

    const teams = await apiGet<TeamRow[]>('/api/v1/teams')
    const dictTeam = teams.find((t) => t.teamId === targetTeamId)
    expect(dictTeam, `GET /api/v1/teams 应能解析目标团队 ${targetTeamId}`).toBeTruthy()

    const teamPidIdx = await headerIndex(teamCard(page), '团队编号')
    const teamNameIdx = await headerIndex(teamCard(page), '团队名称')
    const teamPatientIdx = await headerIndex(teamCard(page), '管理患者数')

    const teamCells = await cellsOfRowByKey(teamCard(page), teamPidIdx, targetTeamId)
    expect(teamCells, `团队页应有编号 ${targetTeamId} 那一行`).not.toBeNull()
    // 跨页同源：患者在患者页看到的团队名，必须就是团队页那一行的团队名
    expect(teamCells![teamNameIdx], '团队页「团队名称」应等于字典值').toBe(dictTeam!.name)
    expect(teamCells![teamNameIdx], '患者页所属团队与团队页团队名称应同源同值').toBe(targetTeamName)
    // 16 号用例只在对平两条 API 读路径，团队页这一列的 DOM 腿在这里补上
    expect(teamCells![teamPatientIdx], '团队页「管理患者数」应等于 API patientCount').toBe(
      String(dictTeam!.patientCount),
    )

    // ── 6) 退出登录 ───────────────────────────────────────────
    await realLogout(page)
    await expect(page).toHaveURL(/\/login/, { timeout: 15_000 })
    expect(isLoginPath(new URL(page.url()).pathname), '退出后应停在登录页').toBe(true)
    const tokenAfter = await page.evaluate((k) => localStorage.getItem(k), LS_TOKEN_KEY)
    expect(tokenAfter, '退出后 localStorage 不该留 JWT').toBeFalsy()
    // 侧边栏随之消失（否则「退出」只是改了个地址）
    await expect(page.locator('.el-menu')).toHaveCount(0)

    // ── 7) 反证：这条链真的按顺序打了 staging，且除登录外零写 ──
    expect(net, '应真的发出过登录请求（BASE_URL 打空也会让上面全绿）').toContain(
      'POST /api/v1/auth/login',
    )
    const firstPatients = net.indexOf('GET /api/v1/admin/patients')
    const firstTeamStats = net.indexOf('GET /api/v1/admin/teams/stats')
    expect(firstPatients, '未捕获到患者列表请求（患者页取数形状变了，本用例的判据已失效）').toBeGreaterThanOrEqual(
      0,
    )
    // /api/v1/admin/teams/stats 只有团队管理页会发（api/index.ts:326 唯一调用点 teams/index.vue:260）
    // ⇒ 它是「浏览器确实走到过团队页」的信号，而不是患者页顺带发出的旁路请求。
    expect(
      firstTeamStats,
      '未捕获到团队统计请求（说明没真正落到团队管理页，串联断了）',
    ).toBeGreaterThanOrEqual(0)
    expect(
      firstTeamStats,
      `串联顺序应为「患者页在前、团队页在后」：stats=${firstTeamStats} patients=${firstPatients}`,
    ).toBeGreaterThan(firstPatients)

    // 零写自证：非 GET 只允许登录那一条。跑红即说明链 A 的读段被人加进了写操作。
    const nonGet = net.filter((m) => !m.startsWith('GET '))
    expect(nonGet, 'S1 声明零写：除 POST /api/v1/auth/login 外不该发出任何写请求').toEqual([
      'POST /api/v1/auth/login',
    ])
  })
})
