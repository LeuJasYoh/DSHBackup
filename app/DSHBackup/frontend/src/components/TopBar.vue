<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { go, isBusy, openBackupFolder, startBackup, store } from "../store";
import { taskTitle } from "../lib/format";

defineEmits<{ help: [] }>();

const taskPercent = computed(() => Math.round(store.task?.percent ?? 0));
const backupLabel = computed(() => store.startingTask ? "正在启动…" : store.task?.kind === "backup" ? "备份中…" : "立即备份");

// 只有页级滚动容器会把内容送到这条栏下面；设置页滚的是内层列表，栏下没有东西经过。
const scrolled = ref(false);
let pageScroller: HTMLElement | null = null;
function onScroll(event: Event) {
  if (event.target !== pageScroller) return;
  scrolled.value = (event.target as HTMLElement).scrollTop > 8;
}
onMounted(() => {
  pageScroller = document.getElementById("content");
  document.addEventListener("scroll", onScroll, true);
});
onUnmounted(() => document.removeEventListener("scroll", onScroll, true));
watch(() => store.page, () => { scrolled.value = false; });
</script>

<template>
  <header class="topbar" :class="{ 'is-scrolled': scrolled }">
    <div class="top-actions">
      <button v-if="store.startingTask || store.task" class="task-pill" type="button" title="回到总览查看进度" @click="go('dashboard')">
        <span class="spinner"></span>
        <span>{{ store.startingTask ? "正在启动…" : store.task ? `${taskTitle(store.task.kind)} ${taskPercent}%` : "" }}</span>
      </button>
      <span v-else class="top-status"><i></i>本地运行正常</span>
      <span v-if="store.previewMode" class="preview-pill">UI 预览</span>
      <button class="icon-btn" type="button" aria-label="帮助" title="帮助" @click="$emit('help')">
        <svg class="icon"><use href="#i-help"></use></svg>
      </button>
      <button class="btn btn-secondary" type="button" aria-label="打开备份目录" title="打开备份目录" :disabled="isBusy" @click="openBackupFolder()">
        <svg class="icon"><use href="#i-folder"></use></svg><span class="btn-label">打开目录</span>
      </button>
      <button class="btn btn-primary" type="button" aria-label="立即备份" title="立即备份" :disabled="isBusy" @click="startBackup(false)">
        <svg class="icon"><use href="#i-plus"></use></svg><span class="btn-label">{{ backupLabel }}</span>
      </button>
    </div>
  </header>
</template>
