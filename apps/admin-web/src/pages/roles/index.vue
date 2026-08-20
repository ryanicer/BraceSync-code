<template>
  <div class="roles">
    <el-row :gutter="16">
      <!-- 角色列表 -->
      <el-col :span="10">
        <div class="page-card">
          <div class="page-card-title">角色列表（预置角色：医生 / 运营 / 客服）</div>
          <el-table :data="roles" size="small" v-loading="loading" highlight-current-row @row-click="selectRole">
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
          <p class="matrix-note">
            预置角色权限由系统锁定（PRD §7D.11），一期不支持自定义角色编辑；
            点击角色查看权限矩阵。
          </p>
        </div>
      </el-col>

      <!-- 权限矩阵 -->
      <el-col :span="14">
        <div class="page-card">
          <div class="page-card-title">功能模块 × 角色 权限矩阵（PRD §7D.11）</div>
          <el-table :data="matrixRows" size="small" border>
            <el-table-column prop="page" label="页面/模块" width="160" />
            <el-table-column label="运营管理员" align="center">
              <template #default="{ row }">
                <el-checkbox :model-value="row.admin" disabled />
              </template>
            </el-table-column>
            <el-table-column label="医生" align="center">
              <template #default="{ row }">
                <el-checkbox :model-value="row.doctor" disabled />
                <span v-if="row.doctorNote" class="scope-note">{{ row.doctorNote }}</span>
              </template>
            </el-table-column>
            <el-table-column label="客服" align="center">
              <template #default="{ row }">
                <el-checkbox :model-value="row.cs" disabled />
                <span v-if="row.csNote" class="scope-note">{{ row.csNote }}</span>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { fetchAdminRoles } from '../../api'
import type { AdminRoleRow } from '../../mock/system'
import { ROLE_PAGE_MATRIX } from '../../router/permissions'
import { pageRoutes } from '../../router'

interface MatrixRow {
  page: string
  admin: boolean
  doctor: boolean
  cs: boolean
  doctorNote: string
  csNote: string
}

const roles = ref<AdminRoleRow[]>([])
const loading = ref(false)

// 医生数据范围注记（PRD §7D.11 表格）
const DOCTOR_NOTES: Record<string, string> = {
  '/monitor': '仅本团队患者',
  '/alerts': '仅本团队患者',
  '/orthosis-log': '仅本团队患者',
}
const CS_NOTES: Record<string, string> = {
  '/communication': '仅查看与标记',
}

const matrixRows = pageRoutes.map<MatrixRow>((r) => ({
  page: String(r.meta?.title ?? r.path),
  admin: ROLE_PAGE_MATRIX.admin.includes(r.path),
  doctor: ROLE_PAGE_MATRIX.doctor.includes(r.path),
  cs: ROLE_PAGE_MATRIX.cs.includes(r.path),
  doctorNote: DOCTOR_NOTES[r.path] ?? '',
  csNote: CS_NOTES[r.path] ?? '',
}))

function selectRole() {
  // 预置角色权限只读展示；自定义角色编辑待后端 RBAC 接口就绪后启用
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
</style>
