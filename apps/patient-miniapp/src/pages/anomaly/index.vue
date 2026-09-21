<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">异常监测</text>
    </view>

    <!-- Segmented -->
    <view class="section seg-section">
      <view class="segmented">
        <view :class="['seg-btn', { 'seg-active': activeTab === 'wearing' }]" @click="activeTab = 'wearing'"><text>佩戴异常</text></view>
        <view :class="['seg-btn', { 'seg-active': activeTab === 'pressure' }]" @click="activeTab = 'pressure'"><text>压力异常</text></view>
      </view>
    </view>

    <!-- Calendar -->
    <view class="section cal-section">
      <view class="cal-header">
        <view class="cal-nav" @click="prevMonth"><text>{{ NAV_PREV }}</text></view>
        <text class="cal-title">{{ monthLabel }}</text>
        <view class="cal-nav" @click="nextMonth"><text>{{ NAV_NEXT }}</text></view>
      </view>
      <view class="cal-weekdays">
        <text v-for="w in WEEKDAYS" :key="w">{{ w }}</text>
      </view>
      <view class="cal-grid">
        <view
          v-for="(cell, i) in calCells"
          :key="i"
          :class="['cal-cell', { 'cal-empty': !cell.day, 'cal-sel': cell.key === selectedDate, 'cal-today': cell.key === todayKey }]"
          @click="cell.day && pickDate(cell.key)"
        >
          <text v-if="cell.day" class="cal-num">{{ cell.day }}</text>
          <view v-if="cell.day && cell.level !== 'ok'" :class="['cal-dot', cell.level]"></view>
        </view>
      </view>
    </view>

    <!-- Legend -->
    <view class="section legend-section">
      <view class="cal-legend">
        <view class="cal-legend-item"><view class="cal-legend-dot red"></view><text>严重异常</text></view>
        <view class="cal-legend-item"><view class="cal-legend-dot orange"></view><text>警告</text></view>
        <view class="cal-legend-item"><view class="cal-legend-dot empty"></view><text>无异常</text></view>
      </view>
    </view>

    <!-- Detail Panel -->
    <view class="section detail-section">
      <template v-if="activeTab === 'wearing'">
        <view v-if="wearingDetail" class="detail-card">
          <view class="detail-date-header"><text>{{ selectedDate }} · {{ wearingDetail.statusText }}</text></view>
          <view class="detail-wearing-hero">
            <text class="dwh-value" :style="{ color: detailColor }">{{ wearingDetail.hours }}</text>
            <text class="dwh-unit">h</text>
          </view>
          <view class="detail-bar-wrap">
            <view class="detail-bar" :style="{ width: barWidth + '%', background: detailColor }"></view>
          </view>
          <view class="detail-bar-labels"><text>0h</text><text>目标 16h</text><text>18h</text></view>
          <view :class="['detail-hint', { 'detail-hint-warn': wearingDetail.status !== 'ok' }]"><text>{{ wearingDetail.hintText }}</text></view>
        </view>
        <view v-else class="detail-empty"><text>该日期无佩戴记录</text></view>
      </template>
      <template v-else>
        <view class="detail-card">
          <view class="detail-date-header">
            <text>{{ selectedDate }}</text>
            <text v-if="pressureDetail.length > 0" :style="{ color: pressureHeaderColor }"> · {{ pressureDetail.length }}条异常</text>
          </view>
          <view v-if="pressureDetail.length > 0">
            <view v-for="(item, ii) in pressureDetail" :key="ii" :class="['ap-item', 'ap-item-' + item.level]">
              <view class="ap-item-head">
                <view v-if="item.point" :class="['ap-item-point', 'ap-point-' + item.level]"><text>{{ item.point }}</text></view>
                <text :class="['ap-item-type', 'ap-type-' + item.level]">{{ item.type }}</text>
                <text v-if="item.threshold" class="ap-item-threshold">阈值{{ item.threshold }}</text>
              </view>
              <text class="ap-item-detail">{{ item.detail }}</text>
              <text class="ap-item-meta">{{ item.meta }}</text>
            </view>
          </view>
          <view v-else class="detail-empty"><text>该日期无压力异常事件</text></view>
        </view>
      </template>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { request } from '../../utils/request'
import { useAuthStore } from '../../stores/auth'
import type { Alert, PaginatedResponse } from '@bracesync/shared-types'
import { alertsToPressureMap, type PressureAnomalyItem } from '../../utils/anomaly'

// 佩戴记录：后端 data-service DailyWearDayDTO（GET /patients/:patientId/daily-wear，T076）
// 真机教训：DTO 只有 wearMinutes，hours/status 必须前端派生，不可直接消费
export interface DailyWearDay {
  date: string
  wearMinutes: number
  avgPressure: number
  maxPressure: number
  maxPoint: string
  frameCount: number
  abnormalCount: number
}

export interface WearingRecord {
  date: string
  hours: number
  status: 'ok' | 'warn' | 'error'
}

// 派生口径：目标 16h（设计稿 detail-bar-labels「目标 16h」）
// ok ≥16h；warn ≥4h（目标的 25%）；error <4h（严重不足）
const WEAR_TARGET_H = 16

function toWearingRecord(d: DailyWearDay): WearingRecord {
  const hours = Math.round(d.wearMinutes / 6) / 10
  const status: WearingRecord['status'] = hours >= WEAR_TARGET_H ? 'ok' : hours >= 4 ? 'warn' : 'error'
  return { date: d.date, hours, status }
}

const authStore = useAuthStore()

const WEEKDAYS = ['日', '一', '二', '三', '四', '五', '六']
const MONTH_NAMES = ['1月', '2月', '3月', '4月', '5月', '6月', '7月', '8月', '9月', '10月', '11月', '12月']
// 月导航箭头用 mustache 绑定：模板里写 &lt;/&gt; 实体在微信小程序端按字面渲染（真机实测）
const NAV_PREV = '<'
const NAV_NEXT = '>'

function pad(n: number): string {
  return n < 10 ? '0' + n : '' + n
}

const now = new Date()
const todayKey = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`

// 数据
const wearingData = ref<WearingRecord[]>([])
const activeTab = ref<'wearing' | 'pressure'>('wearing')
const wearingError = ref('')
const pressureError = ref('')
const pressureByDate = ref<Map<string, PressureAnomalyItem[]>>(new Map())

// 日历状态（默认当月，选中今天）
const currentYear = ref(now.getFullYear())
const currentMonth = ref(now.getMonth() + 1)
const selectedDate = ref(todayKey)

const monthLabel = computed(() => `${currentYear.value}年${MONTH_NAMES[currentMonth.value - 1]}`)

const wearingMap = computed(() => {
  const m = new Map<string, WearingRecord>()
  for (const item of wearingData.value) m.set(item.date, item)
  return m
})

// 合成日历格子：佩戴 + 压力任一 error → error；任一 warn → warn；否则 ok
function getAnomalyLevel(dateKey: string): 'ok' | 'warn' | 'error' {
  const w = wearingMap.value.get(dateKey)
  const p = pressureByDate.value.get(dateKey)
  if (w?.status === 'error' || p?.some((it) => it.level === 'error')) return 'error'
  if (w?.status === 'warn' || (p && p.length > 0)) return 'warn'
  return 'ok'
}

interface CalCell {
  key: string | null
  day: number | null
  level: 'ok' | 'warn' | 'error'
}

const calCells = computed<CalCell[]>(() => {
  const y = currentYear.value
  const m = currentMonth.value
  const daysInMonth = new Date(y, m, 0).getDate()
  const firstDow = new Date(y, m - 1, 1).getDay()
  const cells: CalCell[] = []
  for (let i = 0; i < firstDow; i++) cells.push({ key: null, day: null, level: 'ok' })
  for (let d = 1; d <= daysInMonth; d++) {
    const key = `${y}-${pad(m)}-${pad(d)}`
    cells.push({ key, day: d, level: getAnomalyLevel(key) })
  }
  while (cells.length % 7 !== 0) cells.push({ key: null, day: null, level: 'ok' })
  return cells
})

function prevMonth() {
  if (currentMonth.value === 1) {
    currentMonth.value = 12
    currentYear.value--
  } else {
    currentMonth.value--
  }
}

function nextMonth() {
  if (currentMonth.value === 12) {
    currentMonth.value = 1
    currentYear.value++
  } else {
    currentMonth.value++
  }
}

function pickDate(dateKey: string) {
  selectedDate.value = dateKey
}

// —— 佩戴详情卡 ——
const wearingDetail = computed(() => {
  const d = wearingMap.value.get(selectedDate.value)
  if (!d) return null
  return {
    hours: d.hours,
    status: d.status,
    statusText: d.status === 'error' ? '严重不足' : d.status === 'warn' ? '佩戴不足' : '佩戴达标',
    hintText:
      d.status === 'ok'
        ? '当日佩戴时长达到医生建议的 16h 目标'
        : '当日佩戴时长低于医生建议的 16h 目标，请关注佩戴习惯',
  }
})

const detailColor = computed(() => {
  const s = wearingDetail.value?.status
  if (s === 'error') return '#ef4444'
  if (s === 'warn') return '#f59e0b'
  return '#2563EB'
})

const barWidth = computed(() => Math.round((wearingDetail.value?.hours ?? 0) / 18 * 100))

// —— 压力详情卡 ——
const pressureDetail = computed(() => pressureByDate.value.get(selectedDate.value) ?? [])

const pressureHeaderColor = computed(() =>
  pressureDetail.value.some((it) => it.level === 'error') ? '#dc2626' : '#d97706'
)

// 加载真实告警：GET /api/v1/alerts?patientId=xxx（文本化规则见 utils/anomaly.ts）
async function loadPressure() {
  pressureError.value = ''
  try {
    const patientId = authStore.patientId
    if (!patientId) {
      pressureError.value = '请先登录'
      pressureByDate.value = new Map()
      return
    }
    const res = await request<PaginatedResponse<Alert>>({
      url: '/api/v1/alerts',
      method: 'GET',
      data: { patientId, page: 1, pageSize: 100 },
    })
    pressureByDate.value = alertsToPressureMap(res?.list ?? [])
  } catch (e: unknown) {
    pressureError.value = e instanceof Error ? e.message : '加载失败'
    pressureByDate.value = new Map()
  }
}

// 加载佩戴数据：调 data-service 患者日佩戴聚合 GET /patients/:patientId/daily-wear（T076）
// 无数据/失败时不抛错不 toast，直接空态占位（符合「不自造假数据」要求，E2E 由 route mock 兜住）
async function loadWearing() {
  wearingError.value = ''
  const patientId = authStore.patientId
  if (!patientId) {
    wearingError.value = '请先登录'
    wearingData.value = []
    return
  }
  try {
    const today = new Date()
    const endY = today.getFullYear()
    const endM = String(today.getMonth() + 1).padStart(2, '0')
    const endD = String(today.getDate()).padStart(2, '0')
    // 默认拉 21 天范围
    const start = new Date(Date.now() - 20 * 86400_000)
    const sY = start.getFullYear()
    const sM = String(start.getMonth() + 1).padStart(2, '0')
    const sD = String(start.getDate()).padStart(2, '0')
    const list = await request<DailyWearDay[]>({
      url: `/api/v1/patients/${patientId}/daily-wear`,
      method: 'GET',
      data: { start: `${sY}-${sM}-${sD}`, end: `${endY}-${endM}-${endD}` },
    })
    wearingData.value = Array.isArray(list) ? list.map(toWearingRecord) : []
  } catch {
    wearingData.value = []
  }
}

onMounted(() => {
  void loadPressure()
  void loadWearing()
})
</script>

<style scoped>
.page { padding-bottom: 200rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.page-title { font-size: 28rpx; font-weight: 500; color: #94a3b8; letter-spacing: 1rpx; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.seg-section { margin-top: 16rpx; }
.segmented { display: flex; background: #f1f5f9; border-radius: 20rpx; padding: 6rpx; gap: 4rpx; }
.seg-btn { flex: 1; text-align: center; padding: 14rpx 0; font-size: 26rpx; font-weight: 500; color: #64748b; border-radius: 16rpx; transition: all 0.2s; }
.seg-active { background: #fff; color: #2563EB; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.08); }

/* Calendar */
.cal-section { margin-top: 40rpx; }
.cal-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 24rpx; padding: 0 8rpx; }
.cal-title { font-size: 34rpx; font-weight: 600; color: #1e293b; }
.cal-nav { width: 64rpx; height: 64rpx; border-radius: 50%; background: #f1f5f9; color: #475569; font-size: 32rpx; display: flex; align-items: center; justify-content: center; }
.cal-nav:active { background: #e2e8f0; }
.cal-weekdays { display: grid; grid-template-columns: repeat(7, 1fr); text-align: center; font-size: 22rpx; font-weight: 500; color: #94a3b8; margin-bottom: 8rpx; padding: 0 4rpx; }
.cal-grid { display: grid; grid-template-columns: repeat(7, 1fr); padding: 4rpx; }
.cal-cell { height: 94rpx; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 4rpx; border-radius: 20rpx; position: relative; }
.cal-cell:not(.cal-empty):active { background: #f1f5f9; }
.cal-empty { pointer-events: none; }
.cal-num { font-size: 28rpx; font-weight: 500; color: #475569; line-height: 1; }
.cal-sel { background: #eff6ff; }
.cal-sel .cal-num { color: #2563EB; font-weight: 600; }
.cal-today .cal-num { font-weight: 700; }
.cal-today:not(.cal-sel) .cal-num { color: #1e293b; }
.cal-dot { width: 10rpx; height: 10rpx; border-radius: 50%; }
.cal-dot.error { background: #ef4444; }
.cal-dot.warn { background: #f59e0b; }

/* Legend */
.legend-section { margin-top: 24rpx; }
.cal-legend { display: flex; gap: 32rpx; justify-content: center; font-size: 22rpx; color: #94a3b8; }
.cal-legend-item { display: flex; align-items: center; gap: 8rpx; }
.cal-legend-dot { width: 16rpx; height: 16rpx; border-radius: 50%; display: inline-block; }
.cal-legend-dot.red { background: #ef4444; }
.cal-legend-dot.orange { background: #f59e0b; }
.cal-legend-dot.empty { background: #e2e8f0; }

/* Detail Panel */
.detail-section { margin-top: 24rpx; padding-bottom: 40rpx; }
.detail-card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.detail-date-header { font-size: 28rpx; font-weight: 500; color: #1e293b; margin-bottom: 32rpx; }
.detail-empty { font-size: 26rpx; color: #94a3b8; text-align: center; padding: 48rpx 0; }

/* Wearing */
.detail-wearing-hero { display: flex; align-items: baseline; justify-content: center; gap: 8rpx; margin-bottom: 24rpx; }
.dwh-value { font-size: 96rpx; font-weight: 300; letter-spacing: 4rpx; line-height: 1; }
.dwh-unit { font-size: 32rpx; color: #94a3b8; }
.detail-bar-wrap { height: 20rpx; background: #f1f5f9; border-radius: 10rpx; overflow: hidden; margin-bottom: 8rpx; }
.detail-bar { height: 100%; border-radius: 10rpx; transition: width 0.3s; }
.detail-bar-labels { display: flex; justify-content: space-between; font-size: 20rpx; color: #cbd5e1; margin-bottom: 24rpx; }
.detail-hint { font-size: 24rpx; color: #64748b; text-align: center; line-height: 1.5; padding: 16rpx 24rpx; background: #f8fafc; border-radius: 16rpx; }
.detail-hint-warn { background: #fff7ed; color: #c2410c; }

/* Pressure */
.ap-item { padding: 20rpx 0; border-bottom: 1rpx solid #f1f5f9; display: flex; flex-direction: column; gap: 10rpx; }
.ap-item:first-child { padding-top: 0; }
.ap-item:last-child { border-bottom: none; padding-bottom: 0; }
.ap-item-head { display: flex; align-items: center; gap: 16rpx; }
.ap-item-point { font-size: 22rpx; font-weight: 600; padding: 2rpx 12rpx; border-radius: 6rpx; }
.ap-item-point text { color: #fff; }
.ap-point-error { background: #ef4444; }
.ap-point-warn { background: #f59e0b; }
.ap-item-type { font-size: 24rpx; font-weight: 500; }
.ap-type-error { color: #dc2626; }
.ap-type-warn { color: #d97706; }
.ap-item-threshold { font-size: 22rpx; color: #94a3b8; }
.ap-item-detail { font-size: 26rpx; color: #475569; line-height: 1.4; }
.ap-item-meta { font-size: 22rpx; color: #94a3b8; }
</style>
