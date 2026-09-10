<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">我的</text>
    </view>

    <view class="section">
      <view class="profile-card">
        <view class="avatar">{{ avatarChar }}</view>
        <view class="profile-info">
          <view class="profile-name">{{ name || '未登录' }}</view>
          <view class="profile-meta">患者 ID：{{ patientId || '—' }}</view>
        </view>
      </view>
    </view>

    <view class="section">
      <view class="menu-group">
        <view class="menu-item" @click="goWifiSetup">
          <text class="menu-ic">📶</text>
          <text class="menu-text">配置家庭 WiFi</text>
          <text class="menu-arrow">›</text>
        </view>
        <view class="menu-item" @click="bindWechat">
          <text class="menu-ic">💬</text>
          <text class="menu-text">绑定微信</text>
          <text class="menu-arrow">›</text>
        </view>
      </view>
    </view>

    <view class="section" v-if="authStore.isLoggedIn">
      <view class="btn-logout" @click="logout"><text>退出登录</text></view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { getPatientId } from '../../utils/token'

const authStore = useAuthStore()

const name = computed(() => '')
const patientId = computed(() => getPatientId())
const avatarChar = computed(() => (name.value ? name.value.charAt(0) : '患'))

function goWifiSetup() {
  uni.navigateTo({ url: '/pages/wifi-setup/index' })
}

function bindWechat() {
  // 退出当前登录态 → 走微信登录 → 命中未绑定 → 授权手机号匹配同一档案
  authStore.logout()
  uni.reLaunch({ url: '/pages/login/index' })
}

function logout() {
  uni.showModal({
    title: '提示',
    content: '确定要退出登录吗？',
    success: (r) => {
      if (r.confirm) {
        authStore.logout()
        uni.reLaunch({ url: '/pages/login/index' })
      }
    },
  })
}
</script>

<style scoped>
.page { min-height: 100%; padding-bottom: 80rpx; background: #f8fafc; }
.page-header { padding: 32rpx 48rpx 16rpx; }
.page-title { font-size: 28rpx; font-weight: 500; color: #94a3b8; letter-spacing: 1rpx; }
.section { padding: 24rpx 40rpx 0; }
.profile-card { display: flex; align-items: center; gap: 32rpx; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 40rpx; }
.avatar { width: 104rpx; height: 104rpx; border-radius: 50%; background: #2563EB; color: #fff; display: flex; align-items: center; justify-content: center; font-size: 44rpx; font-weight: 500; flex-shrink: 0; }
.profile-info { flex: 1; min-width: 0; }
.profile-name { font-size: 36rpx; font-weight: 500; color: #1e293b; }
.profile-meta { font-size: 24rpx; color: #94a3b8; margin-top: 8rpx; font-family: monospace; }
.menu-group { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; overflow: hidden; }
.menu-item { display: flex; align-items: center; gap: 24rpx; padding: 28rpx 32rpx; border-bottom: 1rpx solid #f1f5f9; }
.menu-item:last-child { border-bottom: none; }
.menu-ic { font-size: 32rpx; }
.menu-text { flex: 1; font-size: 28rpx; color: #1e293b; }
.menu-arrow { font-size: 32rpx; color: #cbd5e1; }
.btn-logout { padding: 28rpx 0; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; text-align: center; }
.btn-logout text { color: #ef4444; font-size: 30rpx; font-weight: 500; }
</style>
