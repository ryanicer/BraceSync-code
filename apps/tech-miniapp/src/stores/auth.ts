import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { TechLoginResult } from '@bracesync/shared-types'
import { getToken, setToken, removeToken, getTechId, setTechId, removeTechId } from '../utils/token'
import { request } from '../utils/request'

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

  /** 真实登录（USE_MOCK=false，T037）：POST /api/v1/tech/login，成功存 token+techId，失败抛错由页面提示 */
  async function loginWithPassword(phone: string, password: string) {
    const result = await request<TechLoginResult>({
      url: '/api/v1/tech/login',
      method: 'POST',
      data: { phone, password },
    })
    login(result.token, result.techId, result.name)
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
