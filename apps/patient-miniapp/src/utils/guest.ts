/**
 * 游客体验模式（审核/演示用）
 *
 * 背景：小程序审核要求「先体验功能、再自主选择授权登录」。
 * 本产品为已建档用户的绑定式服务，外部人员无档案无法登录。
 * 游客模式用本地演示数据让用户/审核员直接浏览核心功能，不触碰真实后端。
 *
 * 安全边界：
 * - 仅 GET 读接口返回本地 mock；所有写接口（POST/PUT/DELETE）一律拒绝。
 * - 游客 token 为本地固定值，不经过 wx-login，后端不会承认。
 * - 不写任何真实患者数据，不调用配网/解绑等敏感接口。
 */
const GUEST_KEY = 'bracesync_guest_mode'
export const GUEST_PATIENT_ID = 'DEMO-20260101'
export const GUEST_TOKEN = 'guest-local-demo-token'

export function isGuest(): boolean {
  try {
    return uni.getStorageSync(GUEST_KEY) === '1'
  } catch {
    return false
  }
}

/** 进入游客模式：写入本地标记 + 演示token/patientId（复用既有登录态存储） */
export function enterGuest(): void {
  const TOKEN_KEY = 'bracesync_token'
  const PATIENT_ID_KEY = 'bracesync_patient_id'
  try {
    uni.setStorageSync(GUEST_KEY, '1')
    uni.setStorageSync(TOKEN_KEY, GUEST_TOKEN)
    uni.setStorageSync(PATIENT_ID_KEY, GUEST_PATIENT_ID)
  } catch (e) {
    console.error('enterGuest failed', e)
  }
}

/** 退出游客模式：清掉本地标记与登录态，回登录页 */
export function exitGuest(): void {
  const TOKEN_KEY = 'bracesync_token'
  const PATIENT_ID_KEY = 'bracesync_patient_id'
  try {
    uni.removeStorageSync(GUEST_KEY)
    uni.removeStorageSync(TOKEN_KEY)
    uni.removeStorageSync(PATIENT_ID_KEY)
  } catch (e) {
    console.error('exitGuest failed', e)
  }
}
