<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">实时监测</text>
    </view>

    <!-- 异常事件入口（T019B：导航至告警详情页） -->
    <view class="section" style="margin-top: 8rpx;">
      <view class="anomaly-entry" @click="goAnomaly">
        <view class="anomaly-entry-left">
          <text class="anomaly-entry-icon">🔔</text>
          <text class="anomaly-entry-text">异常事件</text>
        </view>
        <text class="anomaly-entry-arrow">›</text>
      </view>
    </view>

    <view class="section" style="margin-top: 16rpx;">
      <view class="segmented">
        <view :class="['seg-btn', { 'seg-active': segment === 'day' }]" @click="switchSegment('day')"><text>日</text></view>
        <view :class="['seg-btn', { 'seg-active': segment === 'week' }]" @click="switchSegment('week')"><text>周</text></view>
        <view :class="['seg-btn', { 'seg-active': segment === 'month' }]" @click="switchSegment('month')"><text>月</text></view>
      </view>
    </view>

    <view class="hero">
      <text class="hero-label">{{ activePoint ? activePoint.pointId + ' · 当前压力值' : '暂无数据' }}</text>
      <view class="hero-value-wrap">
        <text class="hero-number">{{ heroValue }}</text>
        <text class="hero-unit">N</text>
      </view>
      <view class="hero-meta">
        <view class="hero-meta-left">
          <view class="dot dot-blue"></view>
          <text class="meta-text">20-60N 正常范围</text>
        </view>
        <view class="hero-meta-right">
          <text class="battery-icon">🔋</text>
          <text class="meta-bold">85%</text>
          <view class="dot dot-green"></view>
        </view>
      </view>
    </view>

    <view class="section" style="margin-top: 16rpx;">
      <text class="section-title">压力分布热力图</text>
      <view class="card">
        <PressureHeatmap
          v-if="sensorPoints.length > 0"
          :points="sensorPoints"
          :active-index="activeIndex"
          @select="onSelectPoint"
        />
      </view>
    </view>

    <view class="section trend-section">
      <text class="section-title">{{ activePoint ? activePoint.pointId : '' }} · {{ segLabel }}压力趋势</text>
      <view class="card curve-card">
        <PressureCurve :data="trendData" :labels="trendLabels" :max-value="75" :height="180" />
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { onPullDownRefresh } from '@dcloudio/uni-app'
// T031 组件迁移：3 个压力组件已移至患者端本地（跨端 view/text/canvas 写法，H5/mp 通用）。
// 全仓库仅患者端使用，故移出共享包，单份维护，无需 #ifdef 双份导入。
import PressureHeatmap from '../../components/PressureHeatmap.vue'
import PressureCurve from '../../components/PressureCurve.vue'
import type { SensorPoint } from '@bracesync/shared-types'
import { mockSensorPoints, mockTrendData } from '../../mock/monitor'

// MOCK 数据加载
// 替换计划: 接 data-service getPatientRealtime + getPatientHistory
const sensorPoints = ref<SensorPoint[]>([])
const activeIndex = ref(-1)
const segment = ref<'day' | 'week' | 'month'>('day')

const activePoint = computed(() =>
  activeIndex.value >= 0 ? sensorPoints.value[activeIndex.value] : undefined
)
const heroValue = computed(() =>
  activePoint.value ? activePoint.value.pressureValue.toFixed(2) : '--'
)
const segLabel = computed(() => {
  const map = { day: '今日', week: '本周', month: '本月' }
  return map[segment.value]
})

const trendData = ref<{ timestamp: string; value: number }[]>([])
const trendLabels = computed(() => {
  if (segment.value === 'day') return ['0:00', '6:00', '12:00', '18:00', '24:00']
  if (segment.value === 'week') return ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
  return ['1日', '8日', '15日', '22日', '30日']
})

function loadData() {
  const points = mockSensorPoints()
  sensorPoints.value = points
  let maxIdx = 0
  points.forEach((p, i) => {
    if (p.pressureValue > points[maxIdx].pressureValue) maxIdx = i
  })
  activeIndex.value = maxIdx
  trendData.value = mockTrendData(points[maxIdx].pressureValue)
}

function onSelectPoint(index: number) {
  activeIndex.value = index
  const base = sensorPoints.value[index]?.pressureValue || 42
  trendData.value = mockTrendData(base)
}

function switchSegment(seg: 'day' | 'week' | 'month') {
  segment.value = seg
  const base = activePoint.value?.pressureValue || 42
  trendData.value = mockTrendData(base)
}

// T019B: 导航至异常事件页
function goAnomaly() {
  uni.navigateTo({ url: '/pages/anomaly/index' })
}

onMounted(() => {
  loadData()
})

// 下拉刷新（PRD 7A.2）
onPullDownRefresh(() => {
  loadData()
  uni.showToast({ title: '数据已刷新', icon: 'none', duration: 800 })
  uni.stopPullDownRefresh()
})
</script>

<style scoped>
.page { padding-bottom: 180rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.page-title { font-size: 28rpx; font-weight: 500; color: #94a3b8; letter-spacing: 1rpx; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.section-title { font-size: 28rpx; font-weight: 500; color: #1e293b; margin-bottom: 20rpx; display: block; letter-spacing: 0.6rpx; }
.segmented { display: flex; background: #f1f5f9; border-radius: 20rpx; padding: 6rpx; gap: 4rpx; }
.seg-btn { flex: 1; text-align: center; padding: 14rpx 0; font-size: 26rpx; font-weight: 500; color: #64748b; border-radius: 16rpx; transition: all 0.2s; }
.seg-active { background: #fff; color: #2563EB; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.08); }
.hero { text-align: center; padding: 40rpx 48rpx; }
.hero-label { font-size: 24rpx; color: #94a3b8; display: block; margin-bottom: 20rpx; }
.hero-value-wrap { display: flex; align-items: baseline; justify-content: center; gap: 8rpx; }
.hero-number { font-size: 120rpx; font-weight: 300; color: #1e293b; letter-spacing: 4rpx; line-height: 1; }
.hero-unit { font-size: 32rpx; color: #94a3b8; }
.hero-meta { display: flex; align-items: center; justify-content: space-between; margin-top: 32rpx; padding: 0 24rpx; }
.hero-meta-left, .hero-meta-right { display: flex; align-items: center; gap: 12rpx; }
.dot { width: 12rpx; height: 12rpx; border-radius: 50%; flex-shrink: 0; }
.dot-blue { background: #2563EB; }
.dot-green { background: #22c55e; }
.battery-icon { font-size: 24rpx; }
.meta-text { font-size: 24rpx; color: #94a3b8; }
.meta-bold { font-size: 24rpx; color: #1e293b; font-weight: 500; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); padding: 24rpx; margin-bottom: 16rpx; }
.anomaly-entry { display: flex; align-items: center; justify-content: space-between; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 20rpx; padding: 20rpx 28rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.anomaly-entry-left { display: flex; align-items: center; gap: 12rpx; }
.anomaly-entry-icon { font-size: 32rpx; }
.anomaly-entry-text { font-size: 28rpx; color: #1e293b; font-weight: 500; }
.anomaly-entry-arrow { font-size: 36rpx; color: #94a3b8; }
.curve-card { padding: 24rpx 16rpx; }
.trend-section { padding-bottom: 40rpx; }
</style>