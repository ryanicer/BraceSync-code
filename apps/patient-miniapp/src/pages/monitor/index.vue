<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">实时监测</text>
      <view class="refresh-btn" :class="{ 'loading': loading }" @click="onRefresh">
        <text>{{ loading ? '刷新中…' : '🔄 刷新' }}</text>
      </view>
    </view>

    <!-- 异常事件入口（T019B：导航至告警详情页）— 暂时隐藏 -->

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
          <text class="meta-bold">{{ battery > 0 ? battery + '%' : '—' }}</text>
          <view :class="['dot', battery >= 20 ? 'dot-green' : 'dot-red']"></view>
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

    <view class="section" style="margin-top: 16rpx;">
      <view class="segmented">
        <view :class="['seg-btn', { 'seg-active': segment === 'day' }]" @click="switchSegment('day')"><text>日</text></view>
        <view :class="['seg-btn', { 'seg-active': segment === 'week' }]" @click="switchSegment('week')"><text>周</text></view>
        <view :class="['seg-btn', { 'seg-active': segment === 'month' }]" @click="switchSegment('month')"><text>月</text></view>
      </view>
    </view>

    <view class="section trend-section">
      <text class="section-title">{{ activePoint ? activePoint.pointId : '' }} · {{ segLabel }}压力趋势</text>
      <view class="card curve-card">
        <PressureCurve :data="trendData" :labels="trendLabels" :max-value="trendMaxValue" :time-range="trendTimeRange" :height="180" />
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { onPullDownRefresh } from '@dcloudio/uni-app'
import PressureHeatmap from '../../components/PressureHeatmap.vue'
import PressureCurve from '../../components/PressureCurve.vue'
import type { PressureRecord, SensorPoint } from '@bracesync/shared-types'
import { request } from '../../utils/request'
import { logger } from '../../utils/logger'
import { useAuthStore } from '../../stores/auth'

// 后端 data-service RealtimeSnapshot（返回结构）简化接口描述
interface RealtimeSnapshot {
  deviceId?: string
  status?: string
  todayHours?: number
  maxPressure?: number
  maxPoint?: string
  battery?: number
  pressureRecords?: PressureRecord[]
  events?: number
}

// GET /patients/:patientId/records 返回分页结构（data-service HistoryPage）
// 防御式声明：同时兼容真实后端的 { list, total, page, pageSize } 和旧/裸数组
interface HistoryPage {
  list: PressureRecord[]
  total: number
  page: number
  pageSize: number
}

const authStore = useAuthStore()

const sensorPoints = ref<SensorPoint[]>([])
const activeIndex = ref(-1)
const segment = ref<'day' | 'week' | 'month'>('day')
const loading = ref(false)
const battery = ref(0)

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

// 趋势图 Y 轴 max：有数据时向上取整到 15N 刻度，确保曲线不贴顶
const trendMaxValue = computed(() => {
  const values = trendData.value.map(p => p.value).filter(v => v > 0)
  if (values.length === 0) return 75
  const max = Math.max(...values)
  return Math.max(75, Math.ceil(max / 15) * 15)
})

// 趋势图时间范围（毫秒），用于 PressureCurve 按真实时间定位 X 坐标
const trendTimeRange = computed(() => {
  const now = new Date()
  const y = now.getFullYear()
  const m = now.getMonth()
  const d = now.getDate()
  if (segment.value === 'day') {
    const start = new Date(y, m, d, 0, 0, 0).getTime()
    return { start, end: start + 24 * 60 * 60 * 1000 }
  }
  if (segment.value === 'week') {
    const weekday = now.getDay() === 0 ? 7 : now.getDay()
    const monday = new Date(y, m, d - (weekday - 1), 0, 0, 0)
    return { start: monday.getTime(), end: monday.getTime() + 7 * 24 * 60 * 60 * 1000 }
  }
  const first = new Date(y, m, 1, 0, 0, 0)
  const nextMonth = new Date(y, m + 1, 1, 0, 0, 0)
  return { start: first.getTime(), end: nextMonth.getTime() }
})

// 取某帧中指定点的压力值（找不到则返回最大值兜底）
function getPointValue(r: PressureRecord, pointId?: string): number {
  if (pointId) {
    const p = (r.points || []).find(pt => pt.pointId === pointId)
    if (p) return p.pressureValue
  }
  let maxP = 0
  for (const p of r.points || []) {
    if (p.pressureValue > maxP) maxP = p.pressureValue
  }
  return maxP
}

// 按 period 分桶聚合：day=30 分钟桶，week/month=1 天桶，取每桶平均值
function aggregateByPeriod(records: PressureRecord[], pointId: string | undefined, period: 'day' | 'week' | 'month') {
  if (records.length === 0) return []
  const bucketMs = period === 'day' ? 30 * 60 * 1000 : 24 * 60 * 60 * 1000
  const buckets = new Map<number, { sum: number; count: number }>()
  for (const r of records) {
    const ts = new Date(r.timestamp).getTime()
    if (isNaN(ts)) continue
    const key = Math.floor(ts / bucketMs) * bucketMs
    const val = getPointValue(r, pointId)
    if (val <= 0) continue
    const b = buckets.get(key) || { sum: 0, count: 0 }
    b.sum += val
    b.count++
    buckets.set(key, b)
  }
  return Array.from(buckets.entries())
    .sort((a, b) => a[0] - b[0])
    .map(([ts, b]) => ({ timestamp: new Date(ts).toISOString(), value: parseFloat((b.sum / b.count).toFixed(2)) }))
    .filter(x => x.value > 0)
}

// 分页采样：week/month 取多页覆盖全周期，day 一页足够
async function fetchRecordsByPeriod(patientId: string, period: 'day' | 'week' | 'month', dateStr: string): Promise<PressureRecord[]> {
  const PAGE_SIZE = 100
  if (period === 'day') {
    const raw = await request<HistoryPage | PressureRecord[]>({
      url: `/api/v1/patients/${patientId}/records`,
      method: 'GET',
      data: { period, date: dateStr, pageSize: PAGE_SIZE },
    })
    return Array.isArray(raw) ? raw : (raw?.list ?? [])
  }
  // week/month：最多取 5 页（500 条），均匀采样覆盖时间段
  const all: PressureRecord[] = []
  for (let page = 1; page <= 5; page++) {
    const raw = await request<HistoryPage | PressureRecord[]>({
      url: `/api/v1/patients/${patientId}/records`,
      method: 'GET',
      data: { period, date: dateStr, page, pageSize: PAGE_SIZE },
    })
    const pageRecords = Array.isArray(raw) ? raw : (raw?.list ?? [])
    all.push(...pageRecords)
    if (pageRecords.length < PAGE_SIZE) break
  }
  return all
}

// 根据 period 参数调用 records 端点，按选中点 + 分桶聚合生成趋势
async function loadTrend(baseVal: number) {
  const patientId = authStore.patientId
  const pointId = activePoint.value?.pointId
  if (!patientId) {
    trendData.value = []
    return
  }
  const today = new Date()
  const yyyy = today.getFullYear()
  const mm = String(today.getMonth() + 1).padStart(2, '0')
  const dd = String(today.getDate()).padStart(2, '0')
  const dateStr = `${yyyy}-${mm}-${dd}`
  try {
    const records = await fetchRecordsByPeriod(patientId, segment.value, dateStr)
    logger.info('[T178] loadTrend', { period: segment.value, pointId, recordCount: records.length })
    if (records.length > 0) {
      trendData.value = aggregateByPeriod(records, pointId, segment.value)
      logger.info('[T178] trend 聚合后', { 点数: trendData.value.length, 首条: trendData.value[0], 末条: trendData.value[trendData.value.length - 1] })
      if (trendData.value.length > 0) return
    }
    // 空记录或聚合后空：fallback 给当前单点避免图表空
    trendData.value = [{ timestamp: new Date().toISOString(), value: parseFloat(baseVal.toFixed(2)) }]
    logger.warn('[T178] loadTrend: records 为空或聚合后无点, fallback', { baseVal, pointId })
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e)
    logger.error('[T178] loadTrend catch', { msg, pointId })
    trendData.value = [{ timestamp: new Date().toISOString(), value: parseFloat(baseVal.toFixed(2)) }]
  }
}

// 真实加载：data-service GET /patients/:patientId/realtime
async function loadData() {
  if (loading.value) return
  loading.value = true
  const patientId = authStore.patientId
  if (!patientId) {
    loading.value = false
    uni.showToast({ title: '请先登录', icon: 'none' })
    return
  }
  try {
    const snap = await request<RealtimeSnapshot>({
      url: `/api/v1/patients/${patientId}/realtime`,
      method: 'GET',
    })
    const recs = snap?.pressureRecords ?? []
    battery.value = snap?.battery ?? 0
    const points: SensorPoint[] = recs.length && recs[0].points ? recs[0].points : []
    sensorPoints.value = points
    let maxIdx = -1
    if (points.length > 0) {
      maxIdx = 0
      for (let i = 1; i < points.length; i++) {
        if (points[i].pressureValue > points[maxIdx].pressureValue) maxIdx = i
      }
    }
    activeIndex.value = maxIdx >= 0 ? maxIdx : -1
    const base = maxIdx >= 0 ? points[maxIdx].pressureValue : snap?.maxPressure ?? 0
    void loadTrend(base)
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : '加载实时数据失败'
    uni.showToast({ title: msg, icon: 'none' })
    sensorPoints.value = []
    trendData.value = []
  } finally {
    loading.value = false
  }
}

// 手动刷新按钮：直接调 loadData（loading 守卫防并发）
function onRefresh() {
  void loadData()
}

function onSelectPoint(index: number) {
  activeIndex.value = index
  const base = sensorPoints.value[index]?.pressureValue ?? 0
  void loadTrend(base)
}

function switchSegment(seg: 'day' | 'week' | 'month') {
  if (segment.value === seg) return
  segment.value = seg
  const base = activePoint.value?.pressureValue ?? 0
  void loadTrend(base)
}

// T019B: 导航至异常事件页
function goAnomaly() {
  uni.navigateTo({ url: '/pages/anomaly/index' })
}

onMounted(() => {
  void loadData()
})

// 下拉刷新（PRD 7A.2）
onPullDownRefresh(() => {
  void loadData().finally(() => {
    uni.showToast({ title: '数据已刷新', icon: 'none', duration: 800 })
    uni.stopPullDownRefresh()
  })
})
</script>

<style scoped>
.page { padding-bottom: 180rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; display: flex; align-items: center; justify-content: space-between; }
.page-title { font-size: 28rpx; font-weight: 500; color: #94a3b8; letter-spacing: 1rpx; }
.refresh-btn { font-size: 24rpx; color: #2563EB; background: #eff6ff; padding: 12rpx 24rpx; border-radius: 24rpx; transition: opacity 0.2s; }
.refresh-btn.loading { opacity: 0.6; pointer-events: none; }
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
.dot-red { background: #ef4444; }
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