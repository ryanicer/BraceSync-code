import { classifyScannedSsid, type WifiBandVerdict } from '@bracesync/shared-utils'

/** onGetWifiList 偶发不回调，最多等 3s；超时按不可用降级，不拖住失败页。 */
const SCAN_TIMEOUT_MS = 3000

type WifiListResult = { wifiList?: Array<{ SSID?: string }> }

/**
 * T744 B 层：读手机当前扫到的 WiFi 名称，核对用户填写的 SSID。
 * 微信 WiFi 系列接口（startWifi/getWifiList）官方文档已标注「该系列接口已下架」，
 * iOS 还会跳系统设置页打断失败页；因此任何失败/超时一律回 'unavailable'，
 * 由页面降级为 A 层常驻提示，不把平台限制伪装成"未找到网络"。
 */
export function scanPhoneSsid(inputSsid: string, timeoutMs = SCAN_TIMEOUT_MS): Promise<WifiBandVerdict> {
  return new Promise((resolve) => {
    let settled = false
    let timer: ReturnType<typeof setTimeout> | null = null

    const done = (verdict: WifiBandVerdict) => {
      if (settled) return
      settled = true
      if (timer) clearTimeout(timer)
      uni.offGetWifiList(onList as unknown as Parameters<typeof uni.offGetWifiList>[0])
      resolve(verdict)
    }

    const onList = (res: WifiListResult) => {
      done(classifyScannedSsid(inputSsid, (res?.wifiList ?? []).map((w) => w?.SSID ?? '')))
    }

    try {
      uni.startWifi({
        success: () => {
          uni.onGetWifiList(onList as unknown as Parameters<typeof uni.onGetWifiList>[0])
          timer = setTimeout(() => done('unavailable'), timeoutMs)
          uni.getWifiList({ fail: () => done('unavailable') })
        },
        fail: () => done('unavailable'),
      })
    } catch {
      done('unavailable')
    }
  })
}
