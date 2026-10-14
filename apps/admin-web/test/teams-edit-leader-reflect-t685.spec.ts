/**
 * T685 —— admin-web「编辑团队」弹窗负责人回显（F-9，T279 旧账同源）。
 *
 * 缺陷原貌：openEdit 只吃 GET /teams 列表概要投影的 row.leader。列表这一腿历史上不带 leader
 * （现网旧包实测每项 leader 键缺失 ⇒ row.leader=undefined ⇒ teamForm.leader='' ），
 * 于是负责人下拉恒空，用户无法确认现状、保存易误改。
 *
 * 本用例判别式（把 api 层换成可控桩，直连 openEdit 真接线，不手工 emit）：
 *   - fetchTeams 返回的行 **不含 leader**（复现列表缺列）；
 *   - fetchDoctors 返回 []（负责人 doctorId 不在候选集，复现现网医生投影筛子）；
 *   - fetchTeamDetailApi 返回 leader=D0001 / leaderName=李医师（权威源）。
 * 修前：openEdit 只读 row.leader ⇒ 恒空、且不调详情；
 * 修后：openEdit 调详情取 leader，并把不在候选的负责人并入下拉 ⇒ 回显「李医师」。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createPinia } from 'pinia'
import TeamsPage from '../src/pages/teams/index.vue'

// 列表行：刻意不带 leader / leaderName（复现「列表概要投影缺列」）
const LIST_ROW: Record<string, unknown> = {
  teamId: 'TEAM01', name: '脊柱矫形一组', memberCount: 1, patientCount: 0,
  teamType: 'medical', createdAt: '2026-01-01T00:00:00Z', status: 'active', description: null,
}
// 详情（权威源）：带 leader=D0001 / leaderName=李医师
const DETAIL = { ...LIST_ROW, leader: 'D0001', leaderName: '李医师' }

const detailCalls = vi.hoisted(() => [] as string[])

vi.mock('../src/api', () => ({
  fetchTeams: vi.fn(async () => [LIST_ROW]),
  fetchDoctors: vi.fn(async () => []), // 候选集不含负责人 doctorId
  fetchTeamStats: vi.fn(async () => ({ teamCount: 1, memberCount: 1, managedPatientCount: 0, unassignedPatientCount: 0 })),
  fetchTeamDetailApi: vi.fn(async (teamId: string) => { detailCalls.push(teamId); return DETAIL }),
  createTeamApi: vi.fn(async () => DETAIL),
  updateTeamApi: vi.fn(async () => DETAIL),
  deleteTeamApi: vi.fn(async () => undefined),
  fetchTeamMembersApi: vi.fn(async () => ({ doctors: [], technicians: [] })),
  addTeamMemberApi: vi.fn(async () => ({})),
  removeTeamMemberApi: vi.fn(async () => undefined),
}))

let wrapper: VueWrapper | null = null

async function settle(rounds = 10) {
  for (let i = 0; i < rounds; i++) {
    await flushPromises()
  }
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { render: () => null } },
      { path: '/teams', component: { render: () => null } },
    ],
  })
  await router.push('/teams')
  await router.isReady()
  wrapper = mount(TeamsPage, { global: { plugins: [router, createPinia(), ElementPlus] } })
  await settle()
  return wrapper!
}

describe('T685 编辑团队弹窗负责人回显', () => {
  beforeEach(() => { detailCalls.length = 0 })
  afterEach(() => { wrapper?.unmount(); wrapper = null })

  it('点编辑 → 从团队详情端点取负责人并回显（列表缺列、候选无该医生时仍成立）', async () => {
    const w = await mountPage()
    const editBtn = w.findAll('button').find((b) => (b.text() || '').includes('编辑'))
    expect(editBtn, '行内应有「编辑」按钮').toBeTruthy()
    await editBtn!.trigger('click')
    await settle()

    // 修后：openEdit 走权威详情端点
    expect(detailCalls, '编辑弹窗应从团队详情端点取负责人').toContain('TEAM01')

    // 修后：负责人回显当前负责人「李医师」——即便列表行不含 leader、候选医生为空。
    // 定位「负责人」表单项内的 select，读其选中态展示文本（EP 单选用 .el-select__placeholder，
    // 未选时带 is-transparent 且文案为占位；已选则显标签）。
    const items = w.findAll('.el-form-item').filter((el) => (el.text() || '').includes('负责人'))
    expect(items.length, '弹窗应有「负责人」表单项').toBeGreaterThan(0)
    expect(items[0].text(), '负责人应回显「李医师」').toContain('李医师')
    expect(items[0].text(), '负责人不应停留在占位文案').not.toContain('请选择负责人')
  })
})
