import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { TECH_PASSWORD_MAX_LEN, TECH_PASSWORD_MIN_LEN, TECH_PASSWORD_RULE_HINT, isValidTechPassword, techPasswordError } from '../src/utils/password'

/**
 * T486 前端口令强度规则 —— 与后端逐格同构
 *
 * 真源只有一份：services/user-service/internal/handler/testdata/tech-password-rule.json
 * Go 侧 TestT486_RuleTableParityGoSide 与本文件读同一张表、各自逐格判。
 * 任何一侧改规则而没改表，另一侧必判红；改了表没改码，两侧都判红。
 */
const TABLE = fileURLToPath(new URL(
  '../../../services/user-service/internal/handler/testdata/tech-password-rule.json',
  import.meta.url,
))

type RuleCase = { password: string; ok: boolean; why: string }
type RuleTable = { minLength: number; maxLength: number; cases: RuleCase[] }

const table = JSON.parse(readFileSync(TABLE, 'utf8')) as RuleTable

describe('T486 口令规则与共享表对拍（前端侧）', () => {
  it('表的窗口常量与前端常量一致', () => {
    expect(TECH_PASSWORD_MIN_LEN).toBe(table.minLength)
    expect(TECH_PASSWORD_MAX_LEN).toBe(table.maxLength)
  })

  it('表非空且两向都有样本（全 ok 或全拒的表证不了任何规则）', () => {
    expect(table.cases.length).toBeGreaterThan(0)
    expect(table.cases.filter((c) => c.ok).length).toBeGreaterThan(0)
    expect(table.cases.filter((c) => !c.ok).length).toBeGreaterThan(0)
  })

  it('每条用例的判定与表一致', () => {
    for (const c of table.cases) {
      // 失败信息带上两种度量：Go 数字节、JS 数 UTF-16 单元，分叉就体现在这两个数上
      expect(
        isValidTechPassword(c.password),
        `pwd=${JSON.stringify(c.password)} bytes=${Buffer.byteLength(c.password, 'utf8')} ` +
          `chars=${[...c.password].length} 表判 ${c.ok}（${c.why}）`,
      ).toBe(c.ok)
    }
  })

  it('techPasswordError 合法返回 null、非法返回同一句提示', () => {
    expect(techPasswordError('ab1def')).toBeNull()
    expect(techPasswordError('1234567')).toBe(TECH_PASSWORD_RULE_HINT)
  })
})

describe('T486 前端规则单独格（表之外的补充，防止整块规则被删仍对拍通过）', () => {
  it('长度边界：5 拒 / 6 收 / 16 收 / 17 拒', () => {
    expect(isValidTechPassword('ab1de')).toBe(false)
    expect(isValidTechPassword('ab1def')).toBe(true)
    expect(isValidTechPassword('ab1defghijklmnop')).toBe(true)
    expect(isValidTechPassword('ab1defghijklmnopq')).toBe(false)
  })

  it('必须同时含字母与数字', () => {
    expect(isValidTechPassword('abcdefgh')).toBe(false)
    expect(isValidTechPassword('12345678')).toBe(false)
    expect(isValidTechPassword('abc123')).toBe(true)
  })

  it('符号可以但不得单独成口令', () => {
    expect(isValidTechPassword('!@#$%^')).toBe(false)
    expect(isValidTechPassword('ab1#cd')).toBe(true)
  })

  it('空串与非 ASCII 一律拒', () => {
    expect(isValidTechPassword('')).toBe(false)
    expect(isValidTechPassword('密码abc1')).toBe(false)
    expect(isValidTechPassword('ab1\u00a0cd')).toBe(false) // 不换行空格
  })
})
