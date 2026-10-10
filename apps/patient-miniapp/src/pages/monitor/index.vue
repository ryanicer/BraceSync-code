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
        <text class="hero-unit">{{ unit }}</text>
      </view>
      <view class="hero-meta">
        <view v-show="heroRangeHintVisible(unit) && heroRange" class="hero-meta-left">
          <view class="dot dot-blue"></view>
          <text class="meta-text">{{ heroRange }}</text>
        </view>
        <view class="hero-meta-right">
          <text class="battery-icon">🔋</text>
          <text class="meta-bold">{{ battery > 0 ? battery + '%' : '—' }}</text>
          <view :class="['dot', battery >= 20 ? 'dot-green' : 'dot-red']"></view>
        </view>
      </view>
    </view>

    <view class="section" style="margin-top: 16rpx;">
      <view class="section-title-row">
        <text class="section-title">压力分布热力图</text>
        <view v-if="calibratedFlag !== null" :class="['calib-badge', calibratedFlag ? 'calib-on' : 'calib-off']">
          <text>{{ calibratedFlag ? '已校准' : '未校准' }}</text>
        </view>
        <!-- T513 双单位：稿面 monitor.html:154 = 标题行右侧二档分段；无帧时仍可见可点（PRD 四.6） -->
        <view class="segmented unit-seg">
          <view
            v-for="u in PRESSURE_UNITS"
            :key="u"
            :class="['seg-btn', { 'seg-active': unit === u }]"
            @click="switchUnit(u)"
          ><text>{{ u }}</text></view>
        </view>
      </view>
      <view class="card">
        <PressureHeatmap
          v-if="sensorPoints.length > 0"
          :points="sensorPoints"
          :active-index="activeIndex"
          :selected-by-user="userTappedPoint"
          :unit="unit"
          :kpa-by-point="kpaByPoint"
          :heatmap-max-kpa="heatmapMaxKpa"
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
      <text class="section-title">{{ trendTitle }}</text>
      <view class="card curve-card">
        <PressureCurve :data="trendData" :labels="trendLabels" :max-value="trendMaxValue" :time-range="trendTimeRange" :height="180" />
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { logErrorText, userErrorCopy, unitNumberText, heroRangeHintVisible, heroRangeText, PRESSURE_UNITS, type PressureUnit } from '@bracesync/shared-utils'
import { onPullDownRefresh } from '@dcloudio/uni-app'
import PressureHeatmap from '../../components/PressureHeatmap.vue'
import PressureCurve from '../../components/PressureCurve.vue'
import type { PressureRecord, SensorPoint } from '@bracesync/shared-types'
import { TREND_CURVE_MAX_N } from '@bracesync/constants'
import { request } from '../../utils/request'
import { logger } from '../../utils/logger'
import { formatPressureValue } from '../../utils/format'
import { trendSectionTitle } from '../../utils/monitor-copy'
import { cstDateStr, trendInterval, trendWindow, toTrendSeries, type TrendSegment } from '../../utils/trend-window'
import { readStoredUnit, persistUnit } from '../../utils/unit-pref'
import { useAuthStore } from '../../stores/auth'

// 后端 data-service RealtimeSnapshot（返回结构）简化接口描述
interface SnapshotHeatmapPoint {
  pointId: string
  pressureValue: number
  /** T508：与 pressureValue 同一次换算的下发值；null = 面积未配置/非法 */
  pressureKpa?: number | null
}

interface RealtimeSnapshot {
  deviceId?: string
  status?: string
  todayHours?: number
  maxPressure?: number
  maxPoint?: string
  battery?: number
  pressureRecords?: PressureRecord[]
  events?: number
  /** T513 双单位取数面：以下三项均为 T508 在同一快照响应里下发的派生值 */
  pressureHeatmap?: SnapshotHeatmapPoint[]
  /** 设备有效受压面积。本卡只声明字段、不参与任何换算（换算由后端做，前端重复实现＝双口径） */
  contactAreaCm2?: number | null
  heatmapMaxKpa?: number | null
  /** T601：hero「正常范围」的两条配置边界（与告警引擎同源，随 T296 链同响应下发） */
  pressureLowN?: number
  pressureHighN?: number
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
// T173：最新帧是否已应用基线校准（null = 暂无数据，不展示角标）
const calibratedFlag = ref<boolean | null>(null)
const segment = ref<TrendSegment>('day')
const loading = ref(false)
const battery = ref(0)
// T444 M-1：热力图详情行只在患者真正点选后显示数值，否则显示稿面默认句
const userTappedPoint = ref(false)
// T513 双单位：起版档 = 本地记忆档 → 默认 N（裁定 e；稿面的 ?unit= 钩子仅演示，实现不读）
const unit = ref<PressureUnit>(readStoredUnit())
// T513：快照 pressureHeatmap[].pressureKpa 按点位号索引；heatmapMaxKpa 同响应下发
const kpaByPoint = ref<Record<string, number | null>>({})
const heatmapMaxKpa = ref<number | null>(null)
// T601：hero「正常范围」两条配置边界（同快照下发；null = 未到 / 字段缺席 ⇒ 文案行隐藏）
const pressureRangeLow = ref<number | null>(null)
const pressureRangeHigh = ref<number | null>(null)
// 副文案随配置派生（写死字面量会与 sys_configs 漂移，T601 缺陷本体）
const heroRange = computed(() => heroRangeText(pressureRangeLow.value, pressureRangeHigh.value))

const activePoint = computed(() =>
  activeIndex.value >= 0 ? sensorPoints.value[activeIndex.value] : undefined
)
const heroValue = computed(() => {
  const pt = activePoint.value
  if (!pt) return unitNumberText(unit.value, '--', null)
  return unitNumberText(unit.value, formatPressureValue(pt.pressureValue), kpaByPoint.value[pt.pointId] ?? null)
})
const segLabel = computed(() => {
  const map = { day: '今日', week: '本周', month: '本月' }
  return map[segment.value]
})
// T444 M-2：无点位时不渲染前置分隔符（修前实测渲染成「· 今日压力趋势」）
const trendTitle = computed(() => trendSectionTitle(activePoint.value?.pointId, segLabel.value))

const trendData = ref<{ timestamp: string; value: number }[]>([])
const trendLabels = computed(() => {
  if (segment.value === 'day') return ['0:00', '6:00', '12:00', '18:00', '24:00']
  if (segment.value === 'week') return ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
  return ['1日', '8日', '15日', '22日', '30日']
})

// 趋势图 Y 轴 max：有数据时向上取整到 15N 刻度，确保曲线不贴顶（75 = TREND_CURVE_MAX_N 占位）
const trendMaxValue = computed(() => {
  const values = trendData.value.map(p => p.value).filter(v => v > 0)
  if (values.length === 0) return TREND_CURVE_MAX_N
  const max = Math.max(...values)
  return Math.max(TREND_CURVE_MAX_N, Math.ceil(max / 15) * 15)
})

// T620：本页取样时刻。date 参数与 X 轴窗口必须由同一枚「现在」推出来，
// 否则两次 Date.now() 跨分钟/跨日界时，查询窗口与坐标轴窗口会不是同一天。
const nowMs = ref(Date.now())

// 趋势图时间范围（毫秒），用于 PressureCurve 按真实时间定位 X 坐标
// T620：窗口按东八区墙钟切（与后端 periodRange 同口径），不再读设备本地时区
const trendTimeRange = computed(() => trendWindow(segment.value, nowMs.value))

// T620 趋势取数：桶宽与桶界由后端按东八区墙钟计算（records 端点新增 interval 参数），
// 本页不再翻页拼明细、不再自己分桶、不再丢弃 0 值帧。
// 为什么必须换：设备约 31 秒一帧，明细分页的 pageSize 上限是 100（架构 §3.5），
// 「日」档一页只覆盖约 50 分钟 ⇒ 曲线必然只剩零星孤点；周/月档 5 页也只有约 4 小时。
async function fetchTrendBuckets(patientId: string, period: TrendSegment, dateStr: string, interval: string): Promise<PressureRecord[]> {
  const raw = await request<HistoryPage | PressureRecord[]>({
    url: `/api/v1/patients/${patientId}/records`,
    method: 'GET',
    data: { period, date: dateStr, interval },
  })
  return Array.isArray(raw) ? raw : (raw?.list ?? [])
}

// 按当前档 + 选中点位取趋势序列（数据面 = 后端桶，渲染面 = PressureCurve）
async function loadTrend(baseVal: number) {
  const patientId = authStore.patientId
  const pointId = activePoint.value?.pointId
  if (!patientId) {
    trendData.value = []
    return
  }
  const at = Date.now()
  nowMs.value = at
  const period = segment.value
  const dateStr = cstDateStr(at)
  const interval = trendInterval(period)
  try {
    const buckets = await fetchTrendBuckets(patientId, period, dateStr, interval)
    logger.info('[T620] loadTrend', { period, interval, dateStr, pointId, bucketCount: buckets.length })
    if (buckets.length > 0) {
      trendData.value = toTrendSeries(buckets, pointId)
      logger.info('[T620] trend 桶序列', { 点数: trendData.value.length, 首条: trendData.value[0], 末条: trendData.value[trendData.value.length - 1] })
      if (trendData.value.length > 0) return
    }
    // 空窗口：fallback 给当前单点避免图表空（时刻取本页同一枚 nowMs，落进窗口内）
    trendData.value = [{ timestamp: new Date(at).toISOString(), value: parseFloat(baseVal.toFixed(2)) }]
    logger.warn('[T620] loadTrend: 桶为空, fallback 单点', { baseVal, pointId, dateStr, interval })
  } catch (e: unknown) {
    const msg = logErrorText(e)
    logger.error('[T620] loadTrend catch', { msg, pointId })
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
    // T513：kPa 一律取同一响应里后端派生好的值，前端不自算换算（派发单 §三 选型 A）
    const byPoint: Record<string, number | null> = {}
    for (const hp of snap?.pressureHeatmap ?? []) byPoint[hp.pointId] = hp.pressureKpa ?? null
    kpaByPoint.value = byPoint
    heatmapMaxKpa.value = snap?.heatmapMaxKpa ?? null
    // T601：边界取同一响应下发的配置值；字段缺席按 null 处理（文案行隐藏，不猜值不回落旧字面量）
    pressureRangeLow.value = typeof snap?.pressureLowN === 'number' ? snap.pressureLowN : null
    pressureRangeHigh.value = typeof snap?.pressureHighN === 'number' ? snap.pressureHighN : null
    calibratedFlag.value = recs.length ? recs[0].calibrated === true : null
    let maxIdx = -1
    if (points.length > 0) {
      maxIdx = 0
      for (let i = 1; i < points.length; i++) {
        if (points[i].pressureValue > points[maxIdx].pressureValue) maxIdx = i
      }
    }
    activeIndex.value = maxIdx >= 0 ? maxIdx : -1
    // 每次重载（含手动/下拉刷新）都回到「未点选」态：详情行重新显示稿面默认句
    userTappedPoint.value = false
    const base = maxIdx >= 0 ? points[maxIdx].pressureValue : snap?.maxPressure ?? 0
    // T206：realtime snapshot 关键日志，便于 SSH frontend 远程反查数值口径
    logger.info('[T206] realtime snapshot received', {
      recordCount: recs.length,
      calibratedFlag: calibratedFlag.value,
      battery: battery.value,
      pointCount: points.length,
      maxPointId: maxIdx >= 0 ? points[maxIdx].pointId : null,
      maxPressure: base,
      sampleValues: points.slice(0, 5).map(p => ({ id: p.pointId, v: p.pressureValue })),
      // T513：当前档 + 后端同响应下发的换算值，便于 SSH frontend 远程反查「显示的是哪一档」
      unit: unit.value,
      heatmapMaxKpa: heatmapMaxKpa.value,
      sampleKpa: points.slice(0, 5).map(p => ({ id: p.pointId, kpa: byPoint[p.pointId] ?? null })),
    })
    void loadTrend(base)
  } catch (e: unknown) {
    const msg = userErrorCopy(e, { scope: 'patient', fallback: '加载实时数据失败' })
    uni.showToast({ title: msg, icon: 'none' })
    sensorPoints.value = []
    trendData.value = []
    // T513：取不到帧就别留上一帧的换算值（fail-closed，禁止沿用上一帧）
    kpaByPoint.value = {}
    heatmapMaxKpa.value = null
    // T601：取数失败同样清边界（fail-closed，不沿用上一帧配置）
    pressureRangeLow.value = null
    pressureRangeHigh.value = null
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
  userTappedPoint.value = true
  const base = sensorPoints.value[index]?.pressureValue ?? 0
  void loadTrend(base)
}

function switchSegment(seg: 'day' | 'week' | 'month') {
  if (segment.value === seg) return
  segment.value = seg
  const base = activePoint.value?.pressureValue ?? 0
  void loadTrend(base)
}

// T513 双单位（裁定 e）：切档只改显示档并写本地记忆，不重新请求、不写任何服务端字段
function switchUnit(u: PressureUnit) {
  if (unit.value === u) return
  unit.value = u
  persistUnit(u)
}

// T019B: 导航至异常监测页（tabBar 页须用 switchTab，navigateTo 会静默失败）
function goAnomaly() {
  uni.switchTab({ url: '/pages/anomaly/index' })
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
.section-title-row { display: flex; align-items: center; justify-content: space-between; gap: 16rpx; margin-bottom: 20rpx; }
.section-title-row .section-title { margin-bottom: 0; }
/* T513 双单位分段（稿面 monitor.html:154：outer padding 2px / gap 1px / btn 3px 14px / 12px，px×2=rpx）*/
.unit-seg { flex: none; margin-left: auto; padding: 4rpx; gap: 2rpx; }
.unit-seg .seg-btn { flex: none; padding: 6rpx 28rpx; font-size: 24rpx; }
.calib-badge { padding: 4rpx 16rpx; border-radius: 18rpx; font-size: 20rpx; }
.calib-badge.calib-on { background: #dcfce7; color: #15803d; }
.calib-badge.calib-off { background: #fef3c7; color: #b45309; }
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