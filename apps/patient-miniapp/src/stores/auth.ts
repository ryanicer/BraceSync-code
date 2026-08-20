import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getToken, setToken, removeToken, getPatientId, setPatientId, removePatientId } from '../utils/token'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getToken())
  const patientId = ref<string | null>(getPatientId())
  const isLoggedIn = ref(!!token.value)

  function login(t: string, pid: string) {
    token.value = t
    patientId.value = pid
    isLoggedIn.value = true
    setToken(t)
    setPatientId(pid)
  }

  function logout() {
    token.value = null
    patientId.value = null
    isLoggedIn.value = false
    removeToken()
    removePatientId()
  }

  return { token, patientId, isLoggedIn, login, logout }
})