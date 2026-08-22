<template>
  <view class="page">
    <view class="brand-header">
      <view class="brand-logo"><text class="brand-icon">⚡</text></view>
      <text class="brand-name">矫治通</text>
      <text class="brand-desc">矫形支具佩戴监测与管理</text>
    </view>

    <view class="tabs-wrap">
      <view class="tabs">
        <view :class="['tab', { 'tab-active': activeTab === 'login' }]" @click="activeTab = 'login'"><text>登录</text></view>
        <view :class="['tab', { 'tab-active': activeTab === 'register' }]" @click="activeTab = 'register'"><text>注册</text></view>
      </view>
    </view>

    <view v-if="activeTab === 'login'" class="panel">
      <view class="input-group">
        <text class="input-icon">📱</text>
        <input type="number" class="input-field" placeholder="请输入手机号" maxlength="11" v-model="phone" />
      </view>
      <view class="input-group">
        <text class="input-icon">🔒</text>
        <input type="number" class="input-field input-sms" placeholder="验证码" v-model="smsCode" />
        <view :class="['sms-btn', { 'sms-disabled': smsCountdown > 0 }]" @click="sendSMS">
          <text>{{ smsCountdown > 0 ? smsCountdown + 's' : '获取验证码' }}</text>
        </view>
      </view>
      <view class="agree-row">
        <view :class="['checkbox', { 'checkbox-checked': agreed }]" @click="agreed = !agreed">
          <text v-if="agreed" class="check-mark">✓</text>
        </view>
        <text class="agree-text">已阅读并同意</text>
        <text class="agree-link">《用户协议》</text>
        <text class="agree-text">和</text>
        <text class="agree-link">《隐私政策》</text>
      </view>
      <view class="btn-primary" @click="doLogin"><text>登录</text></view>

      <view class="divider"><text class="divider-text">其他登录方式</text></view>

      <view class="btn-wechat" @click="wechatLogin">
        <text class="wechat-icon">💬</text>
        <text>微信授权登录</text>
      </view>
    </view>

    <view v-else class="panel">
      <view class="input-group">
        <text class="input-icon">📱</text>
        <input type="number" class="input-field" placeholder="请输入手机号" maxlength="11" v-model="phone" />
      </view>
      <view class="input-group">
        <text class="input-icon">🔒</text>
        <input type="number" class="input-field input-sms" placeholder="验证码" v-model="smsCode" />
        <view :class="['sms-btn', { 'sms-disabled': smsCountdown > 0 }]" @click="sendSMS">
          <text>{{ smsCountdown > 0 ? smsCountdown + 's' : '获取验证码' }}</text>
        </view>
      </view>
      <view class="input-group">
        <text class="input-icon">🔑</text>
        <input :password="!showPwd" class="input-field" placeholder="设置密码（6-16位）" v-model="password" />
        <text class="pwd-toggle" @click="showPwd = !showPwd">{{ showPwd ? '🙈' : '👁' }}</text>
      </view>
      <view class="input-group">
        <text class="input-icon">🔑</text>
        <input :password="!showPwd" class="input-field" placeholder="确认密码" v-model="confirmPassword" />
      </view>
      <view class="agree-row">
        <view :class="['checkbox', { 'checkbox-checked': agreed }]" @click="agreed = !agreed">
          <text v-if="agreed" class="check-mark">✓</text>
        </view>
        <text class="agree-text">已阅读并同意</text>
        <text class="agree-link">《用户协议》</text>
        <text class="agree-text">和</text>
        <text class="agree-link">《隐私政策》</text>
      </view>
      <view class="btn-primary" @click="doRegister"><text>注册</text></view>
      <view class="switch-to-login" @click="activeTab = 'login'">
        <text class="muted-text">已有账号？</text><text class="link-text">去登录</text>
      </view>
    </view>

    <view v-if="toastVisible" class="toast">
      <text class="toast-icon">✓</text>
      <text class="toast-text">{{ toastText }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, onUnmounted } from 'vue'
import { useAuthStore } from '../../stores/auth'

const authStore = useAuthStore()
const activeTab = ref<'login' | 'register'>('login')
const phone = ref('')
const smsCode = ref('')
const password = ref('')
const confirmPassword = ref('')
const showPwd = ref(false)
const agreed = ref(true)
const smsCountdown = ref(0)
const toastVisible = ref(false)
const toastText = ref('')
let smsTimer: ReturnType<typeof setInterval> | null = null

function sendSMS() {
  if (smsCountdown.value > 0) return
  if (!/^1\d{10}$/.test(phone.value)) {
    uni.showToast({ title: '请输入正确的手机号', icon: 'none' })
    return
  }
  smsCountdown.value = 60
  smsTimer = setInterval(() => {
    smsCountdown.value--
    if (smsCountdown.value <= 0 && smsTimer) {
      clearInterval(smsTimer)
      smsTimer = null
    }
  }, 1000)
  uni.showToast({ title: '验证码已发送（mock）', icon: 'none' })
}

function checkAgreed(): boolean {
  if (!agreed.value) {
    uni.showToast({ title: '请先同意用户协议和隐私政策', icon: 'none' })
    return false
  }
  return true
}

function showToast(text: string) {
  toastText.value = text
  toastVisible.value = true
  setTimeout(() => {
    toastVisible.value = false
    uni.switchTab({ url: '/pages/monitor/index' })
  }, 1500)
}

// MOCK: 跳过验证直接登录
// 替换计划: utils/request.ts 设 USE_MOCK=false，接 user-service POST /api/v1/auth/login
function doLogin() {
  if (!checkAgreed()) return
  if (!/^1\d{10}$/.test(phone.value)) {
    uni.showToast({ title: '请输入正确的手机号', icon: 'none' })
    return
  }
  if (!smsCode.value) {
    uni.showToast({ title: '请输入验证码', icon: 'none' })
    return
  }
  authStore.login('mock-token-001', 'pat-001')
  showToast('登录成功，正在跳转...')
}

function doRegister() {
  if (!checkAgreed()) return
  if (!/^1\d{10}$/.test(phone.value)) {
    uni.showToast({ title: '请输入正确的手机号', icon: 'none' })
    return
  }
  if (!password.value || password.value.length < 6 || password.value.length > 16) {
    uni.showToast({ title: '密码需为 6-16 位', icon: 'none' })
    return
  }
  if (password.value !== confirmPassword.value) {
    uni.showToast({ title: '两次密码不一致', icon: 'none' })
    return
  }
  authStore.login('mock-token-001', 'pat-001')
  showToast('注册成功，正在跳转...')
}

// MOCK: 微信授权登录
// 替换计划: uni.login 获取 code -> user-service POST /api/v1/auth/wechat
function wechatLogin() {
  if (!checkAgreed()) return
  authStore.login('mock-token-001', 'pat-001')
  showToast('微信授权登录成功，正在跳转...')
}

onUnmounted(() => {
  if (smsTimer) clearInterval(smsTimer)
})
</script>

<style scoped>
/* 微信端视口单位兼容性存疑，小程序端改用 page 高度撑满；H5 端保留 100vh */
/* #ifdef MP-WEIXIN */
.page { padding-bottom: 240rpx; min-height: 100%; }
/* #endif */
/* #ifndef MP-WEIXIN */
.page { padding-bottom: 240rpx; min-height: 100vh; }
/* #endif */
.brand-header { text-align: center; padding: 112rpx 0 40rpx; }
.brand-logo { width: 144rpx; height: 144rpx; border-radius: 40rpx; background: linear-gradient(135deg, #2563EB, #6366f1); margin: 0 auto; display: flex; align-items: center; justify-content: center; box-shadow: 0 8rpx 32rpx rgba(37, 99, 235, 0.25); }
.brand-icon { font-size: 72rpx; }
.brand-name { display: block; font-size: 44rpx; font-weight: 600; color: #1e293b; margin-top: 32rpx; letter-spacing: 2rpx; }
.brand-desc { display: block; font-size: 24rpx; color: #94a3b8; margin-top: 8rpx; }
.tabs-wrap { padding: 0 64rpx; }
.tabs { display: flex; background: #f1f5f9; border-radius: 20rpx; padding: 6rpx; gap: 4rpx; }
.tab { flex: 1; text-align: center; padding: 20rpx 0; font-size: 30rpx; font-weight: 500; color: #64748b; border-radius: 16rpx; transition: all 0.2s; }
.tab-active { background: #fff; color: #2563EB; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.08); }
.panel { padding: 40rpx 64rpx 0; }
.input-group { display: flex; align-items: center; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 0 28rpx; margin-bottom: 24rpx; }
.input-icon { font-size: 32rpx; margin-right: 20rpx; }
.input-field { flex: 1; width: 0; height: 88rpx; line-height: 88rpx; border: none; padding: 0; font-size: 30rpx; color: #1e293b; }
.input-sms { min-width: 0; }
.pwd-toggle { font-size: 32rpx; padding: 0 8rpx; }
.sms-btn { flex-shrink: 0; padding: 12rpx 0 12rpx 24rpx; }
.sms-btn text { font-size: 26rpx; font-weight: 500; color: #2563EB; white-space: nowrap; }
.sms-disabled text { color: #cbd5e1; }
.agree-row { display: flex; align-items: center; gap: 8rpx; margin-bottom: 40rpx; font-size: 22rpx; }
.checkbox { width: 28rpx; height: 28rpx; border: 2rpx solid #cbd5e1; border-radius: 6rpx; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.checkbox-checked { background: #2563EB; border-color: #2563EB; }
.check-mark { color: #fff; font-size: 20rpx; line-height: 1; }
.agree-text { color: #94a3b8; }
.agree-link { color: #2563EB; }
.btn-primary { width: 100%; padding: 28rpx 0; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 32rpx; font-weight: 500; }
.divider { display: flex; align-items: center; margin: 56rpx 0 40rpx; }
.divider::before, .divider::after { content: ''; flex: 1; height: 1rpx; background: #e2e8f0; }
.divider-text { padding: 0 28rpx; font-size: 24rpx; color: #94a3b8; }
.btn-wechat { width: 100%; padding: 26rpx 0; background: #07C160; border-radius: 24rpx; display: flex; align-items: center; justify-content: center; gap: 16rpx; }
.btn-wechat text { color: #fff; font-size: 30rpx; font-weight: 500; }
.wechat-icon { font-size: 36rpx; }
.switch-to-login { text-align: center; margin-top: 24rpx; }
.muted-text { font-size: 24rpx; color: #94a3b8; }
.link-text { font-size: 24rpx; color: #2563EB; }
.toast { position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%); background: #1e293b; padding: 32rpx 56rpx; border-radius: 24rpx; display: flex; align-items: center; gap: 16rpx; z-index: 200; box-shadow: 0 16rpx 48rpx rgba(0, 0, 0, 0.2); }
.toast-icon { color: #22c55e; font-size: 36rpx; }
.toast-text { color: #fff; font-size: 28rpx; }
</style>