<template>
  <view class="page">
    <view class="success-anim">
      <view class="check-circle">
        <text class="check-icon">✓</text>
      </view>
      <view class="success-title">绑定成功</view>
      <view class="success-sub">已匹配到您孩子的就诊档案</view>
    </view>

    <view class="archive-card">
      <view class="archive-label">匹配到的就诊档案</view>
      <view class="archive-row">
        <view class="archive-key">患者姓名</view>
        <view class="archive-val">{{ name || '—' }}</view>
      </view>
      <view class="archive-row">
        <view class="archive-key">患者 ID</view>
        <view class="archive-val mono">{{ patientId || '—' }}</view>
      </view>
    </view>

    <view class="content">
      <view class="btn-primary" @click="enterHome"><text>进入首页</text></view>
      <view class="foot-tip">如档案信息有误，请联系门诊工作人员</view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { onLoad } from '@dcloudio/uni-app'

const name = ref('')
const patientId = ref('')

onLoad((query) => {
  if (query?.name) name.value = decodeURIComponent(query.name as string)
  if (query?.patientId) patientId.value = decodeURIComponent(query.patientId as string)
})

function enterHome() {
  uni.switchTab({ url: '/pages/monitor/index' })
}
</script>

<style scoped>
.page { min-height: 100%; padding-bottom: 80rpx; }
.success-anim { margin-top: 200rpx; text-align: center; }
.check-circle { width: 176rpx; height: 176rpx; border-radius: 50%; background: #22c55e; margin: 0 auto; display: flex; align-items: center; justify-content: center; box-shadow: 0 16rpx 48rpx rgba(34,197,94,0.35); }
.check-icon { font-size: 88rpx; color: #fff; }
.success-title { font-size: 44rpx; font-weight: 600; color: #1e293b; margin-top: 48rpx; }
.success-sub { font-size: 28rpx; color: #64748b; margin-top: 16rpx; }
.archive-card { margin: 72rpx 64rpx 0; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 28rpx; padding: 40rpx; }
.archive-label { font-size: 24rpx; color: #94a3b8; margin-bottom: 28rpx; letter-spacing: 1rpx; }
.archive-row { display: flex; align-items: center; padding: 20rpx 0; border-bottom: 1rpx solid #f1f5f9; }
.archive-row:last-child { border-bottom: none; }
.archive-key { font-size: 26rpx; color: #64748b; width: 144rpx; flex-shrink: 0; }
.archive-val { font-size: 28rpx; color: #1e293b; font-weight: 500; flex: 1; }
.archive-val.mono { font-family: monospace; color: #64748b; }
.content { padding: 0 64rpx; margin-top: 80rpx; }
.btn-primary { width: 100%; padding: 28rpx 0; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 32rpx; font-weight: 500; }
.foot-tip { text-align: center; margin-top: 32rpx; font-size: 24rpx; color: #94a3b8; }
</style>
