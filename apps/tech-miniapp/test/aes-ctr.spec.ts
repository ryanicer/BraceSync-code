import { describe, it, expect, vi, afterEach } from 'vitest'
import { encryptWifiPayload, _aesCtrEncryptForTest } from '../src/utils/aes-ctr'

/**
 * 用 WebCrypto 加密作为"已知正确"参照，与纯 JS 实现逐字节比对。
 * Node 18+ 内置 crypto.subtle（vitest 环境为 node）。
 */
async function webCryptoAesCtr(
  keyHex: string,
  ivHex: string,
  plainHex: string
): Promise<string> {
  const key = hexToBytes(keyHex)
  const iv = hexToBytes(ivHex)
  const plain = hexToBytes(plainHex)
  const subtle = (globalThis as any).crypto.subtle
  const cryptoKey = await subtle.importKey('raw', key, { name: 'AES-CTR' }, false, ['encrypt'])
  const buf = await subtle.encrypt({ name: 'AES-CTR', counter: iv, length: 128 }, cryptoKey, plain)
  return bytesToHex(new Uint8Array(buf))
}

function hexToBytes(hex: string): Uint8Array {
  const b = new Uint8Array(hex.length / 2)
  for (let i = 0; i < b.length; i++) b[i] = parseInt(hex.substr(i * 2, 2), 16)
  return b
}
function bytesToHex(b: Uint8Array): string {
  return Array.from(b).map((x) => x.toString(16).padStart(2, '0')).join('')
}

describe('T118: 纯 JS AES-128-CTR 与 WebCrypto 等价性', () => {
  const KEY = '2b7e151628aed2a6abf7158809cf4f3c'

  it('单块明文（≤16B）应与 WebCrypto 完全一致', async () => {
    const iv = 'f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff'
    const plain = '6bc1bee22e409f96e93d7e117393172a' // 16B
    const expected = await webCryptoAesCtr(KEY, iv, plain)
    const actual = _aesCtrEncryptForTest(KEY, iv, plain)
    expect(actual).toBe(expected)
  })

  it('多块明文（>16B）counter 自增应与 WebCrypto 一致', async () => {
    const iv = '00000001000000000000000000000000'
    const plain =
      '6bc1bee22e409f96e93d7e117393172a' +
      'ae2d8a571e03ac9c9eb76fac45af8e51' +
      '30c81c46a35ce411' // 40B = 2 块 + 8 字节尾部
    const expected = await webCryptoAesCtr(KEY, iv, plain)
    const actual = _aesCtrEncryptForTest(KEY, iv, plain)
    expect(actual).toBe(expected)
  })

  it('真实配网 JSON 明文应与 WebCrypto 一致', async () => {
    const iv = '00000001000000000000000000000000' // seq=1
    const plain = bytesToHex(new TextEncoder().encode('{"ssid":"Hospital_5G","pwd":"P@ssw0rd","seq":1}'))
    const expected = await webCryptoAesCtr(KEY, iv, plain)
    const actual = _aesCtrEncryptForTest(KEY, iv, plain)
    expect(actual).toBe(expected)
  })

  it('空明文应返回空串', () => {
    const iv = '00000001000000000000000000000000'
    expect(_aesCtrEncryptForTest(KEY, iv, '')).toBe('')
  })
})

describe('T118: NIST SP 800-38A AES-128-CTR 已知向量（独立验证）', () => {
  // CTR-AES128.Encrypt — NIST SP 800-38A, F.5.1
  const KEY = '2b7e151628aed2a6abf7158809cf4f3c'
  const IV = 'f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff'
  const PLAIN = '6bc1bee22e409f96e93d7e117393172a'
  const CIPHER = '874d6191b620e3261bef6864990db6ce'

  it('应匹配 NIST 公布的密文', () => {
    expect(_aesCtrEncryptForTest(KEY, IV, PLAIN)).toBe(CIPHER)
  })
})

describe('T118: encryptWifiPayload 降级路径（模拟小程序无 WebCrypto）', () => {
  // 不在 describe 顶部捕获 originalCrypto——CI Node 18 的 globalThis.crypto
  // 可能在模块加载时不可用，仅在测试运行时由 vitest 环境注入。
  // 所有 mock 用 vi.spyOn，afterEach 中 vi.restoreAllMocks() 统一恢复，
  // 避免直接赋值 getter-only 属性或重定义 crypto 属性导致恢复失败。

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('crypto.subtle 不存在时，应走纯 JS 路径且结果与 WebCrypto 一致', async () => {
    const cryptoObj = (globalThis as any).crypto
    // subtle 是原型上的 getter，用 spyOn 模拟返回 undefined
    vi.spyOn(cryptoObj, 'subtle', 'get').mockReturnValue(undefined)

    const key = 'a1a13b09c2d2e3f4a5b6c7d8e9f0a1b2'
    const seq = 1
    const ssid = 'Hospital_5G'
    const pwd = 'P@ssw0rd'

    const fromPureJs = await encryptWifiPayload(ssid, pwd, key, seq)

    // 恢复真实 subtle 后用 WebCrypto 算期望值
    vi.restoreAllMocks()
    const plain = bytesToHex(new TextEncoder().encode(JSON.stringify({ ssid, pwd, seq })))
    const iv = '00000001000000000000000000000000'
    const expected = await webCryptoAesCtr(key, iv, plain)

    expect(fromPureJs).toBe(expected)
    // 铁证：密文头不再是明文 7b2273...
    expect(fromPureJs.slice(0, 8)).not.toBe('7b227373')
  })

  it('WebCrypto 抛错时，应降级到纯 JS 并产出正确密文', async () => {
    const subtle = (globalThis as any).crypto.subtle
    vi.spyOn(subtle, 'importKey').mockRejectedValue(new Error('simulated failure'))

    const key = 'a1a13b09c2d2e3f4a5b6c7d8e9f0a1b2'
    const seq = 7
    const ssid = 'TestWiFi'
    const pwd = 'secret'

    const fromFallback = await encryptWifiPayload(ssid, pwd, key, seq)

    vi.restoreAllMocks()
    const plain = bytesToHex(new TextEncoder().encode(JSON.stringify({ ssid, pwd, seq })))
    const iv = '00000007000000000000000000000000'
    const expected = await webCryptoAesCtr(key, iv, plain)

    expect(fromFallback).toBe(expected)
  })
})

describe('T118: 密文头不再是明文 JSON', () => {
  it('任意输入下，密文前 8 字节不应是 {"ssid 的 hex 7b2273736964223a', async () => {
    const key = '00112233445566778899aabbccddeeff'
    const ssid = 'MyWifi'
    const pwd = '12345678'
    for (let seq = 1; seq <= 5; seq++) {
      const cipher = await encryptWifiPayload(ssid, pwd, key, seq)
      expect(cipher.slice(0, 16)).not.toBe('7b2273736964223a')
    }
  })
})
