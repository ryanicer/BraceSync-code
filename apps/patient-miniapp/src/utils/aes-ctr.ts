/**
 * AES-128-CTR WiFi 凭据加密（纯 JS 实现，T118）
 *
 * 不依赖 crypto.subtle / TextEncoder（微信小程序真机与 CI Node 18 均可能缺失）。
 * 密钥：provision_key_hex（16B → 32hex）
 * IV：低 4B = seq（小端），高 12B 补 0
 * 明文：JSON { ssid, pwd, seq }
 */

// ===== AES-128 S-box =====
const SBOX = new Uint8Array([
  0x63,0x7c,0x77,0x7b,0xf2,0x6b,0x6f,0xc5,0x30,0x01,0x67,0x2b,0xfe,0xd7,0xab,0x76,
  0xca,0x82,0xc9,0x7d,0xfa,0x59,0x47,0xf0,0xad,0xd4,0xa2,0xaf,0x9c,0xa4,0x72,0xc0,
  0xb7,0xfd,0x93,0x26,0x36,0x3f,0xf7,0xcc,0x34,0xa5,0xe5,0xf1,0x71,0xd8,0x31,0x15,
  0x04,0xc7,0x23,0xc3,0x18,0x96,0x05,0x9a,0x07,0x12,0x80,0xe2,0xeb,0x27,0xb2,0x75,
  0x09,0x83,0x2c,0x1a,0x1b,0x6e,0x5a,0xa0,0x52,0x3b,0xd6,0xb3,0x29,0xe3,0x2f,0x84,
  0x53,0xd1,0x00,0xed,0x20,0xfc,0xb1,0x5b,0x6a,0xcb,0xbe,0x39,0x4a,0x4c,0x58,0xcf,
  0xd0,0xef,0xaa,0xfb,0x43,0x4d,0x33,0x85,0x45,0xf9,0x02,0x7f,0x50,0x3c,0x9f,0xa8,
  0x51,0xa3,0x40,0x8f,0x92,0x9d,0x38,0xf5,0xbc,0xb6,0xda,0x21,0x10,0xff,0xf3,0xd2,
  0xcd,0x0c,0x13,0xec,0x5f,0x97,0x44,0x17,0xc4,0xa7,0x7e,0x3d,0x64,0x5d,0x19,0x73,
  0x60,0x81,0x4f,0xdc,0x22,0x2a,0x90,0x88,0x46,0xee,0xb8,0x14,0xde,0x5e,0x0b,0xdb,
  0xe0,0x32,0x3a,0x0a,0x49,0x06,0x24,0x5c,0xc2,0xd3,0xac,0x62,0x91,0x95,0xe4,0x79,
  0xe7,0xc8,0x37,0x6d,0x8d,0xd5,0x4e,0xa9,0x6c,0x56,0xf4,0xea,0x65,0x7a,0xae,0x08,
  0xba,0x78,0x25,0x2e,0x1c,0xa6,0xb4,0xc6,0xe8,0xdd,0x74,0x1f,0x4b,0xbd,0x8b,0x8a,
  0x70,0x3e,0xb5,0x66,0x48,0x03,0xf6,0x0e,0x61,0x35,0x57,0xb9,0x86,0xc1,0x1d,0x9e,
  0xe1,0xf8,0x98,0x11,0x69,0xd9,0x8e,0x94,0x9b,0x1e,0x87,0xe9,0xce,0x55,0x28,0xdf,
  0x8c,0xa1,0x89,0x0d,0xbf,0xe6,0x42,0x68,0x41,0x99,0x2d,0x0f,0xb0,0x54,0xbb,0x16,
])

const RCON = new Uint8Array([0x01,0x02,0x04,0x08,0x10,0x20,0x40,0x80,0x1b,0x36])

function xtime(x: number): number {
  return ((x << 1) ^ (x & 0x80 ? 0x1b : 0)) & 0xff
}

function mul(a: number, b: number): number {
  let r = 0
  for (let i = 0; i < 8; i++) {
    if (b & 1) r ^= a
    a = xtime(a)
    b >>= 1
  }
  return r
}

/** AES-128 密钥扩展 → 11 组 16B 轮密钥 */
function keyExpansion(key: Uint8Array): Uint8Array[] {
  const roundKeys: Uint8Array[] = []
  const w = new Uint8Array(176) // 11 * 16
  w.set(key, 0)
  for (let i = 4; i < 44; i++) {
    let t0 = w[(i - 1) * 4], t1 = w[(i - 1) * 4 + 1], t2 = w[(i - 1) * 4 + 2], t3 = w[(i - 1) * 4 + 3]
    if (i % 4 === 0) {
      // RotWord
      const tmp = t0; t0 = t1; t1 = t2; t2 = t3; t3 = tmp
      // SubWord
      t0 = SBOX[t0]; t1 = SBOX[t1]; t2 = SBOX[t2]; t3 = SBOX[t3]
      // Rcon
      t0 ^= RCON[(i / 4) - 1]
    }
    w[i * 4] = w[(i - 4) * 4] ^ t0
    w[i * 4 + 1] = w[(i - 4) * 4 + 1] ^ t1
    w[i * 4 + 2] = w[(i - 4) * 4 + 2] ^ t2
    w[i * 4 + 3] = w[(i - 4) * 4 + 3] ^ t3
  }
  for (let r = 0; r < 11; r++) {
    roundKeys.push(w.slice(r * 16, r * 16 + 16))
  }
  return roundKeys
}

/** AES-128 单块加密（16B 输入 → 16B 输出） */
function aesEncryptBlock(block: Uint8Array, roundKeys: Uint8Array[]): Uint8Array {
  const state = new Uint8Array(block)
  // AddRoundKey 0
  for (let i = 0; i < 16; i++) state[i] ^= roundKeys[0][i]

  for (let round = 1; round <= 10; round++) {
    // SubBytes
    for (let i = 0; i < 16; i++) state[i] = SBOX[state[i]]
    // ShiftRows
    const s = new Uint8Array(state)
    state[1] = s[5]; state[5] = s[9]; state[9] = s[13]; state[13] = s[1]
    state[2] = s[10]; state[6] = s[14]; state[10] = s[2]; state[14] = s[6]
    state[3] = s[15]; state[7] = s[3]; state[11] = s[7]; state[15] = s[11]
    // MixColumns (skip last round)
    if (round < 10) {
      for (let c = 0; c < 4; c++) {
        const i = c * 4
        const a0 = state[i], a1 = state[i + 1], a2 = state[i + 2], a3 = state[i + 3]
        state[i] = mul(a0, 2) ^ mul(a1, 3) ^ a2 ^ a3
        state[i + 1] = a0 ^ mul(a1, 2) ^ mul(a2, 3) ^ a3
        state[i + 2] = a0 ^ a1 ^ mul(a2, 2) ^ mul(a3, 3)
        state[i + 3] = mul(a0, 3) ^ a1 ^ a2 ^ mul(a3, 2)
      }
    }
    // AddRoundKey
    for (let i = 0; i < 16; i++) state[i] ^= roundKeys[round][i]
  }
  return state
}

// ===== 工具函数 =====

function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2)
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(hex.substr(i * 2, 2), 16)
  }
  return bytes
}

function bytesToHex(bytes: Uint8Array): string {
  let s = ''
  for (let i = 0; i < bytes.length; i++) {
    s += bytes[i].toString(16).padStart(2, '0')
  }
  return s
}

/** 手写 UTF-8 编码（T115：微信小程序无 TextEncoder） */
function encodeUtf8(str: string): Uint8Array {
  const bytes: number[] = []
  for (let i = 0; i < str.length; i++) {
    let code = str.charCodeAt(i)
    if (code < 0x80) {
      bytes.push(code)
    } else if (code < 0x800) {
      bytes.push(0xc0 | (code >> 6), 0x80 | (code & 0x3f))
    } else if (code < 0xd800 || code >= 0xe000) {
      bytes.push(0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f))
    } else {
      // surrogate pair
      i++
      const code2 = str.charCodeAt(i)
      code = 0x10000 + (((code & 0x3ff) << 10) | (code2 & 0x3ff))
      bytes.push(
        0xf0 | (code >> 18),
        0x80 | ((code >> 12) & 0x3f),
        0x80 | ((code >> 6) & 0x3f),
        0x80 | (code & 0x3f),
      )
    }
  }
  return new Uint8Array(bytes)
}

/** IV 构造：低 4B = seq（小端），高 12B 补 0 */
function buildIv(seq: number): Uint8Array {
  const iv = new Uint8Array(16)
  const dv = new DataView(iv.buffer)
  dv.setUint32(0, seq >>> 0, true)
  return iv
}

/**
 * 加密 WiFi 明文为密文 hex（纯 JS AES-128-CTR）。
 * @param ssid WiFi 名称
 * @param pwd WiFi 密码
 * @param provisionKeyHex 32 位 hex（16B 密钥）
 * @param seq 序列号（防重放）
 * @returns 密文 hex 字符串
 */
export function encryptWifiPayload(
  ssid: string,
  pwd: string,
  provisionKeyHex: string,
  seq: number
): string {
  const plaintext = JSON.stringify({ ssid, pwd, seq })
  const plainBytes = encodeUtf8(plaintext)
  const keyBytes = hexToBytes(provisionKeyHex)
  const roundKeys = keyExpansion(keyBytes)
  const iv = buildIv(seq)

  const cipher = new Uint8Array(plainBytes.length)
  const counter = new Uint8Array(iv)

  for (let offset = 0; offset < plainBytes.length; offset += 16) {
    const keystream = aesEncryptBlock(counter, roundKeys)
    const blockLen = Math.min(16, plainBytes.length - offset)
    for (let i = 0; i < blockLen; i++) {
      cipher[offset + i] = plainBytes[offset + i] ^ keystream[i]
    }
    // 计数器自增（小端）
    let carry = 1
    for (let i = 0; i < 16 && carry; i++) {
      const v = counter[i] + carry
      counter[i] = v & 0xff
      carry = v >> 8
    }
  }

  return bytesToHex(cipher)
}
