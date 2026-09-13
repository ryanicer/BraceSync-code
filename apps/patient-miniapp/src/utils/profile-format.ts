/**
 * T187 profile 只读页的展示层格式化（纯函数，无 uni/DOM 依赖，可 vitest）。
 *
 * 存在的理由：T186 契约里 gender 是字符串枚举、且除 patientId/name/phone/status/
 * createdAt/updatedAt 外全部字段可空，"null 该显示成什么" 必须有唯一口径，
 * 否则每个区块各写一套三元。
 */

/** 后端未给值（null / undefined / 空串）时的占位 */
export const DASH = '—'

export function textOrDash(value: string | null | undefined): string {
  return value ? value : DASH
}

/** 无头像字段 → 取姓名首字（中文名即姓氏）；姓名为空时退化为「患」 */
export function avatarCharOf(name: string | null | undefined): string {
  const ch = name?.trim().charAt(0)
  return ch || '患'
}

const GENDER_TEXT: Record<string, string> = {
  male: '男',
  female: '女',
}

/** T186 gender 为字符串枚举（'male'/'female'/null），非 0/1 */
export function genderText(gender: string | null | undefined): string {
  return (gender && GENDER_TEXT[gender]) || DASH
}

/** age=0 是合法值（婴幼儿患者），故按 null 判定而非真假判定 */
export function ageText(age: number | null | undefined): string {
  return age == null ? DASH : `${age}岁`
}

export function cobbText(cobbAngle: number | null | undefined): string {
  return cobbAngle == null ? DASH : `${cobbAngle}°`
}
