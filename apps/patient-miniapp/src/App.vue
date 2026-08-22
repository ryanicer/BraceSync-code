<script setup lang="ts">
// App 入口（uni-app 约定：App.vue 无 template，页面由 pages.json 路由渲染）
</script>

<style>
/* 全局样式 — 对齐设计稿基础色板 */
page {
  font-family: 'Inter', 'Noto Sans SC', 'PingFang SC', sans-serif;
  background: #f8fafc;
  color: #1e293b;
}
/* 微信端页面根节点撑满视口，供页面内 min-height: 100% 生效 */
/* #ifdef MP-WEIXIN */
page { height: 100%; }
/* #endif */
view,
text,
input,
button {
  box-sizing: border-box;
}
</style>