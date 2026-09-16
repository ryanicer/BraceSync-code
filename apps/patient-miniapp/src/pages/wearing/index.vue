<!--
  佩戴管理（T224，设计稿 wearing.html 唯一基准）。
  - PT-18 统计：今日佩戴时长环形图 + 目标 18h/天 达标率；本周柱状图 + 日均/最高单日/累计。
  - 数据源：GET /patients/:patientId/daily-wear（T076，患者自查）。DTO 只有 wearMinutes，
    hours/status 均为前端派生（T221 真机教训：不可直接消费不存在的字段）。
  - PT-19 提醒：后端零端点（T098-Q4 §2），本轮仅按设计呈现 UI，开关禁用 +「待开放」，
    不做假保存；存取 + 订阅消息链路另立卡。
-->
<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">佩戴管理</text>
    </view>

    <!-- PT-18: 今日佩戴时长 -->
    <view class="section">
      <text class="section-title">今日佩戴时长</text>
      <view class="ring-wrap">
        <view class="ring-box">
          <canvas type="2d" id="wearRing" class="ring-canvas" style="width: 160px; height: 160px"></canvas>
          <view class="ring-text">
            <text class="ring-val">{{ todayHoursText }}</text>
            <text class="ring-unit">小时</text>
          </view>
        </view>
        <text class="ring-target">目标: 18小时/天 · 达标率 {{ rateText }}%</text>
      </view>
    </view>

    <!-- PT-18: 本周统计 -->
    <view class="section">
      <text class="section-title">本周统计</text>
      <view class="chart-card">
        <canvas type="2d" id="weekChart" class="week-canvas" style="width: 100%; height: 160px"></canvas>
      </view>
      <view class="stats-row">
        <view class="stat">
          <text class="stat-label">日均佩戴</text>
          <view class="stat-value">{{ avgText }}<text class="stat-unit">h</text></view>
        </view>
        <view class="stat">
          <text class="stat-label">最高单日</text>
          <view class="stat-value">{{ maxText }}<text class="stat-unit">h</text></view>
        </view>
        <view class="stat">
          <text class="stat-label">累计佩戴</text>
          <view class="stat-value">{{ totalText }}<text class="stat-unit">h</text></view>
        </view>
      </view>
    </view>

    <!-- PT-19: 佩戴提醒（待开放，静态呈现） -->
    <view class="section remind-section">
      <view class="section-title-row">
        <text class="section-title">佩戴提醒</text>
        <text class="pending-tag">待开放</text>
      </view>
      <view class="remind-list">
        <view class="remind-card">
          <view class="remind-row">
            <view class="remind-info">
              <text class="remind-label">每日佩戴开始提醒</text>
              <text class="remind-desc">提醒开始佩戴支具</text>
            </view>
            <view class="sw sw-on"><view class="sw-knob"></view></view>
          </view>
          <view class="remind-detail">
            <view class="remind-time-row">
              <text>提醒时间</text>
              <text class="time-pill">08:00</text>
            </view>
            <text class="remind-sub">重复: 每天</text>
          </view>
        </view>
        <view class="remind-card">
          <view class="remind-row">
            <view class="remind-info">
              <text class="remind-label">佩戴时长不足提醒</text>
              <text class="remind-desc">当日佩戴不足目标时长时提醒</text>
            </view>
            <view class="sw sw-on"><view class="sw-knob"></view></view>
          </view>
          <view class="remind-detail">
            <view class="remind-time-row">
              <text>提醒时间</text>
              <text class="time-pill">20:00</text>
            </view>
            <text class="remind-sub">最低阈值: <text class="select-pill">16</text> 小时</text>
          </view>
        </view>
        <view class="remind-card">
          <view class="remind-row">
            <view class="remind-info">
              <text class="remind-label">连续佩戴超时提醒</text>
              <text class="remind-desc">连续佩戴超过设定时长提醒休息</text>
            </view>
            <view class="sw"><view class="sw-knob"></view></view>
          </view>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, getCurrentInstance } from 'vue'
import { request } from '../../utils/request'
import { useAuthStore } from '../../stores/auth'

// 目标 18h/天：wearing.html PT-18「目标: 18小时/天」；与 T221 异常监测页的 16h 口径不同源，勿混用
const WEAR_TARGET_H = 18

// data-service DailyWearDayDTO（T076）
interface DailyWearDay {
  date: string
  wearMinutes: number
  avgPressure: number
  maxPressure: number
  maxPoint: string
  frameCount: number
  abnormalCount: number
}

const auth = useAuthStore()

function pad(n: number): string {
  return n < 10 ? '0' + n : '' + n
}

const now = new Date()
const todayKey = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`

const days = ref<DailyWearDay[]>([])

function toHours(d: DailyWearDay): number {
  return Math.round(d.wearMinutes / 6) / 10
}

const todayHours = computed(() => {
  const rec = days.value.find((d) => d.date === todayKey)
  return rec ? toHours(rec) : 0
})

const todayHoursText = computed(() => todayHours.value.toFixed(1))
const rateText = computed(() => String(Math.round((todayHours.value / WEAR_TARGET_H) * 100)))

// 本周（周一起）逐日：无记录/未到 → null
const weekHours = computed<(number | null)[]>(() => {
  const arr: (number | null)[] = [null, null, null, null, null, null, null]
  const byDate = new Map(days.value.map((d) => [d.date, toHours(d)]))
  const dow = now.getDay()
  const todayIdx = dow === 0 ? 6 : dow - 1
  const monday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - todayIdx)
  for (let i = 0; i <= todayIdx; i++) {
    const d = new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + i)
    const key = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
    arr[i] = byDate.has(key) ? byDate.get(key)! : null
  }
  return arr
})

const withData = computed(() => weekHours.value.filter((v): v is number => v != null))

const avgText = computed(() =>
  withData.value.length === 0
    ? '0.0'
    : (withData.value.reduce((a, b) => a + b, 0) / withData.value.length).toFixed(1)
)
const maxText = computed(() =>
  withData.value.length === 0 ? '0.0' : Math.max(...withData.value).toFixed(1)
)
const totalText = computed(() =>
  withData.value.length === 0 ? '0' : String(Math.round(withData.value.reduce((a, b) => a + b, 0)))
)

// 加载：本周范围（周一 → 今日，闭区间；后端 Asia/Shanghai 切日）
async function loadDailyWear() {
  const patientId = auth.patientId
  if (!patientId) {
    days.value = []
    return
  }
  try {
    const dow = now.getDay()
    const monday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - (dow === 0 ? 6 : dow - 1))
    const list = await request<DailyWearDay[]>({
      url: `/api/v1/patients/${patientId}/daily-wear`,
      method: 'GET',
      data: {
        start: `${monday.getFullYear()}-${pad(monday.getMonth() + 1)}-${pad(monday.getDate())}`,
        end: todayKey,
      },
    })
    days.value = Array.isArray(list) ? list : []
  } catch {
    days.value = []
  }
}

// —— canvas 绘制（模式同 PressureCurve.vue：MP-WEIXIN type="2d" node + H5 原生元素） ——
const instance = getCurrentInstance()

function getPixelRatio(): number {
  try {
    return uni.getSystemInfoSync().pixelRatio || 2
  } catch {
    return 2
  }
}

function renderRing(ctx: any, w: number, h: number) {
  ctx.clearRect(0, 0, w, h)
  const stroke = 12
  const cx = w / 2
  const cy = h / 2
  const r = w / 2 - stroke / 2 - 6
  ctx.beginPath()
  ctx.arc(cx, cy, r, 0, Math.PI * 2)
  ctx.strokeStyle = '#e2e8f0'
  ctx.lineWidth = stroke
  ctx.stroke()
  const frac = Math.max(0, Math.min(1, todayHours.value / WEAR_TARGET_H))
  if (frac > 0) {
    ctx.beginPath()
    ctx.arc(cx, cy, r, -Math.PI / 2, -Math.PI / 2 + frac * Math.PI * 2)
    ctx.strokeStyle = '#2563EB'
    ctx.lineCap = 'round'
    ctx.lineWidth = stroke
    ctx.stroke()
  }
}

// 柱状图口径同设计稿 weekChart：0–24h 网格，≥18h 蓝 / ≥12h 琥珀 / 其余红
function renderWeekChart(ctx: any, w: number, h: number) {
  ctx.clearRect(0, 0, w, h)
  const pad = { top: 12, right: 16, bottom: 28, left: 36 }
  const cw = w - pad.left - pad.right
  const ch = h - pad.top - pad.bottom
  const maxVal = 24
  ctx.strokeStyle = '#e2e8f0'
  ctx.lineWidth = 0.5
  for (const val of [0, 6, 12, 18, 24]) {
    const y = pad.top + ch - (val / maxVal) * ch
    ctx.beginPath()
    ctx.moveTo(pad.left, y)
    ctx.lineTo(w - pad.right, y)
    ctx.stroke()
    ctx.fillStyle = '#94a3b8'
    ctx.font = '10px sans-serif'
    ctx.textAlign = 'right'
    ctx.fillText(val + 'h', pad.left - 8, y + 4)
  }
  const labels = ['一', '二', '三', '四', '五', '六', '日']
  const barW = Math.min((cw / 7) * 0.6, 24)
  weekHours.value.forEach((v, i) => {
    const x = pad.left + (cw / 7) * i + (cw / 7 - barW) / 2
    ctx.fillStyle = '#94a3b8'
    ctx.textAlign = 'center'
    ctx.fillText(labels[i], x + barW / 2, h - 8)
    if (v == null) return
    const barH = (v / maxVal) * ch
    const y = pad.top + ch - barH
    ctx.fillStyle = v >= 18 ? '#2563EB' : v >= 12 ? '#f59e0b' : '#ef4444'
    ctx.fillRect(x, y, barW, barH)
    ctx.fillStyle = '#1e293b'
    ctx.font = '10px sans-serif'
    ctx.fillText(v + 'h', x + barW / 2, y - 4)
  })
}

function drawCanvas(id: string, render: (ctx: any, w: number, h: number) => void, retry = 3) {
  // #ifdef MP-WEIXIN
  const query = uni.createSelectorQuery().in(instance && instance.proxy) as any
  query
    .select('#' + id)
    .fields({ node: true, size: true })
    .exec((res: any) => {
      const info = res && res[0]
      if (!info || !info.node) {
        if (retry > 0) setTimeout(() => drawCanvas(id, render, retry - 1), 50)
        return
      }
      const canvas = info.node
      const width = info.width
      const height = info.height
      if (!width || !height) return
      const dpr = getPixelRatio()
      canvas.width = width * dpr
      canvas.height = height * dpr
      const ctx = canvas.getContext('2d')
      ctx.scale(dpr, dpr)
      render(ctx, width, height)
    })
  // #endif
  // #ifndef MP-WEIXIN
  // H5：uni-canvas 包装层内是原生 canvas 元素
  const el = document.getElementById(id)
  if (!el) {
    if (retry > 0) setTimeout(() => drawCanvas(id, render, retry - 1), 50)
    return
  }
  const canvas: any = (el.querySelector && el.querySelector('canvas')) || el
  const width = canvas.clientWidth || canvas.offsetWidth
  const height = canvas.clientHeight || canvas.offsetHeight
  if (!width || !height) return
  const dpr = getPixelRatio()
  canvas.width = width * dpr
  canvas.height = height * dpr
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.scale(dpr, dpr)
  render(ctx, width, height)
  // #endif
}

function drawAll() {
  drawCanvas('wearRing', renderRing)
  drawCanvas('weekChart', renderWeekChart)
}

onMounted(() => {
  void loadDailyWear().then(drawAll)
  drawAll()
})

watch(days, () => drawAll())
</script>

<style scoped>
.page { min-height: 100%; background: #f8fafc; padding-bottom: 60rpx; }
.page-header { padding: 32rpx 48rpx 16rpx; }
.page-title { font-size: 28rpx; font-weight: 500; color: #94a3b8; letter-spacing: 1rpx; }
.section { padding: 0 40rpx; margin-top: 32rpx; }
.section-title { font-size: 28rpx; font-weight: 500; color: #1e293b; margin-bottom: 20rpx; display: block; letter-spacing: 0.6rpx; }
.section-title-row { display: flex; align-items: center; gap: 16rpx; margin-bottom: 20rpx; }
.section-title-row .section-title { margin-bottom: 0; }
.pending-tag { font-size: 20rpx; color: #94a3b8; background: #f1f5f9; border-radius: 8rpx; padding: 4rpx 12rpx; }

.ring-wrap { display: flex; flex-direction: column; align-items: center; padding: 16rpx 0; }
.ring-box { position: relative; width: 160px; height: 160px; }
.ring-canvas { display: block; }
.ring-text { position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%); display: flex; flex-direction: column; align-items: center; }
.ring-val { font-size: 80rpx; font-weight: 300; color: #1e293b; line-height: 1; }
.ring-unit { font-size: 28rpx; color: #94a3b8; margin-top: 4rpx; }
.ring-target { font-size: 26rpx; color: #2563EB; margin-top: 16rpx; }

.chart-card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); padding: 32rpx; }
.week-canvas { display: block; width: 100%; height: 160px; }

.stats-row { display: flex; gap: 16rpx; margin-top: 16rpx; }
.stat { flex: 1; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 24rpx; text-align: center; }
.stat-label { font-size: 22rpx; color: #94a3b8; margin-bottom: 8rpx; display: block; }
.stat-value { font-size: 40rpx; font-weight: 500; color: #1e293b; }
.stat-unit { font-size: 24rpx; color: #94a3b8; font-weight: 400; }

.remind-list { display: flex; flex-direction: column; gap: 16rpx; }
.remind-card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); padding: 32rpx; }
.remind-row { display: flex; align-items: center; justify-content: space-between; }
.remind-info { display: flex; flex-direction: column; }
.remind-label { font-size: 28rpx; font-weight: 500; color: #1e293b; }
.remind-desc { font-size: 22rpx; color: #94a3b8; margin-top: 4rpx; }
.remind-detail { margin-top: 24rpx; padding-top: 24rpx; border-top: 1rpx solid #e2e8f0; }
.remind-time-row { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16rpx; font-size: 26rpx; color: #1e293b; }
.time-pill { padding: 8rpx 16rpx; border: 1rpx solid #e2e8f0; border-radius: 24rpx; font-size: 26rpx; color: #1e293b; background: #f1f5f9; }
.remind-sub { font-size: 24rpx; color: #94a3b8; }
.select-pill { padding: 6rpx 12rpx; border: 1rpx solid #e2e8f0; border-radius: 8rpx; font-size: 24rpx; color: #1e293b; background: #fff; margin: 0 4rpx; }

.sw { width: 88rpx; height: 48rpx; border-radius: 48rpx; background: #cbd5e1; position: relative; flex-shrink: 0; }
.sw-on { background: #2563EB; }
.sw-knob { position: absolute; width: 36rpx; height: 36rpx; left: 6rpx; top: 6rpx; background: #fff; border-radius: 50%; }
.sw-on .sw-knob { transform: translateX(40rpx); }
</style>
