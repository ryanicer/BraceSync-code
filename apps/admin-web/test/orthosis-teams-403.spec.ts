// T350 第 8 轮打回项 2：矫形日志页不得替医护去打 GET /api/v1/teams。
//
// 现场（Ella T345 §六 观察项 R-teams）：doctor_li 进该页，同页其余请求都 200，只有
// GET /api/v1/teams 回 403 —— 网关 rbac.go 的 adminOnlyPatterns 把它收在 admin 专属，
// 这是正确行为，不该放开全量。而这一枪只为填组织字典给基本信息卡兜底，团队名本就由
// GET /admin/patients/:id 的 teamName 字段带出（user-service patientSelect LEFT JOIN teams），
// ⇒ 处置取「非 admin 不发这一枪」，与 T348 对 dashboard 页的口径一致（见 dashboard-role-403.spec.ts）。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus, { ElMessage } from 'element-plus'
import type { RoleKey } from '../src/router/permissions'

vi.mock('../src/api', () => ({
  // 医护 token 打过去必 403，按网关返回原文造
  fetchTeams: vi.fn(async (): Promise<never> => {
    throw new Error('forbidden: role not allowed for this endpoint')
  }),
  fetchPatients: vi.fn(async () => ({ list: [], total: 0, page: 1, pageSize: 50 })),
  fetchPatientDetail: vi.fn(async () => null),
  fetchAlerts: vi.fn(async () => ({ list: [], total: 0, page: 1, pageSize: 10 })),
  fetchSystemSettings: vi.fn(async () => ({ wearingTargetHours: 8 })),
  fetchOrthosisPlans: vi.fn(async () => []),
  saveOrthosisPlanApi: vi.fn(async () => ({})),
  fetchFeelingLogs: vi.fn(async () => []),
  fetchFeelingLogsAdmin: vi.fn(async () => ({ list: [], total: 0, page: 1, pageSize: 20 })),
  fetchPatientDailyWear: vi.fn(async () => []),
  fetchHealthReports: vi.fn(async () => []),
  replyFeelingLogApi: vi.fn(async () => ({})),
  patientNameOf: (id: string) => id,
  teamNameOf: (id: string) => id,
}))

import * as api from '../src/api'
import { useAuthStore } from '../src/stores/auth'
import OrthosisLogPage from '../src/pages/orthosis-log/index.vue'

async function mountAs(role: RoleKey | null) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.user = role ? { name: '验收账号', role } : null
  const wrapper = mount(OrthosisLogPage, { global: { plugins: [pinia, ElementPlus] } })
  for (let i = 0; i < 5; i++) await flushPromises()
  return wrapper
}

describe('矫形日志页 · GET /api/v1/teams 按角色发（T350 R-teams）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    document.body.innerHTML = ''
  })

  it('医护不发这一枪：该端点网关 admin 专属，打过去必 403', async () => {
    await mountAs('doctor')
    expect(vi.mocked(api.fetchTeams)).not.toHaveBeenCalled()
  })

  it('客服同样不发（它也进不了本页，这里只锁发请求的判据）', async () => {
    await mountAs('cs')
    expect(vi.mocked(api.fetchTeams)).not.toHaveBeenCalled()
  })

  it('未知角色 / 未登录按不可读处理，与路由守卫 fail-closed 同向', async () => {
    await mountAs(null)
    expect(vi.mocked(api.fetchTeams)).not.toHaveBeenCalled()
  })

  // 反证：上一条不能是「恒不调用」的空锁 —— 运营的兜底字典照旧要填
  it('运营管理员仍取团队字典（判据不是写死 false）', async () => {
    vi.mocked(api.fetchTeams).mockResolvedValueOnce([] as never)
    await mountAs('admin')
    expect(vi.mocked(api.fetchTeams)).toHaveBeenCalledTimes(1)
  })

  it('医护进页不因缺 /teams 报错或弹错误提示（其余取数照常）', async () => {
    const errorSpy = vi.spyOn(ElMessage, 'error')
    const wrapper = await mountAs('doctor')

    expect(errorSpy).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('患者工作台')
    expect(vi.mocked(api.fetchFeelingLogsAdmin)).toHaveBeenCalled()
    expect(vi.mocked(api.fetchPatients)).toHaveBeenCalled()
    errorSpy.mockRestore()
  })
})
