<template>
  <div class="fd-props">
    <div class="fd-props-head">节点属性</div>
    <div class="fd-props-body">
      <div v-if="!kind" class="fd-props-empty">
        <el-icon :size="34" color="#CBD5E1"><Setting /></el-icon>
        <p>请选择一个节点以编辑属性</p>
      </div>
      <el-form v-else label-position="top" size="small" @submit.prevent>
        <el-form-item label="节点名称">
          <el-input v-model="form.name" placeholder="输入节点名称" @change="emit('change')" />
        </el-form-item>
        <el-form-item label="节点类型">
          <span class="fd-kind-tag" :style="{ borderColor: def.color, color: def.color, background: tint(def.color) }">
            {{ def.name }}
          </span>
        </el-form-item>

        <el-form-item v-if="has('role')" label="处理角色">
          <el-select v-model="form.assigneeRole" placeholder="-- 选择角色 --" clearable class="w-full" @change="emit('change')">
            <el-option v-for="r in ROLE_OPTIONS" :key="r" :label="r" :value="r" />
          </el-select>
        </el-form-item>

        <el-form-item v-if="has('deadline')" :label="kind === 'delay' ? '延时时长' : '处理时限'">
          <div class="fd-deadline">
            <el-input-number v-model="form.timeLimit" :min="1" :max="9999" controls-position="right" class="fd-time" @change="emit('change')" />
            <el-select v-model="form.timeUnit" class="fd-unit" @change="emit('change')">
              <el-option v-for="u in TIME_UNITS" :key="u.value" :label="u.label" :value="u.value" />
            </el-select>
          </div>
          <p v-if="kind === 'delay'" class="fd-declared">{{ DECLARED_ONLY_TEXT }}</p>
        </el-form-item>

        <el-form-item v-if="has('notify')" label="通知方式">
          <div class="fd-chips">
            <el-check-tag
              v-for="c in NOTIFY_CHANNELS"
              :key="c.value"
              :checked="form.channels.includes(c.value)"
              @change="toggleChannel(c.value)"
            >
              {{ c.label }}
            </el-check-tag>
          </div>
          <p class="fd-declared">{{ DECLARED_ONLY_TEXT }}</p>
        </el-form-item>

        <el-form-item v-if="has('escalation')" label="超时升级">
          <el-switch v-model="form.escalationEnabled" active-text="启用超时升级" @change="emit('change')" />
          <el-select
            v-if="form.escalationEnabled"
            v-model="form.escalationTargetRole"
            placeholder="升级目标角色"
            class="w-full mt-8"
            @change="emit('change')"
          >
            <el-option v-for="r in ESCALATION_TARGETS" :key="r" :label="r" :value="r" />
          </el-select>
          <p class="fd-declared">{{ DECLARED_ONLY_TEXT }}</p>
        </el-form-item>

        <el-form-item v-if="has('condition')" label="条件配置">
          <el-input
            v-model="form.expression"
            type="textarea"
            :rows="3"
            placeholder="输入判断依据，如：压力值 > 100 时走左侧分支"
            @change="emit('change')"
          />
          <p class="fd-declared">{{ DECLARED_ONLY_TEXT }}</p>
        </el-form-item>

        <el-form-item v-if="has('note')" label="备注">
          <el-input
            v-model="form.note"
            type="textarea"
            :rows="2"
            maxlength="200"
            show-word-limit
            placeholder="给其他管理员看的说明（最多 200 字）"
            @change="emit('change')"
          />
        </el-form-item>

        <p v-if="def.hint" class="fd-hint">{{ def.hint }}</p>

        <el-button class="fd-del" type="danger" plain size="small" @click="emit('delete')">删除节点</el-button>
      </el-form>
    </div>

    <!-- 保存前结构校验的问题清单（T285 §6.2）：点一条选中对应节点 -->
    <div v-if="issues.length" class="fd-issues">
      <div class="fd-issues-head">
        <span>校验结果</span>
        <b class="fd-issues-count">{{ errorCount }} 错误 / {{ issues.length - errorCount }} 提示</b>
      </div>
      <ul>
        <li
          v-for="(it, i) in issues"
          :key="`${it.code}-${i}`"
          :class="it.level === 'error' ? 'is-error' : 'is-warn'"
          @click="it.nodeIds?.length && emit('locate', it.nodeIds[0])"
        >
          <em>{{ it.code.replace(/^V\d+_/, '') }}</em>{{ it.message }}
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Setting } from '@element-plus/icons-vue'
import {
  ESCALATION_TARGETS, NOTIFY_CHANNELS, ROLE_OPTIONS, TIME_UNITS,
  kindDef, type FlowKind, type NodeForm, type NotifyChannel, type PropGroup,
} from './kinds'
import type { Issue } from './validateGraph'

const props = defineProps<{ kind: FlowKind | ''; form: NodeForm; issues?: Issue[] }>()
const emit = defineEmits<{
  (e: 'change'): void
  (e: 'delete'): void
  (e: 'locate', nodeId: string): void
}>()

const def = computed(() => kindDef((props.kind || 'process') as FlowKind))
const has = (g: PropGroup) => def.value.groups.includes(g)
const issues = computed(() => props.issues ?? [])
const errorCount = computed(() => issues.value.filter((i) => i.level === 'error').length)

/** T285 §9.3：这四项后端本期没有消费者，不标注的话 admin 会以为配了就会执行 */
const DECLARED_ONLY_TEXT = '本期仅配置，不自动执行'

/** 设计稿的属性类型标签是浅底描边胶囊，Element Plus 的 el-tag 没有类别色变体，这里按色值算个浅底 */
function tint(color: string): string {
  const n = color.replace('#', '')
  const r = parseInt(n.slice(0, 2), 16)
  const g = parseInt(n.slice(2, 4), 16)
  const b = parseInt(n.slice(4, 6), 16)
  return `rgba(${r}, ${g}, ${b}, 0.1)`
}

function toggleChannel(key: NotifyChannel) {
  const at = props.form.channels.indexOf(key)
  if (at >= 0) props.form.channels.splice(at, 1)
  else props.form.channels.push(key)
  emit('change')
}
</script>

<style scoped>
.fd-props {
  width: 320px;
  flex-shrink: 0;
  background: #fff;
  border-left: 1px solid #e2e8f0;
  display: flex;
  flex-direction: column;
}
.fd-props-head {
  padding: 16px 18px;
  border-bottom: 1px solid #e8ecf0;
  font-size: 13px;
  font-weight: 600;
  color: #1e293b;
}
.fd-props-body {
  flex: 1;
  overflow-y: auto;
  padding: 16px 18px;
}
.fd-props-empty {
  text-align: center;
  color: #94a3b8;
  font-size: 13px;
  padding: 40px 20px;
}
.fd-props-empty p { margin: 10px 0 0; }
.w-full { width: 100%; }
.mt-8 { margin-top: 8px; }
.fd-kind-tag {
  display: inline-block;
  padding: 3px 12px;
  border-radius: 12px;
  border: 1px solid;
  font-size: 12px;
  line-height: 18px;
}
.fd-deadline { display: flex; gap: 8px; width: 100%; }
.fd-time { flex: 1; }
.fd-time :deep(.el-input__inner) { text-align: left; }
.fd-unit { width: 84px; }
.fd-chips { display: flex; flex-wrap: wrap; gap: 6px; }
.fd-declared {
  font-size: 11px;
  color: #B45309;
  background: #FFFBEB;
  border: 1px solid #FDE68A;
  border-radius: 4px;
  padding: 2px 6px;
  margin: 6px 0 0;
  line-height: 1.5;
}
.fd-hint {
  font-size: 12px;
  color: #94a3b8;
  line-height: 1.6;
  margin: 4px 0 14px;
}
.fd-del { width: 100%; margin-top: 6px; }
.fd-issues {
  border-top: 1px solid #e8ecf0;
  padding: 12px 18px 16px;
  max-height: 210px;
  overflow-y: auto;
}
.fd-issues-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
  font-weight: 600;
  color: #1e293b;
  margin-bottom: 6px;
}
.fd-issues-count { font-weight: 500; color: #64748b; font-size: 11px; }
.fd-issues ul { list-style: none; margin: 0; padding: 0; }
.fd-issues li {
  font-size: 12px;
  line-height: 1.6;
  padding: 5px 8px;
  border-radius: 4px;
  margin-bottom: 4px;
  cursor: pointer;
}
.fd-issues li em { font-style: normal; font-weight: 600; margin-right: 6px; }
.fd-issues li.is-error { background: #FEF2F2; color: #B91C1C; }
.fd-issues li.is-warn { background: #F8FAFC; color: #475569; }
</style>
