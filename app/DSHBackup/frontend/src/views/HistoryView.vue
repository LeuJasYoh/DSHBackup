<script setup lang="ts">
import { computed, ref } from "vue";
import SnapshotTable from "../components/SnapshotTable.vue";
import {
  cloudEnabled,
  openSettingsTab,
  refresh as refreshBootstrap,
  refreshCloud,
  snapshots,
  startVerify,
  store,
  deleteSnapshot,
  openExternal,
  openLocalSnapshot,
  openRestore,
} from "../store";
import type { Snapshot } from "../lib/types";

const filter = ref<"all" | "local" | "cloud">("all");

const filtered = computed(() => {
  if (filter.value === "local") {
    return snapshots.value.filter((snapshot) => snapshot.location === "local" || snapshot.location === "both");
  }
  if (filter.value === "cloud") {
    return snapshots.value.filter((snapshot) => snapshot.location === "cloud" || snapshot.location === "both");
  }
  return snapshots.value;
});

const localCount = computed(() => snapshots.value.filter((snapshot) => snapshot.location === "local" || snapshot.location === "both").length);
const cloudCount = computed(() => snapshots.value.filter((snapshot) => snapshot.location === "cloud" || snapshot.location === "both").length);
const busy = computed(() => !!store.task || store.refreshing || store.cloudRefreshing);

async function refreshList() {
  if (store.boot?.github.connected && cloudEnabled.value) {
    await refreshCloud();
  } else {
    await refreshBootstrap(false);
  }
}

function openSnapshot(snapshot: Snapshot) {
  if (snapshot.localPath) {
    void openLocalSnapshot(snapshot.id);
  } else if (snapshot.cloudHtmlUrl) {
    void openExternal(snapshot.cloudHtmlUrl);
  }
}
</script>

<template>
  <section class="page" :class="{ active: store.page === 'history' }">
    <div class="page-actions">
      <div class="context-label">
        <span class="dot"></span>
        <span>本地与云端快照统一管理</span>
      </div>
      <button class="btn btn-secondary" :disabled="busy" @click="refreshList">
        <span v-if="store.cloudRefreshing || store.refreshing" class="spinner"></span>
        <svg v-else class="icon"><use href="#i-refresh"></use></svg>
        {{ store.cloudRefreshing ? "刷新中…" : "刷新" }}
      </button>
    </div>

    <div class="card filter-card">
      <div class="segmented">
        <button :class="{ active: filter === 'all' }" @click="filter = 'all'">全部</button>
        <button :class="{ active: filter === 'local' }" @click="filter = 'local'">本地</button>
        <button :class="{ active: filter === 'cloud' }" @click="filter = 'cloud'">云端</button>
      </div>
      <span class="filter-count">
        共 {{ filtered.length }} 份备份
        <template v-if="filter === 'all'">· 本地 {{ localCount }} / 云端 {{ cloudCount }}</template>
      </span>
    </div>

    <div v-if="store.boot?.cloudError" class="alert cloud-alert">
      <div class="alert-icon">!</div>
      <div class="alert-copy">
        <strong>云端列表读取失败</strong>
        <span>{{ store.boot.cloudError }}</span>
      </div>
      <button class="btn btn-secondary btn-small" @click="refreshList">重试</button>
    </div>

    <div v-if="filter === 'cloud' && !store.boot?.github.connected" class="alert">
      <div class="alert-icon">!</div>
      <div class="alert-copy">
        <strong>尚未连接 GitHub</strong>
        <span>在设置中连接 GitHub 私有仓库后，才能查看云端快照。</span>
      </div>
      <button class="btn btn-secondary btn-small" @click="openSettingsTab('cloud')">去设置</button>
    </div>

    <SnapshotTable
      v-if="filtered.length"
      :snapshots="filtered"
      @restore="openRestore($event.id)"
      @verify="startVerify($event)"
      @open="openSnapshot($event)"
      @delete-local="deleteSnapshot($event, 'local')"
      @delete-cloud="deleteSnapshot($event, 'cloud')"
    />

    <div v-else class="card empty-state">
      <div class="empty-icon">
        <svg class="icon-lg"><use href="#i-history"></use></svg>
      </div>
      <h2>{{ filter === "all" ? "还没有备份历史" : "该位置没有快照" }}</h2>
      <p>完成一次备份后，快照会出现在这里；本地和云端共用同一份清单。</p>
    </div>

    <div class="card note-card">
      <div class="note-icon">
        <svg class="icon"><use href="#i-cloud"></use></svg>
      </div>
      <div>
        <strong>本地和云端共用一份快照清单</strong>
        <p>云端归档始终加密；本地是否加密由“安全”设置决定。删除云端快照不会影响本地副本，反之亦然。</p>
      </div>
    </div>
  </section>
</template>
