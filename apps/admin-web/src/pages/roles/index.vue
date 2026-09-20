<template>
  <div class="roles">
    <el-row :gutter="16">
      <!-- 角色列表 -->
      <el-col :span="12">
        <div class="page-card">
          <div class="page-card-title roles-title">
            <span>角色列表</span>
            <el-button type="primary" size="small" @click="openCreateDialog">+ 新增角色</el-button>
          </div>
          <el-table
            :data="roles"
            size="small"
            v-loading="loading"
            highlight-current-row
            @row-click="selectRole"
          >
            <el-table-column prop="name" label="角色名称" width="100" />
            <el-table-column prop="description" label="描述" min-width="120" show-overflow-tooltip />
            <el-table-column prop="memberCount" label="成员数" width="60" />
            <el-table-column label="创建时间" width="92">
              <template #default="{ row }">{{ (row.createdAt || '').slice(0, 10) }}</template>
            </el-table-column>
            <el-table-column label="状态" width="60">
              <template #default="{ row }">
                <el-tag :type="row.status === 'enabled' ? 'success' : 'danger'" size="small">
                  {{ row.status === 'enabled' ? '启用' : '禁用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="112" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click.stop="openEditDialog(row)">编辑</el-button>
                <el-tooltip v-if="row.preset" content="预置角色不可删除" placement="top">
                  <span class="del-wrap"><el-button link type="danger" size="small" disabled>删除</el-button></span>
                </el-tooltip>
                <el-popconfirm v-else :title="`确认删除角色 ${row.name}？`" @confirm="removeRole(row)">
                  <template #reference>
                    <el-button link type="danger" size="small" @click.stop>删除</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
          <p class="matrix-note">点击角色查看并编辑权限矩阵。</p>
        </div>
      </el-col>

      <!-- 权限矩阵 -->
      <el-col :span="12">
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

    <!-- 新增/编辑角色弹窗（设计稿 权限控制.html:180-204） -->
    <el-dialog v-model="dialogVisible" :title="dialogMode === 'create' ? '新增角色' : '编辑角色'" width="480px">
      <el-form label-width="90px">
        <el-form-item label="角色名称" required>
          <el-input v-model="dialogForm.name" :disabled="dialogMode === 'edit' && dialogForm.preset" maxlength="64" placeholder="请输入角色名称" />
        </el-form-item>
        <el-form-item label="角色描述">
          <el-input v-model="dialogForm.description" maxlength="255" placeholder="描述该角色的职责范围" />
        </el-form-item>
        <template v-if="dialogMode === 'create'">
          <el-form-item label="权限模板">
            <el-select v-model="dialogForm.template" class="role-template" placeholder="自定义" @change="applyTemplate">
              <el-option label="自定义" value="" />
              <el-option v-for="t in templates" :key="t.key" :label="t.name" :value="t.key" />
            </el-select>
          </el-form-item>
          <el-form-item label="功能模块">
            <div class="module-tree">
              <el-checkbox-group v-model="dialogForm.modules">
                <el-checkbox
                  v-for="m in MODULE_OPTIONS"
                  :key="m.key"
                  :value="m.key"
                  :disabled="!!dialogForm.template"
                >{{ m.label }}</el-checkbox>
              </el-checkbox-group>
            </div>
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="dialogSaving" @click="saveRole">保存角色</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import {
  fetchAdminRoles, fetchRolePermissionsApi, updateRolePermissionsApi,
  fetchRoleTemplates, createRoleApi, updateRoleApi, deleteRoleApi,
  type RoleTemplateItem,
} from '../../api'
import type { AdminRoleRow } from '../../mock/system'
import { pageRoutes } from '../../router'
import { ROLE_PAGE_MATRIX } from '../../router/permissions'

// T253-11.2: 功能模块清单（key 对齐后端模板 modules 键，label 对齐路由标题）
const MODULE_OPTIONS: { key: string; label: string }[] = [
  { key: 'dashboard', label: '数据概览' },
  { key: 'realtime', label: '实时监控' },
  { key: 'patients', label: '患者管理' },
  { key: 'teams', label: '团队管理' },
  { key: 'devices', label: '设备管理' },
  { key: 'alerts', label: '告警管理' },
  { key: 'comm', label: '患者沟通' },
  { key: 'orthosis', label: '矫形日志' },
  { key: 'install', label: '安装记录' },
  { key: 'tech', label: '技师管理' },
  { key: 'perm', label: '权限控制' },
  { key: 'config', label: '系统配置' },
]

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

// ── T253-11.2 角色增删改 ──
const dialogVisible = ref(false)
const dialogMode = ref<'create' | 'edit'>('create')
const dialogSaving = ref(false)
const templates = ref<RoleTemplateItem[]>([])
const editingRole = ref<AdminRoleRow | null>(null)
const dialogForm = ref({
  name: '',
  description: '',
  preset: false,
  template: '',
  modules: [] as string[],
})

function openCreateDialog() {
  dialogMode.value = 'create'
  editingRole.value = null
  dialogForm.value = { name: '', description: '', preset: false, template: '', modules: [] }
  dialogVisible.value = true
}

function openEditDialog(row: AdminRoleRow) {
  dialogMode.value = 'edit'
  editingRole.value = row
  dialogForm.value = { name: row.name, description: row.description, preset: row.preset, template: '', modules: [] }
  dialogVisible.value = true
}

function applyTemplate(key: string) {
  const tpl = templates.value.find((t) => t.key === key)
  dialogForm.value.modules = tpl ? [...tpl.permissions.modules] : []
}

async function saveRole() {
  const f = dialogForm.value
  if (!f.name.trim()) {
    ElMessage.warning('请输入角色名称')
    return
  }
  dialogSaving.value = true
  try {
    if (dialogMode.value === 'create') {
      if (f.template) {
        await createRoleApi({ name: f.name.trim(), description: f.description.trim() || undefined, template: f.template })
      } else {
        if (f.modules.length === 0) {
          ElMessage.warning('自定义角色请至少勾选一个功能模块')
          dialogSaving.value = false
          return
        }
        await createRoleApi({
          name: f.name.trim(),
          description: f.description.trim() || undefined,
          permissions: { scope: 'team', modules: [...f.modules] },
        })
      }
      ElMessage.success('角色已创建')
    } else if (editingRole.value) {
      const payload: { name?: string; description?: string } = {}
      if (!f.preset) payload.name = f.name.trim()
      payload.description = f.description.trim()
      await updateRoleApi(editingRole.value.roleId, payload)
      ElMessage.success('角色已保存')
    }
    dialogVisible.value = false
    await loadRoles()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    dialogSaving.value = false
  }
}

async function removeRole(row: AdminRoleRow) {
  try {
    await deleteRoleApi(row.roleId)
    ElMessage.success('角色已删除')
    if (selectedRole.value?.roleId === row.roleId) {
      selectedRole.value = null
      currentPerms.value = []
    }
    await loadRoles()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '删除失败')
  }
}

async function loadRoles() {
  loading.value = true
  try {
    roles.value = await fetchAdminRoles()
    if (selectedRole.value) {
      const fresh = roles.value.find((r) => r.roleId === selectedRole.value?.roleId)
      selectedRole.value = fresh ?? null
    }
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  loadRoles()
  try {
    templates.value = await fetchRoleTemplates()
  } catch {
    // 模板拉取失败不阻断页面，下拉退化为「自定义」
  }
})
</script>

<style scoped>
.roles-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.matrix-note {
  font-size: 12px;
  color: #999;
  margin-top: 12px;
}
.del-wrap {
  display: inline-block;
  margin-left: 8px;
}
.module-tree {
  border: 1px solid #e8ecf0;
  border-radius: 6px;
  padding: 8px 12px;
  max-height: 180px;
  overflow-y: auto;
  width: 100%;
}
.module-tree .el-checkbox {
  margin-right: 16px;
}
.role-template {
  width: 100%;
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
