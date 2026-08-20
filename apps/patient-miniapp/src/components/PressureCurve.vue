<!--
  压力趋势曲线（T031 组件迁移后为患者端单份组件，跨端渲染）。
  - 原共享包版本用内联 <svg>，微信小程序 WXML 不支持 SVG，故改用 canvas 重写。
  - canvas 节点获取分平台：MP-WEIXIN 用 type="2d" 新画布 API（createSelectorQuery 取 node）；
    H5 用模板引用拿原生 <canvas> 元素。二者共用同一套 render() 绘制逻辑。
  - 绘制内容：网格线 / Y 轴刻度 / X 轴标签 / 渐变面积 / 折线 / 末端点。
-->
<template>
  <view class="pressure-curve" :style="{ height: height + 'px' }">
    <view v-if="data.length === 0" class="curve-empty"><text>暂无数据</text></view>
    <canvas
      v-else
      type="2d"
      id="pressureCurveCanvas"
      ref="canvasRef"
      class="curve-canvas"
      :style="{ width: '100%', height: height + 'px' }"
    ></canvas>
  </view>
</template>

<script setup lang="ts">
import { ref, watch, onMounted, getCurrentInstance } from 'vue'

const props = withDefaults(defineProps<{
  data: { timestamp: string; value: number }[]
  maxValue?: number
  labels?: string[]
  height?: number
}>(), {
  maxValue: 75,
  height: 180,
})

const instance = getCurrentInstance()
const canvasRef = ref<any>(null)

const padLeft = 40
const padRight = 16
const padTop = 20
const padBottom = 42

function render(ctx: any, width: number, height: number) {
  ctx.clearRect(0, 0, width, height)
  const maxValue = props.maxValue
  const chartWidth = width - padLeft - padRight
  const chartHeight = height - padTop - padBottom
  const getY = (v: number) => padTop + chartHeight * ((maxValue - v) / maxValue)
  const getX = (i: number) =>
    props.data.length <= 1 ? padLeft : padLeft + (chartWidth / (props.data.length - 1)) * i

  // 网格线 + Y 轴刻度
  const steps = 5
  const step = maxValue / steps
  for (let i = 0; i <= steps; i++) {
    const val = Math.round(i * step)
    const y = getY(val)
    ctx.strokeStyle = '#e2e8f0'
    ctx.lineWidth = 0.5
    ctx.beginPath()
    ctx.moveTo(padLeft, y)
    ctx.lineTo(width - padRight, y)
    ctx.stroke()
    ctx.fillStyle = '#94a3b8'
    ctx.font = '11px sans-serif'
    ctx.textAlign = 'right'
    ctx.textBaseline = 'middle'
    ctx.fillText(val + 'N', padLeft - 8, y)
  }

  // X 轴标签
  const labels = props.labels || []
  if (labels.length > 0) {
    ctx.fillStyle = '#94a3b8'
    ctx.font = '10px sans-serif'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'alphabetic'
    const denom = Math.max(labels.length - 1, 1)
    labels.forEach((label, i) => {
      const x = padLeft + (chartWidth / denom) * i
      ctx.fillText(label, x, height - 4)
    })
  }

  if (props.data.length === 0) return
  const points = props.data.map((d, i) => ({ x: getX(i), y: getY(d.value) }))

  // 渐变面积
  const grad = ctx.createLinearGradient(0, padTop, 0, padTop + chartHeight)
  grad.addColorStop(0, 'rgba(37,99,235,0.12)')
  grad.addColorStop(1, 'rgba(37,99,235,0.02)')
  ctx.beginPath()
  ctx.moveTo(points[0].x, padTop + chartHeight)
  points.forEach(p => ctx.lineTo(p.x, p.y))
  ctx.lineTo(points[points.length - 1].x, padTop + chartHeight)
  ctx.closePath()
  ctx.fillStyle = grad
  ctx.fill()

  // 折线
  ctx.beginPath()
  points.forEach((p, i) => {
    if (i === 0) ctx.moveTo(p.x, p.y)
    else ctx.lineTo(p.x, p.y)
  })
  ctx.strokeStyle = '#2563EB'
  ctx.lineWidth = 2
  ctx.stroke()

  // 末端点
  const last = points[points.length - 1]
  ctx.beginPath()
  ctx.arc(last.x, last.y, 4, 0, Math.PI * 2)
  ctx.fillStyle = '#2563EB'
  ctx.fill()
  ctx.beginPath()
  ctx.arc(last.x, last.y, 2, 0, Math.PI * 2)
  ctx.fillStyle = '#ffffff'
  ctx.fill()
}

function getPixelRatio(): number {
  try {
    return uni.getSystemInfoSync().pixelRatio || 2
  } catch (e) {
    return 2
  }
}

// #ifdef MP-WEIXIN
// 微信 type="2d" 新画布：通过 createSelectorQuery 取 canvas node
function draw(retry = 3) {
  const query = uni.createSelectorQuery().in(instance && instance.proxy)
  query
    .select('#pressureCurveCanvas')
    .fields({ node: true, size: true })
    .exec((res: any) => {
      const info = res && res[0]
      if (!info || !info.node) {
        if (retry > 0) setTimeout(() => draw(retry - 1), 50)
        return
      }
      const canvas = info.node
      const width = info.width
      const height = info.height
      if (!width || !height) return
      const dpr = getPixelRatio()
      canvas.width = width * dpr
      canvas.height = height * dpr
      const ctx = canvas.getContext('2d')
      ctx.scale(dpr, dpr)
      render(ctx, width, height)
    })
}
// #endif

// #ifndef MP-WEIXIN
// H5：通过模板引用解析原生 <canvas> 元素（uni-app H5 可能包一层 uni-canvas）
function resolveCanvasEl(): HTMLCanvasElement | null {
  let node: any = canvasRef.value
  if (!node) return null
  if (node.$el) node = node.$el
  if (node && typeof node.getContext === 'function') return node as HTMLCanvasElement
  if (node && typeof node.querySelector === 'function') {
    const inner = node.querySelector('canvas')
    if (inner) return inner as HTMLCanvasElement
  }
  return null
}

function draw(retry = 3) {
  const canvas = resolveCanvasEl()
  if (!canvas) {
    if (retry > 0) setTimeout(() => draw(retry - 1), 50)
    return
  }
  const width = canvas.clientWidth || canvas.offsetWidth
  const height = canvas.clientHeight || canvas.offsetHeight
  if (!width || !height) {
    if (retry > 0) setTimeout(() => draw(retry - 1), 50)
    return
  }
  const dpr = getPixelRatio()
  canvas.width = width * dpr
  canvas.height = height * dpr
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.scale(dpr, dpr)
  render(ctx, width, height)
}
// #endif

onMounted(() => {
  draw()
})

watch(
  () => [props.data, props.maxValue, props.labels, props.height],
  () => {
    draw()
  },
  { deep: true }
)
</script>

<style scoped>
.pressure-curve {
  width: 100%;
  position: relative;
}
.curve-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #94a3b8;
  font-size: 28rpx;
}
.curve-canvas {
  display: block;
}
</style>
