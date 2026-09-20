<template>
  <div class="roles">
    <el-row :gutter="16">
      <!-- 角色列表 -->
      <el-col :span="10">
        <div class="page-card">
          <div class="page-card-title">角色列表</div>
          <el-table
            :data="roles"
            size="small"
            v-loading="loading"
            highlight-current-row
            @row-click="selectRole"
          >
            <el-table-column prop="name" label="角色名称" width="110" />
            <el-table-column prop="description" label="描述" min-width="160" show-overflow-tooltip />
            <el-table-column prop="memberCount" label="成员数" width="80" />
            <el-table-column label="状态" width="80">
              <template #default="{ row }">
                <el-tag :type="row.status === 'enabled' ? 'success' : 'danger'" size="small">
                  {{ row.status === 'enabled' ? '启用' : '禁用' }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
          <p class="matrix-note">点击角色查看并编辑权限矩阵。</p>
        </div>
      </el-col>

      <!-- 权限矩阵 -->
      <el-col :span="14">
        <div class="page-card">
          <div class="page-card-title">
            功能模块 × 角色 权限矩阵
            <span v-if="selectedRole" class="selected-role">当前：{{ selectedRole.name }}</span>
          </div>
          <el-table :data="matrixRows" size="small" border v-loading="permLoading">
            <el-table-column prop="page" label="页面/模块" width="160" />
            <el-table-column label="有权限" align="center" width="120">
              <template #default="{ row }">
                <el-checkbox
                  :model-value="row.checked"
                  :disabled="!selectedRole || saving"
                  @change="(val: boolean) => togglePerm(row.path, val)"
                />
              </template>
            </el-table-column>
            <el-table-column label="数据范围" min-width="120">
              <template #default="{ row }">
                <span v-if="row.scopeNote" class="scope-note">{{ row.scopeNote }}</span>
                <span v-else style="color:#ccc">—</span>
              </template>
            </el-table-column>
          </el-table>
          <div class="action-bar">
            <el-button
              type="primary"
              :loading="saving"
              :disabled="!selectedRole || !dirty"
              @click="savePermissions"
            >保存权限配置</el-button>
            <span v-if="dirty" class="dirty-hint">有未保存的修改</span>
          </div>
        </div>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { fetchAdminRoles, fetchRolePermissionsApi, updateRolePermissionsApi } from '../../api'
import type { AdminRoleRow } from '../../mock/system'
import { pageRoutes } from '../../router'
import { ROLE_PAGE_MATRIX } from '../../router/permissions'

interface MatrixRow {
  path: string
  page: string
  checked: boolean
  scopeNote: string
}

// 医生 / 客服数据范围注记（PRD §7D.11）
const SCOPE_NOTES: Record<string, string> = {
  '/monitor': '仅本团队患者',
  '/alerts': '仅本团队患者',
  '/orthosis-log': '仅本团队患者',
  '/communication': '仅查看与标记',
}

const roles = ref<AdminRoleRow[]>([])
const loading = ref(false)
const permLoading = ref(false)
const saving = ref(false)
const selectedRole = ref<AdminRoleRow | null>(null)
const currentPerms = ref<string[]>([])
const dirty = ref(false)

const matrixRows = computed<MatrixRow[]>(() => {
  const perms = new Set(currentPerms.value)
  return pageRoutes.map((r) => {
    const path = r.path
    // 默认值从预置矩阵取（首次加载前的兜底）
    const fallbackAdmin = ROLE_PAGE_MATRIX.admin.includes(path)
    return {
      path,
      page: String(r.meta?.title ?? path),
      checked: perms.has(path) || (currentPerms.value.length === 0 && fallbackAdmin),
      scopeNote: SCOPE_NOTES[path] ?? '',
    }
  })
})

async function selectRole(row: AdminRoleRow) {
  selectedRole.value = row
  dirty.value = false
  permLoading.value = true
  try {
    const res = await fetchRolePermissionsApi(row.roleId)
    currentPerms.value = res.permissions
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载权限失败')
  } finally {
    permLoading.value = false
  }
}

function togglePerm(path: string, val: boolean) {
  const set = new Set(currentPerms.value)
  if (val) set.add(path)
  else set.delete(path)
  currentPerms.value = Array.from(set)
  dirty.value = true
}

async function savePermissions() {
  if (!selectedRole.value) return
  saving.value = true
  try {
    await updateRolePermissionsApi(selectedRole.value.roleId, currentPerms.value)
    dirty.value = false
    ElMessage.success('权限配置已保存')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  loading.value = true
  try {
    roles.value = await fetchAdminRoles()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.matrix-note {
  font-size: 12px;
  color: #999;
  margin-top: 12px;
}
.scope-note {
  display: block;
  font-size: 11px;
  color: #999;
}
.selected-role {
  font-size: 12px;
  color: #1a6db5;
  font-weight: 500;
  margin-left: 12px;
}
.action-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
}
.dirty-hint {
  font-size: 12px;
  color: #ee5a24;
}
</style>
