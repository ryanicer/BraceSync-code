<template>
  <div class="technicians">
    <div class="page-card">
      <el-table :data="list" size="small" v-loading="loading">
        <el-table-column prop="techId" label="技师ID" width="110" />
        <el-table-column prop="name" label="姓名" width="100" />
        <el-table-column prop="phoneMasked" label="手机号" width="130" />
        <el-table-column label="所属团队" width="140">
          <template #default="{ row }">{{ teamNameOf(row.teamId) }}</template>
        </el-table-column>
        <el-table-column prop="installCount" label="安装数" width="90" />
        <el-table-column label="认证状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.authStatus === 'authorized' ? 'success' : 'info'" size="small" effect="plain">
              {{ row.authStatus === 'authorized' ? '已认证' : '未认证' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="账号状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'enabled' ? 'success' : 'danger'" size="small">
              {{ row.status === 'enabled' ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="110">
          <template #default="{ row }">
            <el-popconfirm
              :title="row.status === 'enabled' ? `确认禁用技师 ${row.name}？` : `确认启用技师 ${row.name}？`"
              @confirm="toggle(row)"
            >
              <template #reference>
                <el-button size="small" link :type="row.status === 'enabled' ? 'danger' : 'success'">
                  {{ row.status === 'enabled' ? '禁用' : '启用' }}
                </el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        class="pagination"
        v-model:current-page="page"
        :total="total"
        :page-size="pageSize"
        layout="total, prev, pager, next"
        @current-change="loadData"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Technician } from '@bracesync/shared-types'
import { fetchTechnicians, toggleTechnicianApi, teamNameOf } from '../../api'

const list = ref<Technician[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(false)

async function loadData() {
  loading.value = true
  try {
    const res = await fetchTechnicians({ page: page.value, pageSize: pageSize.value })
    list.value = res.list
    total.value = res.total
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

async function toggle(row: Technician) {
  const action = row.status === 'enabled' ? 'disable' : 'enable'
  try {
    await toggleTechnicianApi(row.techId, action)
    // mock 模式本地翻转；真实模式由后端落库后刷新
    row.status = action === 'enable' ? 'enabled' : 'disabled'
    ElMessage.success(action === 'enable' ? '已启用' : '已禁用')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '操作失败')
  }
}

onMounted(loadData)
</script>

<style scoped>
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
