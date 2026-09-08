import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// vi.mock 会被提升到文件顶部；在 factory 内创建 mock 函数并导出，
// 后续 import 拿到的就是同一个 mock 实例。
vi.mock('../src/utils/request', () => {
  const request = vi.fn()
  return { request, USE_MOCK: false }
})

import { request } from '../src/utils/request'
import { getProvisionKey, clearProvisionKeyCache } from '../src/api/provision'

describe('T119: provision key 内存缓存', () => {
  beforeEach(() => {
    clearProvisionKeyCache()
    request.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('同一设备连续两次调用，第二次命中缓存，不调 /provision-key', async () => {
    request.mockResolvedValue({ provision_key_hex: 'a1b2c3d4e5f60718a1b2c3d4e5f60718', expires_in_sec: 300 })

    const first = await getProvisionKey('device-001')
    const second = await getProvisionKey('device-001')

    expect(second.provision_key_hex).toBe(first.provision_key_hex)
    // 铁证：第二次没有发起请求
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('不同设备各自申领，key 天然隔离', async () => {
    let counter = 0
    request.mockImplementation(() => {
      counter += 1
      return Promise.resolve({ provision_key_hex: `key-${counter}`, expires_in_sec: 300 })
    })

    const a = await getProvisionKey('device-A')
    const b = await getProvisionKey('device-B')
    // 再调一次 A 应命中缓存
    const a2 = await getProvisionKey('device-A')

    expect(a.provision_key_hex).toBe('key-1')
    expect(b.provision_key_hex).toBe('key-2')
    expect(a2.provision_key_hex).toBe('key-1') // 命中缓存
    expect(request).toHaveBeenCalledTimes(2) // A、B 各一次，A2 不发
  })

  it('缓存过期后重新申领', async () => {
    vi.useFakeTimers()
    request.mockResolvedValueOnce({ provision_key_hex: 'old-key', expires_in_sec: 300 })
    request.mockResolvedValueOnce({ provision_key_hex: 'new-key', expires_in_sec: 300 })

    const first = await getProvisionKey('device-001')
    // 推进 301 秒，超过 expires_in_sec=300
    vi.advanceTimersByTime(301 * 1000)
    const second = await getProvisionKey('device-001')

    expect(first.provision_key_hex).toBe('old-key')
    expect(second.provision_key_hex).toBe('new-key')
    expect(request).toHaveBeenCalledTimes(2)
  })

  it('缓存命中时 expires_in_sec 返回剩余有效期（≥1s）', async () => {
    vi.useFakeTimers()
    request.mockResolvedValue({ provision_key_hex: 'deadbeef', expires_in_sec: 300 })

    await getProvisionKey('device-001')
    vi.advanceTimersByTime(100 * 1000) // 已过 100s
    const cached = await getProvisionKey('device-001')

    // 剩余约 200s
    expect(cached.expires_in_sec).toBeGreaterThanOrEqual(199)
    expect(cached.expires_in_sec).toBeLessThanOrEqual(201)
    expect(request).toHaveBeenCalledTimes(1)
  })
})
