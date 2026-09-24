// T379 权限矩阵三源跨源门禁（前端准入 × 库授权 × 网关强制）
//
// 为什么要这条门禁（T368 工程验收交回的假绿点 a）：权限域有三套真源，此前只有两两镜像、
// 且**网关那一套从来没有机器对拍**：
//   A 前端 ROLE_PAGE_MATRIX（apps/admin-web/src/router/permissions.ts）—— UX 层守卫，路由拦页；
//   B 库 roles.permissions_json.modules（scripts/db/seed/seed.sql + 迁移前滚）—— 侧栏渲染源；
//   C 网关 rbac.go 的六张端点矩阵（services/gateway/cmd/server/rbac.go）—— 真正的服务端强制面，
//     T260 起「未登记即默认拒绝」，改一处就能让某个可见页整页吃 403。
// permissions.spec.ts（T345/T368/T372）已把 A↔B 钉住，C 只靠注释与 A 互指（rbac.go 头注释
// 「两者须同步变更」），没有任何用例读过它 —— T348（医护概览打 GET /teams 403）与 T351
// （告警页无条件打 admin-only 规则端点）都是靠人踩出来的，不是被门禁拦出来的。
//
// 本门禁把 C 拉进同一张表：按「角色 × 页面」逐格比对三列，并**从源码派生页面到底打哪些端点**
// （页面 import 了哪些 api 函数 → 那些函数在 src/api/*.ts 里的 url 字面量），不手写端点清单，
// 免掉「加了页忘了登记」这种门禁自身的失明。
//
// 判据（失败即红，报错文本自带 角色/页面/端点/缺项）：
//   1 词表完整：ROLE_PAGE_MATRIX 的每个路径都有模块短键与页面目录；页面派生出的端点集非空。
//   2 A↔B 同页：每 (角色, 页面) 格，前端可见 ⇔ 库 modules 含该页短键（双向）。
//   3 A→C 可用：某角色能进的页，其页面 import 到的每个端点网关都必须放行该角色。派生只到「这一枪
//     由该页发起」的粒度、看不到代码里的条件，所以命中 REGISTERED_DIVERGENCES 才允许，
//     且差异集必须与登记表**逐项相等**（修好了忘摘条目也判红）；每条登记要写守卫落点 文件:行。
//   4 C 登记完整：派生端点必须命中 rbac.go 任一堆矩阵（含 publicPatterns），否则落 T260 默认拒绝。
//   5 网关真值绑定：本门禁派生的「(角色, 方法, 路径) → 是否 403」表与 gateway testdata 里的
//     golden 一致；golden 由 services/gateway/cmd/server/rbac_perm_gate_t379_test.go 用**真实中间件**
//     逐行复核 —— 本文件对 rbac.go 的判定复刻若与网关实际行为分叉，Go 那条判红（不在 CI-FE 里，
//     但 golden 文件落在 services/ 下，改动必触发 CI-Go）。
//
// 覆盖边界（如实写，别让下一位当成端到端可用性门禁）：第 3 条量的是**网关层是否放行**。
// 服务层水平鉴权（assertAdminOrSelf / requireSelfScope 的「医生非 admin 非本人 → 403」）不在本表内，
// T379 现网腿实测到该层仍在拦医护读患者数据（见卡内交件），属 rbac.go 头注释自认的 Phase 2 未收口项。
//
// 只读：本门禁只解析源码文本，不写任何一源的真实权限值。唯一写盘是 UPDATE_GATEWAY_TABLE=1 时
// 重写自家 golden（回归用，不在 CI 路径上）。
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative } from 'node:path'

const appRoot = join(dirname(fileURLToPath(import.meta.url)), '..') // apps/admin-web
const repoRoot = join(appRoot, '..', '..')
const read = (rel: string) => readFileSync(join(repoRoot, rel), 'utf8')

type RoleKey = 'admin' | 'doctor' | 'cs'
const ROLES: RoleKey[] = ['admin', 'doctor', 'cs']
const ROLE_ID: Record<RoleKey, string> = { admin: 'ROLE_ADMIN', doctor: 'ROLE_DOCTOR', cs: 'ROLE_CS' }
/** golden 里的调用样本值：路径模板的 ':p' 段一律替成它，Go 侧才能真发这个请求 */
const SAMPLE = 'S1'

// ===== 源 A：前端准入矩阵 + 模块词表 + 路由页目录 =====
interface FrontEnd {
  matrix: Record<RoleKey, string[]>
  keyOfPath: Map<string, string>
  dirOfPath: Map<string, string>
}

function parseFrontEnd(): FrontEnd {
  const src = read('apps/admin-web/src/router/permissions.ts')
  const block = src.match(/ROLE_PAGE_MATRIX[^{]*\{([\s\S]*?)\n\}/)
  if (!block) throw new Error('permissions.ts 里没定位到 ROLE_PAGE_MATRIX（改格式可以，别悄悄改）')
  const matrix = {} as Record<RoleKey, string[]>
  for (const m of block[1].matchAll(/(\w+):\s*\[([\s\S]*?)\]/g)) {
    matrix[m[1] as RoleKey] = [...m[2].matchAll(/'([^']+)'/g)].map((x) => x[1])
  }
  const keyOfPath = new Map<string, string>()
  for (const m of src.matchAll(/\{\s*key:\s*'([^']+)',\s*path:\s*'([^']+)'\s*\}/g)) keyOfPath.set(m[2], m[1])

  // 路由表里的 path -> 页面目录（不手写这张映射：加了页/改了目录名，门禁要跟着动才算有用）
  const routerSrc = read('apps/admin-web/src/router/index.ts')
  const pageRoutes = routerSrc.match(/export const pageRoutes[\s\S]*?\n\]/)
  if (!pageRoutes) throw new Error('router/index.ts 里没定位到 pageRoutes')
  const dirOfPath = new Map<string, string>()
  for (const m of pageRoutes[0].matchAll(/path:\s*'([^']+)'[\s\S]*?import\('\.\.\/pages\/([^/]+)\/index\.vue'\)/g)) {
    dirOfPath.set(m[1], m[2])
  }
  return { matrix, keyOfPath, dirOfPath }
}

// ===== 源 B：库授权（seed.sql 的 roles.permissions_json.modules）=====
function parseSeedModules(): Record<RoleKey, string[]> {
  const seed = read('scripts/db/seed/seed.sql')
  const out = {} as Record<RoleKey, string[]>
  for (const m of seed.matchAll(/'ROLE_(\w+)',[\s\S]*?"modules":(\[[^\]]*\])/g)) {
    const key = m[1].toLowerCase() as RoleKey
    if (ROLES.includes(key)) out[key] = JSON.parse(m[2]) as string[]
  }
  for (const r of ROLES) {
    if (!out[r]) throw new Error(`seed.sql 里没定位到 ROLE_${r.toUpperCase()} 的 modules`)
  }
  return out
}

// ===== 源 C：网关 rbac.go 六堆端点矩阵 =====
interface Pattern { method: string; path: string; segs: string[] }
interface Gateway {
  matrices: Record<string, Pattern[]>
  staffRoles: Set<string>
}
/** 网关矩阵名（少一个就说明 rbac.go 改了堆名或加了新堆，门禁必须知道） */
const GW_MATRICES = [
  'adminOnlyPatterns', 'staffOnlyPatterns', 'doctorAdminOnlyPatterns',
  'techAdminOnlyPatterns', 'provisionKeyPatterns', 'publicPatterns',
] as const

function stripGoComments(src: string): string {
  return src.replace(/\/\/[^\n]*/g, '').replace(/\/\*[\s\S]*?\*\//g, '')
}

function parseGateway(): Gateway {
  const src = stripGoComments(read('services/gateway/cmd/server/rbac.go'))
  const matrices: Record<string, Pattern[]> = {}
  for (const d of src.matchAll(/var\s+(\w+)\s*=\s*\[\]rbacPattern\{([\s\S]*?)\n\}/g)) {
    matrices[d[1]] = [...d[2].matchAll(/rbacOf\(http\.Method(\w+),\s*"([^"]+)"\)/g)]
      .map((x) => ({ method: x[1].toUpperCase(), path: x[2], segs: x[2].split('/') }))
  }
  for (const name of GW_MATRICES) {
    if (!matrices[name]) throw new Error(`rbac.go 里没解析到 ${name}（改名/换写法了？门禁的正则要同步，别让它静默失明）`)
  }
  const staff = src.match(/var\s+staffRoles\s*=\s*\[\]string\{([^}]*)\}/)
  if (!staff) throw new Error('rbac.go 里没定位到 staffRoles')
  const consts: Record<string, string> = {}
  for (const m of src.matchAll(/(\w+)\s*=\s*"(ROLE_[A-Z]+|[a-z]+)"/g)) consts[m[1]] = m[2]
  const staffRoles = new Set(staff[1].split(',').map((x) => consts[x.trim()] ?? x.trim()))
  return { matrices, staffRoles }
}

const hits = (gw: Gateway, name: string, method: string, path: string) =>
  (gw.matrices[name] ?? []).some((p) => {
    const segs = path.split('/')
    return p.method === method && p.segs.length === segs.length &&
      p.segs.every((s, i) => s.startsWith(':') || s === segs[i])
  })

/** 复刻 roleAuthz() 的判定顺序（只喂 /api/v1 业务路径，鉴权白名单不参与）。
 *  返回 deny=网关会不会回 403。与真实中间件的一致性由 testdata golden + Go 用例钉（见文件头第 5 条）。 */
function gatewayVerdict(gw: Gateway, roleId: string, method: string, path: string): { deny: boolean; why: string } {
  if (roleId === ROLE_ID.admin) return { deny: false, why: 'admin 全放行' }
  if (hits(gw, 'provisionKeyPatterns', method, path))
    return { deny: !['technician', 'patient'].includes(roleId), why: 'provision-key 角色白名单' }
  if (hits(gw, 'techAdminOnlyPatterns', method, path))
    return { deny: roleId !== 'technician', why: 'tech-or-admin 专属' }
  if (hits(gw, 'doctorAdminOnlyPatterns', method, path))
    return { deny: roleId !== ROLE_ID.doctor, why: 'doctor-or-admin 专属' }
  if (hits(gw, 'staffOnlyPatterns', method, path))
    return { deny: !gw.staffRoles.has(roleId), why: 'staff-only' }
  if (hits(gw, 'adminOnlyPatterns', method, path)) return { deny: true, why: 'admin-only' }
  if (hits(gw, 'publicPatterns', method, path)) return { deny: false, why: 'public' }
  return { deny: true, why: '未登记 → T260 默认拒绝' }
}
const isRegistered = (gw: Gateway, method: string, path: string) =>
  GW_MATRICES.some((name) => hits(gw, name, method, path))

// ===== 页面 → 端点（派生，不手写）=====
interface Call { method: string; path: string; fn: string }

function parseApiFunctions(): Map<string, Call[]> {
  const files = ['apps/admin-web/src/api/index.ts', 'apps/admin-web/src/api/flow.ts', 'apps/admin-web/src/api/medicalAccount.ts']
  const out = new Map<string, Call[]>()
  for (const rel of files) {
    const src = read(rel)
    const decls = [...src.matchAll(/export\s+(?:async\s+)?function\s+(\w+)|export\s+const\s+(\w+)\s*=/g)]
      .map((m) => ({ name: m[1] ?? m[2], at: m.index ?? 0 }))
    decls.forEach((d, i) => {
      const body = src.slice(d.at, i + 1 < decls.length ? decls[i + 1].at : src.length)
      const calls: Call[] = []
      for (const c of body.matchAll(/url:\s*(['`])([^'`]*)\1([^)]*)/g)) {
        let url = c[2]
        if (!url.startsWith('/api/v1/')) continue // OSS 直传等不经网关的 url 不算端点
        url = url.replace(/\$\{[^}]*\}/g, ':p').split('?')[0]
        const method = c[3].match(/method:\s*'(\w+)'/)?.[1] ?? 'GET'
        if (!['GET', 'POST', 'PUT', 'DELETE', 'PATCH'].includes(method)) {
          throw new Error(`${rel} 的 ${d.name} 里有无法识别的 method: ${method}`)
        }
        calls.push({ method, path: url, fn: d.name })
      }
      if (calls.length) out.set(d.name, calls)
    })
  }
  return out
}

/** 页面目录（含其子组件）import 的 api 函数名集合 */
function parsePageImports(): Map<string, Set<string>> {
  const pagesDir = join(repoRoot, 'apps/admin-web/src/pages')
  const files: string[] = []
  const walk = (d: string) => {
    for (const n of readdirSync(d)) {
      const p = join(d, n)
      if (statSync(p).isDirectory()) walk(p)
      else if (/\.(?:vue|ts)$/.test(n)) files.push(p)
    }
  }
  walk(pagesDir)
  const byDir = new Map<string, Set<string>>()
  for (const f of files) {
    const rel = relative(pagesDir, f).split(/[\\/]/)
    const dir = rel[0]
    const set = byDir.get(dir) ?? new Set<string>()
    for (const h of readFileSync(f, 'utf8').matchAll(/import\s*(?:type\s*)?\{([^}]*)\}\s*from\s*['"][^'"]*api[^'"]*['"]/gs)) {
      for (const raw of h[1].split(',')) {
        const name = raw.trim().replace(/^type\s+/, '').split(/\s+as\s+/)[0].trim()
        if (/^[a-zA-Z]\w*$/.test(name)) set.add(name)
      }
    }
    byDir.set(dir, set)
  }
  return byDir
}

interface Derived {
  front: FrontEnd
  db: Record<RoleKey, string[]>
  gw: Gateway
  /** 角色 -> 该角色可见页的无条件调用集 */
  callsOfRole: Map<RoleKey, Map<string, Call[]>>
  /** 页面 -> 该页 import 出的端点（供差异表定位） */
  callsOfPage: Map<string, Call[]>
}

function derive(): Derived {
  const front = parseFrontEnd()
  const fns = parseApiFunctions()
  const imports = parsePageImports()
  const callsOfPage = new Map<string, Call[]>()
  for (const [path, dir] of front.dirOfPath) {
    const seen = new Map<string, Call>()
    for (const name of imports.get(dir) ?? []) {
      for (const c of fns.get(name) ?? []) seen.set(`${c.method} ${c.path}`, c)
    }
    callsOfPage.set(path, [...seen.values()])
  }
  const callsOfRole = new Map<RoleKey, Map<string, Call[]>>()
  for (const role of ROLES) {
    const m = new Map<string, Call[]>()
    for (const p of front.matrix[role]) m.set(p, callsOfPage.get(p) ?? [])
    callsOfRole.set(role, m)
  }
  return { front, db: parseSeedModules(), gw: parseGateway(), callsOfRole, callsOfPage }
}

// ===== 已登记差异（网关按设计拒，或已实测待修的缺陷）=====
interface Divergence {
  role: RoleKey
  page: string
  method: string
  path: string
  /** guarded = 前端有角色守卫，该角色实际不会发这一枪；defect = 无守卫，现网必吃 403 */
  kind: 'guarded' | 'defect'
  /** 守卫落点（文件:行）或缺陷归口，评审要能一眼查到 */
  at: string
  why: string
}

const key = (d: { role: string; method: string; path: string }) => `${d.role}|${d.method} ${d.path}`
const REGISTERED_DIVERGENCES: Divergence[] = [
  // 告警管理页两张「配置」Tab：网关把规则/模板写端点收口 admin-only，前端按 T351 的判据整块摘掉
  { role: 'doctor', page: '/alerts', method: 'GET', path: '/api/v1/admin/alert-rules', kind: 'guarded', at: 'apps/admin-web/src/pages/alerts/index.vue:376 loadRules 首行 if (!canConfigure.value) return', why: 'Tab2 规则配置 v-if canConfigure（index.vue:99），非 admin 不发也不见' },
  { role: 'doctor', page: '/alerts', method: 'PUT', path: '/api/v1/admin/alert-rules/points', kind: 'guarded', at: 'apps/admin-web/src/pages/alerts/index.vue:99 el-tab-pane v-if="canConfigure"', why: '保存按钮在该 Tab 内，非 admin 无入口' },
  { role: 'doctor', page: '/alerts', method: 'POST', path: '/api/v1/admin/alert-rules/points/reset', kind: 'guarded', at: 'apps/admin-web/src/pages/alerts/index.vue:99 el-tab-pane v-if="canConfigure"', why: '同上' },
  { role: 'doctor', page: '/alerts', method: 'PUT', path: '/api/v1/admin/alert-rules/global', kind: 'guarded', at: 'apps/admin-web/src/pages/alerts/index.vue:99 el-tab-pane v-if="canConfigure"', why: '同上' },
  { role: 'doctor', page: '/alerts', method: 'POST', path: '/api/v1/admin/flow/templates', kind: 'guarded', at: 'apps/admin-web/src/pages/alerts/index.vue:191 流程配置 Tab v-if="canConfigure"（T274 设计器写路由 admin-only）', why: 'Tab4 整块摘掉；Tab3 运行态的两条读已在 T359 移入 staffOnlyPatterns' },
  { role: 'doctor', page: '/alerts', method: 'PUT', path: '/api/v1/admin/flow/templates/:p', kind: 'guarded', at: 'apps/admin-web/src/pages/alerts/index.vue:191', why: '同上' },
  { role: 'doctor', page: '/alerts', method: 'DELETE', path: '/api/v1/admin/flow/templates/:p', kind: 'guarded', at: 'apps/admin-web/src/pages/alerts/index.vue:191', why: '同上' },
  { role: 'doctor', page: '/alerts', method: 'GET', path: '/api/v1/doctors', kind: 'guarded', at: 'apps/admin-web/src/pages/alerts/flow/FlowRuntime.vue:433 if (!canReadRoster.value) return', why: 'T359：转派候选人名录是 admin 域端点，非 admin 手输账号 ID' },
  { role: 'doctor', page: '/orthosis-log', method: 'GET', path: '/api/v1/teams', kind: 'guarded', at: 'apps/admin-web/src/pages/orthosis-log/index.vue:660 if (auth.role === \'admin\') fetchTeams()', why: 'T348 同类处置：团队名映射非 admin 不发' },
  // 门禁上线当天抓到的第一条真缺陷：无角色守卫，医护打开「数据视图」必吃 403 红条 + 图空白（现网实测见 T379 卡内交件）
  { role: 'doctor', page: '/orthosis-log', method: 'GET', path: '/api/v1/admin/settings', kind: 'defect', at: 'apps/admin-web/src/pages/orthosis-log/index.vue:478 loadWearSeries 无条件 fetchSystemSettings()', why: '缺陷待修：网关 admin-only 正确，前端照打违反 T348/T359 已立的「非 admin 不照打」口径；修法（不照打后阈值线怎么画）需产品定，请 PM 立缺陷卡' },
]
const registeredDeny = new Map(REGISTERED_DIVERGENCES.map((d) => [key(d), d]))

// ===== 派生网关判定表（golden 的内容）=====
interface GatewayRow { role: string; method: string; path: string; deny: boolean; why: string }
const GOLDEN = 'services/gateway/cmd/server/testdata/perm-gateway-table.json'

function buildGatewayTable(d: Derived): GatewayRow[] {
  const rows = new Map<string, GatewayRow>()
  for (const role of ROLES) {
    for (const calls of (d.callsOfRole.get(role) ?? new Map()).values()) {
      for (const c of calls) {
        const v = gatewayVerdict(d.gw, ROLE_ID[role], c.method, c.path)
        rows.set(`${ROLE_ID[role]}|${c.method} ${c.path}`, { role: ROLE_ID[role], method: c.method, path: c.path, deny: v.deny, why: v.why })
      }
    }
  }
  return [...rows.values()].sort((a, b) => `${a.role}${a.method}${a.path}`.localeCompare(`${b.role}${b.method}${b.path}`))
}
const concrete = (path: string) => path.split(':p').join(SAMPLE)
const byRowKey = (a: { role: string; method: string; path: string }, b: { role: string; method: string; path: string }) =>
  `${a.role}${a.method}${a.path}`.localeCompare(`${b.role}${b.method}${b.path}`)

describe('T379 权限矩阵三源跨源门禁', () => {
  const d = derive()

  it('派生本身可信：每角色每可见页都派生出了端点（派生空 = 门禁失明，先判红）', () => {
    const empty: string[] = []
    for (const role of ROLES) {
      for (const [page, calls] of d.callsOfRole.get(role) ?? []) {
        if (!calls.length) empty.push(`${role} ${page}`)
      }
    }
    expect(empty, '这些「角色可见页」没派生出任何 api 调用，说明页面→端点的派生失效了：' +
      '页面若不再经 src/api 取数（直接 import request 或换了目录名），本门禁对它就瞎了').toEqual([])
  })

  it('词表同源：前端可见页都能换算模块短键、都有页面目录', () => {
    for (const role of ROLES) {
      for (const p of d.front.matrix[role]) {
        expect(d.front.keyOfPath.has(p), `前端矩阵给 ${role} 放了 ${p}，但 PAGE_MODULES 没有这个路径的短键（库侧无从表达）`).toBe(true)
        expect(d.front.dirOfPath.has(p), `${p} 在 pageRoutes 里找不到对应 pages/ 目录`).toBe(true)
      }
    }
  })

  it('A↔B：每 (角色, 页面) 格，前端准入与库 modules 双向一致', () => {
    const cells: string[] = []
    for (const role of ROLES) {
      // 换不出短键的页由上一条用例单独判红，这里 filter 掉只为满足类型，不吞差异
      const ui = new Set((d.front.matrix[role] ?? [])
        .map((p) => d.front.keyOfPath.get(p))
        .filter((k): k is string => !!k))
      const db = new Set(d.db[role])
      for (const k of [...ui].filter((x) => !db.has(x))) cells.push(`${role}: 前端可见但库未授权 ${k}`)
      for (const k of [...db].filter((x) => !ui.has(x))) cells.push(`${role}: 库授权但前端不可见 ${k}`)
    }
    expect(cells, '前端准入矩阵与 seed.sql 的 roles.permissions_json.modules 对不上：\n' + cells.join('\n')).toEqual([])
  })

  it('C 登记完整：页面用到的每个端点都在网关某堆矩阵里（否则 T260 默认拒绝，admin 也吃 403）', () => {
    const unregistered: string[] = []
    for (const [page, calls] of d.callsOfPage) {
      for (const c of calls) {
        if (!isRegistered(d.gw, c.method, c.path)) unregistered.push(`${page} <- ${c.fn}: ${c.method} ${c.path}`)
      }
    }
    expect(unregistered, '这些端点没登记进 rbac.go 任何矩阵（网关会默认 403）：\n' + unregistered.join('\n')).toEqual([])
  })

  it('A→C：角色能进的页，其端点网关必须放行；只有登记表里的差异可以存在', () => {
    const found: string[] = []
    const hit = new Set<string>()
    for (const role of ROLES) {
      for (const [page, calls] of d.callsOfRole.get(role) ?? []) {
        for (const c of calls) {
          const v = gatewayVerdict(d.gw, ROLE_ID[role], c.method, c.path)
          if (!v.deny) continue
          const k = key({ role, method: c.method, path: c.path })
          const reg = registeredDeny.get(k)
          if (!reg || reg.page !== page) { found.push(`${k} 页面 ${page}：网关 ${v.why}，且不在登记表（或登记的页面不符）`); continue }
          hit.add(k)
        }
      }
    }
    const stale = REGISTERED_DIVERGENCES.map((x) => key(x)).filter((k) => !hit.has(k))
    expect(found, '新增的「可见页 × 网关 403」差异（要么前端加守卫，要么改网关矩阵，别直接往登记表塞）：\n' +
      found.join('\n')).toEqual([])
    expect(stale, '登记表里这些条目已经不再命中（多半是缺陷修好了）：请删掉对应条目，否则门禁对它彻底失明：\n' +
      stale.join('\n')).toEqual([])
  })

  it('已登记差异的定性不漂移：defect 条目只准减少，不许被当成 guarded 蒙过', () => {
    const defects = REGISTERED_DIVERGENCES.filter((x) => x.kind === 'defect')
    for (const x of defects) {
      expect(x.at, `${key(x)} 是缺陷条目，必须写清「无守卫」的落点（文件:行）`).toMatch(/\.vue:\d+/)
      expect(x.why, `${key(x)} 的处置口径要写给 PM 看（谁裁、怎么修）`).toContain('PM')
    }
    // 现网实测：网关对这条确实回 403（静态复刻与真实行为的一致性另由 golden + Go 用例钉）
    const v = gatewayVerdict(d.gw, ROLE_ID.doctor, 'GET', '/api/v1/admin/settings')
    expect(v.deny, '医护打 GET /admin/settings 若已不再 403，缺陷条目该删（上一条用例会同时叫）').toBe(true)
  })

  it('网关判定表 golden 与派生一致（不一致就重跑 UPDATE_GATEWAY_TABLE=1）', () => {
    const rows = buildGatewayTable(d)
    if (process.env.UPDATE_GATEWAY_TABLE) {
      writeFileSync(join(repoRoot, GOLDEN), JSON.stringify({
        generatedBy: 'apps/admin-web/test/perm-gateway-cross-source.spec.ts (UPDATE_GATEWAY_TABLE=1)',
        note: `T379 三源门禁的网关判定表。path 里的参数段以 ${SAMPLE} 为样本值，网关侧 Go 用例逐行发真实请求复核` +
          '（rbac_perm_gate_t379_test.go）：deny 行须回 403，pass 行须非 403。',
        sample: SAMPLE,
        rows: rows.map((r) => ({ ...r, path: concrete(r.path) })).sort(byRowKey),
      }, null, 2) + '\n', 'utf8')
      return
    }
    const golden = JSON.parse(read(GOLDEN)) as { sample: string; rows: GatewayRow[] }
    expect(golden.sample, 'golden 里的样本值与本文件 SAMPLE 不一致').toBe(SAMPLE)
    const norm = (r: GatewayRow) => `${r.role}|${r.method} ${concrete(r.path)}|${r.deny ? 'deny' : 'pass'}|${r.why}`
    const want = rows.map(norm).sort()
    const have = golden.rows.map((r) => norm({ ...r, path: r.path.split(SAMPLE).join(':p') })).sort()
    expect(have, 'golden 与当前三源派生结果不一致（有人改了页面端点/矩阵而没重跑生成）：\n' +
      `新增/变更：${want.filter((x) => !have.includes(x)).join(', ') || '无'}\n` +
      `已失效：${have.filter((x) => !want.includes(x)).join(', ') || '无'}\n` +
      '重跑：UPDATE_GATEWAY_TABLE=1 npx vitest run test/perm-gateway-cross-source.spec.ts').toEqual(want)
    expect(golden.rows.length, 'golden 行数不得为 0（空表等于没钉）').toBeGreaterThan(0)
  })
})
