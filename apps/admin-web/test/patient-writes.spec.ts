// T432 管理端患者写通道（改手机号 / 档案编辑 / 解绑微信）—— mock 层语义用例。
//
// 为什么单测 mock 层：真实模式的 400/409 判定在后端 admin_patient.go，本机跑不到；
// 而页面「未改动的键不下发」「空编辑必须置灰」这类最容易被写反的语义，只有 mock 与后端同形才测得出来。
// 下面每条判据都对着一段 Go 代码写（行号会随改动漂移，故按函数名指认）：
//   validPhone 长度 11 且 1 开头          → handler.go validPhone
//   phone_hash 撞号排除自身 → 409         → admin_patient.go updatePatientAdmin 前置判重
//   DisallowUnknownFields 拒白名单外的键  → admin_patient.go updatePatientAdmin 解码处
//   值域：姓名 1-64 / 性别枚举 / 年龄 0-150 / 诊断 ≤255 / Cobb 0-180 → buildAdminPatientEdit
//   一个字段都没给 → 400                  → buildAdminPatientEdit anyField
//   clearFields 表外/重复/撞列 → 400      → resolveClearFields（T450-②b 乙案）
//   解绑无条件 SET NULL（未绑定亦成功）    → pg.go UnbindPatientWechat
import { describe, it, expect } from 'vitest'
import {
  mockUpdatePatientPhone,
  mockUpdatePatientProfile,
  mockUnbindPatientWechat,
  MOCK_PHONE_AUDIT,
} from '../src/mock/patients'

describe('T432 改手机号（PUT /admin/patients/:id/phone）', () => {
  it('合法号码写通，且 reason 随请求留进审计台账（后端只写日志、不回显）', () => {
    const before = MOCK_PHONE_AUDIT.length
    expect(mockUpdatePatientPhone('PT-001', '13900001111', '患者换号，本人来电')).toEqual({ patientId: 'PT-001' })
    expect(MOCK_PHONE_AUDIT).toHaveLength(before + 1)
    expect(MOCK_PHONE_AUDIT[MOCK_PHONE_AUDIT.length - 1]).toMatchObject({
      patientId: 'PT-001',
      phone: '13900001111',
      reason: '患者换号，本人来电',
    })
  })

  it('空号码必拒 —— 这就是页面不做「留空即不改」三态的原因', () => {
    expect(() => mockUpdatePatientPhone('PT-002', '', '清空试试')).toThrow(/格式/)
  })

  it('10 位 / 11 位非 1 开头都算格式错', () => {
    expect(() => mockUpdatePatientPhone('PT-002', '1390000111', '少一位')).toThrow(/格式/)
    expect(() => mockUpdatePatientPhone('PT-002', '23900001111', '首位不是 1')).toThrow(/格式/)
  })

  it('同一号码撞在别人身上判冲突；写回自己不算撞（后端排除自身，admin_patient.go:95）', () => {
    expect(() => mockUpdatePatientPhone('PT-003', '13700002222', '正常换号')).not.toThrow()
    expect(() => mockUpdatePatientPhone('PT-004', '13700002222', '抢别人的号')).toThrow(/已被其他患者/)
    expect(() => mockUpdatePatientPhone('PT-003', '13700002222', '同号重写自己')).not.toThrow()
  })

  it('患者不存在时报错而不是静默成功', () => {
    expect(() => mockUpdatePatientPhone('PT-999', '13600003333', '不存在的人')).toThrow()
  })
})

describe('T432 档案编辑（PUT /admin/patients/:id）', () => {
  it('只发改过的键：改诊断时姓名/年龄原样不动', () => {
    const patched = mockUpdatePatientProfile('PT-005', { diagnosis: '姿势性侧弯，复查' })
    expect(patched.diagnosis).toBe('姿势性侧弯，复查')
    expect(patched.name).toBe('赵欣然')
    expect(patched.age).toBe(16)
    expect(patched.cobbAngle).toBe(40)
  })

  it('诊断传空串写的是空串而不是 NULL —— 这不是「置空」通道（T450-②b）', () => {
    expect(mockUpdatePatientProfile('PT-006', { diagnosis: '' }).diagnosis).toBe('')
  })

  // T450-②b 乙案（PM 2026-09-28 17:41 拍）：指针表达不出「改回 NULL」这一态，
  // 故新增显式白名单数组键 clearFields。mock 与后端 resolveClearFields 同形：
  // 四列可置空、name 不可、撞列拒、重复拒、表外拒，且「只给 clearFields」也算有效编辑。
  it('clearFields 四列各自落 NULL，且未点名的列不动', () => {
    mockUpdatePatientProfile('PT-001', { gender: 'male', age: 20, diagnosis: '置空测试', cobbAngle: 50 })
    expect(mockUpdatePatientProfile('PT-001', { clearFields: ['diagnosis'] }))
      .toMatchObject({ diagnosis: null, gender: 'male', age: 20, cobbAngle: 50 })
    expect(mockUpdatePatientProfile('PT-001', { clearFields: ['gender', 'age', 'cobbAngle'] }))
      .toMatchObject({ gender: null, age: null, cobbAngle: null })
  })

  it('给值与置空同请求：age=0 是合法值，与「把 age 置空」是两件事', () => {
    const both = mockUpdatePatientProfile('PT-002', { age: 0, clearFields: ['diagnosis'] })
    expect(both.age).toBe(0)
    expect(both.diagnosis).toBeNull()
  })

  it('只给 clearFields 也算有效编辑；空数组不算（后端 anyField 同判据）', () => {
    expect(() => mockUpdatePatientProfile('PT-002', { clearFields: [] })).toThrow(/没有需要保存的修改/)
    expect(() => mockUpdatePatientProfile('PT-002', {})).toThrow(/没有需要保存的修改/)
  })

  it('clearFields 表外字段 / 重复 / 与值键撞同一列 ⇒ 拒，且拒的时候不写坏库内原值', () => {
    const before = mockUpdatePatientProfile('PT-003', { diagnosis: '先天性脊柱侧弯', age: 12 })
    const snap = { diagnosis: before.diagnosis, age: before.age, gender: before.gender }
    expect(() => mockUpdatePatientProfile('PT-003', { clearFields: ['name'] } as never)).toThrow(/只接受/)
    expect(() => mockUpdatePatientProfile('PT-003', { clearFields: ['phone'] } as never)).toThrow(/只接受/)
    expect(() => mockUpdatePatientProfile('PT-003', { clearFields: ['age', 'age'] })).toThrow(/重复/)
    expect(() => mockUpdatePatientProfile('PT-003', { age: 13, clearFields: ['age'] })).toThrow(/二选一/)
    const after = mockUpdatePatientProfile('PT-003', { diagnosis: '先天性脊柱侧弯' })
    expect({ diagnosis: after.diagnosis, age: after.age, gender: after.gender }).toEqual(snap)
  })

  it('空编辑必拒 —— 页面据此把保存按钮置灰，而不是发一个必 400 的请求', () => {
    expect(() => mockUpdatePatientProfile('PT-007', {})).toThrow(/没有需要保存的修改/)
  })

  it('白名单外的键一律拒收（phone/teamId/status 各有专属端点）', () => {
    for (const key of ['phone', 'teamId', 'primaryDoctorId', 'doctorId', 'status']) {
      expect(() => mockUpdatePatientProfile('PT-007', { [key]: 'x' } as never)).toThrow(/白名单之外/)
    }
  })

  it('值域逐条与后端一致：超界即拒，且拒的时候不写坏库内原值', () => {
    // 逐值取原始类型存快照 —— mock 返回的是同一行对象引用，直接比 before/after 两个引用恒等，测不出「写坏」
    const snap = mockUpdatePatientProfile('PT-008', { diagnosis: '腰椎左侧弯' })
    const snapshot = { name: snap.name, age: snap.age, cobbAngle: snap.cobbAngle }
    expect(() => mockUpdatePatientProfile('PT-008', { name: '' })).toThrow(/姓名/)
    expect(() => mockUpdatePatientProfile('PT-008', { name: 'x'.repeat(65) })).toThrow(/姓名/)
    expect(() => mockUpdatePatientProfile('PT-008', { age: -1 })).toThrow(/年龄/)
    expect(() => mockUpdatePatientProfile('PT-008', { age: 151 })).toThrow(/年龄/)
    expect(() => mockUpdatePatientProfile('PT-008', { cobbAngle: 181 })).toThrow(/Cobb/)
    expect(() => mockUpdatePatientProfile('PT-008', { cobbAngle: -0.5 })).toThrow(/Cobb/)
    expect(() => mockUpdatePatientProfile('PT-008', { diagnosis: 'y'.repeat(256) })).toThrow(/诊断/)
    const after = mockUpdatePatientProfile('PT-008', { diagnosis: '腰椎左侧弯' })
    expect({ name: after.name, age: after.age, cobbAngle: after.cobbAngle }).toEqual(snapshot)
  })

  it('患者不存在时报错', () => {
    expect(() => mockUpdatePatientProfile('PT-999', { name: '不存在的人' })).toThrow()
  })
})

describe('T432 解绑微信（POST /admin/patients/:id/unbind-wechat）', () => {
  it('对任意在册患者都成功（后端无条件置 NULL，不判两态）', () => {
    expect(mockUnbindPatientWechat('PT-001')).toEqual({ patientId: 'PT-001' })
    expect(mockUnbindPatientWechat('PT-002')).toEqual({ patientId: 'PT-002' })
  })

  it('患者不存在时报错（后端 404 not found）', () => {
    expect(() => mockUnbindPatientWechat('PT-999')).toThrow()
  })
})
