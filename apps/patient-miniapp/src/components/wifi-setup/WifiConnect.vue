<template>
  <view class="connect">
    <!-- 连接中视图 -->
    <template v-if="phase === 'connecting'">
      <view class="connecting">
        <view class="connect-ic"><view class="spinner"></view></view>
        <text class="connect-text">{{ CONNECT.connectingText }}</text>
        <text class="connect-sub">{{ CONNECT.connectingSub }}</text>
      </view>
      <view class="device-card">
        <view class="device-ic"><text class="glyph">DEV</text></view>
        <view>
          <text class="device-name">{{ deviceName }}</text>
          <text :class="['device-tag', connected ? 'connected' : '']">
            {{ connected ? CONNECT.tagConnected : CONNECT.tagConnecting }}
          </text>
        </view>
      </view>
      <view :class="['btn-primary', connected ? '' : 'btn-disabled']" @click="onNext">
        <text>{{ CONNECT.connectDoneBtn }}</text>
      </view>
    </template>

    <!-- 凭据输入视图 -->
    <template v-else>
      <view class="card-wrap">
        <view class="device-card">
          <view class="device-ic"><text class="glyph">DEV</text></view>
          <view>
            <text class="device-name">{{ deviceName }}</text>
            <text class="device-tag connected">{{ CONNECT.tagConnected }}</text>
          </view>
        </view>
      </view>

      <view class="form-section">
        <text class="form-label">{{ CONNECT.ssidLabel }}</text>
        <view class="input-row">
          <input
            class="input-field"
            :placeholder="CONNECT.ssidPlaceholder"
            placeholder-class="ph"
            :value="ssid"
            @focus="onFocus('ssid')"
            @input="onSsidInput"
          />
        </view>
        <text class="find-link" @click="findModalVisible = true">{{ CONNECT.findLink }}</text>

        <text class="form-label">{{ CONNECT.pwdLabel }}</text>
        <view class="input-row">
          <input
            class="input-field input-pwd"
            :password="!showPwd"
            :placeholder="CONNECT.pwdPlaceholder"
            placeholder-class="ph"
            :value="pwd"
            @focus="onFocus('pwd')"
            @input="onPwdInput"
          />
          <text class="pwd-toggle" @click="showPwd = !showPwd">{{ showPwd ? '隐藏' : '显示' }}</text>
        </view>

        <view class="tips"><text>{{ CONNECT.tips }}</text></view>
      </view>

      <view class="btn-primary" @click="$emit('start')"><text>{{ CONNECT.startBtn }}</text></view>

      <!-- WiFi 名称怎么找 引导 -->
      <view v-if="findModalVisible" class="overlay">
        <view class="modal">
          <text class="modal-head">{{ CONNECT.findModal.title }}</text>
          <view class="modal-body">
            <view v-for="(s, i) in CONNECT.findModal.steps" :key="i" class="modal-step">
              <text class="modal-step-num">{{ i + 1 }}.</text>
              <text class="modal-step-txt">{{ s }}</text>
            </view>
          </view>
          <view class="modal-btn" @click="findModalVisible = false"><text>{{ CONNECT.findModal.confirm }}</text></view>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { CONNECT } from '../../utils/wifi-copy'
import { logger } from '../../utils/logger'

const props = defineProps<{
  phase: 'connecting' | 'form'
  deviceName: string
  connected: boolean
}>()

const emit = defineEmits<{
  (e: 'next'): void
  (e: 'start'): void
}>()

const ssid = ref('')
const pwd = ref('')
const showPwd = ref(false)
const findModalVisible = ref(false)

/** 未建连时按钮置灰：不进入凭据输入，也就不存在"没连蓝牙就写参数" */
function onNext() {
  if (!props.connected) return
  emit('next')
}

function onFocus(field: 'ssid' | 'pwd') {
  logger.info('[T192] 03 输入框获得焦点', { field })
}

function onSsidInput(e: Event) {
  ssid.value = (e as unknown as { detail: { value: string } }).detail.value
}

function onPwdInput(e: Event) {
  pwd.value = (e as unknown as { detail: { value: string } }).detail.value
}

/** 页面在校验前需要拿到原始输入值（trim 在 utils/wifi-state 统一处理） */
function getCredentials() {
  return { ssid: ssid.value, pwd: pwd.value }
}

defineExpose({ getCredentials })
</script>

<style scoped>
.connect { background: #f8fafc; min-height: 100vh; }
.connecting { text-align: center; padding: 120rpx 64rpx 48rpx; }
.connect-ic { width: 160rpx; height: 160rpx; border-radius: 50%; background: linear-gradient(135deg, #2563EB, #6366f1); margin: 0 auto; display: flex; align-items: center; justify-content: center; box-shadow: 0 8rpx 40rpx rgba(37, 99, 235, 0.3); }
.spinner { width: 72rpx; height: 72rpx; border: 6rpx solid rgba(255, 255, 255, 0.3); border-top-color: #fff; border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.connect-text { font-size: 32rpx; font-weight: 500; color: #1e293b; margin-top: 40rpx; display: block; }
.connect-sub { font-size: 24rpx; color: #94a3b8; margin-top: 12rpx; display: block; }

.card-wrap { padding: 40rpx 40rpx 0; }
.device-card { background: #fff; border: 2rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx; display: flex; align-items: center; }
.device-ic { width: 72rpx; height: 72rpx; border-radius: 20rpx; background: #eff6ff; display: flex; align-items: center; justify-content: center; flex-shrink: 0; margin-right: 24rpx; }
.glyph { font-size: 22rpx; font-weight: 700; color: #2563EB; }
.device-name { font-size: 28rpx; font-weight: 500; color: #1e293b; display: block; }
.device-tag { font-size: 20rpx; color: #64748b; background: #f1f5f9; padding: 2rpx 12rpx; border-radius: 12rpx; display: inline-block; margin-top: 6rpx; }
.device-tag.connected { color: #10ac84; background: #e8f5e9; }

.form-section { padding: 0 40rpx; margin-top: 48rpx; }
.form-label { font-size: 26rpx; font-weight: 500; color: #475569; margin-bottom: 16rpx; display: block; }
/* 微信原生 input 无固有宽度，靠 flex 撑会被压成 0 宽（placeholder 不渲染、点不出键盘）——必须给显式宽高 */
.input-row { position: relative; background: #fff; border: 2rpx solid #e2e8f0; border-radius: 24rpx; padding: 0 28rpx; margin-bottom: 28rpx; }
.input-field { width: 100%; box-sizing: border-box; height: 88rpx; font-size: 30rpx; color: #1e293b; }
.input-pwd { padding-right: 96rpx; }
.ph { color: #cbd5e1; }
.pwd-toggle { position: absolute; right: 28rpx; top: 0; height: 88rpx; line-height: 88rpx; padding: 0 8rpx; color: #94a3b8; font-size: 24rpx; }
.find-link { font-size: 24rpx; color: #2563EB; text-align: right; display: block; margin: -16rpx 0 32rpx; }

.tips { margin-top: 32rpx; background: #eff6ff; border: 2rpx solid #bfdbfe; border-radius: 20rpx; padding: 24rpx; }
.tips text { font-size: 24rpx; color: #2563eb; line-height: 1.6; }

.btn-primary { margin: 32rpx 40rpx 0; padding: 28rpx; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 32rpx; font-weight: 500; }
.btn-primary:active { background: #1d4ed8; }
.btn-disabled { background: #cbd5e1; }
.btn-disabled text { color: #f8fafc; }
.btn-disabled:active { background: #cbd5e1; }

.overlay { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.45); z-index: 300; display: flex; align-items: center; justify-content: center; padding: 0 64rpx; }
.modal { background: #fff; border-radius: 32rpx; width: 100%; overflow: hidden; }
.modal-head { padding: 40rpx 40rpx 0; text-align: center; font-size: 32rpx; font-weight: 600; color: #1e293b; display: block; }
.modal-body { padding: 32rpx 40rpx 40rpx; }
.modal-step { display: flex; margin: 12rpx 0; }
.modal-step-num { font-size: 26rpx; color: #64748b; line-height: 1.8; margin-right: 12rpx; }
.modal-step-txt { flex: 1; font-size: 26rpx; color: #64748b; line-height: 1.8; }
.modal-btn { width: 100%; padding: 26rpx; background: #2563EB; text-align: center; }
.modal-btn text { color: #fff; font-size: 30rpx; font-weight: 500; }
</style>
