import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getToken, setToken, removeToken, getPatientId, setPatientId, removePatientId } from '../utils/token'

const BIND_TOKEN_KEY = 'bracesync_bind_token'
const PHONE_TOKEN_KEY = 'bracesync_phone_token'
/** phoneToken 有效期：7 天（后端签发 7 天，客户端同窗口兜底） */
const PHONE_TOKEN_TTL_MS = 7 * 24 * 60 * 60 * 1000

function getBindToken(): string | null {
  try {
    return uni.getStorageSync(BIND_TOKEN_KEY) || null
  } catch {
    return null
  }
}

function setBindToken(t: string): void {
  try {
    uni.setStorageSync(BIND_TOKEN_KEY, t)
  } catch (e) {
    console.error('Failed to set bindToken:', e)
  }
}

function removeBindToken(): void {
  try {
    uni.removeStorageSync(BIND_TOKEN_KEY)
  } catch (e) {
    console.error('Failed to remove bindToken:', e)
  }
}

interface PhoneTokenEntry {
  token: string
  expiresAt: number
}

function getPhoneToken(): string | null {
  try {
    const raw = uni.getStorageSync(PHONE_TOKEN_KEY)
    if (!raw) return null
    const entry = JSON.parse(raw) as PhoneTokenEntry
    if (Date.now() > entry.expiresAt) {
      uni.removeStorageSync(PHONE_TOKEN_KEY)
      return null
    }
    return entry.token
  } catch {
    return null
  }
}

function setPhoneToken(t: string): void {
  try {
    const entry: PhoneTokenEntry = { token: t, expiresAt: Date.now() + PHONE_TOKEN_TTL_MS }
    uni.setStorageSync(PHONE_TOKEN_KEY, JSON.stringify(entry))
  } catch (e) {
    console.error('Failed to set phoneToken:', e)
  }
}

function removePhoneToken(): void {
  try {
    uni.removeStorageSync(PHONE_TOKEN_KEY)
  } catch (e) {
    console.error('Failed to remove phoneToken:', e)
  }
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getToken())
  const patientId = ref<string | null>(getPatientId())
  const isLoggedIn = ref(!!token.value)
  const bindToken = ref<string | null>(getBindToken())

  function login(t: string, pid: string) {
    token.value = t
    patientId.value = pid
    isLoggedIn.value = true
    setToken(t)
    setPatientId(pid)
    // 登录成功后清理绑定态凭证
    removeBindToken()
    removePhoneToken()
  }

  function logout() {
    token.value = null
    patientId.value = null
    isLoggedIn.value = false
    removeToken()
    removePatientId()
    removeBindToken()
    removePhoneToken()
  }

  function saveBindToken(t: string) {
    bindToken.value = t
    setBindToken(t)
  }

  function clearBindToken() {
    bindToken.value = null
    removeBindToken()
  }

  return {
    token,
    patientId,
    isLoggedIn,
    bindToken,
    login,
    logout,
    saveBindToken,
    clearBindToken,
    getPhoneToken,
    setPhoneToken,
    removePhoneToken,
  }
})