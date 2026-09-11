// T144 契约漂移门禁（tech-miniapp 侧，第二前端 app / 真实路径）
//
// 目标：与 admin-web 侧互为补充，证明前端真实消费路径上「读取的字段名」与后端/契约一致。
// 本 spec 只覆盖「当前全绿」的端点。listInstallRecords 因 T143 未合入 + 本单限制作业边界（不动业务代码），
// 暂不入本门禁；待 Iris 修复（改读 res.list）后可在此补充该域。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { bindDevice, setDeviceWifi } from '../src/api/device'
import { getProvisionKey, clearProvisionKeyCache } from '../src/api/provision'

// 走真实路径：强制 USE_MOCK=false，并让 request 可控（对齐 test/provision-cache.spec.ts 既有模式）。
vi.mock('../src/utils/request', () => {
  const request = vi.fn()
  return { request, USE_MOCK: false }
})

import { request } from '../src/utils/request'

describe('T144 契约漂移门禁（tech-miniapp 真实路径）', () => {
  beforeEach(() => {
    request.mockReset()
    clearProvisionKeyCache()
  })

  it('bindDevice 真实响应读取 deviceId/status 并消费非空', async () => {
    request.mockResolvedValue({ deviceId: 'D00001', status: 'online', swapped: false })
    const res = await bindDevice('D00001', 'P00001')
    expect(res.deviceId).toBe('D00001')
    expect(res.status).toBe('online')
  })

  it('setDeviceWifi 真实响应读取 deviceId/wifiStatus', async () => {
    request.mockResolvedValue({ deviceId: 'D00001', wifiStatus: 'connected' })
    const res = await setDeviceWifi('D00001', 'MyWifi')
    expect(res.wifiStatus).toBe('connected')
  })

  it('getProvisionKey 真实响应读取 provision_key_hex/expires_in_sec', async () => {
    request.mockResolvedValue({ provision_key_hex: 'a1b2c3d4e5f60718a1b2c3d4e5f60718', expires_in_sec: 300 })
    const res = await getProvisionKey('device-001')
    expect(res.provision_key_hex).toBeTruthy()
    expect(res.expires_in_sec).toBeGreaterThan(0)
  })
})