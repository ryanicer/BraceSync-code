// T487 后台自助改密的前端口径（弹窗与后端判定共用同一套规则，故单独成文可测）。
import { userErrorCode, userErrorCopy } from '@bracesync/shared-utils'

// 三条判据与 services/user-service/internal/handler/change_password_t487.go 的 validNewAdminPassword 同形：
// 长度按**字节**量（Go 侧 len() 量的也是字节），字母位取 Unicode 的 L 类、数字位取 Nd 类
// （Go 的 unicode.IsDigit 只认 Nd）。两侧不同形的后果是「前端放行、后端回 10400」，
// 而那时用户只会看到一句说不清哪儿不对的通用提示。
export const ADMIN_PWD_MIN_BYTES = 8
export const ADMIN_PWD_MAX_BYTES = 64
export const ADMIN_PWD_RULE = '8-64 位，且同时含字母和数字'

export function passwordByteLen(value: string): number {
  return new TextEncoder().encode(value).length
}

/** 新密码是否不合格（true=不合格，命名成疑问式便于直接当校验器用） */
export function isWeakAdminPassword(value: string): boolean {
  const bytes = passwordByteLen(value)
  if (bytes < ADMIN_PWD_MIN_BYTES || bytes > ADMIN_PWD_MAX_BYTES) return true
  return !(/\p{L}/u.test(value) && /\p{Nd}/u.test(value))
}

/**
 * 改密失败的展示文案。
 * 🔴 后端把「旧密码不对」「新密码太弱」「与当前密码相同」压成同一个 10400，而 T464 之后按码查表
 * 只会得到一句「提交的信息有误，请检查后重试」——弹窗里用户必须知道回去改哪一格，
 * 故这条端点按码补一句可操作的中文。弱密码与同值前端已先拦一层，真打到后端的 10400 主要剩旧密码不匹配。
 */
export function changePasswordErrorCopy(err: unknown): string {
  if (userErrorCode(err) === 10400) {
    return `修改失败：当前密码不正确，或新密码不符合要求（${ADMIN_PWD_RULE}，且不能与当前密码相同）`
  }
  return userErrorCopy(err, { scope: 'admin', fallback: '修改密码失败' })
}

export interface PwdFormFields {
  oldPassword: string
  newPassword: string
  confirmPassword: string
}

/**
 * 单格判定：合格返回 null，不合格返回该格要显示的中文。
 * el-form 的规则与提交前的自证都调这里，判定只此一份（两处各写一遍迟早对不上）。
 */
export function pwdFieldIssue(fields: PwdFormFields, field: keyof PwdFormFields): string | null {
  if (field === 'oldPassword') return fields.oldPassword ? null : '请输入当前密码'
  if (field === 'newPassword') {
    if (!fields.newPassword) return '请输入新密码'
    if (isWeakAdminPassword(fields.newPassword)) return `新密码需${ADMIN_PWD_RULE}`
    if (fields.newPassword === fields.oldPassword) return '新密码不能与当前密码相同'
    return null
  }
  if (!fields.confirmPassword) return '请再次输入新密码'
  return fields.confirmPassword === fields.newPassword ? null : '两次输入的新密码不一致'
}

/** 三格按 旧→新→确认 的顺序取第一处不合格；全合格返回 null */
export function pwdFormIssue(fields: PwdFormFields): string | null {
  for (const field of ['oldPassword', 'newPassword', 'confirmPassword'] as const) {
    const issue = pwdFieldIssue(fields, field)
    if (issue) return issue
  }
  return null
}
