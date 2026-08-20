<template>
  <div class="forbidden-page">
    <div class="forbidden-card">
      <div class="forbidden-icon">🔒</div>
      <h2>403 · 无访问权限</h2>
      <p>当前角色（{{ roleName(auth.role ?? '') }}）无权访问该页面，请联系运营管理员分配权限（PRD §7D.11）。</p>
      <el-button type="primary" @click="goHome">返回首页</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth'
import { roleName } from '../../router/permissions'

const router = useRouter()
const auth = useAuthStore()

function goHome() {
  router.push('/dashboard')
}
</script>

<style scoped>
.forbidden-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f5f7fa;
}
.forbidden-card {
  text-align: center;
  background: #fff;
  border-radius: 12px;
  padding: 48px 64px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}
.forbidden-icon {
  font-size: 48px;
  margin-bottom: 16px;
}
.forbidden-card h2 {
  margin: 0 0 12px;
  color: #333;
}
.forbidden-card p {
  color: #999;
  font-size: 13px;
  margin: 0 0 24px;
}
</style>
