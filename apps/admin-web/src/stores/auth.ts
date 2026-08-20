import { defineStore } from 'pinia'
import { getToken, setToken, removeToken, getStoredUser, setStoredUser } from '../utils/token'
import { roleKeyFromRoleId, type RoleKey } from '../router/permissions'
import { adminLogin } from '../api'

export interface AdminUser {
  name: string
  /** 真实登录由 roleId 映射，未知角色为 null（守卫 fail-closed → 403）；mock 登录恒有值 */
  role: RoleKey | null
  // 以下为真实登录（T046）字段，mock 账号不含
  adminId?: string
  username?: string
  roleId?: string
  scope?: string
}

// 预置演示账号（mock 阶段，USE_MOCK=true 本地开发路径）
const MOCK_ACCOUNTS: Record<RoleKey, AdminUser> = {
  admin: { name: '运营管理员', role: 'admin' },
  doctor: { name: '张建国医生', role: 'doctor' },
  cs: { name: '客服小美', role: 'cs' },
}

export const useAuthStore = defineStore('auth', {
  state: () => {
    let user: AdminUser | null = null
    const stored = getStoredUser()
    if (stored) {
      try {
        user = JSON.parse(stored) as AdminUser
      } catch {
        user = null
      }
    }
    return {
      token: getToken(),
      user,
    }
  },
  getters: {
    isLoggedIn: (state) => Boolean(state.token && state.user),
    role: (state): RoleKey | null => state.user?.role ?? null,
  },
  actions: {
    /** mock 登录（USE_MOCK=true 本地开发）：选择预置角色签发假 token */
    login(role: RoleKey) {
      const account = MOCK_ACCOUNTS[role]
      this.user = { ...account }
      this.token = `mock-token-${role}-${Date.now()}`
      setToken(this.token)
      setStoredUser(JSON.stringify(this.user))
    },
    /** 真实登录（USE_MOCK=false，T046）：POST /api/v1/auth/login，成功存 token+user，失败抛错由页面提示 */
    async loginWithPassword(username: string, password: string) {
      const result = await adminLogin(username, password)
      this.user = {
        name: result.name,
        role: roleKeyFromRoleId(result.roleId),
        adminId: result.adminId,
        username: result.username,
        roleId: result.roleId,
        scope: result.scope,
      }
      this.token = result.token
      setToken(this.token)
      setStoredUser(JSON.stringify(this.user))
    },
    logout() {
      this.token = ''
      this.user = null
      removeToken()
    },
  },
})
