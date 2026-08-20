<template>
  <view class="page">
    <view class="page-header">
      <text class="back-link" @click="goBack">← 返回</text>
      <text class="page-title">保存基线</text>
    </view>

    <view class="section">
      <view class="card">
        <text class="card-title">基线数据校验</text>
        <text class="card-desc">确认 20 点 offset_values 数据完整性</text>

        <!-- 校验状态 -->
        <view class="validation-status">
          <view :class="['val-item', validationStatus.count ? 'val-pass' : 'val-fail']">
            <text class="val-icon">{{ validationStatus.count ? '✓' : '✗' }}</text>
            <text class="val-text">数据点数: {{ offsetValues.length }}/20</text>
          </view>
          <view :class="['val-item', validationStatus.range ? 'val-pass' : 'val-fail']">
            <text class="val-icon">{{ validationStatus.range ? '✓' : '✗' }}</text>
            <text class="val-text">范围校验: {{ validationStatus.range ? '通过' : '异常' }}</text>
          </view>
          <view :class="['val-item', validationStatus.stable ? 'val-pass' : 'val-fail']">
            <text class="val-icon">{{ validationStatus.stable ? '✓' : '✗' }}</text>
            <text class="val-text">稳定性: {{ validationStatus.stable ? '通过' : '待确认' }}</text>
          </view>
        </view>

        <!-- 20 点数据展示 -->
        <view class="offset-grid">
          <view v-for="(v, i) in offsetValues" :key="i" :class="['offset-cell', isOutOfRange(v) ? 'cell-warn' : '']">
            <text class="offset-idx">P{{ String(i + 1).padStart(2, '0') }}</text>
            <text class="offset-val">{{ v }}</text>
          </view>
        </view>

        <!-- 统计摘要 -->
        <view class="stats-row">
          <view class="stat-box">
            <text class="stat-label">平均值</text>
            <text class="stat-value">{{ stats.avg }}</text>
          </view>
          <view class="stat-box">
            <text class="stat-label">最大值</text>
            <text class="stat-value">{{ stats.max }}</text>
          </view>
          <view class="stat-box">
            <text class="stat-label">最小值</text>
            <text class="stat-value">{{ stats.min }}</text>
          </view>
          <view class="stat-box">
            <text class="stat-label">标准差</text>
            <text class="stat-value">{{ stats.std }}</text>
          </view>
        </view>
      </view>
    </view>

    <view class="section">
      <view v-if="allValid" class="btn-primary" @click="saveBaseline"><text>确认保存基线</text></view>
      <view v-else class="btn-disabled"><text>数据校验未通过，无法保存</text></view>
    </view>

    <view v-if="saved" class="toast">
      <text class="toast-icon">✓</text>
      <text class="toast-text">基线已保存</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useInstallStore } from '../../stores/install'
import { mockBaseline } from '../../mock/baseline'

const installStore = useInstallStore()
const saved = ref(false)

// MOCK: 从 installStore 或 mock 获取 offset_values
// 替换计划: 接 device-service GET /api/v1/baselines/{deviceId}/latest
const offsetValues = ref<number[]>(
  installStore.currentBaseline?.offsetValues || mockBaseline().offsetValues
)

function isOutOfRange(v: number): boolean {
  return Math.abs(v) > 0.5
}

const validationStatus = computed(() => ({
  count: offsetValues.value.length === 20,
  range: offsetValues.value.every(v => Math.abs(v) <= 1.0),
  stable: offsetValues.value.filter(v => Math.abs(v) > 0.5).length <= 2,
}))

const allValid = computed(() =>
  validationStatus.value.count && validationStatus.value.range
)

const stats = computed(() => {
  const vals = offsetValues.value
  if (vals.length === 0) return { avg: '--', max: '--', min: '--', std: '--' }
  const avg = vals.reduce((s, v) => s + v, 0) / vals.length
  const max = Math.max(...vals)
  const min = Math.min(...vals)
  const variance = vals.reduce((s, v) => s + (v - avg) ** 2, 0) / vals.length
  const std = Math.sqrt(variance)
  return {
    avg: avg.toFixed(3),
    max: max.toFixed(3),
    min: min.toFixed(3),
    std: std.toFixed(3),
  }
})

// MOCK: 保存基线
// 替换计划: 接 device-service POST /api/v1/baselines
function saveBaseline() {
  installStore.setBaseline(offsetValues.value)
  saved.value = true
  setTimeout(() => {
    uni.navigateBack()
  }, 1200)
}

function goBack() {
  uni.navigateBack()
}

onMounted(() => {
  if (offsetValues.value.length === 0) {
    offsetValues.value = mockBaseline().offsetValues
  }
})
</script>

<style scoped>
.page { padding-bottom: 120rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; display: flex; align-items: baseline; gap: 24rpx; }
.back-link { font-size: 28rpx; color: #94a3b8; }
.page-title { font-size: 40rpx; font-weight: 600; color: #1e293b; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.card-title { font-size: 32rpx; font-weight: 600; color: #1e293b; display: block; margin-bottom: 8rpx; }
.card-desc { font-size: 26rpx; color: #94a3b8; display: block; margin-bottom: 24rpx; }
/* Validation */
.validation-status { display: flex; flex-direction: column; gap: 12rpx; margin-bottom: 24rpx; }
.val-item { display: flex; align-items: center; gap: 12rpx; padding: 16rpx 20rpx; border-radius: 12rpx; }
.val-pass { background: #f0fdf4; }
.val-fail { background: #fef2f2; }
.val-icon { font-size: 28rpx; }
.val-pass .val-icon { color: #22c55e; }
.val-fail .val-icon { color: #ef4444; }
.val-text { font-size: 26rpx; color: #1e293b; }
/* Offset grid */
.offset-grid { display: grid; grid-template-columns: repeat(5, 1fr); gap: 8rpx; margin-bottom: 24rpx; }
.offset-cell { background: #f8fafc; border: 1rpx solid #e2e8f0; border-radius: 8rpx; padding: 8rpx 4rpx; text-align: center; }
.cell-warn { border-color: #f59e0b; background: #fffbeb; }
.offset-idx { font-size: 18rpx; color: #94a3b8; display: block; }
.offset-val { font-size: 22rpx; color: #1e293b; font-weight: 500; display: block; }
/* Stats */
.stats-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12rpx; }
.stat-box { background: #f8fafc; border-radius: 12rpx; padding: 16rpx 8rpx; text-align: center; }
.stat-label { font-size: 20rpx; color: #94a3b8; display: block; margin-bottom: 4rpx; }
.stat-value { font-size: 26rpx; color: #1e293b; font-weight: 600; display: block; }
/* Buttons */
.btn-primary { width: 100%; padding: 24rpx 0; background: #2563EB; border-radius: 16rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
.btn-disabled { width: 100%; padding: 24rpx 0; background: #e2e8f0; border-radius: 16rpx; text-align: center; }
.btn-disabled text { color: #94a3b8; font-size: 30rpx; }
/* Toast */
.toast { position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%); background: #1e293b; padding: 32rpx 56rpx; border-radius: 24rpx; display: flex; align-items: center; gap: 16rpx; z-index: 200; box-shadow: 0 16rpx 48rpx rgba(0, 0, 0, 0.2); }
.toast-icon { color: #22c55e; font-size: 36rpx; }
.toast-text { color: #fff; font-size: 28rpx; }
</style>
