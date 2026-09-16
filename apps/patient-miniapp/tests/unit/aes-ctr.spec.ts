/**
 * T192：配网载荷必须与固件协议定稿逐字节一致。
 *
 * 依据 docs/design/hardware/BLE配网协议确认-小顾-2026-09-05.md §3/§4：
 *   IV = seq 按 **大端** 写入前 4 字节，[4..15] 固定 0x00（seq=1 → 00 00 00 01 00…）
 *   明文 = JSON {ssid, pwd, seq}，线上仅传 AES-128-CTR 密文
 *   🔴 固件不存 seq，解密时只尝试 seq=1/2/3 ⇒ 首个 seq 必须是 1
 * CTR counter 按 128 位大端自增（等价于 WebCrypto AES-CTR），故本测试以 WebCrypto 为参照。
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { webcrypto } from 'node:crypto'
import { createPinia, setActivePinia } from 'pinia'
import { encryptWifiPayload } from '../../src/utils/aes-ctr'
import { useDeviceStore } from '../../src/stores/device'

if (!(globalThis as any).crypto) {
  ;(globalThis as any).crypto = webcrypto
}

/** 16B 测试密钥（32hex）；凭据一律用假值，禁写真实 WiFi 信息 */
const KEY = '2b7e151628aed2a6abf7158809cf4f3c'
const IV_SEQ1 = '00000001000000000000000000000000'

function utf8Hex(str: string): string {
  return Array.from(new TextEncoder().encode(str))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

/** 以 WebCrypto 的 AES-128-CTR 作为独立参照实现（counter 全 16 字节大端自增） */
async function webCryptoAesCtr(keyHex: string, ivHex: string, plain: string): Promise<string> {
  const hexToBytes = (hex: string) => {
    const out = new Uint8Array(hex.length / 2)
    for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.substr(i * 2, 2), 16)
    return out
  }
  const subtle = (globalThis as any).crypto.subtle
  const cryptoKey = await subtle.importKey('raw', hexToBytes(keyHex), { name: 'AES-CTR' }, false, [
    'encrypt',
  ])
  const buf = await subtle.encrypt(
    { name: 'AES-CTR', counter: hexToBytes(ivHex), length: 128 },
    cryptoKey,
    new TextEncoder().encode(plain)
  )
  return Array.from(new Uint8Array(buf))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

describe('T192 — 03 下发载荷与固件解密口径一致', () => {
  it('seq=1 → IV 大端 00000001+12×00，密文应与 WebCrypto 一致', async () => {
    const ssid = 'Clinic_24G'
    const pwd = 'p@ssw0rd-01'
    const plain = JSON.stringify({ ssid, pwd, seq: 1 })
    const expected = await webCryptoAesCtr(KEY, IV_SEQ1, plain)

    expect(encryptWifiPayload(ssid, pwd, KEY, 1)).toBe(expected)
  })

  it('明文跨 3 个分组时，counter 自增方向应与 WebCrypto 一致（小端进位会在第 2 组起偏）', async () => {
    const ssid = 'LongNetworkName_ForMultiBlockCheck'
    const pwd = 'another-long-passphrase-9'
    const seq = 2
    const plain = JSON.stringify({ ssid, pwd, seq })
    expect(plain.length).toBeGreaterThan(48)
    const expected = await webCryptoAesCtr(
      KEY,
      '00000002000000000000000000000000',
      plain
    )

    expect(encryptWifiPayload(ssid, pwd, KEY, seq)).toBe(expected)
  })

  it('载荷长度 = 明文字节数（CTR 不填充），设备侧按密文长度判包', async () => {
    const ssid = 'A'
    const pwd = 'B'
    const plain = JSON.stringify({ ssid, pwd, seq: 1 })
    const hex = encryptWifiPayload(ssid, pwd, KEY, 1)

    expect(hex.length / 2).toBe(new TextEncoder().encode(plain).length)
  })
})

describe('T192 — seq 起点必须是 1（固件只尝试 seq=1/2/3）', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('首次领取 seq = 1，重写时递增', () => {
    const store = useDeviceStore()
    expect(store.nextWifiSeq()).toBe(1)
    expect(store.nextWifiSeq()).toBe(2)
    expect(store.nextWifiSeq()).toBe(3)
  })
})
