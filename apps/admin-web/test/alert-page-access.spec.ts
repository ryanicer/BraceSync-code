// T351 / T359 判据单测：告警管理页两处角色分叉（两张配置 Tab、转派候选人名录）。
//
// 只吃 utils/alertPageAccess.ts 这一纯层，不 mount 页面 SFC —— 页面静态 import FlowDesigner.vue，
// 而 @logicflow/core 的 CJS 产物 require 了 ESM 的 lodash-es，CI 的 Node 18 下会 ERR_REQUIRE_ESM，
// 把整个 spec 变成「0 用例收集却显示全过」的假绿（T275 实测）。页面侧的 DOM 判据走
// e2e/tests/admin-alerts.spec.ts 的「医护角色」段（真实浏览器、四张 Tab 看得见摸得着），
// 「医护点开处理流程 Tab 不再出现 403」走 e2e-real/tests/03-alerts.spec.ts 的 03c（打现网网关）。
import { describe, it, expect } from 'vitest'
import { canConfigureAlerts, canReadDoctorRoster } from '../src/utils/alertPageAccess'

describe('canConfigureAlerts（T351）', () => {
  it('运营管理员可见两张配置 Tab（规则列/流程配置功能不退化）', () => {
    expect(canConfigureAlerts('admin')).toBe(true)
  })

  it('医护不可见：其 token 打 /admin/alert-rules 与模板写端点必 403（模板读端点 T359 已放行 staff，与本判据无关）', () => {
    expect(canConfigureAlerts('doctor')).toBe(false)
  })

  it('客服不可见（也进不了本页，此处只锁判据本身）', () => {
    expect(canConfigureAlerts('cs')).toBe(false)
  })

  it('未知角色 / 未登录（role 为 null）按不可见处理，与路由守卫 fail-closed 同向', () => {
    expect(canConfigureAlerts(null)).toBe(false)
    expect(canConfigureAlerts(undefined)).toBe(false)
  })
})

/**
 * T359 · 转派候选人名录（GET /api/v1/doctors）可读判据。
 *
 * 与上面那组判据同值不同依据（见 utils 注释），所以两条各自锁：本卡放开的是模板**读**，
 * 名录这一枪刻意留在 admin 域 ⇒ 这条判据就是「不许有人把它当成运行态必需去放行」的锁。
 */
describe('canReadDoctorRoster（T359）', () => {
  it('运营管理员读名录：转派下拉照常有候选人（不退化）', () => {
    expect(canReadDoctorRoster('admin')).toBe(true)
  })

  it('医护不发这一枪——该页仍可手输账号 ID 完成转派', () => {
    expect(canReadDoctorRoster('doctor')).toBe(false)
  })

  it('客服不发这一枪（名录含全院编制与名下患者数，不外溢给非管理角色）', () => {
    expect(canReadDoctorRoster('cs')).toBe(false)
  })

  // 前端 RoleKey 只有 admin/doctor/cs 三档（permissions.ts:2），technician 的 roleId 映射不到 ⇒
  // 落进下面这条 null 分支，同样不发。
  it('未知角色 / 未登录按不可读处理，与路由守卫 fail-closed 同向', () => {
    expect(canReadDoctorRoster(null)).toBe(false)
    expect(canReadDoctorRoster(undefined)).toBe(false)
  })
})
