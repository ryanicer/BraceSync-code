<template>
  <view class="page">
    <view class="brand-header">
      <image class="brand-logo" src="/static/logo.png" mode="aspectFit" />
      <text class="brand-name">矫智通</text>
      <text class="brand-desc">穿戴设备数据记录与管理</text>
    </view>

    <view class="content">
      <view
        :class="['btn-login', { 'btn-login-loading': loginLoading }]"
        @click="wechatLogin"
      >
        <text v-if="loginLoading" class="spinner"></text>
        <text>{{ loginLoading ? '正在登录...' : '手机号快捷登录' }}</text>
      </view>

      <view class="btn-guest" @click="guestExperience">
        <text>游客体验</text>
      </view>

      <view class="agreement">
        <view :class="['checkbox', { 'checkbox-checked': agreed }]" @click="agreed = !agreed">
          <text v-if="agreed" class="check-mark">✓</text>
        </view>
        <text class="agree-text">已阅读并同意</text>
        <text class="agree-link" @click="openPrivacy">《用户协议与隐私政策》</text>
      </view>
    </view>

    <view v-if="toastVisible" class="toast">
      <text class="toast-icon">✓</text>
      <text class="toast-text">{{ toastText }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { userErrorCopy } from '@bracesync/shared-utils'
import { useAuthStore } from '../../stores/auth'
import { wxLogin, type WxLoginData, type WxLoginNeedBindData } from '../../api/patient'
import { resolveWxLoginResult } from '../../utils/auth-state'
import { enterGuest, GUEST_TOKEN, GUEST_PATIENT_ID } from '../../utils/guest'

const authStore = useAuthStore()
const agreed = ref(false)
const loginLoading = ref(false)
const toastVisible = ref(false)
const toastText = ref('')

let toastTimer: ReturnType<typeof setTimeout> | null = null

function showSuccessToast(text: string, navigate?: () => void) {
  toastText.value = text
  toastVisible.value = true
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => {
    toastVisible.value = false
    navigate?.()
  }, 800)
}

let pending: Promise<void> | null = null

function checkAgreed(): boolean {
  if (agreed.value) return true
  uni.showModal({
    title: '提示',
    content: '请先阅读并同意协议和隐私政策',
    showCancel: false,
    confirmText: '确定',
  })
  return false
}

async function wechatLoginInner() {
  try {
    let code: string | undefined
    try {
      const res = await new Promise<UniApp.LoginRes>((resolve, reject) => {
        uni.login({
          provider: 'weixin',
          success: (r) => resolve(r),
          fail: (err) => reject(err),
        })
      })
      code = res?.code
    } catch {
      // H5/CI 无微信 SDK → 占位 code
      code = 'h5-fallback-wechat-login-code'
    }
    if (!code) {
      uni.showToast({ title: '登录失败，请重试', icon: 'none' })
      return
    }

    const env = await wxLogin(code)
    const result = resolveWxLoginResult(env.code)

    switch (result.state) {
      case 'SUCCESS': {
        const data = env.data as WxLoginData
        if (!data?.token || !data?.patientId) {
          uni.showToast({ title: '登录失败，请重试', icon: 'none' })
          return
        }
        authStore.login(data.token, data.patientId)
        showSuccessToast('登录成功，正在进入首页...', () => {
          uni.switchTab({ url: '/pages/monitor/index' })
        })
        break
      }
      case 'NEED_BIND': {
        const data = env.data as WxLoginNeedBindData
        if (data?.token) {
          authStore.saveBindToken(data.token)
        }
        uni.navigateTo({ url: '/pages/login/bind' })
        break
      }
      case 'FAIL':
      case 'ERROR':
      default:
        uni.showToast({ title: result.message || '登录失败，请重试', icon: 'none' })
        break
    }
  } catch (e) {
    const msg = userErrorCopy(e, { scope: 'patient', fallback: '登录失败，请重试' })
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    pending = null
    loginLoading.value = false
  }
}

function openPrivacy() {
  // 打开公众平台已配置的《用户隐私保护指引》，无需内置协议正文
  // #ifdef MP-WEIXIN
  if (typeof wx !== 'undefined' && typeof wx.openPrivacyContract === 'function') {
    wx.openPrivacyContract({ fail: () => uni.showToast({ title: '暂无法打开，请稍后重试', icon: 'none' }) })
    return
  }
  // #endif
  uni.showToast({ title: '请在微信中查看协议', icon: 'none' })
}

function guestExperience() {
  // 写本地游客标记 + 演示登录态；同步 Pinia 内存态，否则 monitor 页 patientId 仍为 null 直接 return
  enterGuest()
  authStore.login(GUEST_TOKEN, GUEST_PATIENT_ID)
  uni.switchTab({
    url: '/pages/monitor/index',
    fail: () => uni.reLaunch({ url: '/pages/monitor/index' }),
  })
}

function wechatLogin() {
  if (!checkAgreed()) return
  if (loginLoading.value) return
  loginLoading.value = true
  if (pending) return
  pending = wechatLoginInner()
}
</script>

<style scoped>
.page { min-height: 100%; padding-bottom: 240rpx; }
.brand-header { text-align: center; padding: 160rpx 0 80rpx; }
.brand-logo { width: 144rpx; height: 144rpx; margin: 0 auto; display: block; }
.brand-name { display: block; font-size: 44rpx; font-weight: 600; color: #1e293b; margin-top: 32rpx; letter-spacing: 2rpx; }
.brand-desc { display: block; font-size: 24rpx; color: #94a3b8; margin-top: 8rpx; }
.content { padding: 0 64rpx; }
.btn-login { width: 100%; padding: 28rpx 0; background: #2563EB; border-radius: 24rpx; display: flex; align-items: center; justify-content: center; gap: 16rpx; }
.btn-login text { color: #fff; font-size: 30rpx; font-weight: 500; }
.btn-login-loading { opacity: 0.8; }
.spinner { width: 32rpx; height: 32rpx; border: 4rpx solid rgba(255,255,255,0.3); border-top-color: #fff; border-radius: 50%; animation: spin 0.7s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.agreement { display: flex; align-items: flex-start; flex-wrap: wrap; gap: 8rpx; margin-top: 40rpx; font-size: 22rpx; line-height: 1.6; }
.checkbox { width: 28rpx; height: 28rpx; border: 2rpx solid #cbd5e1; border-radius: 6rpx; display: flex; align-items: center; justify-content: center; flex-shrink: 0; margin-top: 2rpx; }
.checkbox-checked { background: #2563EB; border-color: #2563EB; }
.check-mark { color: #fff; font-size: 20rpx; line-height: 1; }
.agree-text { color: #64748b; }
.agree-link { color: #2563EB; }
.btn-guest { width: 100%; padding: 26rpx 0; margin-top: 24rpx; background: #f1f5f9; border-radius: 24rpx; display: flex; align-items: center; justify-content: center; }
.btn-guest text { color: #475569; font-size: 28rpx; }
.toast { position: fixed; top: 160rpx; left: 50%; transform: translateX(-50%); background: #1e293b; color: #fff; padding: 20rpx 36rpx; border-radius: 16rpx; z-index: 999; display: flex; align-items: center; gap: 12rpx; box-shadow: 0 8rpx 32rpx rgba(0,0,0,0.2); }
.toast-icon { color: #22c55e; font-size: 28rpx; }
.toast-text { font-size: 26rpx; white-space: nowrap; }
</style>
