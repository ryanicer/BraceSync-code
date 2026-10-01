<!--
  压力分布热力图（T031 组件迁移后为患者端单份组件，跨端 view/text 写法，H5/mp 通用）。
  原属共享包 @bracesync/ui-components，因全仓库仅患者端使用且微信小程序需 view/text，已迁入本地单份维护。
-->
<template>
  <view class="pressure-heatmap">
    <view class="heatmap-label"><text>压力片 {{ rows }}×{{ cols }} 网格 (40mm × 50mm)</text></view>
    <SensorGrid
      :rows="rows"
      :cols="cols"
      :cells="coloredCells"
      :active-index="resolvedActiveIndex"
      :unit="unit"
      @select="onSelect"
    />
    <view v-if="showLegend" class="heatmap-legend">
      <view class="legend-item"><view class="legend-swatch" style="background:#60a5fa;"></view><text>低压</text></view>
      <view class="legend-item"><view class="legend-swatch" style="background:#4ade80;"></view><text>正常</text></view>
      <view class="legend-item"><view class="legend-swatch" style="background:#facc15;"></view><text>偏高</text></view>
      <view class="legend-item"><view class="legend-swatch" style="background:#ef4444;"></view><text>高压</text></view>
    </view>
    <view v-if="showDetail" class="heatmap-detail">
      <text>{{ detailLine }}</text>
    </view>
    <view v-if="showAreaWarn" class="heatmap-area-warn">
      <text>{{ AREA_MISSING_HINT }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import SensorGrid from './SensorGrid.vue'
import type { SensorPoint } from '@bracesync/shared-types'
import { HEATMAP_TIERS } from '@bracesync/constants'
import { AREA_MISSING_HINT, areaHintVisible, type PressureUnit } from '@bracesync/shared-utils'
import { heatmapDetailLine } from '../utils/monitor-copy'

const props = withDefaults(defineProps<{
  points: SensorPoint[]
  thresholds?: { lowMax: number; normalMax: number; elevatedMax: number }
  activeIndex?: number
  /** T444 M-1：本页是否由患者点选过点位（未点选则显示稿面默认句，不显示预选点位的数值行） */
  selectedByUser?: boolean
  showLegend?: boolean
  showDetail?: boolean
  /** T513 双单位：显示档。判档（颜色 / isMax / 图例）恒按 N，本 prop 只改数值文本 */
  unit?: PressureUnit
  /** T513：点位号 → 快照下发的 kPa 派生值（`pressureHeatmap[].pressureKpa`）。缺项 = 不可换算 */
  kpaByPoint?: Record<string, number | null>
  /** T513：阈值上限的 kPa 派生值（快照 `heatmapMaxKpa`），null = 面积未配置 / 非法 */
  heatmapMaxKpa?: number | null
}>(), {
  thresholds: () => ({ lowMax: HEATMAP_TIERS.LOW_MAX, normalMax: HEATMAP_TIERS.NORMAL_MAX, elevatedMax: HEATMAP_TIERS.ELEVATED_MAX }),
  activeIndex: -1,
  selectedByUser: false,
  showLegend: true,
  showDetail: true,
  unit: 'N',
  kpaByPoint: () => ({}),
  heatmapMaxKpa: null,
})

const emit = defineEmits<{
  select: [index: number]
}>()

const rows = computed(() => Math.max(...props.points.map(p => p.row)))
const cols = computed(() => Math.max(...props.points.map(p => p.col)))

function getColor(value: number): string {
  const t = props.thresholds
  if (value < t.lowMax) return '#60a5fa'
  if (value < t.normalMax) return '#4ade80'
  if (value < t.elevatedMax) return '#facc15'
  return '#ef4444'
}

const coloredCells = computed(() =>
  props.points.map(p => ({
    id: p.pointId,
    value: p.pressureValue,
    label: p.label,
    color: getColor(p.pressureValue),
    kpa: props.kpaByPoint[p.pointId] ?? null,
  }))
)

const resolvedActiveIndex = computed(() => {
  if (props.activeIndex >= 0) return props.activeIndex
  let maxIdx = 0
  let maxVal = -Infinity
  props.points.forEach((p, i) => {
    if (p.pressureValue > maxVal) {
      maxVal = p.pressureValue
      maxIdx = i
    }
  })
  return maxIdx
})

const activePoint = computed(() => props.points[resolvedActiveIndex.value])

// T444 M-1：稿面 monitor.html:102 的默认句只在「患者还没点选」时呈现；
// 页面为 hero/趋势联动而预选的最大点位，不该冒充患者主动查看的结果。
const detailLine = computed(() => {
  const segs = {
    unit: props.unit,
    pressureKpa: activePoint.value ? props.kpaByPoint[activePoint.value.pointId] ?? null : null,
    elevatedMaxKpa: props.heatmapMaxKpa,
  }
  if (!props.selectedByUser) return heatmapDetailLine(null, null, props.thresholds.elevatedMax, segs)
  return heatmapDetailLine(activePoint.value?.pointId, activePoint.value?.pressureValue, props.thresholds.elevatedMax, segs)
})

// T513 fail-closed（稿面 monitor.html:165 + 顶部注记 三）：kPa 档且后端换算不出来 ⇒ 页内提示。
// 判据取 heatmapMaxKpa（与逐点同一次换算），不看逐点，否则会出现「提示说有、数字说无」。
const showAreaWarn = computed(() => areaHintVisible(props.unit, props.heatmapMaxKpa))

function onSelect(index: number) {
  emit('select', index)
}
</script>

<style scoped>
.pressure-heatmap {
  text-align: center;
}
.heatmap-label {
  font-size: 22rpx;
  color: #94a3b8;
  margin-bottom: 16rpx;
}
.heatmap-legend {
  display: flex;
  justify-content: center;
  gap: 24rpx;
  margin-top: 16rpx;
  font-size: 20rpx;
  color: #94a3b8;
}
.legend-item {
  display: flex;
  align-items: center;
  gap: 6rpx;
}
.legend-swatch {
  width: 24rpx;
  height: 24rpx;
  border-radius: 6rpx;
  flex-shrink: 0;
}
.heatmap-detail {
  margin-top: 12rpx;
  font-size: 22rpx;
  color: #64748b;
}
/* 稿面 monitor.html:165 的 areaWarn：11px / #b45309 / 上边距 6px，按本页 px×2=rpx 换算 */
.heatmap-area-warn {
  margin-top: 12rpx;
  font-size: 22rpx;
  color: #b45309;
}
</style>
