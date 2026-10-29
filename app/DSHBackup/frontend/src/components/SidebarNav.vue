<script setup lang="ts">
import { computed } from "vue";
import { go, latestSnapshot, openRepository, snapshots, store } from "../store";
import { formatTime } from "../lib/format";

const sevenZip = computed(() => {
  const version = store.boot?.app.sevenZip.version ?? "";
  const match = version.match(/(\d+\.\d+)/);
  return match ? `7z ${match[1]}` : "7z";
});
const version = computed(() => store.boot?.app.version ?? "1.2.0");
const platform = computed(() => {
  const value = store.boot?.app.platform ?? "";
  return value.startsWith("windows") ? "Windows x64" : value || "Windows x64";
});
const lastBackup = computed(() => latestSnapshot.value ? formatTime(latestSnapshot.value.createdAt) : "尚未备份");
const updateVersion = computed(() => store.update?.available ? store.update.latestVersion : "");
</script>

<template>
  <aside class="sidebar">
    <div class="brand">
      <div class="brand-mark"><svg class="whale-mark" aria-hidden="true"><use href="#i-whale"></use></svg></div>
      <div class="brand-copy">
        <strong>DSH Backup</strong>
        <span>{{ version }}</span>
      </div>
    </div>

    <div class="nav-label">工作台</div>
    <nav class="nav" aria-label="主导航">
      <button
        class="nav-item"
        :class="{ active: store.page === 'dashboard' }"
        :aria-current="store.page === 'dashboard' ? 'page' : undefined"
        aria-label="总览"
        title="总览"
        @click="go('dashboard')"
      >
        <svg class="icon"><use href="#i-grid"></use></svg>
        <span class="nav-text">总览</span>
      </button>
      <button
        class="nav-item"
        :class="{ active: store.page === 'history' }"
        :aria-current="store.page === 'history' ? 'page' : undefined"
        aria-label="归档历史"
        title="归档历史"
        @click="go('history')"
      >
        <svg class="icon"><use href="#i-archive"></use></svg>
        <span class="nav-text">归档历史</span>
        <span v-if="snapshots.length" class="nav-badge">{{ snapshots.length }}</span>
      </button>
      <button
        class="nav-item"
        :class="{ active: store.page === 'settings' }"
        :aria-current="store.page === 'settings' ? 'page' : undefined"
        aria-label="设置"
        title="设置"
        @click="go('settings')"
      >
        <svg class="icon"><use href="#i-settings"></use></svg>
        <span class="nav-text">设置</span>
      </button>
    </nav>

    <div class="sidebar-spacer"></div>

    <div v-if="store.previewMode" class="preview-note">
      <span class="preview-dot"></span>
      浏览器预览 · 模拟数据
    </div>

    <button class="github-card" type="button" title="打开 GitHub 仓库 LeuJasYoh/DSHBackup" aria-label="打开 GitHub 仓库" @click="openRepository">
      <span class="github-mark"><svg class="icon github-icon"><use href="#i-github"></use></svg></span>
      <span class="github-copy">
        <span>LeuJasYoh/DSHBackup</span>
      </span>
      <span v-if="updateVersion" class="update-badge" :title="`发现新版本 v${updateVersion}`">v{{ updateVersion }}</span>
    </button>

    <div class="sidebar-footer">
      <div class="side-foot-row"><i class="pulse"></i>上次备份 {{ lastBackup }}</div>
      <div class="side-version">{{ platform }} · {{ sevenZip }}</div>
    </div>
  </aside>
</template>
