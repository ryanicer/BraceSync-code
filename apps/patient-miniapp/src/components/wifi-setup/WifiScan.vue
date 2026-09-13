<template>
  <view class="scan">
    <!-- 2.4G 前置提示（PRD §7A.9 约束：必须前置到扫描页） -->
    <view class="notice-2g"><text>{{ SCAN.notice24G }}</text></view>

    <template v-if="devices.length > 0 || scanning">
      <view class="scan-area">
        <view class="radar">
          <view class="ring r1"></view>
          <view class="ring r2"></view>
          <view class="ring r3"></view>
          <view class="radar-core"><text class="core-glyph">BT</text></view>
        </view>
        <text class="scan-text">{{ SCAN.scanningText }}</text>
        <text class="scan-sub">{{ SCAN.scanningSub }}</text>
      </view>

      <view class="device-list">
        <view v-for="d in devices" :key="d.deviceId" class="device-item" @click="$emit('select', d)">
          <view class="device-ic"><text class="glyph">DEV</text></view>
          <view class="device-info">
            <text class="device-name">{{ d.name }}</text>
            <text class="device-tag">{{ signalText(d.RSSI) }}</text>
          </view>
          <text class="device-arrow">›</text>
        </view>
      </view>
    </template>

    <!-- 空态引导 -->
    <view v-else class="empty-state">
      <view class="empty-ic"><text class="glyph glyph-lg">?</text></view>
      <text class="empty-title">{{ SCAN.emptyTitle }}</text>
      <view class="empty-tips">
        <view v-for="(tip, i) in SCAN.emptyTips" :key="i" class="empty-tip">
          <text class="empty-tip-num">{{ i + 1 }}</text>
          <text
            v-if="i === SCAN.emptyTips.length - 1"
            class="empty-tip-txt empty-tip-link"
            @click="$emit('contact')"
          >{{ tip }}</text>
          <text v-else class="empty-tip-txt">{{ tip }}</text>
        </view>
      </view>
      <view class="retry-btn" @click="$emit('retry')"><text>{{ SCAN.retryBtn }}</text></view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { SCAN, SIGNAL_GOOD, SIGNAL_WEAK } from '../../pages/wifi-setup/copy'
import { signalLabel } from '../../utils/wifi-state'
import type { ScannedDevice } from '../../utils/ble'

defineProps<{
  devices: ScannedDevice[]
  scanning: boolean
}>()

defineEmits<{
  (e: 'select', dev: ScannedDevice): void
  (e: 'retry'): void
  (e: 'contact'): void
}>()

/** PRD §7A.9 患技差异表：患者端只说信号良好/弱，不出现 RSSI 数值 */
function signalText(rssi: number): string {
  return signalLabel(rssi, SIGNAL_GOOD, SIGNAL_WEAK)
}
</script>

<style scoped>
.scan { background: #f8fafc; min-height: 100vh; }
.notice-2g { margin: 24rpx 40rpx 0; background: #fff7ed; border: 2rpx solid #fed7aa; border-radius: 20rpx; padding: 24rpx 28rpx; }
.notice-2g text { font-size: 24rpx; color: #c2410c; line-height: 1.6; }

.scan-area { text-align: center; padding: 64rpx 0 40rpx; }
.radar { width: 240rpx; height: 240rpx; margin: 0 auto; position: relative; display: flex; align-items: center; justify-content: center; }
.radar-core { width: 112rpx; height: 112rpx; border-radius: 50%; background: linear-gradient(135deg, #2563EB, #6366f1); display: flex; align-items: center; justify-content: center; z-index: 2; box-shadow: 0 8rpx 32rpx rgba(37, 99, 235, 0.3); }
.core-glyph { color: #fff; font-size: 30rpx; font-weight: 700; }
.ring { position: absolute; border: 4rpx solid rgba(37, 99, 235, 0.25); border-radius: 50%; animation: pulse 2s ease-out infinite; }
.ring.r1 { width: 144rpx; height: 144rpx; }
.ring.r2 { width: 192rpx; height: 192rpx; animation-delay: 0.6s; }
.ring.r3 { width: 240rpx; height: 240rpx; animation-delay: 1.2s; }
@keyframes pulse { 0% { transform: scale(0.5); opacity: 0.8; } 100% { transform: scale(1.2); opacity: 0; } }
.scan-text { font-size: 30rpx; font-weight: 500; color: #1e293b; margin-top: 32rpx; display: block; }
.scan-sub { font-size: 24rpx; color: #94a3b8; margin-top: 8rpx; display: block; }

.device-list { padding: 16rpx 40rpx 0; }
.device-item { display: flex; align-items: center; background: #fff; border: 2rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx; margin-bottom: 20rpx; }
.device-item:active { border-color: #2563EB; background: #eff6ff; }
.device-ic { width: 80rpx; height: 80rpx; border-radius: 20rpx; background: #eff6ff; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.glyph { font-size: 22rpx; font-weight: 700; color: #2563EB; }
.glyph-lg { font-size: 40rpx; color: #94a3b8; }
.device-info { flex: 1; margin-left: 24rpx; min-width: 0; }
.device-name { font-size: 28rpx; font-weight: 500; color: #1e293b; display: block; }
.device-tag { font-size: 20rpx; color: #10ac84; background: #e8f5e9; padding: 2rpx 12rpx; border-radius: 12rpx; display: inline-block; margin-top: 6rpx; }
.device-arrow { color: #cbd5e1; font-size: 36rpx; }

.empty-state { text-align: center; padding: 80rpx 64rpx; }
.empty-ic { width: 160rpx; height: 160rpx; border-radius: 40rpx; background: #f1f5f9; margin: 0 auto; display: flex; align-items: center; justify-content: center; }
.empty-title { font-size: 30rpx; font-weight: 500; color: #475569; margin-top: 32rpx; display: block; }
.empty-tips { margin-top: 32rpx; text-align: left; background: #fff; border: 2rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx; }
.empty-tip { font-size: 24rpx; color: #64748b; line-height: 1.7; padding: 12rpx 0; display: flex; align-items: flex-start; }
.empty-tip-num { width: 36rpx; height: 36rpx; border-radius: 50%; background: #eff6ff; color: #2563EB; font-size: 20rpx; font-weight: 600; display: flex; align-items: center; justify-content: center; flex-shrink: 0; margin-top: 4rpx; margin-right: 16rpx; }
.empty-tip-txt { flex: 1; }
.empty-tip-link { color: #2563EB; text-decoration: underline; }
.retry-btn { margin-top: 40rpx; padding: 24rpx; background: #2563EB; border-radius: 24rpx; text-align: center; }
.retry-btn text { color: #fff; font-size: 30rpx; font-weight: 500; }
</style>
