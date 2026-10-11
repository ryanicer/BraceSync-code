<template>
  <!-- T653 Tab4 顶部：告警类型 ↔ 流程模板绑定（仅 admin 可见，父级 v-if canConfigure 已收口） -->
  <div v-loading="loading" class="ftb-card">
    <div class="ftb-head">
      <div class="ftb-title">告警类型 ↔ 流程模板绑定</div>
      <div class="ftb-tip">
        绑定后该类型新告警将自动按模板创建处理流程；未绑定只通知不建流程。变更只对此后新告警生效，在途实例不追溯。
      </div>
    </div>
    <div v-for="row in rows" :key="row.alertType" class="ftb-row">
      <div class="ftb-type">
        <span class="ftb-type-name">{{ row.alertTypeName }}</span>
        <el-tag v-if="!draft[row.alertType]" type="warning" size="small" effect="light">未绑定</el-tag>
        <span v-else class="ftb-meta">
          {{ nameOf(row) }} · {{ fmtTime(row.updatedAt) }}
        </span>
      </div>
      <div class="ftb-select">
        <el-select v-model="draft[row.alertType]" placeholder="未绑定（仅通知）" clearable filterable style="width: 320px">
          <el-option label="未绑定（仅通知）" :value="''" />
          <el-option
            v-for="t in templates"
            :key="t.templateId"
            :label="t.name"
            :value="t.templateId"
          />
        </el-select>
        <span v-if="missingTemplate(row)" class="ftb-missing">当前绑定模板已不存在</span>
      </div>
    </div>
    <div v-if="!templates.length && !loading" class="ftb-no-tpl">
      尚无流程模板，请先在下方设计器新建并保存。
    </div>
    <div class="ftb-actions">
      <el-button type="primary" :loading="saving" :disabled="!changedCount" @click="save">保存绑定</el-button>
      <span class="ftb-dirty" v-if="changedCount">{{ changedCount }} 项待保存</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { userErrorCopy } from '@bracesync/shared-utils'
import {
  fetchFlowTemplates, fetchFlowTypeBindings, saveFlowTypeBindingsApi,
  type FlowTemplate, type FlowTypeBinding,
} from '../../../api/flow'

const loading = ref(false)
const saving = ref(false)
const rows = ref<FlowTypeBinding[]>([])
const templates = ref<FlowTemplate[]>([])
// alertType → 选中的 templateId（'' = 未绑定）；后端固定四行，reactive 初值在 load 时补齐
const draft = reactive<Record<string, string>>({})

function applyRows(list: FlowTypeBinding[]) {
  rows.value = list
  list.forEach((b) => { draft[b.alertType] = b.templateId ?? '' })
}

/** 已加载行上的模板名（绑定模板被删时回退模板 ID 展示） */
function nameOf(row: FlowTypeBinding): string {
  if (row.templateName) return row.templateName
  return row.templateId ?? ''
}

/** 已绑定但候选列表里找不到该模板（模板被删/异常） */
function missingTemplate(row: FlowTypeBinding): boolean {
  const tid = draft[row.alertType]
  return !!tid && !templates.value.some((t) => t.templateId === tid)
}

function fmtTime(iso: string | null): string {
  if (!iso) return ''
  // 后端给 RFC3339（+08:00），列表只需分钟精度
  return iso.replace('T', ' ').slice(0, 16)
}

const changedCount = computed(() => rows.value.filter((b) => (b.templateId ?? '') !== draft[b.alertType]).length)

async function load() {
  loading.value = true
  try {
    // 模板候选 = flow_template 全量（列表接口 updated_at DESC；nodes/edges 契约恒空，下拉只用名）
    const [list, tpls] = await Promise.all([fetchFlowTypeBindings(), fetchFlowTemplates()])
    applyRows(list)
    templates.value = tpls
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '加载类型绑定失败' }))
  } finally {
    loading.value = false
  }
}

async function save() {
  // PUT 部分覆盖：只提交相对已加载视图有变化的项；空 templateId = 解绑
  const items = rows.value
    .filter((b) => (b.templateId ?? '') !== draft[b.alertType])
    .map((b) => ({ alertType: b.alertType, templateId: draft[b.alertType] }))
  if (!items.length) return
  saving.value = true
  try {
    applyRows(await saveFlowTypeBindingsApi(items))
    ElMessage.success('绑定保存成功')
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '保存绑定失败' }))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.ftb-card {
  background: #fff;
  border: 1px solid #ebeef5;
  border-radius: 8px;
  padding: 16px 20px;
  margin-bottom: 16px;
}
.ftb-head {
  margin-bottom: 12px;
}
.ftb-title {
  font-size: 15px;
  font-weight: 600;
  color: #303133;
}
.ftb-tip {
  margin-top: 4px;
  font-size: 12px;
  color: #909399;
  line-height: 1.6;
}
.ftb-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 0;
  border-bottom: 1px dashed #ebeef5;
}
.ftb-type {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 280px;
}
.ftb-type-name {
  font-size: 13px;
  color: #303133;
  font-weight: 500;
}
.ftb-meta {
  font-size: 12px;
  color: #909399;
}
.ftb-select {
  display: flex;
  align-items: center;
  gap: 10px;
}
.ftb-missing {
  font-size: 12px;
  color: #e6a23c;
}
.ftb-no-tpl {
  margin-top: 8px;
  font-size: 12px;
  color: #909399;
}
.ftb-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 14px;
}
.ftb-dirty {
  font-size: 12px;
  color: #e6a23c;
}
</style>
