// T629 方案C：非医生调 orthosis-plans 写动作的前端闸门与提示（纯层真值表）。
//
// 现场（派发单 §一）：运营管理员保存矫形方案 → 网关 doctorAdminOnlyPatterns 放 admin 过，
// 真正拒的是 user-service savePlan 的医生身份判定（10403 通用句读不出所以然），
// T629 当时在「矫形方案」tab 的保存按钮前加了 canSaveOrthosisPlan 闸门 + DOCTOR_ONLY_HINT。
//
// T660（2026-10-11 Boss 拍板①）：「矫形方案」tab 从患者工作台下架（患者端无消费方），
// 原页面级 mount 用例随入口删除；但后端 orthosis-plans 端点、网关 doctorAdminOnly 矩阵、
// 前端 util 闸门与 10403 码表都保留（将来任何 UI 重新接线仍用同一判据），故本份收敛为纯层测。
import { describe, it, expect } from 'vitest'
import { userErrorCopy } from '@bracesync/shared-utils'
import { canSaveOrthosisPlan, DOCTOR_ONLY_HINT } from '../src/utils/doctorOnlyAccess'

describe('T629 闸门判据 · canSaveOrthosisPlan（T660 UI 下架后 util 仍在册）', () => {
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

  it('提示语明确含「仅对医生开放」，不是 10403 通用句', () => {
    expect(DOCTOR_ONLY_HINT).toContain('仅对医生开放')
    expect(DOCTOR_ONLY_HINT).not.toBe(userErrorCopy({ code: 10403 }, { scope: 'admin' }))
  })
})

describe('T629 不误伤 · 其余 403 文案不动', () => {
  it('10403 的通用句原样保留（本卡没改码表，别的端点仍走它）', () => {
    expect(userErrorCopy({ code: 10403 }, { scope: 'admin' })).toBe('没有该操作的权限，请联系管理员')
  })
})
