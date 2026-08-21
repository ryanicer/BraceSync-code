<template>
  <view class="page">
    <!-- Brand Header -->
    <view class="brand-header">
      <view class="brand-logo"><text class="brand-icon">⚡</text></view>
      <text class="brand-name">矫智通</text>
      <text class="brand-desc">矫形支具佩戴监测与管理 — 技师版</text>
    </view>

    <!-- Pure Login Panel (No Register Tab) -->
    <view class="panel">
      <!-- Phone Input -->
      <view class="input-group">
        <text class="input-icon">📱</text>
        <input 
          type="number" 
          class="input-field" 
          placeholder="请输入手机号" 
          maxlength="11" 
          v-model="phone" 
        />
      </view>

      <!-- Password Input -->
      <view class="input-group">
        <text class="input-icon">🔒</text>
        <input 
          :password="!showPassword" 
          class="input-field" 
          placeholder="请输入密码（6-16 位）" 
          v-model="password" 
        />
        <text class="pwd-toggle" @click="showPassword = !showPassword">
          {{ showPassword ? '🙈' : '👁' }}
        </text>
      </view>

      <!-- Agreement Checkbox -->
      <view class="agree-row">
        <view 
          :class="['checkbox', { 'checkbox-checked': agreed }]" 
          @click="agreed = !agreed"
        >
          <text v-if="agreed" class="check-mark">✓</text>
        </view>
        <text class="agree-text">已阅读并同意</text>
        <text class="agree-link">《用户协议》</text>
        <text class="agree-text">和</text>
        <text class="agree-link">《隐私政策》</text>
      </view>

      <!-- Login Button -->
      <view :class="['btn-primary', { 'btn-disabled': loading }]" @click="doLogin">
        <text>{{ loading ? '登录中...' : '登录' }}</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { USE_MOCK } from '../../utils/request'

const authStore = useAuthStore()
const phone = ref('')
const password = ref('')
const showPassword = ref(false)
const agreed = ref(false)
const loading = ref(false)

function checkAgreed(): boolean {
  if (!agreed.value) {
    uni.showToast({ title: '请先同意用户协议和隐私政策', icon: 'none' })
    return false
  }
  return true
}

function isValidPhone(p: string): boolean {
  return /^1\d{10}$/.test(p)
}

function isValidPassword(pwd: string): boolean {
  return pwd.length >= 6 && pwd.length <= 16
}

async function doLogin() {
  // 1. Check agreement
  if (!checkAgreed()) return

  // 2. Validate inputs
  if (!isValidPhone(phone.value)) {
    uni.showToast({ title: '请输入正确的手机号', icon: 'none' })
    return
  }

  if (!isValidPassword(password.value)) {
    uni.showToast({ title: '密码需为 6-16 位', icon: 'none' })
    return
  }

  loading.value = true
  try {
    // 3. MOCK mode (USE_MOCK = true)
    if (USE_MOCK) {
      // Mock success response
      const mockToken = 'mock-tech-token-001'
      const mockTechId = 'TECH' + Math.random().toString(16).slice(2, 14)

      authStore.login(mockToken, mockTechId)

      uni.showToast({
        title: '登录成功，正在跳转...',
        icon: 'success',
      })

      setTimeout(() => {
        uni.reLaunch({ url: '/pages/bind/index' })
      }, 1500)
      return
    }

    // 4. REAL API (USE_MOCK = false) — POST /api/v1/tech/login
    await authStore.loginWithPassword(phone.value.trim(), password.value)

    uni.showToast({
      title: '登录成功，正在跳转...',
      icon: 'success',
    })

    setTimeout(() => {
      uni.reLaunch({ url: '/pages/bind/index' })
    }, 1500)
  } catch (error) {
    // 失败统一提示（对齐后端防枚举，不区分"用户不存在/密码错误"）
    uni.showToast({
      title: '手机号或密码错误',
      icon: 'none',
    })
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.page { padding-bottom: 240rpx; min-height: 100vh; }
.brand-header { text-align: center; padding: 112rpx 0 40rpx; }
.brand-logo { 
  width: 144rpx; 
  height: 144rpx; 
  border-radius: 40rpx; 
  background: linear-gradient(135deg, #2563EB, #6366f1); 
  margin: 0 auto; 
  display: flex; 
  align-items: center; 
  justify-content: center; 
  box-shadow: 0 8rpx 32rpx rgba(37, 99, 235, 0.25); 
}
.brand-icon { font-size: 72rpx; }
.brand-name { 
  display: block; 
  font-size: 44rpx; 
  font-weight: 600; 
  color: #1e293b; 
  margin-top: 32rpx; 
  letter-spacing: 2rpx; 
}
.brand-desc { 
  display: block; 
  font-size: 24rpx; 
  color: #94a3b8; 
  margin-top: 8rpx; 
}
.panel { padding: 40rpx 64rpx 0; }
.input-group { 
  display: flex; 
  align-items: center; 
  background: #fff; 
  border: 1rpx solid #e2e8f0; 
  border-radius: 24rpx; 
  padding: 0 28rpx; 
  margin-bottom: 24rpx; 
}
.input-icon { font-size: 32rpx; margin-right: 20rpx; }
.input-field { 
  flex: 1; 
  min-width: 0; 
  border: none; 
  padding: 28rpx 0; 
  font-size: 30rpx; 
  color: #1e293b; 
}
.pwd-toggle { font-size: 32rpx; padding: 0 8rpx; cursor: pointer; }
.agree-row { 
  display: flex; 
  align-items: center; 
  gap: 8rpx; 
  margin-bottom: 40rpx; 
  font-size: 22rpx; 
}
.checkbox { 
  width: 28rpx; 
  height: 28rpx; 
  border: 2rpx solid #cbd5e1; 
  border-radius: 6rpx; 
  display: flex; 
  align-items: center; 
  justify-content: center; 
  flex-shrink: 0; 
}
.checkbox-checked { 
  background: #2563EB; 
  border-color: #2563EB; 
}
.check-mark { color: #fff; font-size: 20rpx; line-height: 1; }
.agree-text { color: #94a3b8; }
.agree-link { color: #2563EB; cursor: pointer; }
.btn-primary { 
  width: 100%; 
  padding: 28rpx 0; 
  background: #2563EB; 
  border-radius: 24rpx; 
  text-align: center; 
}
.btn-primary text { 
  color: #fff; 
  font-size: 32rpx; 
  font-weight: 500; 
}
.btn-disabled {
  opacity: 0.6;
}
</style>
