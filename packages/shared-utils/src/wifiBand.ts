/**
 * T744（Boss 2026-10-24 批准）：配网频段体验 A+B 的文案与判定，两端共用一份。
 *
 * A 层：两端配网页 SSID 输入处常驻 2.4G 提示。
 * B 层：配网连接失败后用手机当前能扫到的 WiFi 清单核对用户填写的 SSID，
 *      清单里有 ⇒ 提示可能为仅 5G；没有 ⇒ 提示名称可能拼错/离得太远。
 *      扫不到、无权限、平台不支持（iOS 受限、微信 WiFi 系列接口已下架）⇒ 回 A 层常驻提示兜底。
 *
 * 与 PRD §7A.9.1 ④「不教用户凭网络名判断频段」的关系：Boss 2026-10-24 批准本方案后，
 * 判据是手机扫描到的**网络清单**而非 SSID 命名，本文件也不出现「名称带 5G」这类句式。
 */
export const WIFI_BAND_HINT_24G =
  '设备仅支持 2.4G WiFi，请确认输入的是 2.4G 网络名称（5G 网络设备无法连接）'

export const WIFI_BAND_SCAN_5G = '该网络可能为仅 5G 频段，设备无法连接，请连接 2.4G 网络'
export const WIFI_BAND_SCAN_NOT_FOUND = '未找到该网络，请检查名称拼写或走近路由器'

export type WifiBandVerdict = 'found' | 'notfound' | 'unavailable'

/**
 * 用手机扫到的 SSID 清单核对用户填写的网络。
 * - 清单为空或输入为空 ⇒ unavailable（扫描没拿到有效数据，不能据此说"没找到"）
 * - 精确匹配（SSID 区分大小写，两端 trim）⇒ found
 * - 清单非空且不含 ⇒ notfound
 */
export function classifyScannedSsid(inputSsid: string, scannedSsids: readonly string[]): WifiBandVerdict {
  const target = (inputSsid || '').trim()
  if (!target) return 'unavailable'
  const names = scannedSsids.map((s) => (s || '').trim()).filter(Boolean)
  if (names.length === 0) return 'unavailable'
  return names.includes(target) ? 'found' : 'notfound'
}

export function wifiBandDiagnosis(verdict: WifiBandVerdict): string {
  if (verdict === 'found') return WIFI_BAND_SCAN_5G
  if (verdict === 'notfound') return WIFI_BAND_SCAN_NOT_FOUND
  return WIFI_BAND_HINT_24G
}
