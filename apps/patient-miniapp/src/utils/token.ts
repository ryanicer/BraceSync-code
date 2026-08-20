const TOKEN_KEY = 'bracesync_token'
const PATIENT_ID_KEY = 'bracesync_patient_id'

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

export function getPatientId(): string | null {
  try {
    return uni.getStorageSync(PATIENT_ID_KEY) || null
  } catch {
    return null
  }
}

export function setPatientId(id: string): void {
  try {
    uni.setStorageSync(PATIENT_ID_KEY, id)
  } catch (e) {
    console.error('Failed to set patientId:', e)
  }
}

export function removePatientId(): void {
  try {
    uni.removeStorageSync(PATIENT_ID_KEY)
  } catch (e) {
    console.error('Failed to remove patientId:', e)
  }
}