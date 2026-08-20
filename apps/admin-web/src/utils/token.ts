// Token 存取（localStorage，沿用 T016/T018 token 模式）
const TOKEN_KEY = 'admin_token'
const USER_KEY = 'admin_user'

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

export function removeToken(): void {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
}

export function getStoredUser(): string {
  return localStorage.getItem(USER_KEY) || ''
}

export function setStoredUser(userJson: string): void {
  localStorage.setItem(USER_KEY, userJson)
}
