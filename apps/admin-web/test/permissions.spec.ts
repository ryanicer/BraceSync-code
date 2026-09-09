// 权限矩阵单测（PRD §7D.11 预置角色权限矩阵）
import { describe, it, expect } from 'vitest'
import { ROLE_PAGE_MATRIX, canAccess, roleName, PRESET_ROLES, roleKeyFromRoleId } from '../src/router/permissions'
import { pageRoutes } from '../src/router'

describe('ROLE_PAGE_MATRIX（PRD §7D.11）', () => {
  it('运营管理员可访问全部 13 页', () => {
    expect(ROLE_PAGE_MATRIX.admin).toHaveLength(13) // 断言更新：12→13，依据 T130 新增复查报告页
    for (const route of pageRoutes) {
      expect(canAccess('admin', route.path)).toBe(true)
    }
  })

  it('医生仅可访问 数据概览/实时监控/告警管理/矫形日志/复查报告 5 页', () => {
    expect(ROLE_PAGE_MATRIX.doctor).toEqual(['/dashboard', '/monitor', '/alerts', '/orthosis-log', '/review-records']) // 断言更新：4→5，依据 T130 新增复查报告页
    expect(canAccess('doctor', '/patients')).toBe(false)
    expect(canAccess('doctor', '/settings')).toBe(false)
  })

  it('客服仅可访问 患者沟通 1 页', () => {
    expect(ROLE_PAGE_MATRIX.cs).toEqual(['/communication'])
    expect(canAccess('cs', '/dashboard')).toBe(false)
  })

  it('未知角色一律拒绝', () => {
    expect(canAccess('unknown', '/dashboard')).toBe(false)
    expect(canAccess('', '/dashboard')).toBe(false)
  })

  it('预置角色元信息完整', () => {
    expect(PRESET_ROLES.map((r) => r.key)).toEqual(['admin', 'doctor', 'cs'])
    expect(roleName('admin')).toBe('运营管理员')
    expect(roleName('doctor')).toBe('医生')
    expect(roleName('cs')).toBe('客服')
  })
})

describe('roleKeyFromRoleId（T046 真实登录 roleId 映射）', () => {
  it('后端 roleId → 前端 RoleKey', () => {
    expect(roleKeyFromRoleId('ROLE_ADMIN')).toBe('admin')
    expect(roleKeyFromRoleId('ROLE_DOCTOR')).toBe('doctor')
    expect(roleKeyFromRoleId('ROLE_CS')).toBe('cs')
  })

  it('未知 roleId / 空串 → null（fail-closed）', () => {
    expect(roleKeyFromRoleId('ROLE_X')).toBeNull()
    expect(roleKeyFromRoleId('')).toBeNull()
    expect(roleKeyFromRoleId('admin')).toBeNull()
  })
})
