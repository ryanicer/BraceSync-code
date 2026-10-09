/**
 * T646 患者端隐私门禁（依 T637 设计稿 八 + weide-duty 2026-10-09 22:52 卡内裁定甲）。
 *
 * 在册形状是「键在场、值恒 null」，不是删键 —— 所以本文件的判据分三类，缺一类就漏：
 *   A. 页面不许再摸那两枚键（源码级 NotContain，改坏了会判红而不是靠人眼 review）；
 *   B. 患者侧契约类型仍声明这两枚且为可空（裁定甲的形状钉住，防止有人「顺手」删键造成契约漂移）；
 *   C. 后台面仍真消费这两枚（正对照：收口只圈患者侧，admin 四处可见列不受影响）。
 *
 * 本包 vitest 是 environment:'node'（vitest.config.ts），.vue 挂不起来（同 feelings.spec.ts 的形状）
 * ⇒ 页面接线只能读源码文本；值面（响应里这两枚真是 null）由后端用例钉，
 *   见 services/user-service/internal/handler/patient_privacy_t646_test.go。
 * 稿面 docs/design/patient/profile.html 的行号不从本仓读：code 仓 CI 不检出 docs 仓（T370 教训）。
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
// tests/unit → patient-miniapp → apps → 仓根（四枚上跳，少一枚就拼成 apps/apps/... 的 ENOENT）
const repoRoot = join(here, '..', '..', '..', '..')
const pageFile = readFileSync(join(repoRoot, 'apps/patient-miniapp/src/pages/profile/index.vue'), 'utf8')
const apiFile = readFileSync(join(repoRoot, 'apps/patient-miniapp/src/api/profile.ts'), 'utf8')
const adminPatientsFile = readFileSync(join(repoRoot, 'apps/admin-web/src/pages/patients/index.vue'), 'utf8')

const STAFF_NAME_KEYS = ['teamName', 'doctorName']

/** 只取 interface 的花括号体内文本（避免注释里提到键名造成自咬，也避免读到相邻接口） */
function interfaceBody(src: string, name: string): string {
  const start = src.indexOf(`interface ${name}`)
  expect(start, `契约里应声明 ${name}`).toBeGreaterThanOrEqual(0)
  const open = src.indexOf('{', start)
  expect(open, `${name} 应有接口体`).toBeGreaterThan(-1)
  let depth = 0
  for (let i = open; i < src.length; i += 1) {
    if (src[i] === '{') depth += 1
    else if (src[i] === '}') {
      depth -= 1
      if (depth === 0) return src.slice(open + 1, i)
    }
  }
  throw new Error(`${name} 花括号不配平`)
}

describe('A 患者端页面不再摸医护姓名两枚键', () => {
  it('「我的」页整面不含这两枚键名（含注释与模板，出现即判红）', () => {
    for (const key of STAFF_NAME_KEYS) {
      expect(pageFile, `profile 页不得再引用 ${key}`).not.toContain(key)
    }
  })

  it('「我的医生」副文案只说绑定状态，数据源是同源的 teamId', () => {
    expect(pageFile).toContain('已绑定')
    expect(pageFile).toContain('未绑定')
    expect(pageFile).toMatch(/doctorSubText[\s\S]{0,200}teamId[\s\S]{0,60}已绑定/)
  })

  it('加载日志的两枚布尔读标识而不是姓名（姓名不进日志面）', () => {
    expect(pageFile).toMatch(/hasTeam: !!profile\.value\.teamId/)
    expect(pageFile).toMatch(/hasDoctor: !!profile\.value\.doctorId/)
  })
})

describe('B 患者侧契约形状钉住（裁定甲 = 键在场、值可空）', () => {
  it('PatientProfile 仍声明这两枚键，且类型是可空（值恒 null，不是删键）', () => {
    const body = interfaceBody(apiFile, 'PatientProfile')
    for (const key of STAFF_NAME_KEYS) {
      expect(body, `裁定甲要求 ${key} 键在场`).toContain(key)
      expect(body, `${key} 必须声明为可空（患者侧恒 null）`).toMatch(new RegExp(`${key}: string \\| null`))
    }
  })

  it('自助改档白名单里不得出现这两枚键（患者写通道不接受姓名字段）', () => {
    const body = interfaceBody(apiFile, 'PatientProfileUpdate')
    for (const key of STAFF_NAME_KEYS) {
      expect(body, `写白名单不得含 ${key}`).not.toContain(key)
    }
  })
})

describe('C 正对照：后台面照常可读（收口只圈患者侧）', () => {
  it('admin 患者页仍真消费这两枚键，否则 C 组是空面假绿', () => {
    for (const key of STAFF_NAME_KEYS) {
      expect(adminPatientsFile, `后台患者页应仍读得到 ${key}`).toContain(key)
    }
  })
})
