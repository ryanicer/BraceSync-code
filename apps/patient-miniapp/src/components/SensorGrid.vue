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
        <text class="cell-value">{{ getCell(r, c)?.value }}</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
export interface GridCell {
  id: string
  value: number
  label: string
  color?: string
}

const props = withDefaults(defineProps<{
  rows?: number
  cols?: number
  cells: GridCell[]
  activeIndex?: number
}>(), {
  rows: 4,
  cols: 5,
  activeIndex: -1,
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
