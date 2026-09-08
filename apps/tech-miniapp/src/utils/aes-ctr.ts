/**
 * AES-128-CTR WiFi 凭据加密工具（T089 / T118）
 *
 * 密钥来源：provision_key_hex（云端 T067 下发，HKDF-SHA256 派生 16B → 32hex）
 * IV 构造：seq 按大端写入前 4 字节，后 12 字节 0x00（协议定稿：见 docs/design/hardware/BLE配网协议确认-小顾-2026-09-05.md §3）
 *   自证：seq=1 → IV = 00 00 00 01 00 00 00 00 00 00 00 00 00 00 00 00（16B）
 * 明文格式：JSON { ssid, pwd, seq }（协议定稿 §4）
 *
 * 实现策略（T118 修复）：
 *  - H5 / 现代浏览器：优先使用 WebCrypto SubtleCrypto 的 AES-CTR（crypto.subtle 支持，更快）
 *  - 微信小程序（无 WebCrypto）：回退到内置纯 JS AES-128-CTR 实现
 *  - T118 之前的 XOR 占位"密文"（T089-MOCK）已彻底删除——真机链路恒产出真实 AES-CTR 密文
 *  - 不引入新三方 npm 依赖，避免 CI 扰动
 *
 * T115: 微信小程序运行时不含 TextEncoder，手动实现 UTF-8 编码（与 ble.ts decodeUtf8 对称）。
 */

/**
 * 手动 UTF-8 编码（微信小程序不支持 TextEncoder，T115）
 * 支持 BMP 与代理对（emoji 等），输出严格等价于 TextEncoder.encode。
 */
function encodeUtf8(str: string): Uint8Array {
  const bytes: number[] = []
  for (let i = 0; i < str.length; i++) {
    let code = str.charCodeAt(i)
    // 代理对：高代理 0xD800-0xDBFF 后接低代理 0xDC00-0xDFFF
    if (code >= 0xd800 && code <= 0xdbff && i + 1 < str.length) {
      const low = str.charCodeAt(i + 1)
      if (low >= 0xdc00 && low <= 0xdfff) {
        code = 0x10000 + ((code - 0xd800) << 10) + (low - 0xdc00)
        i++
      }
    }
    if (code < 0x80) {
      bytes.push(code)
    } else if (code < 0x800) {
      bytes.push(0xc0 | (code >> 6), 0x80 | (code & 0x3f))
    } else if (code < 0x10000) {
      bytes.push(0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f))
    } else {
      bytes.push(
        0xf0 | (code >> 18),
        0x80 | ((code >> 12) & 0x3f),
        0x80 | ((code >> 6) & 0x3f),
        0x80 | (code & 0x3f)
      )
    }
  }
  return new Uint8Array(bytes)
}

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

/**
 * IV 构造：seq 按大端写入前 4 字节，后 12 字节 0x00
 * 协议定稿：见 docs/design/hardware/BLE配网协议确认-小顾-2026-09-05.md §3
 * 自证：seq=1 → IV = 00 00 00 01 00 00 00 00 00 00 00 00 00 00 00 00（16B）
 */
function buildIv(seq: number): Uint8Array {
  const iv = new Uint8Array(16)
  const dv = new DataView(iv.buffer)
  dv.setUint32(0, seq >>> 0, false) // 大端写入 seq（协议 §3 定稿）
  return iv
}

// ===== 纯 JS AES-128 实现（T118：微信小程序无 WebCrypto 时的真实加密回退） =====

/** AES S-box（FIPS-197 Figure 7） */
const SBOX = new Uint8Array([
  0x63, 0x7c, 0x77, 0x7b, 0xf2, 0x6b, 0x6f, 0xc5, 0x30, 0x01, 0x67, 0x2b, 0xfe, 0xd7, 0xab, 0x76,
  0xca, 0x82, 0xc9, 0x7d, 0xfa, 0x59, 0x47, 0xf0, 0xad, 0xd4, 0xa2, 0xaf, 0x9c, 0xa4, 0x72, 0xc0,
  0xb7, 0xfd, 0x93, 0x26, 0x36, 0x3f, 0xf7, 0xcc, 0x34, 0xa5, 0xe5, 0xf1, 0x71, 0xd8, 0x31, 0x15,
  0x04, 0xc7, 0x23, 0xc3, 0x18, 0x96, 0x05, 0x9a, 0x07, 0x12, 0x80, 0xe2, 0xeb, 0x27, 0xb2, 0x75,
  0x09, 0x83, 0x2c, 0x1a, 0x1b, 0x6e, 0x5a, 0xa0, 0x52, 0x3b, 0xd6, 0xb3, 0x29, 0xe3, 0x2f, 0x84,
  0x53, 0xd1, 0x00, 0xed, 0x20, 0xfc, 0xb1, 0x5b, 0x6a, 0xcb, 0xbe, 0x39, 0x4a, 0x4c, 0x58, 0xcf,
  0xd0, 0xef, 0xaa, 0xfb, 0x43, 0x4d, 0x33, 0x85, 0x45, 0xf9, 0x02, 0x7f, 0x50, 0x3c, 0x9f, 0xa8,
  0x51, 0xa3, 0x40, 0x8f, 0x92, 0x9d, 0x38, 0xf5, 0xbc, 0xb6, 0xda, 0x21, 0x10, 0xff, 0xf3, 0xd2,
  0xcd, 0x0c, 0x13, 0xec, 0x5f, 0x97, 0x44, 0x17, 0xc4, 0xa7, 0x7e, 0x3d, 0x64, 0x5d, 0x19, 0x73,
  0x60, 0x81, 0x4f, 0xdc, 0x22, 0x2a, 0x90, 0x88, 0x46, 0xee, 0xb8, 0x14, 0xde, 0x5e, 0x0b, 0xdb,
  0xe0, 0x32, 0x3a, 0x0a, 0x49, 0x06, 0x24, 0x5c, 0xc2, 0xd3, 0xac, 0x62, 0x91, 0x95, 0xe4, 0x79,
  0xe7, 0xc8, 0x37, 0x6d, 0x8d, 0xd5, 0x4e, 0xa9, 0x6c, 0x56, 0xf4, 0xea, 0x65, 0x7a, 0xae, 0x08,
  0xba, 0x78, 0x25, 0x2e, 0x1c, 0xa6, 0xb4, 0xc6, 0xe8, 0xdd, 0x74, 0x1f, 0x4b, 0xbd, 0x8b, 0x8a,
  0x70, 0x3e, 0xb5, 0x66, 0x48, 0x03, 0xf6, 0x0e, 0x61, 0x35, 0x57, 0xb9, 0x86, 0xc1, 0x1d, 0x9e,
  0xe1, 0xf8, 0x98, 0x11, 0x69, 0xd9, 0x8e, 0x94, 0x9b, 0x1e, 0x87, 0xe9, 0xce, 0x55, 0x28, 0xdf,
  0x8c, 0xa1, 0x89, 0x0d, 0xbf, 0xe6, 0x42, 0x68, 0x41, 0x99, 0x2d, 0x0f, 0xb0, 0x54, 0xbb, 0x16,
])

/** AES Rcon（轮常量，前 10 轮） */
const RCON = new Uint8Array([0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0x1b, 0x36])

/** GF(2^8) xtime：a · x（即乘 2） */
function xtime(a: number): number {
  return (a << 1) ^ (a & 0x80 ? 0x1b : 0x00) & 0xff
}

/**
 * AES-128 密钥扩展：16B 密钥 → 176B 轮密钥（44 字 × 4 字节）
 * FIPS-197 §5.2 KeyExpansion
 */
function keyExpansion(key: Uint8Array): Uint8Array {
  const rk = new Uint8Array(176)
  rk.set(key.subarray(0, 16), 0)
  for (let i = 16; i < 176; i += 4) {
    const t0 = rk[i - 4]
    const t1 = rk[i - 3]
    const t2 = rk[i - 2]
    const t3 = rk[i - 1]
    if (i % 16 === 0) {
      // RotWord + SubWord + Rcon
      const r = SBOX[t1] ^ RCON[(i >> 4) - 1]
      const s = SBOX[t2]
      const u = SBOX[t3]
      const v = SBOX[t0]
      rk[i] = rk[i - 16] ^ r
      rk[i + 1] = rk[i - 15] ^ s
      rk[i + 2] = rk[i - 14] ^ u
      rk[i + 3] = rk[i - 13] ^ v
    } else {
      rk[i] = rk[i - 16] ^ t0
      rk[i + 1] = rk[i - 15] ^ t1
      rk[i + 2] = rk[i - 14] ^ t2
      rk[i + 3] = rk[i - 13] ^ t3
    }
  }
  return rk
}

/**
 * AES-128 单块加密（16B 输入 → 16B 输出）
 * 状态按列主序排列：state[r + 4*c] 对应第 r 行第 c 列。
 * FIPS-197 §5.1 Cipher
 */
function aesEncryptBlock(block: Uint8Array, rk: Uint8Array): Uint8Array {
  const s = new Uint8Array(block)

  // AddRoundKey(round 0)
  for (let i = 0; i < 16; i++) s[i] ^= rk[i]

  for (let round = 1; round <= 9; round++) {
    // SubBytes
    for (let i = 0; i < 16; i++) s[i] = SBOX[s[i]]

    // ShiftRows（行 r 左移 r 位）
    // Row 1: [s1, s5, s9, s13] → [s5, s9, s13, s1]
    let t = s[1]; s[1] = s[5]; s[5] = s[9]; s[9] = s[13]; s[13] = t
    // Row 2: [s2, s6, s10, s14] → [s10, s14, s2, s6]
    t = s[2]; s[2] = s[10]; s[10] = t
    t = s[6]; s[6] = s[14]; s[14] = t
    // Row 3: [s3, s7, s11, s15] → [s15, s3, s7, s11]
    t = s[3]; s[3] = s[15]; s[15] = s[11]; s[11] = s[7]; s[7] = t

    // MixColumns
    for (let c = 0; c < 4; c++) {
      const i = c * 4
      const a0 = s[i], a1 = s[i + 1], a2 = s[i + 2], a3 = s[i + 3]
      const x2 = xtime(a0), x3 = xtime(a1), x4 = xtime(a2), x5 = xtime(a3)
      s[i] = x2 ^ x3 ^ a1 ^ a2 ^ a3 // 2a0 ^ 3a1 ^ a2 ^ a3
      s[i + 1] = a0 ^ x3 ^ x4 ^ a2 ^ a3 // a0 ^ 2a1 ^ 3a2 ^ a3
      s[i + 2] = a0 ^ a1 ^ x4 ^ x5 ^ a3 // a0 ^ a1 ^ 2a2 ^ 3a3
      s[i + 3] = x2 ^ a0 ^ a1 ^ a2 ^ x5 // 3a0 ^ a1 ^ a2 ^ 2a3
    }

    // AddRoundKey
    const off = round * 16
    for (let i = 0; i < 16; i++) s[i] ^= rk[off + i]
  }

  // Round 10: SubBytes + ShiftRows + AddRoundKey（无 MixColumns）
  for (let i = 0; i < 16; i++) s[i] = SBOX[s[i]]
  let t = s[1]; s[1] = s[5]; s[5] = s[9]; s[9] = s[13]; s[13] = t
  t = s[2]; s[2] = s[10]; s[10] = t
  t = s[6]; s[6] = s[14]; s[14] = t
  t = s[3]; s[3] = s[15]; s[15] = s[11]; s[11] = s[7]; s[7] = t
  for (let i = 0; i < 16; i++) s[i] ^= rk[160 + i]

  return s
}

/**
 * 128-bit 大端自增（CTR counter）
 */
function incrementCounter(counter: Uint8Array): void {
  for (let i = 15; i >= 0; i--) {
    counter[i] = (counter[i] + 1) & 0xff
    if (counter[i] !== 0) break
  }
}

/**
 * 纯 JS AES-128-CTR 加密
 * @param key 16B 密钥
 * @param iv 16B 初始 counter
 * @param plain 明文（任意长度，CTR 无需填充）
 * @returns 密文（与明文等长）
 */
function aesCtrEncrypt(key: Uint8Array, iv: Uint8Array, plain: Uint8Array): Uint8Array {
  const rk = keyExpansion(key)
  const counter = new Uint8Array(iv)
  const out = new Uint8Array(plain.length)
  const blocks = Math.ceil(plain.length / 16)

  for (let b = 0; b < blocks; b++) {
    const keystream = aesEncryptBlock(counter, rk)
    const start = b * 16
    const end = Math.min(start + 16, plain.length)
    for (let i = start; i < end; i++) {
      out[i] = plain[i] ^ keystream[i - start]
    }
    incrementCounter(counter)
  }
  return out
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
  const plainBytes = encodeUtf8(plaintext)
  const keyBytes = hexToBytes(provisionKeyHex)
  const iv = buildIv(seq)

  // 优先 WebCrypto AES-CTR（H5 / 现代浏览器）
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
      // 降级到纯 JS 实现
    }
  }

  // T118: 纯 JS AES-128-CTR（微信小程序无 WebCrypto 时的真实加密回退）
  return bytesToHex(aesCtrEncrypt(keyBytes, iv, plainBytes))
}

/**
 * 仅供测试：直接调用纯 JS AES-128-CTR 实现，用于与 WebCrypto 做等价性校验。
 * 生产代码请使用 encryptWifiPayload。
 */
export function _aesCtrEncryptForTest(
  keyHex: string,
  ivHex: string,
  plainHex: string
): string {
  return bytesToHex(aesCtrEncrypt(hexToBytes(keyHex), hexToBytes(ivHex), hexToBytes(plainHex)))
}
