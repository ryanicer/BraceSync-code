const TOKEN_KEY = 'bracesync_tech_token'
const TECH_ID_KEY = 'bracesync_tech_id'

export function getToken(): string | null {
  try {
    return uni.getStorageSync(TOKEN_KEY) || null
  } catch {
    return null
  }
}

export function setToken(token: string): void {
  try {
    uni.setStorageSync(TOKEN_KEY, token)
  } catch (e) {
    console.error('Failed to set token:', e)
  }
}

export function removeToken(): void {
  try {
    uni.removeStorageSync(TOKEN_KEY)
  } catch (e) {
    console.error('Failed to remove token:', e)
  }
}

export function getTechId(): string | null {
  try {
    return uni.getStorageSync(TECH_ID_KEY) || null
  } catch {
    return null
  }
}

export function setTechId(id: string): void {
  try {
    uni.setStorageSync(TECH_ID_KEY, id)
  } catch (e) {
    console.error('Failed to set techId:', e)
  }
}

export function removeTechId(): void {
  try {
    uni.removeStorageSync(TECH_ID_KEY)
  } catch (e) {
    console.error('Failed to remove techId:', e)
  }
}
