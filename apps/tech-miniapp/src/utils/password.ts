/**
 * T486 技师口令强度规则（前端侧唯一实现，改密页引用；不给登录页用，见下方口径说明）
 *
 * 判据与后端 services/user-service/internal/handler/technician_self_password_t486.go 的
 * validTechPassword 逐格同构，两侧共同对拍共享表
 * services/user-service/internal/handler/testdata/tech-password-rule.json
 * （Go 侧 TestT486_RuleTableParityGoSide / 前端侧 test/password-rule.spec.ts）。
 *
 * 为什么要同一条规则：前端松 ⇒ 用户设出一个后端拒收的口令；前端紧 ⇒ 把合法口令拦在门外。
 * 都只在真机上暴露，单测各绿各的。
 *
 * 「仅限可打印 ASCII」这一格是为了让两侧度量恒等：Go 的 len() 数字节、JS 的 .length 数 UTF-16
 * 单元，一个汉字 1 字符但 3 字节——不限定字符集时同一个口令两侧长度不同，
 * 技师可能设出一个登录页（按字符数判 6-16 位）拒收的口令把自己锁死。
 *
 * 登录页不引用本模块：它只校 6-16 位，是这里的超集。历史口令可能纯字母/纯数字，
 * 收紧登录页会把存量账号挡在门外（改密入口收紧是加约束，登录入口收紧是删访问）。
 */

export const TECH_PASSWORD_MIN_LEN = 6
export const TECH_PASSWORD_MAX_LEN = 16

/** 口令不合法时给用户的中文提示（与后端 UserText(10400) 同义，走 userCopy 通道原样展示） */
export const TECH_PASSWORD_RULE_HINT = '口令需为 6-16 位，包含字母和数字，不支持中文或空格'

/** 可打印 ASCII（0x21-0x7e）：不含空格、控制符与任何非 ASCII 字符 */
const PRINTABLE_ASCII = /^[\x21-\x7e]+$/

export function isValidTechPassword(pwd: string): boolean {
  if (pwd.length < TECH_PASSWORD_MIN_LEN || pwd.length > TECH_PASSWORD_MAX_LEN) return false
  if (!PRINTABLE_ASCII.test(pwd)) return false
  return /[a-zA-Z]/.test(pwd) && /[0-9]/.test(pwd)
}

/** 不合法返回提示句，合法返回 null（页面按返回值挂到对应输入框下方） */
export function techPasswordError(pwd: string): string | null {
  return isValidTechPassword(pwd) ? null : TECH_PASSWORD_RULE_HINT
}
