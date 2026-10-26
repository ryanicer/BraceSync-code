/**
 * T748：位置权限三态判定。
 *
 * 微信 authSetting 里 scope.userLocation 只有三种真相：
 *   true      已授权
 *   false     已拒绝（再次 wx.authorize 不会弹框，必须 wx.openSetting）
 *   undefined 从未询问过（可 wx.authorize 弹系统框）
 * 此前用 `!== false` 把 undefined 当已授权，导致页面显示"已授权"但实际无权，扫描直接失败。
 */
export type LocationAuthState = 'granted' | 'denied' | 'unasked'

export function classifyLocationAuth(
  authSetting: Record<string, unknown> | null | undefined,
): LocationAuthState {
  const value = authSetting?.['scope.userLocation']
  if (value === true) return 'granted'
  if (value === false) return 'denied'
  return 'unasked'
}
