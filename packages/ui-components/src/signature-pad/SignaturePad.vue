<template>
  <view class="signature-pad">
    <view class="pad-header" v-if="showHeader">
      <text class="pad-title">{{ title }}</text>
      <view class="pad-actions">
        <view class="action-clear" @click="clear"><text>清除</text></view>
        <view class="action-confirm" @click="confirm"><text>确认</text></view>
      </view>
    </view>
    <canvas
      ref="canvasRef"
      class="pad-canvas"
      :style="{ width: canvasWidth + 'px', height: canvasHeight + 'px' }"
      @touchstart="onTouchStart"
      @touchmove="onTouchMove"
      @touchend="onTouchEnd"
      @mousedown="onMouseDown"
      @mousemove="onMouseMove"
      @mouseup="onMouseUp"
    />
    <view v-if="isEmpty" class="pad-placeholder">
      <text class="placeholder-text">请在此处签名</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

const props = withDefaults(defineProps<{
  title?: string
  showHeader?: boolean
  width?: number
  height?: number
  lineWidth?: number
  lineColor?: string
}>(), {
  title: '电子签名',
  showHeader: true,
  width: 300,
  height: 200,
  lineWidth: 2,
  lineColor: '#1e293b',
})

const emit = defineEmits<{
  (e: 'confirm', dataUrl: string): void
  (e: 'clear'): void
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
const canvasWidth = ref(props.width)
const canvasHeight = ref(props.height)
const isEmpty = ref(true)
let ctx: CanvasRenderingContext2D | null = null
let drawing = false
let lastX = 0
let lastY = 0

function initCanvas() {
  const canvas = canvasRef.value
  if (!canvas || typeof canvas.getContext !== 'function') return
  // 高分辨率支持
  const dpr = typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1
  canvas.width = canvasWidth.value * dpr
  canvas.height = canvasHeight.value * dpr
  ctx = canvas.getContext('2d')
  if (ctx) {
    ctx.scale(dpr, dpr)
    ctx.strokeStyle = props.lineColor
    ctx.lineWidth = props.lineWidth
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'
  }
}

function getCanvasPos(e: TouchEvent | MouseEvent): { x: number; y: number } {
  const canvas = canvasRef.value
  if (!canvas) return { x: 0, y: 0 }
  const rect = canvas.getBoundingClientRect()
  if ('touches' in e && e.touches.length > 0) {
    return { x: e.touches[0].clientX - rect.left, y: e.touches[0].clientY - rect.top }
  }
  if ('clientX' in e) {
    return { x: (e as MouseEvent).clientX - rect.left, y: (e as MouseEvent).clientY - rect.top }
  }
  return { x: 0, y: 0 }
}

function startDraw(x: number, y: number) {
  drawing = true
  lastX = x
  lastY = y
  isEmpty.value = false
}

function draw(x: number, y: number) {
  if (!drawing || !ctx) return
  ctx.beginPath()
  ctx.moveTo(lastX, lastY)
  ctx.lineTo(x, y)
  ctx.stroke()
  lastX = x
  lastY = y
}

function endDraw() {
  drawing = false
}

function onTouchStart(e: TouchEvent) {
  e.preventDefault()
  const pos = getCanvasPos(e)
  startDraw(pos.x, pos.y)
}

function onTouchMove(e: TouchEvent) {
  e.preventDefault()
  const pos = getCanvasPos(e)
  draw(pos.x, pos.y)
}

function onTouchEnd() {
  endDraw()
}

function onMouseDown(e: MouseEvent) {
  const pos = getCanvasPos(e)
  startDraw(pos.x, pos.y)
}

function onMouseMove(e: MouseEvent) {
  const pos = getCanvasPos(e)
  draw(pos.x, pos.y)
}

function onMouseUp() {
  endDraw()
}

function clear() {
  const canvas = canvasRef.value
  if (!canvas) return
  if (ctx) {
    ctx.clearRect(0, 0, canvasWidth.value, canvasHeight.value)
  }
  isEmpty.value = true
  emit('clear')
}

function confirm() {
  const canvas = canvasRef.value
  if (!canvas || isEmpty.value) return
  const dataUrl = typeof canvas.toDataURL === 'function' ? canvas.toDataURL('image/png') : ''
  emit('confirm', dataUrl)
}

/** 获取签名图片 data URL */
function toDataURL(): string {
  const canvas = canvasRef.value
  if (!canvas || typeof canvas.toDataURL !== 'function') return ''
  return canvas.toDataURL('image/png')
}

defineExpose({ clear, toDataURL, isEmpty })

onMounted(() => {
  initCanvas()
})

onUnmounted(() => {
  ctx = null
})
</script>

<style scoped>
.signature-pad { position: relative; border: 2rpx solid #e2e8f0; border-radius: 16rpx; overflow: hidden; background: #fff; }
.pad-header { display: flex; align-items: center; justify-content: space-between; padding: 16rpx 24rpx; background: #f8fafc; border-bottom: 1rpx solid #e2e8f0; }
.pad-title { font-size: 26rpx; font-weight: 500; color: #1e293b; }
.pad-actions { display: flex; gap: 16rpx; }
.action-clear { padding: 8rpx 20rpx; background: #fee2e2; border-radius: 8rpx; }
.action-clear text { font-size: 24rpx; color: #dc2626; }
.action-confirm { padding: 8rpx 20rpx; background: #dbeafe; border-radius: 8rpx; }
.action-confirm text { font-size: 24rpx; color: #2563EB; }
.pad-canvas { display: block; touch-action: none; }
.pad-placeholder { position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%); pointer-events: none; }
.placeholder-text { font-size: 28rpx; color: #cbd5e1; }
</style>
