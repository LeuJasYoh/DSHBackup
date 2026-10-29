<script setup lang="ts">
import { computed, ref } from "vue";
import SnapshotTable from "../components/SnapshotTable.vue";
import { deleteSnapshot, isBusy, localUsage, openLocalSnapshot, openRestore, refresh, snapshots, startVerify, store } from "../store";
import { formatBytes, formatRelative, kindShort, statusLabel } from "../lib/format";

type Filter = "all" | "manual" | "scheduled" | "pre-restore";
const filter = ref<Filter>("all");
const query = ref("");

const counts = computed(() => ({
  all: snapshots.value.length,
  manual: snapshots.value.filter((item) => item.kind === "manual").length,
  scheduled: snapshots.value.filter((item) => item.kind === "scheduled").length,
  "pre-restore": snapshots.value.filter((item) => item.kind === "pre-restore").length,
}));
const filters: Array<{ id: Filter; label: string }> = [
  { id: "all", label: "全部" }, { id: "manual", label: "手动" }, { id: "scheduled", label: "计划" }, { id: "pre-restore", label: "恢复前" },
];
const readyCount = computed(() => snapshots.value.filter((item) => item.status === "ready").length);
const issueCount = computed(() => snapshots.value.length - readyCount.value);
const filtered = computed(() => {
  const needle = query.value.trim().toLowerCase();
  return snapshots.value.filter((snapshot) => {
    if (filter.value !== "all" && snapshot.kind !== filter.value) return false;
    if (!needle) return true;
    return [
      snapshot.id, snapshot.localPath, kindShort(snapshot.kind), statusLabel(snapshot.status),
      formatRelative(snapshot.createdAt), snapshot.createdAt,
    ].filter(Boolean).join(" ").toLowerCase().includes(needle);
  });
});
function clearFilters() {
  filter.value = "all";
  query.value = "";
}
</script>

<template>
  <section class="page history-page" :class="{ active: store.page === 'history' }">
    <div class="head">
      <div class="head-main">
        <div class="eyebrow"><i class="pip" :class="issueCount ? 'warn' : (snapshots.length ? '' : 'idle')"></i>{{ snapshots.length ? (issueCount ? `${issueCount} 份归档需要处理` : "全部归档可用") : "还没有归档" }}</div>
        <h1>归档历史</h1>
        <p class="sub">
          {{ snapshots.length }} 份 · 共 {{ formatBytes(localUsage) }}<span class="sep">·</span>保留最近 {{ store.boot?.config.retention.local ?? 7 }} 份普通备份<span class="sep">·</span>目录 <span class="mono">{{ store.boot?.config.backupDir }}</span>
        </p>
      </div>
    </div>

    <div class="history-toolbar">
      <label class="search-box">
        <svg class="icon"><use href="#i-search"></use></svg>
        <input v-model="query" type="text" placeholder="搜索归档、日期或状态" aria-label="搜索归档" />
      </label>
      <div class="history-actions">
        <div class="segmented" role="group" aria-label="筛选归档">
          <button v-for="item in filters" :key="item.id" type="button" :class="{ active: filter === item.id }" :aria-pressed="filter === item.id" @click="filter = item.id">
            {{ item.label }}<span style="margin-left:5px;color:var(--faint)">{{ counts[item.id] }}</span>
          </button>
        </div>
        <button class="icon-btn" type="button" aria-label="刷新归档列表" title="刷新" :disabled="isBusy || store.refreshing" @click="refresh(false)">
          <span v-if="store.refreshing" class="spinner"></span>
          <svg v-else class="icon"><use href="#i-refresh"></use></svg>
        </button>
      </div>
    </div>

    <section v-if="filtered.length" class="card table-card" style="margin-top: 20px">
      <div class="table-head">
        <div>
          <div class="card-title">归档列表</div>
          <div class="card-sub">{{ filtered.length }} / {{ snapshots.length }} 份 · 按时间倒序</div>
        </div>
        <button v-if="filter !== 'all' || query.trim()" class="text-button" @click="clearFilters">清除筛选</button>
      </div>
      <SnapshotTable
        :snapshots="filtered"
        @restore="openRestore($event.id)"
        @verify="startVerify($event)"
        @open="openLocalSnapshot($event.id)"
        @delete-local="deleteSnapshot($event)"
      />
      <div class="table-foot">
        <span>保留策略：最近 {{ store.boot?.config.retention.local ?? 7 }} 份普通备份</span>
        <span>共 {{ snapshots.length }} 份 · {{ formatBytes(localUsage) }}</span>
      </div>
    </section>

    <div v-else class="card empty-state">
      <div class="empty-icon"><svg class="icon-lg"><use :href="snapshots.length ? '#i-search' : '#i-archive'"></use></svg></div>
      <h2>{{ snapshots.length ? "没有匹配的归档" : "还没有归档历史" }}</h2>
      <p>{{ snapshots.length ? "换一个关键词或筛选条件试试。" : "完成一次本地备份后，归档会出现在这里。" }}</p>
      <button v-if="snapshots.length" class="btn btn-secondary" @click="clearFilters">清除筛选</button>
    </div>

    <p class="page-note">
      <svg class="icon"><use href="#i-shield"></use></svg>
      <span>“归档可用”表示文件存在且大小符合清单，不代表已经校验。缺失或异常的记录会保留，便于找回归档；删除备份不会删除 DSH 原数据，删除后 6 秒内可以撤销。</span>
    </p>
  </section>
</template>
