<template>
  <view class="page">
    <!-- 顶部用户栏 -->
    <view class="top-bar">
      <view class="user-info">
        <view class="user-avatar"><text>{{ avatarChar }}</text></view>
        <view class="user-meta">
          <text class="user-name">{{ authStore.name || '技师' }}</text>
          <text class="user-role">工号 {{ authStore.techId || '--' }}</text>
        </view>
      </view>
      <view class="data-source-chip ds-cloud">
        <text class="chip-dot"></text>
        <text>云端</text>
      </view>
    </view>

    <!-- 欢迎语 -->
    <view class="welcome">
      <text class="welcome-title">开始工作</text>
      <text class="welcome-sub">选择要执行的操作</text>
    </view>

    <!-- 主操作区 -->
    <view class="action-area">
      <view class="action-card primary" @click="goBind">
        <view class="action-icon">
          <text class="icon-plus">+</text>
        </view>
        <view class="action-text">
          <text class="action-title">新设备安装</text>
          <text class="action-desc">扫码绑定 · 校准 · WiFi 配网</text>
        </view>
        <text class="action-arrow">›</text>
      </view>

      <view class="action-card secondary" @click="goRecords">
        <view class="action-icon">
          <text class="icon-list">≡</text>
        </view>
        <view class="action-text">
          <text class="action-title">安装记录</text>
          <text class="action-desc">查看历史安装与设备状态</text>
        </view>
        <text class="action-arrow">›</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAuthStore } from '../../stores/auth'

const authStore = useAuthStore()

const avatarChar = computed(() => {
  const n = authStore.name
  return n ? n.charAt(0) : '技'
})

function goBind() {
  uni.navigateTo({ url: '/pages/bind/index' })
}

function goRecords() {
  uni.navigateTo({ url: '/pages/records/index' })
}

onMounted(() => {
  if (!authStore.isLoggedIn) {
    uni.reLaunch({ url: '/pages/login/index' })
  }
})
</script>

<style scoped>
.page { min-height: 100vh; background: #f3f4f6; padding-bottom: 120rpx; }
.top-bar { padding: 96rpx 48rpx 32rpx; display: flex; justify-content: space-between; align-items: center; }
.user-info { display: flex; align-items: center; gap: 20rpx; }
.user-avatar { width: 80rpx; height: 80rpx; border-radius: 50%; background: #3B82F6; display: flex; align-items: center; justify-content: center; color: #fff; font-size: 30rpx; font-weight: 600; }
.user-meta { display: flex; flex-direction: column; }
.user-name { font-size: 32rpx; font-weight: 600; color: #1f2937; }
.user-role { font-size: 24rpx; color: #9ca3af; margin-top: 4rpx; }
.data-source-chip { display: inline-flex; align-items: center; gap: 8rpx; padding: 6rpx 16rpx; border-radius: 20rpx; font-size: 22rpx; font-weight: 500; }
.ds-cloud { background: #dbeafe; color: #2563eb; }
.chip-dot { width: 10rpx; height: 10rpx; border-radius: 50%; background: #2563eb; }
.welcome { padding: 16rpx 48rpx 64rpx; }
.welcome-title { display: block; font-size: 48rpx; font-weight: 700; color: #1f2937; line-height: 1.3; }
.welcome-sub { display: block; font-size: 28rpx; color: #9ca3af; margin-top: 12rpx; }
.action-area { padding: 0 48rpx; display: flex; flex-direction: column; gap: 32rpx; }
.action-card { border-radius: 32rpx; padding: 56rpx 48rpx; display: flex; align-items: center; gap: 32rpx; }
.action-card.primary { background: linear-gradient(135deg, #3B82F6, #2563EB); color: #fff; }
.action-card.secondary { background: #f9fafb; border: 3rpx solid #e5e7eb; }
.action-icon { width: 112rpx; height: 112rpx; border-radius: 32rpx; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.action-card.primary .action-icon { background: rgba(255,255,255,0.2); }
.action-card.secondary .action-icon { background: #eff6ff; }
.icon-plus { font-size: 56rpx; color: #fff; }
.icon-list { font-size: 56rpx; color: #3B82F6; }
.action-text { flex: 1; display: flex; flex-direction: column; }
.action-title { font-size: 40rpx; font-weight: 700; margin-bottom: 8rpx; }
.action-card.secondary .action-title { color: #1f2937; }
.action-desc { font-size: 26rpx; opacity: 0.8; }
.action-card.secondary .action-desc { color: #9ca3af; opacity: 1; }
.action-arrow { font-size: 48rpx; opacity: 0.7; }
.action-card.secondary .action-arrow { color: #9ca3af; }
</style>
