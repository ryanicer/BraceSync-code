import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getToken, setToken, removeToken, getTechId, setTechId, removeTechId } from '../utils/token'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getToken())
  const techId = ref<string | null>(getTechId())
  const isLoggedIn = ref(!!token.value)

  function login(t: string, tid: string) {
    token.value = t
    techId.value = tid
    isLoggedIn.value = true
    setToken(t)
    setTechId(tid)
  }

  function logout() {
    token.value = null
    techId.value = null
    isLoggedIn.value = false
    removeToken()
    removeTechId()
  }

  return { token, techId, isLoggedIn, login, logout }
})
