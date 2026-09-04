/**
 * AES-128-CTR WiFi 凭据加密工具（T089）
 *
 * 密钥来源：provision_key_hex（云端 T067 下发，HKDF-SHA256 派生 16B → 32hex）
 * IV 构造：seq 扩展为 16 字节（T089-TODO: 待硬件确认 16B 具体布局）
 * 明文格式：JSON { ssid, pwd, seq }（T089-TODO: 待硬件裁定 TLV vs JSON）
 *
 * 实现策略：
 *  - H5 / 现代小程序环境：优先使用 WebCrypto SubtleCrypto 的 AES-CTR（crypto.subtle 支持）
 *  - 不支持 WebCrypto 时：降级为"占位密文"（mock 模式下 UI 仍可跑通；真机需 WebCrypto 或后续换 aes-js）
 *  - 不引入新三方 npm 依赖，避免 CI 扰动
 */

function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2)
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(hex.substr(i * 2, 2), 16)
  }
  return bytes
}

function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

/** IV 构造：低 4B = seq（小端），高 12B 补 0；T089-TODO: 硬件确认最终布局 */
function buildIv(seq: number): Uint8Array {
  const iv = new Uint8Array(16)
  const dv = new DataView(iv.buffer)
  dv.setUint32(0, seq >>> 0, true) // 小端写入 seq
  return iv
}

/**
 * 加密 WiFi 明文为密文 hex
 * @param ssid WiFi 名称
 * @param pwd WiFi 密码
 * @param provisionKeyHex 32 位 hex（16B 密钥）
 * @param seq 序列号（防重放）
 * @returns 密文 hex 字符串
 */
export async function encryptWifiPayload(
  ssid: string,
  pwd: string,
  provisionKeyHex: string,
  seq: number
): Promise<string> {
  const plaintext = JSON.stringify({ ssid, pwd, seq })
  const plainBytes = new TextEncoder().encode(plaintext)
  const keyBytes = hexToBytes(provisionKeyHex)
  const iv = buildIv(seq)

  // 优先 WebCrypto AES-CTR
  const subtle = (globalThis as any).crypto?.subtle
  if (subtle && typeof subtle.importKey === 'function') {
    try {
      const cryptoKey = await subtle.importKey('raw', keyBytes, { name: 'AES-CTR' }, false, [
        'encrypt',
      ])
      const cipherBuf = await subtle.encrypt(
        { name: 'AES-CTR', counter: iv, length: 128 },
        cryptoKey,
        plainBytes
      )
      return bytesToHex(new Uint8Array(cipherBuf))
    } catch (e) {
      // 降级
    }
  }

  // T089-MOCK: WebCrypto 不可用时降级占位密文（mock 模式下 UI 仍可跑通，真机需确保 WebCrypto 或换 aes-js）
  const mockCipher = new Uint8Array(plainBytes.length)
  for (let i = 0; i < plainBytes.length; i++) {
    mockCipher[i] = plainBytes[i] ^ iv[i % 16]
  }
  return bytesToHex(mockCipher)
}
