<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">异常监测</text>
    </view>

    <view class="section" style="margin-top: 16rpx;">
      <view class="segmented">
        <view :class="['seg-btn', { 'seg-active': activeTab === 'wearing' }]" @click="activeTab = 'wearing'"><text>佩戴异常</text></view>
        <view :class="['seg-btn', { 'seg-active': activeTab === 'pressure' }]" @click="activeTab = 'pressure'"><text>压力异常</text></view>
      </view>
    </view>

    <view v-if="activeTab === 'wearing'" class="section">
      <view class="wearing-list">
        <template v-for="month in wearingByMonth" :key="month.label">
          <view class="month-header"><text>{{ month.label }}</text></view>
          <view v-for="item in month.items" :key="item.date" class="wearing-row">
            <text class="w-date">{{ getDateShort(item.date) }}</text>
            <view class="w-bar-wrap">
              <view
                class="w-bar"
                :style="{ width: Math.min(Math.round(item.hours / 18 * 100), 100) + '%', background: getBarColor(item.status) }"
              ></view>
            </view>
            <text class="w-hours">{{ item.hours }}h</text>
            <view :class="['w-tag', 'w-tag-' + item.status]"><text>{{ item.label }}</text></view>
          </view>
        </template>
      </view>
    </view>

    <view v-else class="section pressure-section">
      <view class="pressure-list">
        <view v-for="(group, gi) in pressureData" :key="group.date" class="p-group">
          <view class="p-group-header" @click="toggleGroup(gi)">
            <text class="p-group-date">{{ group.date }}</text>
            <view :class="['p-group-badge', 'p-badge-' + getGroupLevel(group)]">
              <text>{{ group.items.length }}条异常</text>
            </view>
            <text :class="['p-group-arrow', { 'p-arrow-open': expandedGroups.includes(gi) }]">▾</text>
          </view>
          <view v-if="expandedGroups.includes(gi)" class="p-group-body">
            <view v-for="(item, ii) in group.items" :key="ii" :class="['p-item', 'p-item-' + item.level]">
              <view class="p-item-head">
                <view :class="['p-item-point', 'p-point-' + item.level]"><text>{{ item.point }}</text></view>
                <text :class="['p-item-type', 'p-type-' + item.level]">{{ item.type }}</text>
                <text class="p-item-threshold">阈值{{ item.threshold }}</text>
              </view>
              <text class="p-item-detail">{{ item.detail }}</text>
              <text class="p-item-meta">{{ item.meta }}</text>
            </view>
          </view>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { mockWearingData, mockPressureAnomalies } from '../../mock/history'
import type { WearingRecord, PressureAnomaly } from '../../mock/history'

// MOCK 数据
// 替换计划: 佩戴数据接 data-service 聚合查询，压力异常接 alert-service getAlerts
const wearingData = ref<WearingRecord[]>(mockWearingData())
const pressureData = ref<PressureAnomaly[]>(mockPressureAnomalies())
const activeTab = ref<'wearing' | 'pressure'>('wearing')
const expandedGroups = ref<number[]>([0, 1, 2])

const wearingByMonth = computed(() => {
  const groups: { label: string; items: WearingRecord[] }[] = []
  for (const item of wearingData.value) {
    const parts = item.date.split('-')
    const label = `${parts[0]}年${parseInt(parts[1])}月`
    const last = groups[groups.length - 1]
    if (last && last.label === label) {
      last.items.push(item)
    } else {
      groups.push({ label, items: [item] })
    }
  }
  return groups
})

function getDateShort(date: string): string {
  const parts = date.split('-')
  return `${parts[1]}-${parts[2]}`
}

function getBarColor(status: string): string {
  if (status === 'error') return '#ef4444'
  if (status === 'warn') return '#f59e0b'
  return '#2563EB'
}

function getGroupLevel(group: PressureAnomaly): string {
  return group.items.some((i) => i.level === 'error') ? 'error' : 'warn'
}

function toggleGroup(idx: number) {
  const i = expandedGroups.value.indexOf(idx)
  if (i >= 0) {
    expandedGroups.value.splice(i, 1)
  } else {
    expandedGroups.value.push(idx)
  }
}
</script>

<style scoped>
.page { padding-bottom: 180rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.page-title { font-size: 28rpx; font-weight: 500; color: #94a3b8; letter-spacing: 1rpx; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.segmented { display: flex; background: #f1f5f9; border-radius: 20rpx; padding: 6rpx; gap: 4rpx; }
.seg-btn { flex: 1; text-align: center; padding: 14rpx 0; font-size: 26rpx; font-weight: 500; color: #64748b; border-radius: 16rpx; transition: all 0.2s; }
.seg-active { background: #fff; color: #2563EB; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.08); }
.wearing-list { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; overflow: hidden; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.month-header { font-size: 22rpx; color: #94a3b8; padding: 20rpx 28rpx 8rpx; background: #fafbfc; font-weight: 500; }
.wearing-row { display: flex; align-items: center; gap: 20rpx; padding: 20rpx 28rpx; border-bottom: 1rpx solid #f1f5f9; }
.w-date { font-size: 26rpx; color: #475569; min-width: 76rpx; font-weight: 500; }
.w-bar-wrap { flex: 1; height: 16rpx; background: #f1f5f9; border-radius: 8rpx; overflow: hidden; }
.w-bar { height: 100%; border-radius: 8rpx; transition: width 0.3s; }
.w-hours { font-size: 24rpx; color: #64748b; min-width: 72rpx; text-align: right; font-weight: 500; }
.w-tag { font-size: 20rpx; padding: 4rpx 14rpx; border-radius: 8rpx; min-width: 88rpx; text-align: center; }
.w-tag text { font-weight: 500; }
.w-tag-warn { background: #fef3c7; }
.w-tag-warn text { color: #d97706; }
.w-tag-error { background: #fee2e2; }
.w-tag-error text { color: #dc2626; }
.w-tag-ok { background: #dbeafe; }
.w-tag-ok text { color: #2563EB; }
.pressure-section { padding-bottom: 40rpx; }
.pressure-list { display: flex; flex-direction: column; gap: 20rpx; }
.p-group { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; overflow: hidden; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.p-group-header { display: flex; align-items: center; gap: 20rpx; padding: 24rpx 28rpx; }
.p-group-date { font-size: 28rpx; font-weight: 500; color: #1e293b; }
.p-group-badge { font-size: 20rpx; padding: 4rpx 16rpx; border-radius: 8rpx; }
.p-group-badge text { font-weight: 500; }
.p-badge-error { background: #fee2e2; }
.p-badge-error text { color: #dc2626; }
.p-badge-warn { background: #fef3c7; }
.p-badge-warn text { color: #d97706; }
.p-group-arrow { font-size: 24rpx; color: #94a3b8; margin-left: auto; transition: transform 0.2s; }
.p-arrow-open { transform: rotate(180deg); }
.p-group-body { border-top: 1rpx solid #f1f5f9; }
.p-item { padding: 20rpx 28rpx; border-bottom: 1rpx solid #f8fafc; display: flex; flex-direction: column; gap: 10rpx; }
.p-item-error { background: #fffbfb; }
.p-item-warn { background: #fffefa; }
.p-item-head { display: flex; align-items: center; gap: 16rpx; }
.p-item-point { font-size: 22rpx; font-weight: 600; padding: 2rpx 12rpx; border-radius: 6rpx; }
.p-item-point text { color: #fff; }
.p-point-error { background: #ef4444; }
.p-point-warn { background: #f59e0b; }
.p-item-type { font-size: 24rpx; font-weight: 500; }
.p-type-error { color: #dc2626; }
.p-type-warn { color: #d97706; }
.p-item-threshold { font-size: 22rpx; color: #94a3b8; }
.p-item-detail { font-size: 26rpx; color: #475569; line-height: 1.4; }
.p-item-meta { font-size: 22rpx; color: #94a3b8; }
</style>