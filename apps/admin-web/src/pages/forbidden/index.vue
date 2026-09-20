<template>
  <div class="forbidden-page">
    <div class="forbidden-card">
      <div class="forbidden-icon">🔒</div>
      <h2>403 · 无访问权限</h2>
      <p>当前角色（{{ roleName(auth.role ?? '') }}）无权访问该页面，请联系运营管理员分配权限（PRD §7D.11）。</p>
      <p v-if="reachablePages.length" class="reachable">
        可访问页面：
        <router-link v-for="p in reachablePages" :key="p.path" :to="p.path" class="reachable-link">{{ p.title }}</router-link>
      </p>
      <el-button type="primary" @click="goHome">返回首页</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth'
import { pageRoutes } from '../../router'
import { canAccess, landingPathFor, roleName } from '../../router/permissions'

const router = useRouter()
const auth = useAuthStore()

/** T269 D3：403 页给出本角色可达路径，避免用户被孤立在无出口的整屏页 */
const reachablePages = computed(() =>
  pageRoutes
    .filter((r) => canAccess(auth.role ?? '', r.path))
    .map((r) => ({ path: r.path, title: String(r.meta?.title ?? r.path) })),
)

function goHome() {
  router.push(landingPathFor(auth.role))
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
.forbidden-card .reachable {
  margin-bottom: 20px;
  line-height: 2;
}
.reachable-link {
  color: #1a6db5;
  margin-right: 10px;
}
</style>
