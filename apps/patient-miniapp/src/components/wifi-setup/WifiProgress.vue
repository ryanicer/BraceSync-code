<template>
  <view class="progress">
    <view class="progress-area">
      <view class="progress-ring" :style="ringStyle">
        <view class="ring-inner"><text class="percent">{{ percent }}%</text></view>
      </view>
      <text class="progress-title">{{ title }}</text>
      <text class="progress-sub">{{ sub }}</text>
    </view>

    <view class="device-card">
      <view class="device-ic"><text class="glyph">DEV</text></view>
      <view>
        <text class="device-name">{{ deviceName }}</text>
        <text class="device-tag">{{ PROGRESS.deviceTag }}</text>
      </view>
    </view>

    <view class="step-list">
      <view v-for="(s, i) in PROGRESS.states" :key="i" class="step">
        <view :class="['step-ic', stepClass(i)]">
          <text v-if="stepClass(i) === 'done'" class="step-check">✓</text>
          <view v-else-if="stepClass(i) === 'active'" class="spinner-sm"></view>
          <text v-else class="step-num">{{ i + 1 }}</text>
        </view>
        <text :class="['step-txt', stepClass(i)]">{{ s.step }}</text>
        <text :class="['step-status', stepClass(i)]">{{ statusText(i) }}</text>
      </view>
    </view>

    <view class="cancel-btn" @click="$emit('cancel')"><text>{{ PROGRESS.cancelBtn }}</text></view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { PROGRESS } from '../../utils/wifi-copy'

const props = defineProps<{
  /** 设备推送状态 0/1/2/3；null = 尚未收到推送 */
  code: number | null
  deviceName: string
}>()

defineEmits<{ (e: 'cancel'): void }>()

const current = computed(() => (props.code === null || props.code < 0 ? 0 : Math.min(props.code, 3)))

const title = computed(() => PROGRESS.states[current.value]?.title ?? PROGRESS.idleTitle)
const sub = computed(() =>
  props.code === null ? PROGRESS.idleSub : (PROGRESS.states[current.value]?.sub ?? PROGRESS.idleSub)
)
const percent = computed(() => PROGRESS.states[current.value]?.pct ?? 0)

/** 设计稿环形进度：stroke-dasharray 等价实现为 conic-gradient 填充 */
const ringStyle = computed(() => {
  const deg = Math.round((percent.value / 100) * 360)
  return { background: `conic-gradient(#2563EB ${deg}deg, #e2e8f0 ${deg}deg)` }
})

function stepClass(i: number): string {
  const idx = current.value
  if (i < idx) return 'done'
  if (i === idx && props.code !== null) return 'active'
  if (i === idx && props.code === null) return 'active'
  return 'pending'
}

function statusText(i: number): string {
  const cls = stepClass(i)
  if (cls === 'done') return PROGRESS.stepDoneLabel
  if (cls === 'active') return PROGRESS.stepActiveLabel
  return PROGRESS.stepPendingLabel
}
</script>

<style scoped>
.progress { background: #f8fafc; min-height: 100vh; }
.progress-area { text-align: center; padding: 96rpx 64rpx 48rpx; }
.progress-ring { width: 200rpx; height: 200rpx; margin: 0 auto; border-radius: 50%; position: relative; }
.ring-inner { position: absolute; inset: 32rpx; background: #f8fafc; border-radius: 50%; display: flex; align-items: center; justify-content: center; }
.percent { font-size: 36rpx; font-weight: 600; color: #2563EB; }
.progress-title { font-size: 34rpx; font-weight: 600; color: #1e293b; margin-top: 48rpx; display: block; }
.progress-sub { font-size: 26rpx; color: #94a3b8; margin-top: 12rpx; display: block; }

.device-card { margin: 48rpx 40rpx 0; background: #fff; border: 2rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx; display: flex; align-items: center; }
.device-ic { width: 72rpx; height: 72rpx; border-radius: 20rpx; background: #eff6ff; display: flex; align-items: center; justify-content: center; flex-shrink: 0; margin-right: 24rpx; }
.glyph { font-size: 22rpx; font-weight: 700; color: #2563EB; }
.device-name { font-size: 28rpx; font-weight: 500; color: #1e293b; display: block; }
.device-tag { font-size: 20rpx; color: #10ac84; background: #e8f5e9; padding: 2rpx 12rpx; border-radius: 12rpx; display: inline-block; margin-top: 6rpx; }

.step-list { margin: 48rpx 40rpx 0; background: #fff; border: 2rpx solid #e2e8f0; border-radius: 24rpx; overflow: hidden; }
.step { display: flex; align-items: center; padding: 28rpx 32rpx; border-bottom: 2rpx solid #f1f5f9; }
.step:last-child { border-bottom: none; }
.step-ic { width: 44rpx; height: 44rpx; border-radius: 50%; display: flex; align-items: center; justify-content: center; flex-shrink: 0; margin-right: 24rpx; }
.step-check { font-size: 24rpx; color: #16a34a; }
.step-num { font-size: 24rpx; font-weight: 600; color: #94a3b8; }
.step-ic.done { background: #dcfce7; }
.step-ic.active { background: #2563EB; }
.step-ic.pending { background: #f1f5f9; }
.spinner-sm { width: 24rpx; height: 24rpx; border: 4rpx solid rgba(255, 255, 255, 0.4); border-top-color: #fff; border-radius: 50%; animation: spin 0.7s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.step-txt { flex: 1; font-size: 28rpx; }
.step-txt.done { color: #1e293b; }
.step-txt.active { color: #1e293b; font-weight: 500; }
.step-txt.pending { color: #94a3b8; }
.step-status { font-size: 22rpx; flex-shrink: 0; }
.step-status.done { color: #16a34a; }
.step-status.active { color: #2563EB; }
.step-status.pending { color: #cbd5e1; }

.cancel-btn { margin: 48rpx 40rpx 0; padding: 24rpx; background: #f1f5f9; border-radius: 24rpx; text-align: center; }
.cancel-btn text { color: #475569; font-size: 28rpx; font-weight: 500; }
</style>
