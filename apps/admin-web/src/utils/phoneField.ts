// T361 手机号编辑态语义（医护账号页 / 技师管理页共用）。
//
// 缺陷原貌：编辑弹窗把列表的脱敏串原样预填进输入框当作「原值」，而服务端 PUT 的手机号是
// 三态写语义（key 缺席 = 不改 / '' = 清空 / 非空 = 换新号）。运营只是想改个姓名，
// 只要顺手清掉了那串看起来像占位符的东西，库内真号就落 NULL 了 —— 且不可回滚。
//
// 收口口径：输入框永不预填脱敏串（留空 = 不改），要改号必须填 11 位新号；
// 页面不提供「清除手机号」通道（设计稿亦无该控件），后端 ''=清空 的语义只留给 API 调用方。
import type { PhoneState } from '@bracesync/shared-types'

/** 后端常量 phone.MaskUnavailable 的前端镜像：密文解不开时的展示占位符 */
export const PHONE_PLACEHOLDER = '***'

/** 11 位大陆手机号，与后端 validPhone 同口径 */
export const PHONE_RE = /^1\d{10}$/

/**
 * 编辑弹窗手机号输入框的占位文案 —— 占位符只是提示、不是值，用户不填就不会改动库内号码。
 * 当前号码本身在列表行里展示，弹窗不再把它塞进可编辑框。
 *
 * @param state 服务端回传的三态；undefined 按「无号码」处理（新建态）
 */
export function phonePlaceholder(state: PhoneState | undefined): string {
  if (state === 'masked') return '已绑定手机号，留空即不修改'
  if (state === 'unreadable') return `号码读取失败（${PHONE_PLACEHOLDER}），留空即不修改；填 11 位新号可覆盖`
  return '选填，11位手机号'
}

/**
 * 输入框值 → 写请求的 phone 字段。
 *
 * 🔴 留空返回 undefined（字段整个不下发），绝不返回 ''：'' 会被服务端解释成「清空手机号」。
 */
export function phonePatch(raw: string): string | undefined {
  const v = raw.trim()
  return v === '' ? undefined : v
}
