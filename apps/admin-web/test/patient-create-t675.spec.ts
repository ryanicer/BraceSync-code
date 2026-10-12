// T675 患者创建归属归一（Boss 2026-10-12 拍板）页面级断言：
//  1. 新建患者弹窗没有「医生」表单项，提交给 createPatientApi 的载荷里 doctorId 键根本不存在
//     （不是传 null：键都不能有——后端兼容读老键，但新前端不得再发）；
//  2. 患者域团队下拉的数据源 medicalTeams 只含 teamType='medical'：
//     喂给 fetchTeams 的 9 个团队里夹着 TEAM-201/202 两个 maintenance，必须被滤掉；
//  3. 页面不再触达 fetchDoctors（医生字典在患者页彻底下线）。
//
// 下拉「看不见维护班组」的浏览器侧逐字断言在 e2e/tests/admin-patient-writes.spec.ts
// （happy-dom 里 EP 浮层选项的渲染形态与真实浏览器不同形，这格不在这层量）。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createPinia } from 'pinia'
import type { Patient, Team } from '@bracesync/shared-types'
import PatientsPage from '../src/pages/patients/index.vue'

const fixture = vi.hoisted(() => {
  const r1: Patient = {
    patientId: 'PT-001', name: '林小雨', gender: 'female', age: 13,
    diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 28,
    deviceId: 'DEV-A3F312', teamId: 'TEAM-001', doctorId: 'DOC-001',
    status: 'active', phone: '',
    createdAt: '2026-03-12T09:00:00+08:00', updatedAt: '2026-08-10T18:00:00+08:00',
  }
  const teams: Team[] = [
    { teamId: 'TEAM-001', name: '脊柱侧弯一组', memberCount: 8, patientCount: 186, teamType: 'medical' },
    { teamId: 'TEAM-002', name: '脊柱侧弯二组', memberCount: 7, patientCount: 204, teamType: 'medical' },
    { teamId: 'TEAM-003', name: '术后康复治疗组', memberCount: 6, patientCount: 158, teamType: 'medical' },
    { teamId: 'TEAM-201', name: '设备维护一组', memberCount: 1, patientCount: 0, teamType: 'maintenance' },
    { teamId: 'TEAM-202', name: '设备维护二组', memberCount: 1, patientCount: 0, teamType: 'maintenance' },
  ]
  const m = {
    fetchPatients: vi.fn(),
    fetchTeams: vi.fn(),
    // 仍在 mock 表里留 fetchDoctors：要证明的正是「页面不再调用它」（调用计数恒 0）
    fetchDoctors: vi.fn(),
    teamNameOf: vi.fn((id: string | null | undefined) => id ?? '-'),
    createPatientApi: vi.fn(),
    assignPatientTeamApi: vi.fn(),
    batchBindPatientsApi: vi.fn(),
    updatePatientPhoneApi: vi.fn(),
    updatePatientProfileApi: vi.fn(),
    unbindPatientWechatApi: vi.fn(),
    setPatientPasswordApi: vi.fn(),
  }
  return { m, r1, teams }
})

vi.mock('../src/api', () => fixture.m)

let wrapper: VueWrapper | null = null

const noop = { render: () => null }

async function settle() {
  for (let i = 0; i < 4; i++) {
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))
  }
}

function dialogOf(title: string): HTMLElement {
  const all = [...document.querySelectorAll<HTMLElement>('.el-dialog')]
  const el = all.find((d) => (d.querySelector('.el-dialog__title')?.textContent ?? '').trim() === title)
  if (!el) throw new Error(`未找到弹窗「${title}」`)
  return el
}

function byPlaceholder(scope: HTMLElement, ph: string): HTMLInputElement | HTMLTextAreaElement {
  const field = scope.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[placeholder="${ph}"]`)
  if (!field) throw new Error(`弹窗内找不到 placeholder 为「${ph}」的输入框`)
  return field
}

async function setInput(scope: HTMLElement, ph: string, value: string) {
  const input = byPlaceholder(scope, ph)
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await settle()
}

function buttonOf(scope: HTMLElement, label: string): HTMLButtonElement {
  const btn = [...scope.querySelectorAll('button')].find((b) => (b.textContent ?? '').trim() === label)
  if (!btn) throw new Error(`找不到文案为「${label}」的按钮`)
  return btn
}

async function clickButton(scope: HTMLElement, label: string) {
  buttonOf(scope, label).click()
  await settle()
}

beforeEach(async () => {
  fixture.m.fetchPatients.mockResolvedValue({ list: [fixture.r1], total: 1, page: 1, pageSize: 10 })
  fixture.m.fetchTeams.mockResolvedValue(fixture.teams)
  fixture.m.fetchDoctors.mockResolvedValue([])
  fixture.m.createPatientApi.mockResolvedValue(fixture.r1)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: noop },
      { path: '/patients', component: noop },
      { path: '/abnormal-report', component: noop },
    ],
  })
  wrapper = mount(PatientsPage, { attachTo: document.body, global: { plugins: [router, createPinia(), ElementPlus] } })
  await flushPromises()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

describe('T675 建档去医生', () => {
  it('新建弹窗无「医生」表单项与「请选择医生」下拉', async () => {
    await clickButton(document.querySelector('.page-toolbar') as HTMLElement, '添加患者')
    const dlg = dialogOf('新建患者')
    const labels = [...dlg.querySelectorAll('.el-form-item__label')].map((e) => (e.textContent ?? '').trim())
    expect(labels.some((t) => t.includes('医生')), `表单 label 实际为：${labels.join('、')}`).toBe(false)
    expect(dlg.querySelector('[placeholder*="选择医生"]')).toBeNull()
  })

  it('提交载荷不含 doctorId 键（不是 null，是键不存在），teamId 照常透传', async () => {
    await clickButton(document.querySelector('.page-toolbar') as HTMLElement, '添加患者')
    const dlg = dialogOf('新建患者')
    await setInput(dlg, '请输入姓名', 'T675单元建档')
    await setInput(dlg, '请输入手机号', '13800675002')

    await clickButton(dlg, '确定')
    expect(fixture.m.createPatientApi).toHaveBeenCalledTimes(1)
    const payload = fixture.m.createPatientApi.mock.calls[0]?.[0] as Record<string, unknown>
    expect(payload.name).toBe('T675单元建档')
    expect(payload.phone).toBe('13800675002')
    expect(payload.teamId).toBeNull()
    // 关键判据：doctorId 键不得存在（老前端发 doctorId:null/具体医生 id 的时代结束）
    expect(Object.prototype.hasOwnProperty.call(payload, 'doctorId')).toBe(false)
  })

  it('患者页不再加载医生字典（fetchDoctors 零调用）', async () => {
    await flushPromises()
    expect(fixture.m.fetchDoctors).not.toHaveBeenCalled()
  })
})

describe('T675 团队下拉只显医护团队', () => {
  it('medicalTeams 过滤掉 maintenance：9 选 3，不含 TEAM-201/202', async () => {
    // script setup 的顶层绑定在 VTU 里可经 setupState 读到；这里量的正是模板四个下拉共同的数据源
    const medical = (wrapper!.vm as unknown as { medicalTeams: Team[] }).medicalTeams
    expect(medical.map((t) => t.teamId).sort()).toEqual(['TEAM-001', 'TEAM-002', 'TEAM-003'])
    expect(medical.some((t) => t.teamId === 'TEAM-201' || t.teamId === 'TEAM-202')).toBe(false)
  })

  it('全量团队字典仍保留（teamNameOf 兜底/存量行团队名展示不被过滤波及）', async () => {
    const teams = (wrapper!.vm as unknown as { teams: Team[] }).teams
    expect(teams).toHaveLength(5)
    expect(fixture.m.fetchTeams).toHaveBeenCalledTimes(1)
  })
})
