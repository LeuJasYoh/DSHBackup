<script setup lang="ts">
import { computed } from "vue";
import type { Snapshot } from "../lib/types";
import { store } from "../store";
import { formatBytes, formatDate, kindLabel, statusLabel } from "../lib/format";

const props = defineProps<{ snapshots: Snapshot[]; compact?: boolean }>();
const emit = defineEmits<{
  restore: [Snapshot];
  verify: [Snapshot];
  open: [Snapshot];
  deleteLocal: [Snapshot];
  deleteCloud: [Snapshot];
}>();

const busy = computed(() => !!store.task);

function canRestore(snapshot: Snapshot): boolean {
  if (snapshot.status === "ready" || snapshot.status === "cloud-only") return true;
  if (snapshot.status === "size-mismatch" && snapshot.cloudReleaseId && snapshot.localPath) return true;
  return false;
}

function statusClass(snapshot: Snapshot): string {
  if (snapshot.status === "ready" || snapshot.status === "cloud-only") return "badge success";
  return "badge danger";
}
</script>

<template>
  <div class="table-card card">
    <table>
      <thead>
        <tr>
          <th>时间</th>
          <th>位置</th>
          <th>大小</th>
          <th>加密</th>
          <th>状态</th>
          <th class="align-right">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="snapshot in props.snapshots" :key="`${snapshot.id}-${snapshot.location}`">
          <td>
            <strong>{{ formatDate(snapshot.createdAt) }}</strong>
            <small class="row-sub">{{ kindLabel(snapshot.kind) }}</small>
          </td>
          <td>
            <span class="table-source">
              <span v-if="snapshot.location === 'local' || snapshot.location === 'both'" class="badge local">本地</span>
              <span v-if="snapshot.location === 'cloud' || snapshot.location === 'both'" class="badge cloud">云端</span>
            </span>
          </td>
          <td>{{ formatBytes(snapshot.size) }}</td>
          <td>
            <span class="badge" :class="snapshot.encrypted ? 'success' : 'muted'">
              {{ snapshot.encrypted ? "已加密" : "未加密" }}
            </span>
          </td>
          <td><span :class="statusClass(snapshot)">{{ statusLabel(snapshot.status) }}</span></td>
          <td class="align-right">
            <span class="table-actions">
              <button class="table-action" :disabled="busy || !canRestore(snapshot)" @click="emit('restore', snapshot)">恢复</button>
              <button class="table-action" :disabled="busy" @click="emit('verify', snapshot)">验证</button>
              <button
                v-if="!props.compact && (snapshot.localPath || snapshot.location === 'cloud' || snapshot.location === 'both')"
                class="table-action"
                :disabled="busy"
                @click="emit('open', snapshot)"
              >
                打开
              </button>
              <button
                v-if="!props.compact && snapshot.localPath"
                class="table-action danger-link"
                :disabled="busy"
                @click="emit('deleteLocal', snapshot)"
              >
                删除本地
              </button>
              <button
                v-if="!props.compact && snapshot.cloudReleaseId"
                class="table-action danger-link"
                :disabled="busy"
                @click="emit('deleteCloud', snapshot)"
              >
                删除云端
              </button>
            </span>
          </td>
        </tr>
        <tr v-if="props.snapshots.length === 0">
          <td colspan="6" class="table-empty">没有符合条件的快照</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
