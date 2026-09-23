// T351 判据单测：告警管理页两张「配置」Tab 的角色分叉。
//
// 只吃 utils/alertPageAccess.ts 这一纯层，不 mount 页面 SFC —— 页面静态 import FlowDesigner.vue，
// 而 @logicflow/core 的 CJS 产物 require 了 ESM 的 lodash-es，CI 的 Node 18 下会 ERR_REQUIRE_ESM，
// 把整个 spec 变成「0 用例收集却显示全过」的假绿（T275 实测）。页面侧的 DOM 判据走
// e2e/tests/admin-alerts.spec.ts 的「医护角色」段（真实浏览器、四张 Tab 看得见摸得着）。
import { describe, it, expect } from 'vitest'
import { canConfigureAlerts } from '../src/utils/alertPageAccess'

describe('canConfigureAlerts（T351）', () => {
  it('运营管理员可见两张配置 Tab（规则列/流程配置功能不退化）', () => {
    expect(canConfigureAlerts('admin')).toBe(true)
  })

  it('医护不可见：其 token 打 /admin/alert-rules 与 /admin/flow/templates 必 403', () => {
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
