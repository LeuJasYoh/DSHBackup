<script setup lang="ts">
import { computed } from "vue";
import { go, store } from "../store";
import { modeLabel, taskTitle } from "../lib/format";

defineEmits<{ help: [] }>();

const mode = computed(() => store.boot?.config.mode ?? "local");
const taskPercent = computed(() => Math.round(store.task?.percent ?? 0));
</script>

<template>
  <header class="topbar">
    <div class="topbar-title">
      <h1>{{ store.page === "history" ? "备份历史" : store.page === "settings" ? "设置" : "概览" }}</h1>
      <p>{{ store.page === "history" ? "本地与云端快照" : store.page === "settings" ? "备份方式与云端连接" : "DSH 核心数据备份" }}</p>
    </div>
    <div class="topbar-actions">
      <span v-if="store.previewMode" class="preview-pill">UI 预览</span>
      <button v-if="store.task" class="task-pill" title="回到概览查看进度" @click="go('dashboard')">
        <span class="spinner"></span>
        <span>{{ taskTitle(store.task.kind) }} {{ taskPercent }}%</span>
      </button>
      <span class="mode-pill" :class="{ cloud: mode !== 'local' }">
        <span class="dot"></span>
        <span>{{ modeLabel(mode) }}</span>
      </span>
      <button class="icon-btn" aria-label="帮助" title="帮助" @click="$emit('help')">
        <svg class="icon"><use href="#i-help"></use></svg>
      </button>
    </div>
  </header>
</template>
