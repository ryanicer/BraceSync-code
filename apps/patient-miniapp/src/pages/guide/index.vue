<template>
  <view class="page">
    <view class="brand-header">
      <image class="brand-logo" src="/static/logo.png" mode="aspectFit" />
      <text class="brand-name">矫智通</text>
      <text class="brand-desc">穿戴设备数据记录与管理</text>
    </view>

    <view class="intro">了解产品功能</view>

    <view class="features">
      <view class="feature" v-for="(f, i) in features" :key="i">
        <view class="f-icon">{{ f.icon }}</view>
        <view class="f-body">
          <text class="f-title">{{ f.title }}</text>
          <text class="f-desc">{{ f.desc }}</text>
        </view>
      </view>
    </view>

    <view class="content">
      <view class="btn-login" @click="goLogin">
        <text>登录 / 注册</text>
      </view>
      <text class="hint">本产品面向已建档用户，登录后查看个人数据</text>
    </view>
  </view>
</template>

<script setup lang="ts">
const features = [
  { icon: '📊', title: '实时数据', desc: '查看穿戴设备的实时状态与数据分布' },
  { icon: '🔔', title: '异常提醒', desc: '异常记录与提醒，及时了解使用情况' },
  { icon: '📈', title: '使用报告', desc: '按周期查看佩戴记录与报告' },
  { icon: '📱', title: '设备管理', desc: '配网连接并管理您的穿戴设备' },
]

function goLogin() {
  uni.navigateTo({ url: '/pages/login/index' })
}
</script>

<style scoped>
.page { min-height: 100vh; padding-bottom: 120rpx; background: #fff; }
.brand-header { text-align: center; padding: 120rpx 0 40rpx; }
.brand-logo { width: 128rpx; height: 128rpx; margin: 0 auto; display: block; }
.brand-name { display: block; font-size: 40rpx; font-weight: 600; color: #1e293b; margin-top: 24rpx; letter-spacing: 2rpx; }
.brand-desc { display: block; font-size: 22rpx; color: #94a3b8; margin-top: 8rpx; }
.intro { font-size: 28rpx; font-weight: 600; color: #1e293b; padding: 24rpx 48rpx 8rpx; }
.features { padding: 8rpx 48rpx; }
.feature { display: flex; align-items: center; gap: 24rpx; padding: 28rpx 0; border-bottom: 1rpx solid #f1f5f9; }
.f-icon { font-size: 44rpx; width: 72rpx; text-align: center; }
.f-body { display: flex; flex-direction: column; gap: 6rpx; }
.f-title { font-size: 28rpx; color: #334155; font-weight: 500; }
.f-desc { font-size: 22rpx; color: #94a3b8; }
.content { padding: 56rpx 64rpx 0; }
.btn-login { width: 100%; padding: 28rpx 0; background: #2563EB; border-radius: 24rpx; display: flex; align-items: center; justify-content: center; }
.btn-login text { color: #fff; font-size: 30rpx; font-weight: 500; }
.hint { display: block; text-align: center; font-size: 20rpx; color: #94a3b8; margin-top: 24rpx; }
</style>
