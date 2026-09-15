import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { TechLoginResult } from '@bracesync/shared-types'
import { getToken, setToken, removeToken, getTechId, setTechId, removeTechId } from '../utils/token'
import { request } from '../utils/request'
import { logger, maskPhone } from '../utils/logger'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getToken())
  const techId = ref<string | null>(getTechId())
  const name = ref<string | null>(null)
  const isLoggedIn = ref(!!token.value)

  function login(t: string, tid: string, displayName?: string) {
    token.value = t
    techId.value = tid
    name.value = displayName || null
    isLoggedIn.value = true
    setToken(t)
    setTechId(tid)
  }

  /** T208: 真实登录：POST /api/v1/tech/login
   *  - 登录前清残留 mock token（避免被 request 拦截器挂到登录请求上）
   *  - 手机号规范化（去空格/去 +86/仅保留数字）
   *  - 成功存 token+techId，失败抛错由页面提示 */
  async function loginWithPassword(phone: string, password: string) {
    // 1) T208 防御：清残留 mock token
    const existingToken = getToken()
    if (existingToken && existingToken.startsWith('mock-')) {
      logger.warn('[T208]', 'loginWithPassword: removing stale mock token', { tokenPrefix: existingToken.slice(0, 10) })
      removeToken()
      removeTechId()
    }

    // 2) 手机号规范化：trim → 去 +86/86 前缀 → 仅保留数字
    const normalized = String(phone).trim()
      .replace(/^\+?86/, '')
      .replace(/[^\d]/g, '')

    logger.info('[T208]', 'loginWithPassword', {
      rawPhone: maskPhone(phone),
      normalizedPhone: maskPhone(normalized),
      rawLen: String(phone).length,
      normalizedLen: normalized.length,
      pwdLen: password.length,
    })

    const result = await request<TechLoginResult>({
      url: '/api/v1/tech/login',
      method: 'POST',
      data: { phone: normalized, password },
    })

    login(result.token, result.techId, result.name)
    logger.info('[T208]', 'loginWithPassword success', { techId: result.techId, name: result.name })
  }

  function logout() {
    token.value = null
    techId.value = null
    name.value = null
    isLoggedIn.value = false
    removeToken()
    removeTechId()
  }

  /**
   * T089-R3-3: 登录成功后跳转首页，不是 bind
   * 安装入口在首页双卡片：新设备安装→bind / 安装记录→records
   */
  function goAfterLogin() {
    uni.reLaunch({ url: '/pages/home/index' })
  }

  return { token, techId, name, isLoggedIn, login, loginWithPassword, logout, goAfterLogin }
})
