<template>
  <span :class="['status-indicator', `status-${status}`]">
    <span class="dot"></span>
    <span class="label">{{ label }}</span>
  </span>
</template>

<script setup lang="ts">
defineProps<{
  status: 'online' | 'offline' | 'warning' | 'loading'
  label: string
}>()
</script>

<style scoped>
.status-indicator {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.status-online .dot {
  background-color: #22c55e;
}
.status-offline .dot {
  background-color: #9ca3af;
}
.status-warning .dot {
  background-color: #f59e0b;
}
.status-loading .dot {
  background-color: #6366f1;
  animation: pulse 1s ease-in-out infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}
</style>