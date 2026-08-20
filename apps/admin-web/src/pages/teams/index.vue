<template>
  <div class="teams">
    <div class="page-card">
      <el-table :data="teams" size="small" v-loading="loading">
        <el-table-column prop="teamId" label="团队ID" width="120" />
        <el-table-column prop="name" label="团队名称" min-width="180" />
        <el-table-column prop="memberCount" label="成员数" width="100" />
        <el-table-column prop="patientCount" label="管理患者数" width="120" />
        <el-table-column label="团队排行" min-width="160">
          <template #default="{ row }">
            <span v-if="rankingOf(row.name) >= 0">
              第 {{ rankingOf(row.name) + 1 }} 名 · 达标率 {{ rankingRate(row.name) }}%
            </span>
            <span v-else class="no-rank">暂无排行数据</span>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Team, TeamRanking } from '@bracesync/shared-types'
import { fetchTeams, fetchTeamRanking } from '../../api'

const teams = ref<Team[]>([])
const ranking = ref<TeamRanking[]>([])
const loading = ref(false)

function rankingOf(teamName: string): number {
  return ranking.value.findIndex((r) => r.teamName === teamName)
}

function rankingRate(teamName: string): number {
  const found = ranking.value.find((r) => r.teamName === teamName)
  return found ? found.complianceRate : 0
}

onMounted(async () => {
  loading.value = true
  try {
    const [teamsRes, rankingRes] = await Promise.all([fetchTeams(), fetchTeamRanking()])
    teams.value = teamsRes
    ranking.value = rankingRes
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.no-rank {
  color: #999;
}
</style>
