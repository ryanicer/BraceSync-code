import type { Device } from '@bracesync/shared-types'

/**
 * T406（U4）：设备 ID 统一从这一处出，mock / E2E 夹具 / e2e helpers 同读一个常量。
 * 旧值 `PRS-ML05-RC-001` 是 T380 清理过的假串形态（3 位尾号，现网精确命中 0）。
 * 现网 5 台是 `...20260701001`–`005` 批次（T406 只读 `GET /devices` 实测），
 * mock 不许冒充现网设备，所以取「形态合法（日期 8 + 序号 3）但属保留合成批次」的 1970-01-01。
 */
export const MOCK_DEVICE_ID = 'PRS-ML05-RC-19700101001'

export function mockDevice(): Device {
  return {
    deviceId: MOCK_DEVICE_ID,
    model: 'PRS-ML05-RC',
    firmwareVersion: 'v1.2.3',
    patientId: 'pat-001',
    wifiSsid: '2.4G-Network',
    // T508 契约新键：mock 与真实写路径同形（未配置 = null，前端 kPa 档显示 --）
    contactAreaCm2: null,
    bindTime: '2026-07-01T08:00:00Z',
    status: 'online',
    lastReportAt: new Date().toISOString(),
  }
}