<script setup lang="ts">
import type { Snapshot } from "../lib/types";
import { isBusy, store } from "../store";
import { formatBytes, formatDate, formatRelative, kindShort, restoreBlockReason, statusLabel } from "../lib/format";

const props = defineProps<{ snapshots: Snapshot[]; compact?: boolean }>();
const emit = defineEmits<{ restore: [Snapshot]; verify: [Snapshot]; open: [Snapshot]; deleteLocal: [Snapshot] }>();
const blocked = (snapshot: Snapshot) => restoreBlockReason(snapshot, store.boot?.key);
const archiveName = (snapshot: Snapshot) => snapshot.localPath?.split(/[\\/]/).pop() || `dsh-backup-${snapshot.id}.7z`;
const statusVariant = (snapshot: Snapshot) => {
  if (snapshot.status === "ready") return "success";
  return snapshot.status === "size-mismatch" ? "warning" : "danger";
};
</script>

<template>
  <div class="table-wrap">
    <table class="snapshot-table">
      <thead>
        <tr>
          <th scope="col">归档</th>
          <th scope="col">范围</th>
          <th scope="col">大小</th>
          <th scope="col">状态</th>
          <th scope="col" style="text-align: right">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="snapshot in props.snapshots" :key="snapshot.id">
          <td class="archive-cell">
            <div class="file-line">
              <span class="file-name" :title="snapshot.localPath || snapshot.id">{{ archiveName(snapshot) }}</span>
              <svg v-if="snapshot.encrypted" class="icon file-lock" aria-label="已加密" role="img"><use href="#i-lock"></use></svg>
            </div>
            <div class="file-sub" :title="formatDate(snapshot.createdAt)">{{ formatRelative(snapshot.createdAt) }} · {{ kindShort(snapshot.kind) }}</div>
          </td>
          <td>{{ snapshot.desktopIncluded ? "DSH + 桌面" : "仅 DSH" }}</td>
          <td class="size-cell">{{ formatBytes(snapshot.size) }}</td>
          <td><span class="status-pill" :class="statusVariant(snapshot)"><i></i>{{ statusLabel(snapshot.status) }}</span></td>
          <td>
            <div class="table-actions">
              <button class="table-action" type="button" :disabled="isBusy || !!blocked(snapshot)" :title="blocked(snapshot) || '校验归档完整性'" aria-label="校验归档" @click="emit('verify', snapshot)">
                <svg class="icon"><use href="#i-refresh"></use></svg>
              </button>
              <button class="table-action" type="button" :disabled="isBusy || !!blocked(snapshot)" :title="blocked(snapshot) || '恢复这份备份'" aria-label="恢复归档" @click="emit('restore', snapshot)">
                <svg class="icon"><use href="#i-restore"></use></svg>
              </button>
              <button v-if="!props.compact" class="table-action" type="button" :disabled="isBusy || !snapshot.localPath || snapshot.status === 'missing'" title="在资源管理器中打开" aria-label="打开归档位置" @click="emit('open', snapshot)">
                <svg class="icon"><use href="#i-folder"></use></svg>
              </button>
              <button v-if="!props.compact" class="table-action danger-link" type="button" :disabled="isBusy" title="删除这份备份" aria-label="删除本地归档" @click="emit('deleteLocal', snapshot)">
                <svg class="icon"><use href="#i-trash"></use></svg>
              </button>
            </div>
          </td>
        </tr>
        <tr v-if="!props.snapshots.length"><td colspan="5" class="table-empty">还没有归档</td></tr>
      </tbody>
    </table>
  </div>
</template>
