/**
 * T0518 患者端「我的」档案关键字段只读（甲方反馈 FB-20261021-014，卡 1133005753001000518）。
 *
 * 产品口径：姓名 / 性别 / 年龄属医疗档案关键字段，患者端只读、无任何编辑控件；
 * 修改入口仅后台 admin-web（运营/技师）。身高 / 体重 / 紧急联系人不受影响。
 *
 * 本包 vitest 是 environment:'node'，.vue 挂不起来（同 profile-privacy-t646.spec.ts 形状）
 * ⇒ 页面接线只能读源码文本；「admin 改完患者端真能读到新值」由 e2e-real 21/27 真网链钉。
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
// tests/unit → patient-miniapp → apps → 仓根（四枚上跳）
const repoRoot = join(here, '..', '..', '..', '..')
const pageFile = readFileSync(join(repoRoot, 'apps/patient-miniapp/src/pages/profile/index.vue'), 'utf8')
const apiFile = readFileSync(join(repoRoot, 'apps/patient-miniapp/src/api/profile.ts'), 'utf8')

/**
 * 去掉注释后的页面代码面。
 * 恢复点注记里必须点名旧控件（editNickname / editAge …），直接扫全文会自咬；
 * 「代码里不得再有」的判据只该看真正会执行的标记与脚本。
 */
function stripComments(src: string): string {
  return src
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/\/\/[^\n]*/g, '')
}
const pageCode = stripComments(pageFile)

/** 只取 interface 的花括号体内文本（避免注释里提到键名造成自咬） */
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

describe('A 弹层里姓名/性别/年龄是只读展示，不是输入控件', () => {
  it('三行静态值分别绑定本人档案（nameText / genderText / ageText）', () => {
    expect(pageFile).toMatch(/姓名<\/text>\s*<text class="form-static">\{\{ nameText \}\}/)
    expect(pageFile).toMatch(/性别<\/text>\s*<text class="form-static">\{\{ genderText\(profile\?\.gender\) \}\}/)
    expect(pageFile).toMatch(/年龄<\/text>\s*<text class="form-static">\{\{ ageText\(profile\?\.age\) \}\}/)
  })

  it('三个可编辑控件的状态/钩子全部退场（出现任一即判红，防复活）', () => {
    for (const gone of ['editNickname', 'editGender', 'editAge', 'onGenderChange', '<radio']) {
      expect(pageCode, `页面代码不得再出现 ${gone}`).not.toContain(gone)
    }
  })

  it('常驻提示逐字在册，且含甲方要求的指路口径', () => {
    expect(pageFile).toContain('姓名、年龄、性别为医疗档案信息，不可自助修改；如需变更请联系运营人员修改。')
  })

  it('样式有只读行定义（.form-static），不是拿输入框样式冒充静态值', () => {
    expect(pageFile).toMatch(/\.form-static\s*\{/)
  })
})

describe('B 保存载荷不再携带姓名/性别/年龄', () => {
  it('saveProfile 不组装这三枚键（payload.name / payload.gender / payload.age 均不得出现）', () => {
    for (const key of ['name', 'gender', 'age']) {
      expect(pageCode, `页面不得组装 payload.${key}`).not.toMatch(new RegExp(`payload\\.${key}\\b`))
    }
  })

  it('身高/体重/紧急联系人仍可编辑（本卡只收口三枚关键字段）', () => {
    for (const kept of ['editHeight', 'editWeight', 'editEmergencyName', 'editEmergencyPhone', 'editEmergencyRelation']) {
      expect(pageFile, `回归字段 ${kept} 不得随本卡退场`).toContain(`v-model="${kept}"`)
    }
  })
})

describe('C 患者侧写契约收窄（新客户端根本发不出这三键）', () => {
  it('PatientProfileUpdate 不含 name / gender / age 声明', () => {
    const body = interfaceBody(apiFile, 'PatientProfileUpdate')
    for (const key of ['name', 'gender', 'age']) {
      // 行首声明才算数：emergencyContactName 里的 Name 不命中 \b 边界
      expect(body, `写白名单不得含 ${key}`).not.toMatch(new RegExp(`^\\s*${key}\\?:`, 'm'))
    }
  })

  it('可写字段仍在册（身高/体重/紧急联系人 ×3），不是把接口掏空', () => {
    const body = interfaceBody(apiFile, 'PatientProfileUpdate')
    for (const key of ['heightCm', 'weightKg', 'emergencyContactName', 'emergencyContactPhone', 'emergencyContactRelation']) {
      expect(body).toMatch(new RegExp(`^\\s*${key}\\?:`, 'm'))
    }
  })
})
