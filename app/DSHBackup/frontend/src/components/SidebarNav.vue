<script setup lang="ts">
import { computed } from "vue";
import { go, store } from "../store";

const sevenZip = computed(() => {
  const version = store.boot?.app.sevenZip.version ?? "";
  const match = version.match(/(\d+\.\d+)/);
  return match ? `7z ${match[1]}` : "7z";
});
const version = computed(() => store.boot?.app.version ?? "1.1.0");
</script>

<template>
  <aside class="sidebar">
    <div class="brand">
      <div class="brand-mark"><img src="/icon.svg" alt="" /></div>
      <div class="brand-copy">
        <strong>DSH Backup</strong>
        <span>核心数据备份</span>
      </div>
    </div>

    <nav class="nav" aria-label="主导航">
      <button
        class="nav-item"
        :class="{ active: store.page === 'dashboard' }"
        :aria-current="store.page === 'dashboard' ? 'page' : undefined"
        aria-label="概览"
        @click="go('dashboard')"
      >
        <svg class="icon"><use href="#i-grid"></use></svg>
        <span>概览</span>
      </button>
      <button
        class="nav-item"
        :class="{ active: store.page === 'history' }"
        :aria-current="store.page === 'history' ? 'page' : undefined"
        aria-label="备份历史"
        @click="go('history')"
      >
        <svg class="icon"><use href="#i-history"></use></svg>
        <span>备份历史</span>
      </button>
      <button
        class="nav-item"
        :class="{ active: store.page === 'settings' }"
        :aria-current="store.page === 'settings' ? 'page' : undefined"
        aria-label="设置"
        @click="go('settings')"
      >
        <svg class="icon"><use href="#i-settings"></use></svg>
        <span>设置</span>
      </button>
    </nav>

    <div class="sidebar-spacer"></div>

    <div v-if="store.previewMode" class="preview-note">
      <span class="preview-dot"></span>
      浏览器预览 · 模拟数据
    </div>

    <div class="sidebar-note">
      <div class="sidebar-mode">
        <span class="dot"></span>
        <span>本地备份</span>
      </div>
      <p>本地归档 · 自主管理</p>
    </div>

    <div class="sidebar-footer">
      <span>v{{ version }}</span>
      <span>{{ sevenZip }}</span>
    </div>
  </aside>
</template>
