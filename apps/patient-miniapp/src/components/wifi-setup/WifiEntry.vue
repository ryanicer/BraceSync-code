<template>
  <view class="entry">
    <!-- 入口卡片（由设备页带入的当前设备） -->
    <view class="entry-card">
      <text class="entry-label">{{ ENTRY.currentDeviceLabel }}</text>
      <text class="entry-name">{{ ENTRY.deviceName }}</text>
      <text class="entry-meta">{{ ENTRY.deviceNoLabel }} {{ deviceName }} · {{ networkText }}</text>
      <view class="entry-btn" @click="$emit('start')"><text>{{ ENTRY.startBtn }}</text></view>
    </view>

    <!-- 前置检查清单 -->
    <view class="section">
      <text class="prep-title">{{ ENTRY.prepTitle }}</text>

      <view class="prep-item">
        <view class="prep-ic blue"><text class="glyph">BT</text></view>
        <view class="prep-txt">
          <text class="prep-name">{{ ENTRY.prepItems[0].name }}</text>
          <text class="prep-sub">{{ ENTRY.prepItems[0].sub }}</text>
        </view>
        <text :class="['prep-status', bluetoothOn ? 'status-done' : 'status-todo']">
          {{ bluetoothOn ? ENTRY.statusDone : ENTRY.statusTodo }}
        </text>
      </view>

      <view class="prep-item">
        <view class="prep-ic amber"><text class="glyph">LOC</text></view>
        <view class="prep-txt">
          <text class="prep-name">{{ ENTRY.prepItems[1].name }}</text>
          <text class="prep-sub">{{ ENTRY.prepItems[1].sub }}</text>
        </view>
        <text :class="['prep-status', locationOn ? 'status-done' : 'status-todo']">
          {{ locationOn ? ENTRY.locDone : ENTRY.locTodo }}
        </text>
      </view>

      <view class="prep-item">
        <view class="prep-ic blue"><text class="glyph">OK</text></view>
        <view class="prep-txt">
          <text class="prep-name">{{ ENTRY.prepItems[2].name }}</text>
          <text class="prep-sub">{{ ENTRY.prepItems[2].sub }}</text>
        </view>
        <!-- 上电状态前端不可探测，不给假绿态（见交付偏差表 D-1） -->
        <text class="prep-status status-todo">{{ ENTRY.powerHint }}</text>
      </view>
    </view>

    <view class="hint-box"><text>{{ ENTRY.hint }}</text></view>

    <!-- 蓝牙未开引导 -->
    <view v-if="btModalVisible" class="modal-overlay">
      <view class="modal">
        <view class="modal-ic blue"><text class="glyph glyph-lg">BT</text></view>
        <text class="modal-title">{{ ENTRY.btModal.title }}</text>
        <text class="modal-body">{{ ENTRY.btModal.body }}</text>
        <view class="modal-btn" @click="onEnableBluetooth"><text>{{ ENTRY.btModal.primary }}</text></view>
        <view class="modal-btn secondary" @click="btModalVisible = false">
          <text>{{ ENTRY.btModal.secondary }}</text>
        </view>
      </view>
    </view>

    <!-- 位置权限引导 -->
    <view v-if="locModalVisible" class="modal-overlay">
      <view class="modal">
        <view class="modal-ic amber"><text class="glyph glyph-lg">LOC</text></view>
        <text class="modal-title">{{ ENTRY.locModal.title }}</text>
        <text class="modal-body">{{ ENTRY.locModal.body }}</text>
        <view class="modal-btn" @click="onGrantLocation"><text>{{ ENTRY.locModal.primary }}</text></view>
        <view class="modal-btn secondary" @click="locModalVisible = false">
          <text>{{ ENTRY.locModal.secondary }}</text>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ENTRY } from '../../pages/wifi-setup/copy'

defineProps<{
  deviceName: string
  networkText: string
  bluetoothOn: boolean
  locationOn: boolean
}>()

const emit = defineEmits<{
  (e: 'start'): void
  (e: 'enable-bluetooth'): void
  (e: 'grant-location'): void
}>()

const btModalVisible = ref(false)
const locModalVisible = ref(false)

function onEnableBluetooth() {
  btModalVisible.value = false
  emit('enable-bluetooth')
}

function onGrantLocation() {
  locModalVisible.value = false
  emit('grant-location')
}

/** 前置检查未通过时由页面调用，弹出设计稿对应引导窗 */
function showBlockingModal(which: 'bluetooth' | 'location') {
  if (which === 'bluetooth') btModalVisible.value = true
  else locModalVisible.value = true
}

defineExpose({ showBlockingModal })
</script>

<style scoped>
.entry { background: #f8fafc; }
.entry-card { margin: 48rpx 40rpx; background: #fff; border: 2rpx solid #e2e8f0; border-radius: 28rpx; padding: 40rpx; display: flex; flex-direction: column; }
.entry-label { font-size: 24rpx; color: #94a3b8; letter-spacing: 1rpx; }
.entry-name { font-size: 36rpx; font-weight: 600; color: #1e293b; margin-top: 12rpx; }
.entry-meta { font-size: 24rpx; color: #94a3b8; margin-top: 8rpx; }
.entry-btn { margin-top: 32rpx; padding: 24rpx; background: #2563EB; border-radius: 24rpx; text-align: center; }
.entry-btn text { color: #fff; font-size: 30rpx; font-weight: 500; }
.entry-btn:active { background: #1d4ed8; }

.section { padding: 0 40rpx; margin-top: 40rpx; }
.prep-title { font-size: 28rpx; font-weight: 600; color: #1e293b; margin-bottom: 24rpx; display: block; }
.prep-item { display: flex; align-items: center; background: #fff; border: 2rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx; margin-bottom: 20rpx; }
.prep-ic { width: 72rpx; height: 72rpx; border-radius: 20rpx; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.prep-ic.blue { background: #eff6ff; }
.prep-ic.amber { background: #fef3c7; }
.glyph { font-size: 22rpx; font-weight: 700; color: #2563EB; letter-spacing: 0; }
.prep-ic.amber .glyph { color: #b45309; }
.glyph-lg { font-size: 28rpx; }
.prep-txt { flex: 1; margin-left: 24rpx; }
.prep-name { font-size: 28rpx; font-weight: 500; color: #1e293b; display: block; }
.prep-sub { font-size: 22rpx; color: #94a3b8; margin-top: 4rpx; display: block; }
.prep-status { font-size: 22rpx; font-weight: 500; flex-shrink: 0; }
.status-done { color: #10ac84; }
.status-todo { color: #f59e0b; }

.hint-box { margin: 32rpx 40rpx; background: #eff6ff; border: 2rpx solid #bfdbfe; border-radius: 20rpx; padding: 24rpx; }
.hint-box text { font-size: 24rpx; color: #2563eb; line-height: 1.6; }

.modal-overlay { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.45); z-index: 300; display: flex; align-items: flex-end; }
.modal { background: #fff; border-radius: 32rpx 32rpx 0 0; width: 100%; padding: 40rpx 40rpx 64rpx; }
.modal-ic { width: 112rpx; height: 112rpx; border-radius: 32rpx; margin: 16rpx auto 24rpx; display: flex; align-items: center; justify-content: center; }
.modal-ic.blue { background: #eff6ff; }
.modal-ic.amber { background: #fef3c7; }
.modal-ic.amber .glyph { color: #b45309; }
.modal-title { font-size: 34rpx; font-weight: 600; color: #1e293b; text-align: center; display: block; }
.modal-body { font-size: 26rpx; color: #64748b; line-height: 1.7; margin: 16rpx 0 40rpx; text-align: center; display: block; }
.modal-btn { width: 100%; padding: 26rpx; background: #2563EB; border-radius: 24rpx; text-align: center; }
.modal-btn text { color: #fff; font-size: 30rpx; font-weight: 500; }
.modal-btn:active { background: #1d4ed8; }
.modal-btn.secondary { background: #f1f5f9; margin-top: 20rpx; }
.modal-btn.secondary text { color: #475569; }
</style>
