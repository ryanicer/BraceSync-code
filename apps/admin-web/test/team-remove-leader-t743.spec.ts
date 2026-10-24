import { beforeEach, describe, expect, it } from 'vitest'
import { __resetOrgForTest, mockRemoveTeamMember, mockTeamDetail, mockTeamMembers, mockTeams } from '../src/mock/org'

// T743：移除负责人时 teams.leader 同步置空；非 leader / 不存在的 memberId 不动 leader；
// 孤儿团队仍在列表里（Boss：孤儿团队必须可见）。
describe('T743 移除负责人同步清 leader（mock 镜像真实后端口径）', () => {
  beforeEach(() => __resetOrgForTest())

  it('移除 leader：成员摘除且 leader/leaderName 置空，团队仍在列表', () => {
    // 骨科一组 leader=张主任（DOC-101）
    mockRemoveTeamMember('TEAM-101', 'DOC-101', 'doctor')

    const detail = mockTeamDetail('TEAM-101')
    expect(detail.leader).toBeNull()
    expect(detail.leaderName).toBeNull()

    const members = mockTeamMembers('TEAM-101')
    expect(members.doctors.map((d) => d.memberId)).not.toContain('DOC-101')

    expect(mockTeams().map((t) => t.teamId)).toContain('TEAM-101')
  })

  it('移除非 leader：leader 保持原值', () => {
    mockRemoveTeamMember('TEAM-101', 'DOC-102', 'doctor')

    expect(mockTeamDetail('TEAM-101').leader).toBe('DOC-101')
    expect(mockTeamMembers('TEAM-101').doctors.map((d) => d.memberId)).not.toContain('DOC-102')
  })

  it('不存在的 memberId 幂等 no-op：leader 不被误清', () => {
    expect(() => mockRemoveTeamMember('TEAM-101', 'DOC-NOT-EXIST', 'doctor')).not.toThrow()
    expect(mockTeamDetail('TEAM-101').leader).toBe('DOC-101')
  })

  it('重复移除 leader 仍幂等，leader 保持空', () => {
    mockRemoveTeamMember('TEAM-101', 'DOC-101', 'doctor')
    expect(() => mockRemoveTeamMember('TEAM-101', 'DOC-101', 'doctor')).not.toThrow()
    expect(mockTeamDetail('TEAM-101').leader).toBeNull()
  })
})
