<script setup lang="ts">
import { computed } from "vue";
import { go, store } from "../store";
import { taskTitle } from "../lib/format";
defineEmits<{ help: [] }>();
const taskPercent = computed(() => Math.round(store.task?.percent ?? 0));
</script>

<template>
  <header class="topbar">
    <div class="topbar-title"><h1>{{ store.page === 'history' ? '备份历史' : store.page === 'settings' ? '设置' : '概览' }}</h1><p>{{ store.page === 'history' ? '本地归档管理' : store.page === 'settings' ? '备份与安全偏好' : 'DSH 核心数据备份' }}</p></div>
    <div class="topbar-actions">
      <span v-if="store.previewMode" class="preview-pill">UI 预览</span>
      <button v-if="store.startingTask || store.task" class="task-pill" title="回到概览查看进度" @click="go('dashboard')"><span class="spinner"></span><span>{{ store.startingTask ? '正在启动…' : store.task ? `${taskTitle(store.task.kind)} ${taskPercent}%` : '' }}</span></button>
      <span class="mode-pill"><span class="dot"></span><span>本地备份</span></span>
      <button class="icon-btn" aria-label="帮助" title="帮助" @click="$emit('help')"><svg class="icon"><use href="#i-help"></use></svg></button>
    </div>
  </header>
</template>
