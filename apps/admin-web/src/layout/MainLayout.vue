<template>
  <el-container class="layout">
    <el-aside width="210px" class="sidebar">
      <div class="sidebar-header">🏥 运营平台</div>
      <el-menu
        :default-active="route.path"
        router
        background-color="#2c3e50"
        text-color="#bdc3c7"
        active-text-color="#ffffff"
        class="sidebar-menu"
      >
        <el-menu-item v-for="item in visibleMenus" :key="item.path" :index="item.path">
          <span class="menu-icon">{{ item.icon }}</span>
          <span>{{ item.title }}</span>
        </el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="top-nav">
        <h2 class="top-nav-title">{{ currentTitle }}</h2>
        <div class="top-nav-right">
          <el-tag v-if="auth.role" size="small" type="info" effect="plain">{{ roleName(auth.role) }}</el-tag>
          <span class="user-name">{{ auth.user?.name }}</span>
          <el-button size="small" @click="handleLogout">退出</el-button>
        </div>
      </el-header>
      <el-main class="page-content">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import { pageRoutes } from '../router'
import { canAccess, roleName } from '../router/permissions'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

interface MenuItem {
  path: string
  title: string
  icon: string
}

// 侧边栏菜单：12 页按当前角色权限过滤（PRD §7D.11）
const visibleMenus = computed<MenuItem[]>(() => {
  return pageRoutes
    .filter((r) => canAccess(auth.role ?? '', r.path))
    .map((r) => ({
      path: r.path,
      title: String(r.meta?.title ?? ''),
      icon: String(r.meta?.icon ?? ''),
    }))
})

const currentTitle = computed(() => String(route.meta?.title ?? ''))

async function handleLogout() {
  try {
    await ElMessageBox.confirm('确认退出登录？', '提示', { type: 'warning' })
  } catch {
    return
  }
  auth.logout()
  router.push('/login')
}
</script>

<style scoped>
.layout {
  min-height: 100vh;
}
.sidebar {
  background: #2c3e50;
}
.sidebar-header {
  color: #fff;
  font-size: 16px;
  font-weight: 600;
  padding: 20px;
  border-bottom: 1px solid #34495e;
}
.sidebar-menu {
  border-right: none;
}
.sidebar-menu :deep(.el-menu-item.is-active) {
  background: #1a6db5;
}
.menu-icon {
  margin-right: 10px;
}
.top-nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
  border-bottom: 1px solid #e8ecf0;
}
.top-nav-title {
  font-size: 18px;
  font-weight: 600;
  margin: 0;
}
.top-nav-right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.user-name {
  font-size: 13px;
  color: #333;
}
.page-content {
  background: #f5f7fa;
}
</style>
