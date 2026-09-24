import { test, expect, type Page } from '@playwright/test'
import { realLogin, menuItems, getAuthToken } from '../real-helpers'
import { requireDeployedBuild } from '../deploy-guard'

/**
 * T368 · 医护侧栏/路由越权：前端准入矩阵与库 ROLE_DOCTOR.modules 不同源
 *
 * 修前现场（staging 现网只读实测，2026-09-24 10:38-10:44，原文见 docs 仓
 * docs/tasks/iris/T368-医护页面准入同源-证据/修前现场-me-permissions.txt）：
 *   doctor_li  GET /api/v1/admin/me/permissions → modules(4) = dashboard/realtime/alerts/orthosis
 *              同一时刻侧栏 6 项（多出 复查报告 / 复查模板管理）
 *   ops_admin  modules(15) ← 与前端矩阵恰好相等，所以这条分叉在 admin 上看不出来
 *   cs_wang    modules(1)  ← 同上
 *
 * 修法 = Boss 2026-09-24 09:3x 裁定 (a)「补库」：迁移 000028 给 ROLE_DOCTOR.modules
 *   补 review / review_tpl 两键（前端 6 页不动）。
 *
 * 三条用例的分工（判据全部是「同一账号、同一时刻、两个来源数出来的页数相等」，
 * 而不是各自钉一个常量 —— 常量只会跟着改动一起被改，等式才会留住分叉）：
 *   14.1 医护：补库后两处相等（post-deploy：迁移未上前 staging 仍是 4 vs 6，按守卫跳过）；
 *   14.2 运营：不退化（当前 staging 已相等，本条现在就真跑）；
 *   14.3 客服：不退化（同上）。
 *
 * T372 追加第 16 个页面模块 abnormal_report（迁移 000029）⇒ 上面两行的终值都变了：
 * doctor 6 → 7、admin 15 → 16。两侧常量若留在 6/15，000029 前滚到 staging 的那一刻
 * 这两条就红在「自己钉的数」上而不是分叉上，所以一并抬到终值；
 * 而 000029 前滚之前 staging 仍回旧值 ⇒ 两条都挂 deploy-guard（14.2 原先无守卫，
 * 因为它的旧终值当时已在 staging 现网成立）。守卫只问「库侧到没到终值」，
 * 前滚后自动转真跑。
 */

interface MePerms {
  roleId: string
  scope: string
  modules: string[]
}

/** 以当前登录态读 /me/permissions（接口侧的「库」，与侧栏的前端矩阵是两个来源） */
async function readMePermissions(page: Page): Promise<MePerms> {
  const token = await getAuthToken(page)
  expect(token, '应拿到当前账号的 JWT').toBeTruthy()
  const res = await page.request.get('/api/v1/admin/me/permissions', {
    headers: { Authorization: `Bearer ${token}` },
  })
  expect(res.status(), 'GET /me/permissions 应 200').toBe(200)
  const body = await res.json()
  expect(body?.code, '业务码应为 0').toBe(0)
  const d = body.data
  expect(Array.isArray(d?.modules), 'data.modules 应为数组').toBe(true)
  return { roleId: String(d.roleId), scope: String(d.scope), modules: d.modules as string[] }
}

/** 侧栏可见项文案（前端矩阵换算出来的页面准入） */
async function sidebarTitles(page: Page): Promise<string[]> {
  const items = page.locator('.sidebar-menu .el-menu-item')
  await expect(items.first()).toBeVisible({ timeout: 20_000 })
  const texts = await items.allInnerTexts()
  return texts.map((t) => t.trim()).filter(Boolean)
}

test.describe('14-角色页面准入与库同源（T368）', () => {
  test('14.1 doctor_li：侧栏页数 = /me/permissions 项数 = 7（T368 补 review/review_tpl，T372 补 abnormal_report）', async ({ page }) => {
    await realLogin(page, 'doctor_li')
    const sidebar = await sidebarTitles(page)
    // 守卫之前先读数：否则「跳过」= 两个数都没采到，卡上拿不出修前现场（post-deploy 阶段
    // 这条日志就是比对两侧的原始证据，CI run 日志可反查）。
    const pre = await readMePermissions(page)
    console.log(`[e2e-real][t368] doctor_li 侧栏 ${sidebar.length} 项 = ${JSON.stringify(sidebar)} ` +
      `| /me/permissions ${pre.modules.length} 项 = ${JSON.stringify(pre.modules)}`)

    await requireDeployedBuild(page, {
      marker: 'T372-doctor-abnormal-report-module',
      why: '000028 已前滚（staging 现为 6 项），000029 未前滚 ⇒ /me/permissions 仍少 abnormal_report，本条判据无意义',
      probe: async () => (await readMePermissions(page)).modules.length === 7,
    })

    const perms = await readMePermissions(page)
    expect(perms.roleId, '登录账号应是医护').toBe('ROLE_DOCTOR')
    expect(perms.scope, '补模块键不该顺带改数据范围').toBe('team')
    // 两个来源逐元素相等：接口按 PAGE_MODULES 序回，侧栏按 pageRoutes 序渲染
    expect(perms.modules).toEqual(['dashboard', 'realtime', 'abnormal_report', 'alerts', 'orthosis', 'review', 'review_tpl'])
    expect(sidebar, `侧栏 ${sidebar.length} 项 vs 接口 ${perms.modules.length} 项 = 又分叉了`).toHaveLength(7)
    // 侧栏每项文案含 emoji 图标（「📄 复查报告」），所以用子串存在性而非整等 —— toContain 对数组
    // 是「元素整等」，写成 toContain('复查报告') 会永远判红。
    expect(sidebar.some((t) => t.includes('复查报告')), '医护侧栏应有「复查报告」').toBe(true)
    expect(sidebar.some((t) => t.includes('复查模板管理')), '医护侧栏应有「复查模板管理」').toBe(true)
  })

  test('14.2 ops_admin：侧栏页数 = /me/permissions 项数 = 16（不退化，T372 补 abnormal_report）', async ({ page }) => {
    await realLogin(page)
    const sidebar = await sidebarTitles(page)
    const pre = await readMePermissions(page)
    console.log(`[e2e-real][t372] ops_admin 侧栏 ${sidebar.length} 项 | /me/permissions ${pre.modules.length} 项`
      + ` = ${JSON.stringify(pre.modules)}`)

    await requireDeployedBuild(page, {
      marker: 'T372-admin-abnormal-report-module',
      why: '000029 未前滚到 staging 库 ⇒ /me/permissions 仍回 000026 的 15 项，缺 abnormal_report',
      probe: async () => (await readMePermissions(page)).modules.length === 16,
    })

    const perms = await readMePermissions(page)
    expect(perms.roleId).toBe('ROLE_ADMIN')
    expect(perms.modules).toHaveLength(16)
    expect(perms.modules.indexOf('abnormal_report'), '模块序 = 侧栏序：异常报告在患者管理之后、团队管理之前')
      .toBe(3)
    expect(sidebar).toHaveLength(16)
    expect(sidebar.some((t) => t.includes('异常报告')), '运营侧栏应有「异常报告」').toBe(true)
  })

  test('14.3 cs_wang：侧栏页数 = /me/permissions 项数 = 1（不退化）', async ({ page }) => {
    await realLogin(page, 'cs_wang')
    const sidebar = await sidebarTitles(page)
    const perms = await readMePermissions(page)
    expect(perms.roleId).toBe('ROLE_CS')
    expect(perms.modules).toEqual(['comm'])
    expect(sidebar).toHaveLength(1)
    expect(sidebar[0]).toContain('患者沟通')
  })
})
