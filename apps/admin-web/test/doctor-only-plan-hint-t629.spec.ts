// T629 方案C：非医生点「保存新方案」要看到「仅医生开放」的明确提示，且请求不发出。
//
// 现场（派发单 §一）：运营管理员在「矫形日志·患者工作台」点保存 → 横幅「没有该操作的权限，
// 请联系管理员」。这一句是展示层按信封码 10403 查表（packages/shared-utils/src/errorCopy.ts:40）
// 得到的通用句：网关 rbac.go 的 doctorAdminOnlyPatterns 放 admin 过，真正拒的是 user-service
// savePlan 的医生身份判定（handler.go:1577-1585，doctors.admin_id 查不到行即 403）。
// ⇒ 按钮保留、权限矩阵不动，点击时按角色就地说明并阻止这一枪（utils/doctorOnlyAccess.ts）。
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus, { ElMessage, ElSelect } from 'element-plus'
import { userErrorCopy } from '@bracesync/shared-utils'
import type { RoleKey } from '../src/router/permissions'
import { canSaveOrthosisPlan, DOCTOR_ONLY_HINT } from '../src/utils/doctorOnlyAccess'

vi.mock('../src/api', () => ({
  fetchTeams: vi.fn(async () => []),
  fetchPatients: vi.fn(async () => ({ list: [], total: 0, page: 1, pageSize: 50 })),
  fetchPatientDetail: vi.fn(async () => null),
  fetchAlerts: vi.fn(async () => ({ list: [], total: 0, page: 1, pageSize: 10 })),
  fetchSystemSettings: vi.fn(async () => ({ wearingTargetHours: 8 })),
  fetchOrthosisPlans: vi.fn(async () => []),
  saveOrthosisPlanApi: vi.fn(async () => ({
    planId: 'PLAN-T629',
    patientId: 'PAT-T629',
    doctorId: 'DOC-T629',
    content: '新版方案',
    version: 'v1',
    createdAt: '2026-10-09T00:00:00.000Z',
  })),
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

/** 进页 → 选中患者（工作台面板 v-if="patientId" 才渲染）→ 填方案正文 → 点「保存新方案」 */
async function clickSaveAs(role: RoleKey | null) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.user = role ? { name: '验收账号', role } : null
  const wrapper = mount(OrthosisLogPage, { global: { plugins: [pinia, ElementPlus] } })
  for (let i = 0; i < 5; i++) await flushPromises()

  const select = wrapper.findAllComponents(ElSelect).find((s) => s.classes('patient-select'))
  if (!select) throw new Error('患者下拉未渲染（工作台面板的入口条件变了？）')
  await select.vm.$emit('update:modelValue', 'PAT-T629')
  for (let i = 0; i < 3; i++) await flushPromises()
  await wrapper.find('textarea').setValue('新版矫形方案：夜间佩戴 10 小时')
  await wrapper.find('.save-btn').trigger('click')
  for (let i = 0; i < 3; i++) await flushPromises()
  return wrapper
}

describe('T629 闸门判据 · canSaveOrthosisPlan', () => {
  it('只有 doctor 放行（真值表逐格）', () => {
    expect(canSaveOrthosisPlan('doctor')).toBe(true)
    expect(canSaveOrthosisPlan('admin')).toBe(false)
    expect(canSaveOrthosisPlan('cs')).toBe(false)
  })

  // 反证：上一条若写成恒 false，第 1 格就红了；若写成恒 true，这三格就红了
  it('未知角色 / 未登录 / 缺参一律 fail-closed，与路由守卫同向', () => {
    expect(canSaveOrthosisPlan(null)).toBe(false)
    expect(canSaveOrthosisPlan(undefined)).toBe(false)
  })
})

describe('T629 患者工作台 · 非医生点「保存新方案」', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    document.body.innerHTML = ''
  })

  it('运营管理员：出「仅医生开放」提示，且 POST 请求不发出', async () => {
    const warnSpy = vi.spyOn(ElMessage, 'warning')
    await clickSaveAs('admin')

    expect(warnSpy).toHaveBeenCalledTimes(1)
    expect(warnSpy.mock.calls[0][0]).toBe(DOCTOR_ONLY_HINT)
    expect(DOCTOR_ONLY_HINT).toContain('仅对医生开放')
    expect(vi.mocked(api.saveOrthosisPlanApi)).not.toHaveBeenCalled()
    warnSpy.mockRestore()
  })

  it('客服同样被挡（他进不了本页，这里锁的是同一判据）', async () => {
    const warnSpy = vi.spyOn(ElMessage, 'warning')
    await clickSaveAs('cs')

    expect(vi.mocked(api.saveOrthosisPlanApi)).not.toHaveBeenCalled()
    expect(warnSpy).toHaveBeenCalledTimes(1)
    warnSpy.mockRestore()
  })

  // 反证：闸门不能是「恒拦」——医生那一枪照旧要打出去
  it('医生行为不变：请求照发、成功提示照出', async () => {
    const successSpy = vi.spyOn(ElMessage, 'success')
    const warnSpy = vi.spyOn(ElMessage, 'warning')
    await clickSaveAs('doctor')

    expect(vi.mocked(api.saveOrthosisPlanApi)).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.saveOrthosisPlanApi)).toHaveBeenCalledWith('PAT-T629', '新版矫形方案：夜间佩戴 10 小时')
    expect(successSpy).toHaveBeenCalledWith('方案已保存')
    expect(warnSpy).not.toHaveBeenCalled()
    successSpy.mockRestore()
    warnSpy.mockRestore()
  })
})

describe('T629 不误伤 · 其余 403 文案不动', () => {
  it('10403 的通用句原样保留（本卡没改码表，别的端点仍走它）', () => {
    expect(userErrorCopy({ code: 10403 }, { scope: 'admin' })).toBe('没有该操作的权限，请联系管理员')
  })
})
