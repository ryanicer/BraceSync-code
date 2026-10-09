/**
 * T646 患者端「我的」页隐私门（依 T637 设计稿 §八）。
 *
 * 设计稿要求的断言口径是「按字段名集合，比按值断言稳」：值是 seed 数据可以恰好为空，
 * 字段名是契约。本包 vitest 是 node 环境、.vue 挂不起来（同 feelings.spec.ts 的形状），
 * 所以走源码级断言：改坏了会判红，而不是靠人眼 review。
 *
 * 三面各扫一枚：
 *   1. 页面（pages/profile/index.vue）—— 整份源码不许再出现那两枚键名（连日志字段都不许摸）；
 *   2. 患者侧载荷类型（api/profile.ts 的 PatientProfile 接口体）—— 键名不在声明里；
 *   3. 在场正对照（admin-web 患者页）—— 同一对键名在后台那一面照旧在场，
 *      否则 1/2 两格的「扫不到」可能只是尺子扫了空面或字面量写错了。
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const appsRoot = join(here, '..', '..', '..')
const srcRoot = join(here, '..', '..', 'src')

const profilePage = readFileSync(join(srcRoot, 'pages/profile/index.vue'), 'utf8')
const profileApi = readFileSync(join(srcRoot, 'api/profile.ts'), 'utf8')
const adminPatientsPage = readFileSync(
  join(appsRoot, 'admin-web', 'src', 'pages', 'patients', 'index.vue'),
  'utf8',
)

/** 患者侧要收口的两枚医护姓名键（设计稿 §八 点名）。 */
const STAFF_NAME_KEYS = ['teamName', 'doctorName']

/** 取 `export interface <name> { … }` 的接口体（只比声明的键名，注释里提到键名不算在场）。 */
function interfaceBody(src: string, name: string): string {
  const head = `export interface ${name} {`
  const start = src.indexOf(head)
  expect(start, `找不到接口声明：${name}`).toBeGreaterThanOrEqual(0)
  let depth = 0
  for (let i = start + head.length - 1; i < src.length; i++) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') {
      depth--
      if (depth === 0) return src.slice(start + head.length, i)
    }
  }
  throw new Error(`${name} 的接口花括号不闭合，扫描面取不出来`)
}

describe('T646 患者端「我的」页：医护姓名不在载荷面上', () => {
  for (const key of STAFF_NAME_KEYS) {
    it(`页面源码不含 ${key}（副文案与日志都不许再摸这一枚）`, () => {
      expect(profilePage).not.toContain(key)
    })
    it(`患者侧载荷类型不含 ${key}（键集合收口，不是值置空）`, () => {
      expect(interfaceBody(profileApi, 'PatientProfile')).not.toContain(key)
    })
  }

  it('正对照：同一对键名在后台患者页照旧在场（尺子扫的不是空面）', () => {
    for (const key of STAFF_NAME_KEYS) {
      expect(adminPatientsPage).toContain(key)
    }
  })

  it('副文案只说绑定状态（两词都在场，入口没被删空）', () => {
    expect(profilePage).toContain('已绑定')
    expect(profilePage).toContain('未绑定')
    expect(profilePage).toMatch(/doctorSubText[\s\S]{0,200}teamId[\s\S]{0,60}已绑定/)
  })

  it('自助写白名单不因为收口而扩到姓名（请求体白名单里也不许出现这两枚键）', () => {
    const body = interfaceBody(profileApi, 'PatientProfileUpdate')
    for (const key of STAFF_NAME_KEYS) {
      expect(body).not.toContain(key)
    }
  })
})
