// T372 第 14 格防回潮：权限控制页「减权方向」必须能保存。
//
// 现场（Alice 2026-09-24 22:20 现网写读闭环）：在权限矩阵页取消勾选一个带子权限的模块
// （如「告警管理」）后点「保存权限配置」，现网必 400 / code 10400 —— 前端 PUT 的 items
// 里仍留着 `alerts.view` 这类键，而后端 validatePermissionItems
// (services/user-service/internal/handler/permissions_t257.go) 要求每个子权限的所属模块
// 必须在 modules 里，否则是一条永远渲染不出来的死配置。
// 修前 main 上此页零用例（全仓无一条挂载 pages/roles）⇒ 三道前端门禁全绿、只有现网暴露。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises, type DOMWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import type { RolePermissions } from '@bracesync/shared-types'

// GET /admin/roles/{id}/permissions 的实况形状（后端恒回 items 数组）：三个带子权限的模块
// 外加一项无子权限的 abnormal_report（子权限目录按 T368 已裁口径保持 9 组 23 项未收组）
const LOADED: RolePermissions = {
  scope: 'team',
  modules: ['patients', 'alerts', 'comm', 'abnormal_report'],
  items: [
    'patients.view',
    'patients.edit',
    'alerts.view',
    'alerts.process',
    'alerts.config_rules',
    'comm.view_feedback',
  ],
}

vi.mock('../src/api', () => ({
  fetchAdminRoles: vi.fn(async () => [
    {
      roleId: 'ROLE_TEST_MATRIX', name: '测试角色', description: '挂该页用',
      memberCount: 1, createdAt: '2026-09-24T10:00:00Z', status: 'enabled', preset: false,
    },
  ]),
  fetchRolePermissionsApi: vi.fn(async (): Promise<RolePermissions> => ({
    ...LOADED,
    modules: [...LOADED.modules],
    items: LOADED.items ? [...LOADED.items] : undefined,
  })),
  updateRolePermissionsApi: vi.fn(async (): Promise<void> => undefined),
  fetchRoleTemplates: vi.fn(async () => []),
  createRoleApi: vi.fn(async () => undefined),
  updateRoleApi: vi.fn(async () => undefined),
  deleteRoleApi: vi.fn(async () => undefined),
}))

import * as api from '../src/api'
import RolesPage from '../src/pages/roles/index.vue'

async function settle() {
  for (let i = 0; i < 5; i++) await flushPromises()
}

// 页面结构：左卡＝角色列表（点行 selectRole 才拉权限），右卡＝功能模块 × 角色 权限矩阵
async function openMatrix() {
  const wrapper = mount(RolesPage, {
    attachTo: document.body,
    global: { plugins: [ElementPlus] },
  })
  await settle()
  const cards = wrapper.findAll('.page-card')
  await cards[0].findAll('tbody tr')[0].trigger('click')
  await settle()
  // 基数判据：PAGE_MODULES 16 键（T372 抬到 16）＝矩阵行数
  expect(cards[1].findAll('tbody tr').length).toBe(16)
  return { wrapper, matrix: cards[1] }
}

// 每次按「页面/模块」列文案现查该行——勾选会触发 el-table 重渲染，握着旧 DOMWrapper 不稳
function rowOf(matrix: DOMWrapper<Element>, page: string): DOMWrapper<Element> {
  const hit = matrix.findAll('tbody tr').filter((r) => r.findAll('td')[0].text().trim() === page)
  expect(hit.length, `矩阵里找不到页面「${page}」`).toBe(1)
  return hit[0]
}

async function toggle(matrix: DOMWrapper<Element>, page: string, next: boolean) {
  const row = rowOf(matrix, page)
  await row.find('input[type="checkbox"]').setValue(next)
  await settle()
  const checked = (rowOf(matrix, page).find('input[type="checkbox"]').element as HTMLInputElement).checked
  expect(checked, `勾回后「${page}」行状态没变`).toBe(next)
}

async function save(matrix: DOMWrapper<Element>) {
  const btn = matrix.findAll('button').find((b) => b.text().trim() === '保存权限配置')
  expect(btn, '找不到「保存权限配置」按钮').toBeTruthy()
  await btn!.trigger('click')
  await settle()
}

function lastSaved(): RolePermissions {
  const calls = vi.mocked(api.updateRolePermissionsApi).mock.calls
  expect(calls.length, '没有发出保存请求').toBe(1)
  return calls[0][1] as RolePermissions
}

describe('权限控制页 · 减权方向保存（T372 第 14 格）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    document.body.innerHTML = ''
  })

  it('取消勾选带子权限的模块后保存：items 不再残留该模块前缀的键', async () => {
    const { matrix } = await openMatrix()

    await toggle(matrix, '告警管理', false)
    await save(matrix)

    const payload = lastSaved()
    expect(payload.modules).not.toContain('alerts')
    expect(payload.items?.filter((k) => k.startsWith('alerts.'))).toEqual([])
    // 正向对照：只剪被关掉的那个模块，其余模块的子权限一条都不许掉
    expect(payload.items).toEqual(expect.arrayContaining([
      'patients.view', 'patients.edit', 'comm.view_feedback',
    ]))
    expect(payload.items).toHaveLength(3)
    // scope 不被顺带改写
    expect(payload.scope).toBe('team')
  })

  it('关掉再勾回：items 快照不被原地洗成空集', async () => {
    const { matrix } = await openMatrix()

    await toggle(matrix, '告警管理', false)
    await toggle(matrix, '告警管理', true)
    await save(matrix)

    const payload = lastSaved()
    expect(payload.modules).toContain('alerts')
    expect(payload.items).toEqual(LOADED.items)
  })

  it('取消勾选没有子权限的模块：本来就能保存，items 不受牵连', async () => {
    const { matrix } = await openMatrix()

    await toggle(matrix, '异常报告', false)
    await save(matrix)

    const payload = lastSaved()
    expect(payload.modules).not.toContain('abnormal_report')
    expect(payload.items).toEqual(LOADED.items)
  })

  it('库里未细化（GET 不回 items）时保存：省略语义保持省略，不被剪成空数组', async () => {
    vi.mocked(api.fetchRolePermissionsApi).mockImplementationOnce(async () => ({
      scope: 'team',
      modules: [...LOADED.modules],
    }))
    const { matrix } = await openMatrix()

    await toggle(matrix, '告警管理', false)
    await save(matrix)

    const payload = lastSaved()
    expect(payload.modules).not.toContain('alerts')
    expect(payload.items).toBeUndefined()
  })
})
