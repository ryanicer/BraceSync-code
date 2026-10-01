<!--
  传感器网格（T031 组件迁移后为患者端单份组件，跨端 view/text 写法，H5/mp 通用）。
  原属共享包 @bracesync/ui-components，因全仓库仅患者端使用且微信小程序需 view/text，已迁入本地单份维护。
-->
<template>
  <view class="sensor-grid">
    <view class="grid-row" v-for="r in rows" :key="r">
      <view
        v-for="c in cols"
        :key="c"
        :class="['grid-cell', { 'grid-cell-active': isActive(r, c) }]"
        :style="cellStyle(r, c)"
        hover-class="grid-cell-hover"
        @click="onSelect(r, c)"
      >
        <text class="cell-id">{{ getCell(r, c)?.id }}</text>
        <text class="cell-value">{{ cellText(r, c) }}</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { formatPressureValue } from '../utils/format'
import { unitNumberText, type PressureUnit } from '@bracesync/shared-utils'

export interface GridCell {
  id: string
  value: number
  label: string
  color?: string
  /** T513：后端在同一快照响应里算好的 kPa 派生值；null/缺 = 不可换算（稿面 fail-closed 显示 --） */
  kpa?: number | null
}

const props = withDefaults(defineProps<{
  rows?: number
  cols?: number
  cells: GridCell[]
  activeIndex?: number
  unit?: PressureUnit
}>(), {
  rows: 4,
  cols: 5,
  activeIndex: -1,
  unit: 'N',
})

const emit = defineEmits<{
  select: [index: number]
}>()

function getCellIndex(r: number, c: number): number {
  return (r - 1) * props.cols + (c - 1)
}

function getCell(r: number, c: number): GridCell | undefined {
  return props.cells[getCellIndex(r, c)]
}

function isActive(r: number, c: number): boolean {
  return getCellIndex(r, c) === props.activeIndex
}

function cellStyle(r: number, c: number): Record<string, string> {
  const cell = getCell(r, c)
  return cell?.color ? { backgroundColor: cell.color } : {}
}

// T513：格子里只有数字、不带单位字母（稿面 monitor.html:295 同形）；
// 颜色与 isMax 由上游按 N 判档，本函数只管「这一档怎么写」
function cellText(r: number, c: number): string {
  const cell = getCell(r, c)
  return unitNumberText(props.unit, formatPressureValue(cell?.value), cell?.kpa)
}

function onSelect(r: number, c: number) {
  emit('select', getCellIndex(r, c))
}
</script>

<style scoped>
.sensor-grid {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8rpx;
}
.grid-row {
  display: flex;
  gap: 8rpx;
}
.grid-cell {
  width: 100rpx;
  height: 100rpx;
  border-radius: 16rpx;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  border: 4rpx solid transparent;
  color: #fff;
}
.grid-cell-hover {
  opacity: 0.85;
}
.grid-cell-active {
  border-color: #2563EB;
}
.cell-id {
  font-size: 18rpx;
  font-weight: 600;
  opacity: 0.85;
  line-height: 1;
}
.cell-value {
  font-size: 26rpx;
  font-weight: 700;
  line-height: 1.2;
}
</style>
