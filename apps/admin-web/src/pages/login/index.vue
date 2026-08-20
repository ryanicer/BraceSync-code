<template>
  <div class="login-page">
    <div class="login-card">
      <h1 class="login-title">🏥 矫智通运营平台</h1>
      <p class="login-subtitle">医生 / 运营 / 客服统一后台（PRD §1.2）</p>

      <!-- mock 模式（USE_MOCK=true 本地开发）：预置角色下拉 + 任意密码 -->
      <el-form v-if="isMock" label-position="top" class="login-form">
        <el-form-item label="选择角色（mock 预置账号）">
          <el-select v-model="selectedRole" class="full-width">
            <el-option
              v-for="role in PRESET_ROLES"
              :key="role.key"
              :label="role.name"
              :value="role.key"
            >
              <span>{{ role.name }}</span>
              <span class="role-desc">{{ role.description }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="password" type="password" placeholder="mock 阶段任意密码" show-password />
        </el-form-item>
        <el-button type="primary" class="full-width" @click="handleLogin">登录</el-button>
      </el-form>

      <!-- 真实模式（USE_MOCK=false）：用户名 + 密码 → POST /api/v1/auth/login -->
      <el-form
        v-else
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
        class="login-form"
        @submit.prevent
      >
        <el-form-item label="用户名" prop="username">
          <el-input
            v-model="form.username"
            placeholder="请输入用户名"
            autocomplete="username"
            @keyup.enter="handleLogin"
          />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input
            v-model="form.password"
            type="password"
            placeholder="请输入密码"
            autocomplete="current-password"
            show-password
            @keyup.enter="handleLogin"
          />
        </el-form-item>
        <el-button type="primary" class="full-width" :loading="loading" @click="handleLogin">登 录</el-button>
      </el-form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { useAuthStore } from '../../stores/auth'
import { PRESET_ROLES, type RoleKey } from '../../router/permissions'
import { USE_MOCK } from '../../utils/request'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const isMock = USE_MOCK

// mock 模式状态
const selectedRole = ref<RoleKey>('admin')
const password = ref('')

// 真实模式状态
const formRef = ref<FormInstance>()
const loading = ref(false)
const form = reactive({ username: '', password: '' })
const rules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
}

function redirectAfterLogin() {
  const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : ''
  router.push(redirect || '/dashboard')
}

async function handleLogin() {
  if (isMock) {
    auth.login(selectedRole.value)
    ElMessage.success(`欢迎，${auth.user?.name}`)
    redirectAfterLogin()
    return
  }
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  loading.value = true
  try {
    await auth.loginWithPassword(form.username.trim(), form.password)
    ElMessage.success(`欢迎，${auth.user?.name}`)
    redirectAfterLogin()
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #2c3e50 0%, #1a6db5 100%);
}
.login-card {
  width: 400px;
  background: #fff;
  border-radius: 12px;
  padding: 40px 36px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2);
}
.login-title {
  font-size: 20px;
  font-weight: 600;
  color: #333;
  margin: 0 0 8px;
  text-align: center;
}
.login-subtitle {
  font-size: 13px;
  color: #999;
  text-align: center;
  margin: 0 0 24px;
}
.login-form {
  margin-top: 16px;
}
.full-width {
  width: 100%;
}
.role-desc {
  float: right;
  font-size: 12px;
  color: #999;
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
