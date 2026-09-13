<template>
  <view class="success">
    <view class="success-anim">
      <view class="check-circle"><text class="check">✓</text></view>
      <text class="success-title">{{ SUCCESS.title }}</text>
      <text class="success-sub">{{ SUCCESS.sub }}</text>
    </view>

    <view class="info-card">
      <text class="info-label">{{ SUCCESS.infoLabel }}</text>
      <view class="info-row">
        <text class="info-key">{{ SUCCESS.deviceKey }}</text>
        <text class="info-val">{{ deviceLabel }}</text>
      </view>
      <view class="info-row">
        <text class="info-key">{{ SUCCESS.networkKey }}</text>
        <text class="info-val">{{ networkName }}</text>
      </view>
      <view class="info-row">
        <text class="info-key">{{ SUCCESS.statusKey }}</text>
        <view class="tag-online"><view class="dot"></view><text>{{ SUCCESS.onlineTag }}</text></view>
      </view>
    </view>

    <view class="benefit">
      <text class="benefit-head">{{ SUCCESS.benefitTitle }}</text>
      <text v-for="(b, i) in SUCCESS.benefits" :key="i" class="benefit-line">· {{ b }}</text>
    </view>

    <view class="btn-primary" @click="$emit('primary')"><text>{{ SUCCESS.primaryBtn }}</text></view>
    <view class="btn-secondary" @click="$emit('secondary')"><text>{{ SUCCESS.secondaryBtn }}</text></view>
  </view>
</template>

<script setup lang="ts">
import { SUCCESS } from '../../pages/wifi-setup/copy'

/**
 * deviceLabel 由页面传入广播名（BSYNC- + device_id 后 6 位）。
 * PRD §7A.9 患技差异表：完整 device_id 不展示给患者。
 */
defineProps<{
  deviceLabel: string
  networkName: string
}>()

defineEmits<{ (e: 'primary'): void; (e: 'secondary'): void }>()
</script>

<style scoped>
.success { background: #f8fafc; min-height: 100vh; }
.success-anim { margin-top: 180rpx; text-align: center; }
.check-circle { width: 176rpx; height: 176rpx; border-radius: 50%; background: #22c55e; margin: 0 auto; display: flex; align-items: center; justify-content: center; box-shadow: 0 16rpx 48rpx rgba(34, 197, 94, 0.35); }
.check { font-size: 88rpx; font-weight: 700; color: #fff; line-height: 1; }
.success-title { font-size: 44rpx; font-weight: 600; color: #1e293b; margin-top: 48rpx; display: block; }
.success-sub { font-size: 28rpx; color: #64748b; margin-top: 16rpx; display: block; }

.info-card { margin: 72rpx 40rpx 0; background: #fff; border: 2rpx solid #e2e8f0; border-radius: 28rpx; padding: 36rpx; }
.info-label { font-size: 24rpx; color: #94a3b8; margin-bottom: 24rpx; display: block; letter-spacing: 1rpx; }
.info-row { display: flex; align-items: center; padding: 20rpx 0; border-bottom: 2rpx solid #f1f5f9; }
.info-row:last-child { border-bottom: none; }
.info-key { font-size: 26rpx; color: #64748b; width: 160rpx; flex-shrink: 0; }
.info-val { font-size: 28rpx; color: #1e293b; font-weight: 500; flex: 1; }
.tag-online { display: inline-flex; align-items: center; }
.tag-online text { font-size: 22rpx; color: #10ac84; background: #e8f5e9; padding: 4rpx 16rpx; border-radius: 20rpx; }
.dot { width: 12rpx; height: 12rpx; border-radius: 50%; background: #10ac84; margin-right: 8rpx; }

.benefit { margin: 48rpx 40rpx 0; background: #eff6ff; border: 2rpx solid #bfdbfe; border-radius: 20rpx; padding: 28rpx; }
.benefit-head { font-size: 26rpx; font-weight: 700; color: #2563eb; display: block; margin-bottom: 8rpx; }
.benefit-line { font-size: 24rpx; color: #2563eb; line-height: 1.7; display: block; }

.btn-primary { margin: 64rpx 40rpx 0; padding: 28rpx; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 32rpx; font-weight: 500; }
.btn-secondary { margin: 24rpx 40rpx 0; padding: 28rpx; background: #f1f5f9; border-radius: 24rpx; text-align: center; }
.btn-secondary text { color: #475569; font-size: 30rpx; font-weight: 500; }
</style>
