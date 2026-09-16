/**
 * T187 profile 只读页 —— 契约字段 → 展示文案 的映射单测。
 *
 * 数据基准取自 T186 后端测试的同一份患者样本
 * （services/user-service/internal/handler/handler_impl_test.go `samplePatient()`），
 * 断言的是「wire 上的字面量」而不是前端自己造的对象，用于锁死 camelCase 契约口径。
 */
import { describe, expect, it } from 'vitest'
import {
  ageText,
  avatarCharOf,
  cobbText,
  DASH,
  genderText,
  textOrDash,
} from '../../src/utils/profile-format'

describe('avatarCharOf — 无头像字段，姓名首字占位（PRD §7A.5-1）', () => {
  it('取姓名首字（中文名即姓氏）', () => {
    expect(avatarCharOf('患者小明')).toBe('患')
    expect(avatarCharOf('李医师')).toBe('李')
  })

  it('name 缺失/空白时退化为「患」，不渲染空圈', () => {
    expect(avatarCharOf(null)).toBe('患')
    expect(avatarCharOf(undefined)).toBe('患')
    expect(avatarCharOf('')).toBe('患')
    expect(avatarCharOf('   ')).toBe('患')
  })
})

describe('genderText — T186 gender 是字符串枚举，非 0/1', () => {
  it("male/female → 男/女", () => {
    expect(genderText('male')).toBe('男')
    expect(genderText('female')).toBe('女')
  })

  it('null / 未知值 → 占位符，不显示英文原文', () => {
    expect(genderText(null)).toBe(DASH)
    expect(genderText(undefined)).toBe(DASH)
    expect(genderText('other')).toBe(DASH)
    // 若后端哪天改成数字枚举，这里必须显式失败而不是渲染成 "0"
    expect(genderText(0 as unknown as string)).toBe(DASH)
  })
})

describe('ageText / cobbText — 可空数值', () => {
  it('正常值带单位', () => {
    expect(ageText(14)).toBe('14岁')
    expect(cobbText(28)).toBe('28°')
  })

  it('0 是合法值，不得被当成缺失', () => {
    expect(ageText(0)).toBe('0岁')
    expect(cobbText(0)).toBe('0°')
  })

  it('null → 占位符', () => {
    expect(ageText(null)).toBe(DASH)
    expect(cobbText(null)).toBe(DASH)
  })
})

describe('textOrDash — 可空字符串（diagnosis/teamName/doctorName/deviceId）', () => {
  it('有值原样透出', () => {
    expect(textOrDash('胸椎右侧凸')).toBe('胸椎右侧凸')
    expect(textOrDash('PRS-001')).toBe('PRS-001')
  })

  it('null / undefined / 空串 一律走占位符，不出现空白行', () => {
    expect(textOrDash(null)).toBe(DASH)
    expect(textOrDash(undefined)).toBe(DASH)
    expect(textOrDash('')).toBe(DASH)
  })
})
