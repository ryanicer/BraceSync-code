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
      @select="onSelect"
    />
    <view v-if="showLegend" class="heatmap-legend">
      <view class="legend-item"><view class="legend-swatch" style="background:#60a5fa;"></view><text>低压</text></view>
      <view class="legend-item"><view class="legend-swatch" style="background:#4ade80;"></view><text>正常</text></view>
      <view class="legend-item"><view class="legend-swatch" style="background:#facc15;"></view><text>偏高</text></view>
      <view class="legend-item"><view class="legend-swatch" style="background:#ef4444;"></view><text>高压</text></view>
    </view>
    <view v-if="showDetail" class="heatmap-detail">
      <text>{{ activePoint?.pointId }} · {{ activePoint?.pressureValue }}N · 阈值上限 {{ thresholds.elevatedMax }}N</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import SensorGrid from './SensorGrid.vue'
import type { SensorPoint } from '@bracesync/shared-types'

const props = withDefaults(defineProps<{
  points: SensorPoint[]
  thresholds?: { lowMax: number; normalMax: number; elevatedMax: number }
  activeIndex?: number
  showLegend?: boolean
  showDetail?: boolean
}>(), {
  thresholds: () => ({ lowMax: 20, normalMax: 40, elevatedMax: 60 }),
  activeIndex: -1,
  showLegend: true,
  showDetail: true,
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
</style>
